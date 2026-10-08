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

// InitConfig initializes the singleton config manager
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

func (cm *ConfigManager) getFirebaseClient() *storage.FirebaseClient {
	if cm.fbClient != nil {
		return cm.fbClient
	}
	cfg := cm.Get()
	cm.fbClient = storage.NewFirebaseClient(cfg.Firebase)
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
		cfg.Monitoring.RequestTimeoutSeconds = 15
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

// Save persists the provided config
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

	_ = os.WriteFile(cm.configPath, data, 0644)
	if info, err := os.Stat(cm.configPath); err == nil {
		cm.lastModified = info.ModTime()
	}

	cm.config = &cfg
	cm.fbClient = storage.NewFirebaseClient(cfg.Firebase)
	return nil
}

// ============================================================================
// TARGETS MANAGEMENT (STRICTLY FIREBASE STORAGE)
// ============================================================================

// GetTargets returns list of monitoring targets strictly from Firebase RTDB
func (cm *ConfigManager) GetTargets() []models.Target {
	fb := cm.getFirebaseClient()
	fbTargets, err := fb.FetchTargetsFromFirebase()
	if err == nil && len(fbTargets) > 0 {
		cm.mu.Lock()
		cm.config.Targets = fbTargets
		cm.mu.Unlock()
		return fbTargets
	}

	// If Firebase targets node is empty, seed initial targets to Firebase RTDB
	cm.mu.RLock()
	var targets []models.Target
	if cm.config != nil && len(cm.config.Targets) > 0 {
		targets = make([]models.Target, len(cm.config.Targets))
		copy(targets, cm.config.Targets)
	} else {
		targets = GetDefaultConfig().Targets
	}
	cm.mu.RUnlock()

	// Seed to Firebase asynchronously
	go func(list []models.Target) {
		for _, t := range list {
			t.IntervalMinutes = 5
			_ = fb.SaveTargetToFirebase(t)
		}
	}(targets)

	for i := range targets {
		targets[i].IntervalMinutes = 5
	}
	return targets
}

// AddTarget adds a new target strictly to Firebase RTDB
func (cm *ConfigManager) AddTarget(target models.Target) error {
	target.IntervalMinutes = 5
	fb := cm.getFirebaseClient()

	if err := fb.SaveTargetToFirebase(target); err != nil {
		log.Printf("[FIREBASE TARGET WARN] Could not save target to Firebase: %v", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, t := range cm.config.Targets {
		if t.ID == target.ID {
			cm.config.Targets[i] = target
			return nil
		}
	}
	cm.config.Targets = append(cm.config.Targets, target)
	return nil
}

// UpdateTarget updates an existing target strictly in Firebase RTDB
func (cm *ConfigManager) UpdateTarget(target models.Target) error {
	target.IntervalMinutes = 5
	fb := cm.getFirebaseClient()

	if err := fb.SaveTargetToFirebase(target); err != nil {
		log.Printf("[FIREBASE TARGET WARN] Could not update target in Firebase: %v", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, t := range cm.config.Targets {
		if t.ID == target.ID {
			cm.config.Targets[i] = target
			return nil
		}
	}
	cm.config.Targets = append(cm.config.Targets, target)
	return nil
}

// DeleteTarget removes a target strictly from Firebase RTDB
func (cm *ConfigManager) DeleteTarget(id string) error {
	fb := cm.getFirebaseClient()
	if err := fb.DeleteTargetFromFirebase(id); err != nil {
		log.Printf("[FIREBASE TARGET WARN] Could not delete target from Firebase: %v", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	newTargets := make([]models.Target, 0, len(cm.config.Targets))
	for _, t := range cm.config.Targets {
		if t.ID != id {
			newTargets = append(newTargets, t)
		}
	}
	cm.config.Targets = newTargets
	return nil
}

// ============================================================================
// USERS MANAGEMENT (STRICTLY FIREBASE STORAGE)
// ============================================================================

// GetUsers returns list of configured alert users strictly from Firebase RTDB
func (cm *ConfigManager) GetUsers() []models.User {
	fb := cm.getFirebaseClient()
	fbUsers, err := fb.FetchUsersFromFirebase()
	if err == nil && len(fbUsers) > 0 {
		cm.mu.Lock()
		cm.config.Users = fbUsers
		cm.mu.Unlock()
		return fbUsers
	}

	// If Firebase users node is empty, seed initial users to Firebase RTDB
	cm.mu.RLock()
	var users []models.User
	if cm.config != nil && len(cm.config.Users) > 0 {
		users = make([]models.User, len(cm.config.Users))
		copy(users, cm.config.Users)
	} else {
		users = GetDefaultConfig().Users
	}
	cm.mu.RUnlock()

	// Seed to Firebase asynchronously
	go func(list []models.User) {
		for _, u := range list {
			_ = fb.SaveUserToFirebase(u)
		}
	}(users)

	return users
}

// AddUser adds a new user recipient strictly to Firebase RTDB
func (cm *ConfigManager) AddUser(user models.User) error {
	fb := cm.getFirebaseClient()
	if err := fb.SaveUserToFirebase(user); err != nil {
		log.Printf("[FIREBASE USER WARN] Could not save user to Firebase: %v", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, u := range cm.config.Users {
		if u.ID == user.ID {
			cm.config.Users[i] = user
			return nil
		}
	}
	cm.config.Users = append(cm.config.Users, user)
	return nil
}

// UpdateUser updates an existing user strictly in Firebase RTDB
func (cm *ConfigManager) UpdateUser(user models.User) error {
	fb := cm.getFirebaseClient()
	if err := fb.SaveUserToFirebase(user); err != nil {
		log.Printf("[FIREBASE USER WARN] Could not update user in Firebase: %v", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	for i, u := range cm.config.Users {
		if u.ID == user.ID {
			cm.config.Users[i] = user
			return nil
		}
	}
	cm.config.Users = append(cm.config.Users, user)
	return nil
}

// DeleteUser removes a user strictly from Firebase RTDB
func (cm *ConfigManager) DeleteUser(id string) error {
	fb := cm.getFirebaseClient()
	if err := fb.DeleteUserFromFirebase(id); err != nil {
		log.Printf("[FIREBASE USER WARN] Could not delete user from Firebase: %v", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	newUsers := make([]models.User, 0, len(cm.config.Users))
	for _, u := range cm.config.Users {
		if u.ID != id {
			newUsers = append(newUsers, u)
		}
	}
	cm.config.Users = newUsers
	return nil
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
				IntervalMinutes: 5,
				RecipientEmails: []string{"vijay.loganathan@zadroit.com"},
			},
		},
	}
}
