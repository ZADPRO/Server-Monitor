package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"serverMonitoring/models"
)

// FirebaseClient handles persisting and retrieving health check records, targets, users, and email configs from Firebase Realtime Database
type FirebaseClient struct {
	config models.FirebaseConfig
	client *http.Client
}

// NewFirebaseClient creates a new Firebase client
func NewFirebaseClient(cfg models.FirebaseConfig) *FirebaseClient {
	return &FirebaseClient{
		config: cfg,
		client: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
}

// UpdateConfig updates Firebase configuration dynamically
func (f *FirebaseClient) UpdateConfig(cfg models.FirebaseConfig) {
	f.config = cfg
}

// GetConfig returns current Firebase configuration
func (f *FirebaseClient) GetConfig() models.FirebaseConfig {
	return f.config
}

// CleanDatabaseURL ensures database URL has no trailing slashes
func (f *FirebaseClient) CleanDatabaseURL() string {
	return strings.TrimRight(strings.TrimSpace(f.config.DatabaseURL), "/")
}

// Helper to make authenticated HTTP requests to Firebase Realtime Database REST API
func (f *FirebaseClient) doRequest(method, path string, payload interface{}) ([]byte, int, error) {
	baseURL := f.CleanDatabaseURL()
	if !f.config.Enabled || baseURL == "" {
		return nil, 0, fmt.Errorf("firebase is disabled or database_url is empty")
	}

	cleanPath := strings.Trim(path, "/")
	endpoint := fmt.Sprintf("%s/%s.json", baseURL, cleanPath)
	if f.config.AuthSecret != "" {
		endpoint += fmt.Sprintf("?auth=%s", f.config.AuthSecret)
	}

	var bodyReader io.Reader
	if payload != nil {
		dataBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal JSON payload: %w", err)
		}
		bodyReader = bytes.NewBuffer(dataBytes)
	}

	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request (%s): %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("network request failed (%s %s): %w", method, endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return respBody, resp.StatusCode, fmt.Errorf("permission denied (HTTP %d). Please set Firebase Realtime Database rules to .read: true, .write: true", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, fmt.Errorf("firebase HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, resp.StatusCode, nil
}

// ============================================================================
// 1. LOGS STORAGE (server_monitoring_logs & current_status)
// ============================================================================

// SaveLog saves a health check result into Firebase Database in real-time
func (f *FirebaseClient) SaveLog(result models.HealthCheckResult) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}

	collection := f.config.Collection
	if collection == "" {
		collection = "server_monitoring_logs"
	}

	payload := map[string]interface{}{
		"id":               result.ID,
		"service_name":     result.ServiceName,
		"service name":     result.ServiceName,
		"type":             result.Type,
		"url":              result.URL,
		"status":           result.Status,
		"hit_time":         result.HitTime,
		"hit time":         result.HitTime,
		"message":          result.Message,
		"data":             result.Data,
		"response_time_ms": result.ResponseTimeMs,
		"http_status":      result.HTTPStatus,
		"timestamp":        result.Timestamp,
		"error_detail":     result.ErrorDetail,
		"target_id":        result.TargetID,
	}

	// 1. Append record to Firebase Realtime DB collection: POST /server_monitoring_logs.json
	_, _, err := f.doRequest("POST", collection, payload)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to push log to Firebase DB (%s): %v", collection, err)
		return err
	}

	// 2. Also update current status snapshot: PUT /current_status/{target_id}.json
	if result.TargetID != "" {
		_, _, _ = f.doRequest("PUT", fmt.Sprintf("current_status/%s", result.TargetID), payload)
	}

	log.Printf("[FIREBASE REALTIME] Pushed log to Firebase DB: service='%s' (status=%v)", result.ServiceName, result.Status)
	return nil
}

