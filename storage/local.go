package storage

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"serverMonitoring/models"
)

type LocalStorage struct {
	mu           sync.RWMutex
	filePath     string
	logs         []models.HealthCheckResult
	latestStatus map[string]models.HealthCheckResult
	maxLogs      int
	firebase     *FirebaseClient
}

// NewLocalStorage creates a new local storage manager with disk persistence and Firebase integration
func NewLocalStorage(filePath string, maxLogs int, fb *FirebaseClient) (*LocalStorage, error) {
	if maxLogs <= 0 {
		maxLogs = 5000
	}

	ls := &LocalStorage{
		filePath:     filePath,
		logs:         make([]models.HealthCheckResult, 0),
		latestStatus: make(map[string]models.HealthCheckResult),
		maxLogs:      maxLogs,
		firebase:     fb,
	}

	if err := ls.load(); err != nil {
		log.Printf("[STORAGE] Initializing new logs store (could not load existing: %v)", err)
	}

	return ls, nil
}

func (ls *LocalStorage) GetFirebaseClient() *FirebaseClient {
	return ls.firebase
}

func (ls *LocalStorage) load() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	data, err := os.ReadFile(ls.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var storedLogs []models.HealthCheckResult
	if err := json.Unmarshal(data, &storedLogs); err != nil {
		return err
	}

	ls.logs = storedLogs
	for _, l := range storedLogs {
		if l.TargetID != "" {
			if existing, ok := ls.latestStatus[l.TargetID]; !ok || l.Timestamp > existing.Timestamp {
				ls.latestStatus[l.TargetID] = l
			}
		}
	}

	return nil
}

func (ls *LocalStorage) persistUnlocked() error {
	dir := filepath.Dir(ls.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		dir = os.TempDir()
		ls.filePath = filepath.Join(dir, "logs.json")
		_ = os.MkdirAll(dir, 0755)
	}

	data, err := json.MarshalIndent(ls.logs, "", "  ")
	if err != nil {
		return err
	}

	writeErr := os.WriteFile(ls.filePath, data, 0644)
	if writeErr != nil {
		// Fallback for read-only serverless filesystem like Vercel
		tmpPath := filepath.Join(os.TempDir(), "logs.json")
		_ = os.WriteFile(tmpPath, data, 0644)
	}

	return nil
}

// SaveLog saves a new health check result, sends to Firebase, and updates in-memory and disk records
func (ls *LocalStorage) SaveLog(result models.HealthCheckResult) error {
	ls.mu.Lock()
	// Prepend to list so newest is first
	ls.logs = append([]models.HealthCheckResult{result}, ls.logs...)
	if len(ls.logs) > ls.maxLogs {
		ls.logs = ls.logs[:ls.maxLogs]
	}

	if result.TargetID != "" {
		ls.latestStatus[result.TargetID] = result
	}

	if err := ls.persistUnlocked(); err != nil {
		log.Printf("[STORAGE WARNING] Failed to persist logs to disk: %v", err)
	}
	ls.mu.Unlock()

	// Asynchronously push to Firebase DB in real-time
	if ls.firebase != nil {
		go func(res models.HealthCheckResult) {
			if err := ls.firebase.SaveLog(res); err != nil {
				log.Printf("[FIREBASE REALTIME ERROR] Push failed: %v", err)
			}
		}(result)
	}

	return nil
}

// SyncFromFirebase pulls logs stored in Firebase Database and merges with local store
func (ls *LocalStorage) SyncFromFirebase() (int, error) {
	if ls.firebase == nil {
		return 0, nil
	}

	fbLogs, err := ls.firebase.FetchLogsFromFirebase()
	if err != nil {
		return 0, err
	}

	ls.mu.Lock()
	defer ls.mu.Unlock()

	existingIDs := make(map[string]bool)
	for _, l := range ls.logs {
		if l.ID != "" {
			existingIDs[l.ID] = true
		}
	}

	newCount := 0
	for _, fl := range fbLogs {
		if !existingIDs[fl.ID] {
			ls.logs = append(ls.logs, fl)
			existingIDs[fl.ID] = true
			newCount++
		}
	}

	if newCount > 0 {
		_ = ls.persistUnlocked()
	}

	return newCount, nil
}

