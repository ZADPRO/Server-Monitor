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
)

type ConfigManager struct {
	mu           sync.RWMutex
	configPath   string
	config       *models.Config
	lastModified time.Time
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

// Save persists the provided config to disk
func (cm *ConfigManager) Save(cfg models.Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	dir := filepath.Dir(cm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	info, _ := os.Stat(cm.configPath)
	if info != nil {
		cm.lastModified = info.ModTime()
	}

	cm.config = &cfg
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

// AddTarget adds a new target to monitoring
func (cm *ConfigManager) AddTarget(target models.Target) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, t := range cm.config.Targets {
		if t.ID == target.ID {
			return fmt.Errorf("target with ID '%s' already exists", target.ID)
		}
	}

	cm.config.Targets = append(cm.config.Targets, target)
	return cm.saveUnlocked()
}

// UpdateTarget updates an existing target
func (cm *ConfigManager) UpdateTarget(target models.Target) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	found := false
	for i, t := range cm.config.Targets {
		if t.ID == target.ID {
			cm.config.Targets[i] = target
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("target with ID '%s' not found", target.ID)
	}

	return cm.saveUnlocked()
}

// DeleteTarget removes a target by ID
func (cm *ConfigManager) DeleteTarget(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

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
		return fmt.Errorf("target with ID '%s' not found", id)
	}

	cm.config.Targets = newTargets
	return cm.saveUnlocked()
}

// GetUsers returns list of configured alert users
func (cm *ConfigManager) GetUsers() []models.User {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	users := make([]models.User, len(cm.config.Users))
	copy(users, cm.config.Users)
	return users
}

// AddUser adds a new user recipient
func (cm *ConfigManager) AddUser(user models.User) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, u := range cm.config.Users {
		if u.ID == user.ID {
			return fmt.Errorf("user with ID '%s' already exists", user.ID)
		}
	}

	cm.config.Users = append(cm.config.Users, user)
	return cm.saveUnlocked()
}

// UpdateUser updates an existing user
func (cm *ConfigManager) UpdateUser(user models.User) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	found := false
	for i, u := range cm.config.Users {
		if u.ID == user.ID {
			cm.config.Users[i] = user
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("user with ID '%s' not found", user.ID)
	}

	return cm.saveUnlocked()
}

// DeleteUser removes a user by ID
func (cm *ConfigManager) DeleteUser(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	newUsers := make([]models.User, 0, len(cm.config.Users))
	found := false
	for _, u := range cm.config.Users {
		if u.ID == id {
			found = true
			continue
		}
		newUsers = append(newUsers, u)
	}

	if !found {
		return fmt.Errorf("user with ID '%s' not found", id)
	}

	cm.config.Users = newUsers
	return cm.saveUnlocked()
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