// FetchLogsFromFirebase fetches real-time logs stored in Firebase Database
func (f *FirebaseClient) FetchLogsFromFirebase() ([]models.HealthCheckResult, error) {
	collection := f.config.Collection
	if collection == "" {
		collection = "server_monitoring_logs"
	}

	bodyBytes, _, err := f.doRequest("GET", collection, nil)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return []models.HealthCheckResult{}, nil
	}

	// Try unmarshaling as map of push keys
	var logsMap map[string]models.HealthCheckResult
	if err := json.Unmarshal(bodyBytes, &logsMap); err == nil {
		results := make([]models.HealthCheckResult, 0, len(logsMap))
		for pushKey, item := range logsMap {
			if item.ID == "" {
				item.ID = pushKey
			}
			results = append(results, item)
		}
		return results, nil
	}

	// Fallback: try as array
	var logsArray []models.HealthCheckResult
	if err := json.Unmarshal(bodyBytes, &logsArray); err == nil {
		return logsArray, nil
	}

	return nil, fmt.Errorf("unexpected response format from Firebase logs")
}

// ============================================================================
// 2. TARGETS STORAGE (/targets)
// ============================================================================

// SaveTarget stores or updates a single target in Firebase at /targets/{target_id}.json
func (f *FirebaseClient) SaveTarget(target models.Target) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}
	if target.ID == "" {
		return fmt.Errorf("target ID cannot be empty")
	}

	_, _, err := f.doRequest("PUT", fmt.Sprintf("targets/%s", target.ID), target)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to save target %s to Firebase: %v", target.ID, err)
		return err
	}
	log.Printf("[FIREBASE REALTIME] Synced target '%s' (%s) to Firebase DB", target.Name, target.ID)
	return nil
}

// DeleteTarget removes a target from Firebase at /targets/{target_id}.json
func (f *FirebaseClient) DeleteTarget(targetID string) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}
	if targetID == "" {
		return fmt.Errorf("target ID cannot be empty")
	}

	_, _, err := f.doRequest("DELETE", fmt.Sprintf("targets/%s", targetID), nil)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to delete target %s from Firebase: %v", targetID, err)
		return err
	}
	// Also clean up current status
	_, _, _ = f.doRequest("DELETE", fmt.Sprintf("current_status/%s", targetID), nil)

	log.Printf("[FIREBASE REALTIME] Removed target '%s' from Firebase DB", targetID)
	return nil
}

// SaveAllTargets overwrites the full targets map in Firebase at /targets.json
func (f *FirebaseClient) SaveAllTargets(targets []models.Target) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}

	targetsMap := make(map[string]models.Target)
	for _, t := range targets {
		if t.ID != "" {
			targetsMap[t.ID] = t
		}
	}

	_, _, err := f.doRequest("PUT", "targets", targetsMap)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to save targets map to Firebase: %v", err)
		return err
	}
	log.Printf("[FIREBASE REALTIME] Successfully synced all %d target(s) to Firebase DB", len(targets))
	return nil
}

// FetchTargets retrieves all targets stored in Firebase at /targets.json
func (f *FirebaseClient) FetchTargets() ([]models.Target, error) {
	bodyBytes, _, err := f.doRequest("GET", "targets", nil)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return []models.Target{}, nil
	}

	// Try map format
	var targetsMap map[string]models.Target
	if err := json.Unmarshal(bodyBytes, &targetsMap); err == nil {
		results := make([]models.Target, 0, len(targetsMap))
		for k, t := range targetsMap {
			if t.ID == "" {
				t.ID = k
			}
			results = append(results, t)
		}
		return results, nil
	}

	// Fallback: array format
	var targetsArray []models.Target
	if err := json.Unmarshal(bodyBytes, &targetsArray); err == nil {
		return targetsArray, nil
	}

	return nil, fmt.Errorf("unexpected response format from Firebase targets")
}

// ============================================================================
// 3. USERS & AUTH STORAGE (/auth & /users)
// ============================================================================

// SaveAuthConfig updates credentials in Firebase at /auth.json and /users/{username}.json
func (f *FirebaseClient) SaveAuthConfig(auth models.AuthConfig) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}
	if auth.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// 1. Save root auth snapshot
	_, _, err := f.doRequest("PUT", "auth", auth)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to save auth config to Firebase: %v", err)
		return err
	}

	// 2. Also save to users collection
	userObj := models.User{
		Username:  auth.Username,
		Password:  auth.Password,
		Role:      "admin",
		UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	_, _, _ = f.doRequest("PUT", fmt.Sprintf("users/%s", auth.Username), userObj)

	log.Printf("[FIREBASE REALTIME] Synced auth credentials for user '%s' to Firebase DB", auth.Username)
	return nil
}

