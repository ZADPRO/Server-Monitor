package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"serverMonitoring/models"
	"serverMonitoring/storage"
)

type ConfigManager struct {
	mu           sync.RWMutex
	configPath   string
	config       *models.Config
	lastModified time.Time
	fbClient     *storage.FirebaseClient
}

var (
	instance *ConfigManager
	once     sync.Once
)

// InitConfig initializes the singleton config manager and starts background watcher
func InitConfig(path string) (*ConfigManager, error) {
	var err error
	once.Do(func() {
		instance = &ConfigManager{
			configPath: path,
		}
		err = instance.reload()
		if err == nil {
			instance.startWatcher()
		}
	})
	return instance, err
}

// GetManager returns the existing manager instance
func GetManager() *ConfigManager {
	return instance
}

// SetFirebaseClient sets the Firebase client for real-time config syncing
func (cm *ConfigManager) SetFirebaseClient(fb *storage.FirebaseClient) {
	cm.mu.Lock()
	cm.fbClient = fb
	cm.mu.Unlock()
}

// GetFirebaseClient returns the current Firebase client
func (cm *ConfigManager) GetFirebaseClient() *storage.FirebaseClient {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.fbClient
}

func (cm *ConfigManager) startWatcher() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cm.checkAndReload()
		}
	}()
}

func (cm *ConfigManager) checkAndReload() {
	info, err := os.Stat(cm.configPath)
	if err != nil {
		return
	}

	cm.mu.RLock()
	lastMod := cm.lastModified
	cm.mu.RUnlock()

	if info.ModTime().After(lastMod) {
		log.Printf("[CONFIG] Detected changes in %s, reloading...", cm.configPath)
		if err := cm.reload(); err == nil {
			log.Printf("[CONFIG] Successfully reloaded config from disk.")
		}
	}
}

func (cm *ConfigManager) reload() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file at %s: %w", cm.configPath, err)
	}

	info, _ := os.Stat(cm.configPath)
	if info != nil {
		cm.lastModified = info.ModTime()
	}

	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// Apply defaults if necessary
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Monitoring.IntervalMinutes <= 0 {
		cfg.Monitoring.IntervalMinutes = 5
	}
	if cfg.Monitoring.RequestTimeoutSeconds <= 0 {
		cfg.Monitoring.RequestTimeoutSeconds = 15
	}
	if cfg.Email.SMTPHost == "" {
		cfg.Email.SMTPHost = "smtp.gmail.com"
	}
	if cfg.Email.SMTPPort == 0 {
		cfg.Email.SMTPPort = 587
	}

	cm.config = &cfg
	return nil
}

// Get returns a clone of current config
func (cm *ConfigManager) Get() models.Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return *cm.config
}

// Save persists the provided config to disk and syncs with Firebase
func (cm *ConfigManager) Save(cfg models.Config) error {
	cm.mu.Lock()
	fb := cm.fbClient

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		cm.mu.Unlock()
		return fmt.Errorf("failed to encode config: %w", err)
	}

	dir := filepath.Dir(cm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		cm.mu.Unlock()
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		cm.mu.Unlock()
		return fmt.Errorf("failed to write config file: %w", err)
	}

	info, _ := os.Stat(cm.configPath)
	if info != nil {
		cm.lastModified = info.ModTime()
	}

	cm.config = &cfg
	cm.mu.Unlock()

	// Asynchronously sync updated settings to Firebase
	if fb != nil {
		go func(c models.Config) {
			_ = fb.SyncAllToFirebase(c)
		}(cfg)
	}

	return nil
}

// GetTargets returns list of monitoring targets
func (cm *ConfigManager) GetTargets() []models.Target {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	targets := make([]models.Target, len(cm.config.Targets))
	copy(targets, cm.config.Targets)
	return targets
}

// AddTarget adds a new target to monitoring (local + Firebase)
func (cm *ConfigManager) AddTarget(target models.Target) error {
	cm.mu.Lock()
	fb := cm.fbClient

	for _, t := range cm.config.Targets {
		if t.ID == target.ID {
			cm.mu.Unlock()
			return fmt.Errorf("target with ID '%s' already exists", target.ID)
		}
	}

	cm.config.Targets = append(cm.config.Targets, target)
	err := cm.saveUnlocked()
	cm.mu.Unlock()

	if err == nil && fb != nil {
		go func(t models.Target) {
			_ = fb.SaveTarget(t)
		}(target)
	}

	return err
}

