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
		// Initialize with default config so instance.config is NEVER nil
		defaultCfg := GetDefaultConfig()
		instance.config = &defaultCfg

		err = instance.reload()
		if err == nil {
			instance.startWatcher()
		} else {
			log.Printf("[CONFIG] Using default configuration (Could not load %s: %v)", path, err)
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

	// Try candidate file paths for config.json (support local dev & Vercel Lambda)
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
	if cm.config == nil {
		defaultCfg := GetDefaultConfig()
		return defaultCfg
	}
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
		dir = os.TempDir()
		cm.configPath = filepath.Join(dir, "config.json")
		_ = os.MkdirAll(dir, 0755)
	}

	if err := os.WriteFile(cm.configPath, data, 0644); err != nil {
		// Fallback to /tmp for read-only serverless environment
		tmpPath := filepath.Join(os.TempDir(), "config.json")
		_ = os.WriteFile(tmpPath, data, 0644)
		log.Printf("[CONFIG WARN] Could not write to %s (read-only filesystem), saved to %s: %v", cm.configPath, tmpPath, err)
	}

	if info, err := os.Stat(cm.configPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	cm.config = &cfg
	return nil
}

// GetTargets returns list of monitoring targets
func (cm *ConfigManager) GetTargets() []models.Target {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.config == nil {
		return GetDefaultConfig().Targets
	}
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
	if cm.config == nil {
		return GetDefaultConfig().Users
	}
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
	_ = os.WriteFile(cm.configPath, data, 0644)
	return nil
}

// GetDefaultConfig returns a robust fallback configuration
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
			RequestTimeoutSeconds: 15,
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
		Users: []models.User{
			{ID: "user_1", Name: "Indumathi R", Email: "indumathi.r@zadroit.com"},
			{ID: "user_2", Name: "Vijay Loganathan", Email: "vijay.loganathan@zadroit.com"},
		},
		Targets: []models.Target{
			{
				ID:              "target_backend_1",
				Name:            "Nivas App product management",
				Type:            "backend",
				URL:             "https://nivasappproduct-wishlist.brightoncloudtech.com/checkserver",
				Method:          "GET",
				Enabled:         true,
				ExpectedKeys:    []string{"service", "db"},
				IntervalMinutes: 5,
				RecipientEmails: []string{"indumathi.r@zadroit.com", "vijay.loganathan@zadroit.com"},
			},
			{
				ID:              "target_frontend_1",
				Name:            "Nivas HOC Website",
				Type:            "frontend",
				URL:             "https://nivashoc.com/",
				Method:          "GET",
				Enabled:         true,
				IntervalMinutes: 5,
				RecipientEmails: []string{"indumathi.r@zadroit.com"},
			},
			{
				ID:              "target_frontend_2",
				Name:            "Hotel Sherlock Website",
				Type:            "frontend",
				URL:             "https://hotelsherlockholmes.com/",
				Method:          "GET",
				Enabled:         true,
				IntervalMinutes: 15,
				RecipientEmails: []string{"vijay.loganathan@zadroit.com"},
			},
		},
	}
}
