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

// FirebaseClient handles persisting and retrieving health check records from Firebase Realtime Database
type FirebaseClient struct {
	config models.FirebaseConfig
	client *http.Client
}

// NewFirebaseClient creates a new Firebase client
func NewFirebaseClient(cfg models.FirebaseConfig) *FirebaseClient {
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
	return strings.TrimRight(strings.TrimSpace(f.config.DatabaseURL), "/")
}

// SaveLog saves a health check result into Firebase Database in real-time
func (f *FirebaseClient) SaveLog(result models.HealthCheckResult) error {
	baseURL := f.CleanDatabaseURL()
	if !f.config.Enabled || baseURL == "" {
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
	if !f.config.Enabled || baseURL == "" {
		return nil, fmt.Errorf("firebase is not enabled or database_url is empty")
	}

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

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("permission denied (HTTP %d). Please set Firebase Realtime Database rules to .read: true, .write: true", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("firebase error HTTP %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Firebase Realtime DB returns either a map of pushIDs -> objects, or an array, or null
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

	return nil, fmt.Errorf("unexpected response format from Firebase")
}