// GetLogs returns filtered logs and total matching count
func (ls *LocalStorage) GetLogs(filter models.LogFilter) ([]models.HealthCheckResult, int, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	filtered := make([]models.HealthCheckResult, 0, len(ls.logs))

	for _, item := range ls.logs {
		// Service Name filter
		if filter.ServiceName != "" && filter.ServiceName != "all" {
			if !strings.EqualFold(item.ServiceName, filter.ServiceName) && !strings.Contains(strings.ToLower(item.ServiceName), strings.ToLower(filter.ServiceName)) {
				continue
			}
		}

		// Type filter
		if filter.Type != "" && filter.Type != "all" {
			if !strings.EqualFold(item.Type, filter.Type) {
				continue
			}
		}

		// Status filter
		if filter.Status != "" && filter.Status != "all" {
			if filter.Status == "success" && !item.Status {
				continue
			}
			if (filter.Status == "failure" || filter.Status == "failed") && item.Status {
				continue
			}
		}

		// Date filters (comparing YYYY-MM-DD)
		hitDate := ""
		if len(item.HitTime) >= 10 {
			hitDate = item.HitTime[:10]
		}

		if filter.FromDate != "" && hitDate != "" {
			if hitDate < filter.FromDate {
				continue
			}
		}

		if filter.ToDate != "" && hitDate != "" {
			if hitDate > filter.ToDate {
				continue
			}
		}

		// Full-text search
		if filter.Search != "" {
			term := strings.ToLower(filter.Search)
			dataBytes, _ := json.Marshal(item.Data)
			match := strings.Contains(strings.ToLower(item.ServiceName), term) ||
				strings.Contains(strings.ToLower(item.URL), term) ||
				strings.Contains(strings.ToLower(item.Message), term) ||
				strings.Contains(strings.ToLower(item.ErrorDetail), term) ||
				strings.Contains(strings.ToLower(string(dataBytes)), term)
			if !match {
				continue
			}
		}

		filtered = append(filtered, item)
	}

	total := len(filtered)

	// Pagination
	page := filter.Page
	if page < 1 {
		page = 1
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 25
	}

	start := (page - 1) * limit
	if start >= total {
		return []models.HealthCheckResult{}, total, nil
	}

	end := start + limit
	if end > total {
		end = total
	}

	paginated := filtered[start:end]

	// Assign S.No (serial number) for table display
	resultSlice := make([]models.HealthCheckResult, len(paginated))
	for i, item := range paginated {
		item.SNo = start + i + 1
		resultSlice[i] = item
	}

	return resultSlice, total, nil
}

// GetLatestStatus returns snapshot of latest check for all targets
func (ls *LocalStorage) GetLatestStatus() map[string]models.HealthCheckResult {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	result := make(map[string]models.HealthCheckResult)
	for k, v := range ls.latestStatus {
		result[k] = v
	}
	return result
}

// GetSummary calculates high-level monitoring metrics
func (ls *LocalStorage) GetSummary() models.ServerSummary {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	totalChecks := len(ls.logs)
	passedChecks := 0
	failedChecks := 0
	for _, l := range ls.logs {
		if l.Status {
			passedChecks++
		} else {
			failedChecks++
		}
	}

	uptimePercent := 100.0
	if totalChecks > 0 {
		uptimePercent = (float64(passedChecks) / float64(totalChecks)) * 100.0
	}

	onlineTargets := 0
	offlineTargets := 0
	targetStatuses := make(map[string]models.HealthCheckResult)

	for k, v := range ls.latestStatus {
		targetStatuses[k] = v
		if v.Status {
			onlineTargets++
		} else {
			offlineTargets++
		}
	}

	lastCheck := "Never"
	if len(ls.logs) > 0 {
		lastCheck = ls.logs[0].HitTime
	}

	return models.ServerSummary{
		TotalTargets:   len(ls.latestStatus),
		OnlineTargets:  onlineTargets,
		OfflineTargets: offlineTargets,
		TotalChecks:    totalChecks,
		PassedChecks:   passedChecks,
		FailedChecks:   failedChecks,
		UptimePercent:  uptimePercent,
		LastCheckTime:  lastCheck,
		TargetStatuses: targetStatuses,
	}
}

// PurgeOldLogs removes logs older than the specified number of days
func (ls *LocalStorage) PurgeOldLogs(days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}

	ls.mu.Lock()
	defer ls.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	newLogs := make([]models.HealthCheckResult, 0)
	deletedCount := 0

	for _, l := range ls.logs {
		if l.Timestamp >= cutoff {
			newLogs = append(newLogs, l)
		} else {
			deletedCount++
		}
	}

	if deletedCount > 0 {
		ls.logs = newLogs
		if err := ls.persistUnlocked(); err != nil {
			log.Printf("[STORAGE PURGE WARNING] Could not persist purged logs: %v", err)
		}
		log.Printf("[STORAGE PURGE] Successfully purged %d log(s) older than %d day(s)", deletedCount, days)
	}

	return deletedCount, nil
}

// Close persists remaining logs
func (ls *LocalStorage) Close() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.persistUnlocked()
}