// UpdateTarget updates an existing target (local + Firebase)
func (cm *ConfigManager) UpdateTarget(target models.Target) error {
	cm.mu.Lock()
	fb := cm.fbClient

	found := false
	for i, t := range cm.config.Targets {
		if t.ID == target.ID {
			cm.config.Targets[i] = target
			found = true
			break
		}
	}

	if !found {
		cm.mu.Unlock()
		return fmt.Errorf("target with ID '%s' not found", target.ID)
	}

	err := cm.saveUnlocked()
	cm.mu.Unlock()

	if err == nil && fb != nil {
		go func(t models.Target) {
			_ = fb.SaveTarget(t)
		}(target)
	}

	return err
}

// DeleteTarget removes a target by ID (local + Firebase)
func (cm *ConfigManager) DeleteTarget(id string) error {
	cm.mu.Lock()
	fb := cm.fbClient

	newTargets := make([]models.Target, 0, len(cm.config.Targets))
	found := false
	for _, t := range cm.config.Targets {
		if t.ID == id {
			found = true
			continue
		}
		newTargets = append(newTargets, t)
	}

	if !found {
		cm.mu.Unlock()
		return fmt.Errorf("target with ID '%s' not found", id)
	}

	cm.config.Targets = newTargets
	err := cm.saveUnlocked()
	cm.mu.Unlock()

	if err == nil && fb != nil {
		go func(targetID string) {
			_ = fb.DeleteTarget(targetID)
		}(id)
	}

	return err
}

// SyncWithFirebase pulls existing targets, auth, and email config from Firebase; if empty in Firebase, seeds from local
func (cm *ConfigManager) SyncWithFirebase() error {
	cm.mu.Lock()
	fb := cm.fbClient
	if fb == nil {
		cm.mu.Unlock()
		return nil
	}
	cfgCopy := *cm.config
	cm.mu.Unlock()

	// 1. Sync Targets
	fbTargets, err := fb.FetchTargets()
	if err == nil {
		if len(fbTargets) > 0 {
			cm.mu.Lock()
			cm.config.Targets = fbTargets
			_ = cm.saveUnlocked()
			cm.mu.Unlock()
			log.Printf("[CONFIG SYNC] Loaded %d targets from Firebase DB", len(fbTargets))
		} else if len(cfgCopy.Targets) > 0 {
			// Seed Firebase with local targets
			_ = fb.SaveAllTargets(cfgCopy.Targets)
			log.Printf("[CONFIG SYNC] Seeded Firebase DB with %d local targets", len(cfgCopy.Targets))
		}
	}

	// 2. Sync Auth / User
	fbAuth, err := fb.FetchAuthConfig()
	if err == nil && fbAuth != nil && fbAuth.Username != "" {
		cm.mu.Lock()
		cm.config.Auth = *fbAuth
		_ = cm.saveUnlocked()
		cm.mu.Unlock()
		log.Printf("[CONFIG SYNC] Loaded auth credentials from Firebase DB (User: %s)", fbAuth.Username)
	} else if cfgCopy.Auth.Username != "" {
		_ = fb.SaveAuthConfig(cfgCopy.Auth)
		log.Printf("[CONFIG SYNC] Seeded Firebase DB with local auth credentials (%s)", cfgCopy.Auth.Username)
	}

	// 3. Sync Email Config
	fbEmail, err := fb.FetchEmailConfig()
	if err == nil && fbEmail != nil && len(fbEmail.ToEmails) > 0 {
		cm.mu.Lock()
		cm.config.Email = *fbEmail
		_ = cm.saveUnlocked()
		cm.mu.Unlock()
		log.Printf("[CONFIG SYNC] Loaded email alert config from Firebase DB (%d recipients)", len(fbEmail.ToEmails))
	} else if len(cfgCopy.Email.ToEmails) > 0 {
		_ = fb.SaveEmailConfig(cfgCopy.Email)
		log.Printf("[CONFIG SYNC] Seeded Firebase DB with local email config (%d recipients)", len(cfgCopy.Email.ToEmails))
	}

	return nil
}

func (cm *ConfigManager) saveUnlocked() error {
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	err = os.WriteFile(cm.configPath, data, 0644)
	if err == nil {
		info, _ := os.Stat(cm.configPath)
		if info != nil {
			cm.lastModified = info.ModTime()
		}
	}
	return err
}