// FetchAuthConfig retrieves auth credentials from Firebase at /auth.json
func (f *FirebaseClient) FetchAuthConfig() (*models.AuthConfig, error) {
	bodyBytes, _, err := f.doRequest("GET", "auth", nil)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return nil, nil
	}

	var auth models.AuthConfig
	if err := json.Unmarshal(bodyBytes, &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}

// SaveUser saves an individual user in Firebase at /users/{username}.json
func (f *FirebaseClient) SaveUser(user models.User) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}
	if user.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if user.UpdatedAt == "" {
		user.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	}

	_, _, err := f.doRequest("PUT", fmt.Sprintf("users/%s", user.Username), user)
	if err != nil {
		return err
	}
	log.Printf("[FIREBASE REALTIME] Saved user '%s' to Firebase DB", user.Username)
	return nil
}

// FetchUsers retrieves all users from Firebase at /users.json
func (f *FirebaseClient) FetchUsers() ([]models.User, error) {
	bodyBytes, _, err := f.doRequest("GET", "users", nil)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return []models.User{}, nil
	}

	var usersMap map[string]models.User
	if err := json.Unmarshal(bodyBytes, &usersMap); err == nil {
		results := make([]models.User, 0, len(usersMap))
		for k, u := range usersMap {
			if u.Username == "" {
				u.Username = k
			}
			results = append(results, u)
		}
		return results, nil
	}

	return []models.User{}, nil
}

// ============================================================================
// 4. EMAIL CONFIG STORAGE (/email_config)
// ============================================================================

// SaveEmailConfig stores email alert configuration in Firebase at /email_config.json
func (f *FirebaseClient) SaveEmailConfig(email models.EmailConfig) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return nil
	}

	_, _, err := f.doRequest("PUT", "email_config", email)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to save email config to Firebase: %v", err)
		return err
	}

	log.Printf("[FIREBASE REALTIME] Synced email config (%d recipients) to Firebase DB", len(email.ToEmails))
	return nil
}

// FetchEmailConfig retrieves email alert configuration from Firebase at /email_config.json
func (f *FirebaseClient) FetchEmailConfig() (*models.EmailConfig, error) {
	bodyBytes, _, err := f.doRequest("GET", "email_config", nil)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return nil, nil
	}

	var email models.EmailConfig
	if err := json.Unmarshal(bodyBytes, &email); err != nil {
		return nil, err
	}

	return &email, nil
}

// ============================================================================
// 5. BIDIRECTIONAL FULL SYNCHRONIZATION
// ============================================================================

// SyncAllToFirebase pushes targets, auth, email, and metadata to Firebase Realtime DB
func (f *FirebaseClient) SyncAllToFirebase(cfg models.Config) error {
	if !f.config.Enabled || f.CleanDatabaseURL() == "" {
		return fmt.Errorf("firebase is disabled or database_url is empty")
	}

	log.Printf("[FIREBASE SYNC] Pushing all local configurations to Firebase DB...")

	// 1. Sync Targets
	if err := f.SaveAllTargets(cfg.Targets); err != nil {
		log.Printf("[FIREBASE SYNC WARNING] Targets sync failed: %v", err)
	}

	// 2. Sync Auth / Users
	if err := f.SaveAuthConfig(cfg.Auth); err != nil {
		log.Printf("[FIREBASE SYNC WARNING] Auth sync failed: %v", err)
	}

	// 3. Sync Email Config
	if err := f.SaveEmailConfig(cfg.Email); err != nil {
		log.Printf("[FIREBASE SYNC WARNING] Email config sync failed: %v", err)
	}

	log.Printf("[FIREBASE SYNC] All entities (Targets, Users/Auth, Email Config) synced to Firebase DB.")
	return nil
}
