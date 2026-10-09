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
	once.Do(func() {
		instance = &ConfigManager{
			configPath: path,
		}
		defaultCfg := GetDefaultConfig()
		instance.config = &defaultCfg
		instance.fbClient = storage.NewFirebaseClient(defaultCfg.Firebase)

		reloadErr := instance.reload()
		if reloadErr == nil {
			instance.startWatcher()
			log.Printf("[CONFIG] Loaded successfully from %s", path)
		} else {
			log.Printf("[CONFIG] Using default embedded configuration (Note: %v)", reloadErr)
		}
	})
	return instance, nil
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
		ticker := time.NewTicker(5 * time.Second)
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

	candidates := []string{
		cm.configPath,
		"config.json",
		"./config.json",
		"../config.json",
		"/var/task/config.json",
	}

	if envTaskRoot := os.Getenv("LAMBDA_TASK_ROOT"); envTaskRoot != "" {
		candidates = append(candidates, filepath.Join(envTaskRoot, "config.json"))
	}

	var data []byte
	var readErr error
	foundPath := ""

	for _, p := range candidates {
		if p == "" {
			continue
		}
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			data = b
			foundPath = p
			break
		} else if readErr == nil {
			readErr = err
		}
	}

	if len(data) == 0 {
		return fmt.Errorf("failed to read config file from candidates %v: %v", candidates, readErr)
	}

	if info, err := os.Stat(foundPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config JSON from %s: %w", foundPath, err)
	}

	// Apply defaults
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Monitoring.IntervalMinutes <= 0 {
		cfg.Monitoring.IntervalMinutes = 5
	}
	if cfg.Monitoring.RequestTimeoutSeconds <= 0 {
		cfg.Monitoring.RequestTimeoutSeconds = 60
	}
	if cfg.Email.SMTPHost == "" {
		cfg.Email.SMTPHost = "smtp.gmail.com"
	}
	if cfg.Email.SMTPPort == 0 {
		cfg.Email.SMTPPort = 587
	}

	cm.config = &cfg
	cm.fbClient = storage.NewFirebaseClient(cfg.Firebase)
	return nil
}

