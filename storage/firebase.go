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

// FirebaseClient handles persisting and retrieving health check records, targets, and users from Firebase Realtime Database
type FirebaseClient struct {
	config models.FirebaseConfig
	client *http.Client
}

// NewFirebaseClient creates a new Firebase client
func NewFirebaseClient(cfg models.FirebaseConfig) *FirebaseClient {
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = "https://server-monitor-8ffb0-default-rtdb.firebaseio.com"
	}
	cfg.Enabled = true

	return &FirebaseClient{
		config: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
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
	url := strings.TrimRight(strings.TrimSpace(f.config.DatabaseURL), "/")
	if url == "" {
		url = "https://server-monitor-8ffb0-default-rtdb.firebaseio.com"
	}
	return url
}

// SaveLog saves a health check result into Firebase Database in real-time
func (f *FirebaseClient) SaveLog(result models.HealthCheckResult) error {
	baseURL := f.CleanDatabaseURL()

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

	dataJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode firebase payload: %w", err)
	}

	// 1. Append record to Firebase Realtime DB collection: POST {baseURL}/{collection}.json
	endpoint := fmt.Sprintf("%s/%s.json", baseURL, collection)
	if f.config.AuthSecret != "" {
		endpoint += fmt.Sprintf("?auth=%s", f.config.AuthSecret)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(dataJSON))
	if err != nil {
		return fmt.Errorf("failed to create firebase HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		log.Printf("[FIREBASE WARNING] Failed to push log to Firebase DB (%s): %v", endpoint, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[FIREBASE WARNING] Firebase returned status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("firebase error %d: %s", resp.StatusCode, string(body))
	}

	// 2. Also update current status snapshot: PUT {baseURL}/current_status/{target_id}.json
	if result.TargetID != "" {
		statusEndpoint := fmt.Sprintf("%s/current_status/%s.json", baseURL, result.TargetID)
		if f.config.AuthSecret != "" {
			statusEndpoint += fmt.Sprintf("?auth=%s", f.config.AuthSecret)
		}
		statusReq, _ := http.NewRequest("PUT", statusEndpoint, bytes.NewBuffer(dataJSON))
		if statusReq != nil {
			statusReq.Header.Set("Content-Type", "application/json")
			if sResp, sErr := f.client.Do(statusReq); sErr == nil {
				sResp.Body.Close()
			}
		}
	}

	log.Printf("[FIREBASE REALTIME] Pushed log to Firebase DB: service='%s' (status=%v)", result.ServiceName, result.Status)
	return nil
}

// FetchLogsFromFirebase fetches real-time logs stored in Firebase Database
func (f *FirebaseClient) FetchLogsFromFirebase() ([]models.HealthCheckResult, error) {
	baseURL := f.CleanDatabaseURL()
	collection := f.config.Collection
	if collection == "" {
		collection = "server_monitoring_logs"
	}

	endpoint := fmt.Sprintf("%s/%s.json", baseURL, collection)
	if f.config.AuthSecret != "" {
		endpoint += fmt.Sprintf("?auth=%s", f.config.AuthSecret)
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from firebase (%s): %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("firebase error HTTP %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return []models.HealthCheckResult{}, nil
	}

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

	var logsArray []models.HealthCheckResult
	if err := json.Unmarshal(bodyBytes, &logsArray); err == nil {
		return logsArray, nil
	}

	return nil, fmt.Errorf("unexpected response format from Firebase")
}

// ============================================================================
// FIREBASE TARGETS MANAGEMENT (STRICTLY FIREBASE STORAGE)
// ============================================================================

func (f *FirebaseClient) FetchTargetsFromFirebase() ([]models.Target, error) {
	baseURL := f.CleanDatabaseURL()
	endpoint := fmt.Sprintf("%s/server_monitoring_targets.json", baseURL)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("firebase targets error HTTP %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return []models.Target{}, nil
	}

	var targetsMap map[string]models.Target
	if err := json.Unmarshal(bodyBytes, &targetsMap); err == nil {
		results := make([]models.Target, 0, len(targetsMap))
		for id, target := range targetsMap {
			if target.ID == "" {
				target.ID = id
			}
			// Enforce 5-minute interval for all targets
			target.IntervalMinutes = 5
			results = append(results, target)
		}
		return results, nil
	}

	var targetsArray []models.Target
	if err := json.Unmarshal(bodyBytes, &targetsArray); err == nil {
		for i := range targetsArray {
			targetsArray[i].IntervalMinutes = 5
		}
		return targetsArray, nil
	}

	return []models.Target{}, nil
}

func (f *FirebaseClient) SaveTargetToFirebase(target models.Target) error {
	baseURL := f.CleanDatabaseURL()
	target.IntervalMinutes = 5 // Strictly 5 minutes interval

	endpoint := fmt.Sprintf("%s/server_monitoring_targets/%s.json", baseURL, target.ID)
	dataJSON, err := json.Marshal(target)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", endpoint, bytes.NewBuffer(dataJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firebase error HTTP %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[FIREBASE TARGETS] Saved target '%s' (%s) strictly in Firebase RTDB", target.Name, target.ID)
	return nil
}

func (f *FirebaseClient) DeleteTargetFromFirebase(targetID string) error {
	baseURL := f.CleanDatabaseURL()
	endpoint := fmt.Sprintf("%s/server_monitoring_targets/%s.json", baseURL, targetID)

	req, err := http.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Printf("[FIREBASE TARGETS] Deleted target '%s' strictly from Firebase RTDB", targetID)
	return nil
}

// ============================================================================
// FIREBASE USERS MANAGEMENT (STRICTLY FIREBASE STORAGE)
// ============================================================================

func (f *FirebaseClient) FetchUsersFromFirebase() ([]models.User, error) {
	baseURL := f.CleanDatabaseURL()
	endpoint := fmt.Sprintf("%s/server_monitoring_users.json", baseURL)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("firebase users error HTTP %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if string(bodyBytes) == "null" || len(bodyBytes) == 0 {
		return []models.User{}, nil
	}

	var usersMap map[string]models.User
	if err := json.Unmarshal(bodyBytes, &usersMap); err == nil {
		results := make([]models.User, 0, len(usersMap))
		for id, user := range usersMap {
			if user.ID == "" {
				user.ID = id
			}
			results = append(results, user)
		}
		return results, nil
	}

	var usersArray []models.User
	if err := json.Unmarshal(bodyBytes, &usersArray); err == nil {
		return usersArray, nil
	}

	return []models.User{}, nil
}

func (f *FirebaseClient) SaveUserToFirebase(user models.User) error {
	baseURL := f.CleanDatabaseURL()
	endpoint := fmt.Sprintf("%s/server_monitoring_users/%s.json", baseURL, user.ID)

	dataJSON, err := json.Marshal(user)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", endpoint, bytes.NewBuffer(dataJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("firebase error HTTP %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[FIREBASE USERS] Saved user '%s' (%s) strictly in Firebase RTDB", user.Name, user.Email)
	return nil
}

func (f *FirebaseClient) DeleteUserFromFirebase(userID string) error {
	baseURL := f.CleanDatabaseURL()
	endpoint := fmt.Sprintf("%s/server_monitoring_users/%s.json", baseURL, userID)

	req, err := http.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Printf("[FIREBASE USERS] Deleted user '%s' strictly from Firebase RTDB", userID)
	return nil
}