// Get returns a clone of current config
func (cm *ConfigManager) Get() models.Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.config == nil {
		defaultCfg := GetDefaultConfig()
		return defaultCfg
	}
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
		dir = os.TempDir()
		cm.configPath = filepath.Join(dir, "config.json")
		_ = os.MkdirAll(dir, 0755)
	}

	_ = os.WriteFile(cm.configPath, data, 0644)
	if info, err := os.Stat(cm.configPath); err == nil {
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
	if cm.config == nil {
		return []models.Target{}
	}
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

// GetUsers returns list of configured alert users
func (cm *ConfigManager) GetUsers() []models.User {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.config == nil {
		return []models.User{}
	}
	users := make([]models.User, len(cm.config.Users))
	copy(users, cm.config.Users)
	return users
}

// AddUser adds a new user recipient
func (cm *ConfigManager) AddUser(user models.User) error {
	cm.mu.Lock()
	fb := cm.fbClient

	for _, u := range cm.config.Users {
		if u.ID == user.ID || (u.Username != "" && u.Username == user.Username) {
			cm.mu.Unlock()
			return fmt.Errorf("user with ID/Username already exists")
		}
	}

	cm.config.Users = append(cm.config.Users, user)
	err := cm.saveUnlocked()
	cm.mu.Unlock()

	if err == nil && fb != nil {
		go func(u models.User) {
			_ = fb.SaveUser(u)
		}(user)
	}
	return err
}

// UpdateUser updates an existing user
func (cm *ConfigManager) UpdateUser(user models.User) error {
	cm.mu.Lock()
	fb := cm.fbClient

	found := false
	for i, u := range cm.config.Users {
		if (user.ID != "" && u.ID == user.ID) || (user.Username != "" && u.Username == user.Username) {
			cm.config.Users[i] = user
			found = true
			break
		}
	}

	if !found {
		cm.mu.Unlock()
		return fmt.Errorf("user with ID '%s' not found", user.ID)
	}

	err := cm.saveUnlocked()
	cm.mu.Unlock()

	if err == nil && fb != nil {
		go func(u models.User) {
			_ = fb.SaveUser(u)
		}(user)
	}
	return err
}

// DeleteUser removes a user by ID
func (cm *ConfigManager) DeleteUser(id string) error {
	cm.mu.Lock()
	newUsers := make([]models.User, 0, len(cm.config.Users))
	found := false
	for _, u := range cm.config.Users {
		if u.ID == id || u.Username == id {
			found = true
			continue
		}
		newUsers = append(newUsers, u)
	}
	if !found {
		cm.mu.Unlock()
		return fmt.Errorf("user '%s' not found", id)
	}
	cm.config.Users = newUsers
	err := cm.saveUnlocked()
	cm.mu.Unlock()
	return err
}

// SyncWithFirebase pulls existing targets, auth, users, and email config from Firebase
func (cm *ConfigManager) SyncWithFirebase() error {
	cm.mu.Lock()
	fb := cm.fbClient
	if fb == nil {
		cm.mu.Unlock()
		return nil
	}
	cm.mu.Unlock()

	// 1. Sync Targets from Firebase
	fbTargets, err := fb.FetchTargets()
	if err == nil {
		cm.mu.Lock()
		cm.config.Targets = fbTargets
		_ = cm.saveUnlocked()
		cm.mu.Unlock()
		log.Printf("[CONFIG SYNC] Loaded %d targets from Firebase DB", len(fbTargets))
	}

	// 2. Sync Users from Firebase
	fbUsers, err := fb.FetchUsers()
	if err == nil {
		cm.mu.Lock()
		cm.config.Users = fbUsers
		_ = cm.saveUnlocked()
		cm.mu.Unlock()
		log.Printf("[CONFIG SYNC] Loaded %d users from Firebase DB", len(fbUsers))
	}

	// 3. Sync Auth / User
	fbAuth, err := fb.FetchAuthConfig()
	if err == nil && fbAuth != nil && fbAuth.Username != "" {
		cm.mu.Lock()
		cm.config.Auth = *fbAuth
		_ = cm.saveUnlocked()
		cm.mu.Unlock()
		log.Printf("[CONFIG SYNC] Loaded auth credentials from Firebase DB (User: %s)", fbAuth.Username)
	}

	// 4. Sync Email Config
	fbEmail, err := fb.FetchEmailConfig()
	if err == nil && fbEmail != nil && len(fbEmail.ToEmails) > 0 {
		cm.mu.Lock()
		cm.config.Email = *fbEmail
		_ = cm.saveUnlocked()
		cm.mu.Unlock()
		log.Printf("[CONFIG SYNC] Loaded email alert config from Firebase DB (%d recipients)", len(fbEmail.ToEmails))
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

// GetDefaultConfig returns robust default configuration
func GetDefaultConfig() models.Config {
	return models.Config{
		Server: models.ServerConfig{
			Port: 8080,
			Host: "0.0.0.0",
		},
		Auth: models.AuthConfig{
			Username: "Zadroit",
			Password: "ZadGugSlm06",
		},
		Monitoring: models.MonitoringConfig{
			IntervalMinutes:       5,
			RequestTimeoutSeconds: 60,
		},
		Email: models.EmailConfig{
			Enabled:     true,
			SMTPHost:    "smtp.gmail.com",
			SMTPPort:    587,
			FromEmail:   "development.zadroit@gmail.com",
			AppPassword: "bfitdhmbhcxtjrvg",
			ToEmails: []string{
				"indumathi.r@zadroit.com",
				"vijay.loganathan@zadroit.com",
			},
		},
		Firebase: models.FirebaseConfig{
			Enabled:           true,
			Type:              "realtime",
			APIKey:            "AIzaSyA2sTTwDtuWcWF9Xg2sPfrrYuDLbjJCMUc",
			AuthDomain:        "server-monitor-8ffb0.firebaseapp.com",
			DatabaseURL:       "https://server-monitor-8ffb0-default-rtdb.firebaseio.com",
			ProjectID:         "server-monitor-8ffb0",
			StorageBucket:     "server-monitor-8ffb0.firebasestorage.app",
			MessagingSenderID: "274069716120",
			AppID:             "1:274069716120:web:9585c80e368ac8b9e624ed",
			MeasurementID:     "G-Q7R2VPN390",
			Collection:        "server_monitoring_logs",
			AutoDeleteEnabled: true,
			AutoDeleteDays:    7,
		},
		Users:   []models.User{},
		Targets: []models.Target{},
	}
}
