package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"serverMonitoring/config"
	"serverMonitoring/models"
	"serverMonitoring/monitor"
	"serverMonitoring/notifier"
	"serverMonitoring/storage"
)

type APIHandler struct {
	cfgManager *config.ConfigManager
	storage    storage.Storage
	scheduler  *monitor.Scheduler
	notifier   *notifier.EmailNotifier
}

func NewAPIHandler(cfgMgr *config.ConfigManager, store storage.Storage, sched *monitor.Scheduler) *APIHandler {
	return &APIHandler{
		cfgManager: cfgMgr,
		storage:    store,
		scheduler:  sched,
		notifier:   notifier.GetNotifier(),
	}
}

// RegisterRoutes registers all API and web routes on the given ServeMux
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	handleWithCORS := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			handler(w, r)
		})
	}

	// API Endpoints with CORS
	handleWithCORS("/api/auth/login", h.handleLogin)
	handleWithCORS("/api/status", h.handleStatus)
	handleWithCORS("/api/logs", h.handleLogs)
	handleWithCORS("/api/console-logs", h.handleConsoleLogs)
	handleWithCORS("/api/check-now", h.handleCheckNow)
	handleWithCORS("/api/targets", h.handleTargets)
	handleWithCORS("/api/targets/", h.handleTargetByID)
	handleWithCORS("/api/users", h.handleUsers)
	handleWithCORS("/api/users/", h.handleUserByID)
	handleWithCORS("/api/config", h.handleConfig)
	handleWithCORS("/api/test-email", h.handleTestEmail)
	handleWithCORS("/api/export-logs", h.handleExportLogs)
	handleWithCORS("/api/firebase-status", h.handleFirebaseStatus)
	handleWithCORS("/api/firebase-sync", h.handleFirebaseSyncAll)
	handleWithCORS("/api/firebase-sync-all", h.handleFirebaseSyncAll)
	handleWithCORS("/api/purge-logs", h.handlePurgeLogs)
}

func (h *APIHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "message": "Invalid request body"})
		return
	}

	cfg := h.cfgManager.Get()
	authenticated := false

	// 1. Check against local / memory auth config
	if req.Username == cfg.Auth.Username && req.Password == cfg.Auth.Password {
		authenticated = true
	}

	// 2. Check against Firebase Auth if available
	fb := h.storage.GetFirebaseClient()
	if !authenticated && fb != nil {
		if fbAuth, err := fb.FetchAuthConfig(); err == nil && fbAuth != nil {
			if req.Username == fbAuth.Username && req.Password == fbAuth.Password {
				authenticated = true
			}
		}
	}

	if authenticated {
		token := fmt.Sprintf("zadroit-token-%d", time.Now().UnixNano())
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"message":  "Login successful",
			"token":    token,
			"username": req.Username,
		})
		return
	}

	writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
		"success": false,
		"message": "Invalid username or password",
	})
}

func (h *APIHandler) computeSummary() models.ServerSummary {
	fb := h.storage.GetFirebaseClient()
	var targets []models.Target
	var logs []models.HealthCheckResult

	if fb != nil {
		targets, _ = fb.FetchTargets()
		logs, _ = fb.FetchLogsFromFirebase()
	}

	if len(targets) == 0 && len(logs) == 0 {
		return h.storage.GetSummary()
	}

	// Sort logs newest first
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp > logs[j].Timestamp
	})

	totalChecks := len(logs)
	passedChecks := 0
	failedChecks := 0
	targetStatuses := make(map[string]models.HealthCheckResult)

	for _, l := range logs {
		if l.Status {
			passedChecks++
		} else {
			failedChecks++
		}

		// Keep newest status per target ID / service name
		if l.TargetID != "" {
			if _, exists := targetStatuses[l.TargetID]; !exists {
				targetStatuses[l.TargetID] = l
			}
		}
		if l.ServiceName != "" {
			if _, exists := targetStatuses[l.ServiceName]; !exists {
				targetStatuses[l.ServiceName] = l
			}
		}
	}

	uptimePercent := 100.0
	if totalChecks > 0 {
		uptimePercent = (float64(passedChecks) / float64(totalChecks)) * 100.0
	}

	onlineTargets := 0
	offlineTargets := 0
	for _, t := range targets {
		st, exists := targetStatuses[t.ID]
		if !exists {
			st, exists = targetStatuses[t.Name]
		}
		if exists && st.Status {
			onlineTargets++
		} else if exists && !st.Status {
			offlineTargets++
		}
	}

	lastCheck := "Never"
	if len(logs) > 0 {
		lastCheck = logs[0].HitTime
	}

	cfg := h.cfgManager.Get()
	intervalMins := cfg.Monitoring.IntervalMinutes
	if intervalMins <= 0 {
		intervalMins = 5
	}

	return models.ServerSummary{
		TotalTargets:   len(targets),
		OnlineTargets:  onlineTargets,
		OfflineTargets: offlineTargets,
		TotalChecks:    totalChecks,
		PassedChecks:   passedChecks,
		FailedChecks:   failedChecks,
		UptimePercent:  uptimePercent,
		LastCheckTime:  lastCheck,
		TargetStatuses: targetStatuses,
		IntervalMins:   intervalMins,
	}
}

func filterLogs(allLogs []models.HealthCheckResult, filter models.LogFilter) []models.HealthCheckResult {
	sort.Slice(allLogs, func(i, j int) bool {
		return allLogs[i].Timestamp > allLogs[j].Timestamp
	})

	filtered := make([]models.HealthCheckResult, 0, len(allLogs))
	for _, item := range allLogs {
		if filter.ServiceName != "" && filter.ServiceName != "all" {
			if !strings.EqualFold(item.ServiceName, filter.ServiceName) && !strings.Contains(strings.ToLower(item.ServiceName), strings.ToLower(filter.ServiceName)) {
				continue
			}
		}

		if filter.Type != "" && filter.Type != "all" {
			if !strings.EqualFold(item.Type, filter.Type) {
				continue
			}
		}

		if filter.Status != "" && filter.Status != "all" {
			if filter.Status == "success" && !item.Status {
				continue
			}
			if (filter.Status == "failure" || filter.Status == "failed") && item.Status {
				continue
			}
		}

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

	return filtered
}

func (h *APIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	summary := h.computeSummary()
	writeJSON(w, http.StatusOK, summary)
}

func (h *APIHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 25
	}

	filter := models.LogFilter{
		ServiceName: q.Get("service_name"),
		Type:        q.Get("type"),
		Status:      q.Get("status"),
		FromDate:    q.Get("from_date"),
		ToDate:      q.Get("to_date"),
		Search:      q.Get("search"),
		Page:        page,
		Limit:       limit,
	}

	var allLogs []models.HealthCheckResult
	fb := h.storage.GetFirebaseClient()
	if fb != nil {
		fbLogs, err := fb.FetchLogsFromFirebase()
		if err == nil {
			allLogs = fbLogs
		}
	}

	if len(allLogs) == 0 {
		logs, total, err := h.storage.GetLogs(filter)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"logs":  logs,
				"total": total,
				"page":  page,
				"limit": limit,
			})
			return
		}
	}

	filtered := filterLogs(allLogs, filter)
	total := len(filtered)
	start := (page - 1) * limit
	if start >= total {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"logs":  []models.HealthCheckResult{},
			"total": total,
			"page":  page,
			"limit": limit,
		})
		return
	}

	end := start + limit
	if end > total {
		end = total
	}

	paginated := filtered[start:end]
	resultSlice := make([]models.HealthCheckResult, len(paginated))
	for i, item := range paginated {
		item.SNo = start + i + 1
		resultSlice[i] = item
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  resultSlice,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *APIHandler) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		var req struct {
			TargetID string `json:"target_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		targetID = req.TargetID
	}

	if targetID != "" {
		res, err := h.scheduler.RunCheckForSingleTarget(targetID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		summary := h.computeSummary()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"results": []models.HealthCheckResult{*res},
			"summary": summary,
		})
		return
	}

	results := h.scheduler.RunChecksNow()
	summary := h.computeSummary()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"results": results,
		"summary": summary,
	})
}

func (h *APIHandler) handleTargets(w http.ResponseWriter, r *http.Request) {
	fb := h.storage.GetFirebaseClient()

	switch r.Method {
	case http.MethodGet:
		var targets []models.Target
		var err error
		if fb != nil {
			targets, err = fb.FetchTargets()
		}
		if err != nil || targets == nil {
			targets = []models.Target{}
		}
		writeJSON(w, http.StatusOK, targets)

	case http.MethodPost:
		var target models.Target
		if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid target JSON"})
			return
		}

		if target.ID == "" {
			target.ID = fmt.Sprintf("target_%d", time.Now().UnixNano())
		}
		if target.Name == "" || target.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Name and URL are required"})
			return
		}
		if target.Type == "" {
			target.Type = "backend"
		}
		if target.IntervalMinutes <= 0 {
			target.IntervalMinutes = 5
		}

		if fb != nil {
			if err := fb.SaveTarget(target); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"success": true,
			"target":  target,
			"message": "Target saved directly to Firebase",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleTargetByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/targets/")
	if path == "" {
		http.Error(w, "Missing target ID", http.StatusBadRequest)
		return
	}

	// Handle /api/targets/:id/check
	if strings.HasSuffix(path, "/check") {
		id := strings.TrimSuffix(path, "/check")
		id = strings.TrimSuffix(id, "/")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		res, err := h.scheduler.RunCheckForSingleTarget(id)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		summary := h.computeSummary()
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "results": []models.HealthCheckResult{*res}, "summary": summary})
		return
	}

	id := path
	fb := h.storage.GetFirebaseClient()

	switch r.Method {
	case http.MethodGet:
		var target *models.Target
		if fb != nil {
			targets, _ := fb.FetchTargets()
			for _, t := range targets {
				if t.ID == id {
					target = &t
					break
				}
			}
		}
		if target == nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Target not found"})
			return
		}
		writeJSON(w, http.StatusOK, target)

	case http.MethodPut:
		var target models.Target
		if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid target JSON"})
			return
		}
		target.ID = id
		if target.IntervalMinutes <= 0 {
			target.IntervalMinutes = 5
		}

		if fb != nil {
			if err := fb.SaveTarget(target); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"target":  target,
			"message": "Target updated directly in Firebase",
		})

	case http.MethodDelete:
		if fb != nil {
			if err := fb.DeleteTarget(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Target deleted from Firebase",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleUsers(w http.ResponseWriter, r *http.Request) {
	fb := h.storage.GetFirebaseClient()

	switch r.Method {
	case http.MethodGet:
		var users []models.User
		var err error
		if fb != nil {
			users, err = fb.FetchUsers()
		}
		if err != nil || users == nil {
			users = []models.User{}
		}
		writeJSON(w, http.StatusOK, users)

	case http.MethodPost:
		var user models.User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid user JSON"})
			return
		}

		if user.ID == "" {
			user.ID = fmt.Sprintf("user_%d", time.Now().UnixNano())
		}
		if user.Name == "" || user.Email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Name and Email are required"})
			return
		}

		if fb != nil {
			if err := fb.SaveUser(user); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusCreated, user)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleUserByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if id == "" {
		http.Error(w, "Missing user ID", http.StatusBadRequest)
		return
	}

	fb := h.storage.GetFirebaseClient()

	switch r.Method {
	case http.MethodPut:
		var user models.User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid user JSON"})
			return
		}
		user.ID = id

		if fb != nil {
			if err := fb.SaveUser(user); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, user)

	case http.MethodDelete:
		if fb != nil {
			if err := fb.DeleteUser(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "User deleted from Firebase"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := h.cfgManager.Get()
		safeConfig := cfg
		if safeConfig.Email.AppPassword != "" {
			safeConfig.Email.AppPassword = "••••••••••••••••"
		}
		if safeConfig.Auth.Password != "" {
			safeConfig.Auth.Password = "••••••••"
		}
		writeJSON(w, http.StatusOK, safeConfig)

	case http.MethodPost:
		var newCfg models.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid config JSON"})
			return
		}

		current := h.cfgManager.Get()
		// Preserve passwords if user didn't modify masked placeholder
		if newCfg.Email.AppPassword == "••••••••••••••••" || newCfg.Email.AppPassword == "" {
			newCfg.Email.AppPassword = current.Email.AppPassword
		}
		if newCfg.Auth.Password == "••••••••" || newCfg.Auth.Password == "" {
			newCfg.Auth.Password = current.Auth.Password
		}
		if len(newCfg.Targets) == 0 {
			newCfg.Targets = current.Targets
		}

		if err := h.cfgManager.Save(newCfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Config updated locally and synced with Firebase",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := h.cfgManager.Get()
	err := h.notifier.SendTestEmail(cfg.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to send test email: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Test email sent to %s", strings.Join(cfg.Email.ToEmails, ", ")),
	})
}

func (h *APIHandler) handleExportLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := models.LogFilter{
		ServiceName: q.Get("service_name"),
		Type:        q.Get("type"),
		Status:      q.Get("status"),
		FromDate:    q.Get("from_date"),
		ToDate:      q.Get("to_date"),
		Search:      q.Get("search"),
		Page:        1,
		Limit:       100000,
	}

	var allLogs []models.HealthCheckResult
	fb := h.storage.GetFirebaseClient()
	if fb != nil {
		fbLogs, err := fb.FetchLogsFromFirebase()
		if err == nil {
			allLogs = fbLogs
		}
	}

	if len(allLogs) == 0 {
		logs, _, _ := h.storage.GetLogs(filter)
		allLogs = logs
	}

	filtered := filterLogs(allLogs, filter)

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=server_logs_%s.csv", time.Now().In(models.ISTLocation).Format("20060102_150405")))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"S.No", "Service Name", "Type", "URL", "Status", "Hit Time (IST)", "HTTP Code", "Response Time (ms)", "Message", "Data"})

	for i, item := range filtered {
		statusStr := "FAILURE"
		if item.Status {
			statusStr = "SUCCESS"
		}

		dataJSON, _ := json.Marshal(item.Data)

		writer.Write([]string{
			strconv.Itoa(i + 1),
			item.ServiceName,
			item.Type,
			item.URL,
			statusStr,
			item.HitTime,
			strconv.Itoa(item.HTTPStatus),
			strconv.FormatInt(item.ResponseTimeMs, 10),
			item.Message,
			string(dataJSON),
		})
	}
}

func (h *APIHandler) handleFirebaseStatus(w http.ResponseWriter, r *http.Request) {
	fb := h.storage.GetFirebaseClient()
	if fb == nil {
		writeJSON(w, http.StatusOK, models.FirebaseSyncStatus{
			Enabled: false,
			Status:  "disabled",
			Message: "Firebase client not initialized",
		})
		return
	}

	cfg := fb.GetConfig()
	if !cfg.Enabled || fb.CleanDatabaseURL() == "" {
		writeJSON(w, http.StatusOK, models.FirebaseSyncStatus{
			Enabled:     false,
			Status:      "disabled",
			DatabaseURL: cfg.DatabaseURL,
			Message:     "Firebase is disabled or Database URL is empty",
		})
		return
	}

	// Read logs, targets, users from Firebase
	logs, err := fb.FetchLogsFromFirebase()
	if err != nil {
		writeJSON(w, http.StatusOK, models.FirebaseSyncStatus{
			Enabled:     true,
			Status:      "error",
			DatabaseURL: fb.CleanDatabaseURL(),
			Message:     err.Error(),
		})
		return
	}

	targets, _ := fb.FetchTargets()
	users, _ := fb.FetchUsers()
	emailCfg, _ := fb.FetchEmailConfig()

	writeJSON(w, http.StatusOK, models.FirebaseSyncStatus{
		Enabled:      true,
		Status:       "connected",
		DatabaseURL:  fb.CleanDatabaseURL(),
		LogsCount:    len(logs),
		TargetsCount: len(targets),
		UsersCount:   len(users),
		EmailSynced:  emailCfg != nil,
		Message:      "Real-time bidirectional synchronization active for Logs, Targets, Users, and Emails",
		LastSyncedAt: models.NowFormatted(),
	})
}

func (h *APIHandler) handleFirebaseLogs(w http.ResponseWriter, r *http.Request) {
	fb := h.storage.GetFirebaseClient()
	if fb == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Firebase client not available"})
		return
	}

	logs, err := fb.FetchLogsFromFirebase()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"logs":    []models.HealthCheckResult{},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   len(logs),
		"logs":    logs,
	})
}

func (h *APIHandler) handleFirebaseSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	count, err := h.storage.SyncFromFirebase()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"new_count": count,
		"message":   fmt.Sprintf("Synchronized %d new logs from Firebase", count),
	})
}

func (h *APIHandler) handleFirebaseSyncAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fb := h.storage.GetFirebaseClient()
	if fb == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Firebase client not available"})
		return
	}

	// 1. Sync config & entities to Firebase
	cfg := h.cfgManager.Get()
	if err := fb.SyncAllToFirebase(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 2. Sync logs from Firebase to local
	newLogs, _ := h.storage.SyncFromFirebase()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"message":       "All Targets, Users, Emails, and Logs successfully synchronized with Firebase",
		"targets_count": len(cfg.Targets),
		"new_logs_sync": newLogs,
	})
}

func (h *APIHandler) handleConsoleLogs(w http.ResponseWriter, r *http.Request) {
	logFilePath := "data/app.log"
	data, err := os.ReadFile(logFilePath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"lines":   []string{"Log file initialized. Waiting for output..."},
			"raw":     "",
		})
		return
	}

	rawText := string(data)
	allLines := strings.Split(rawText, "\n")
	limit := 200
	if len(allLines) > limit {
		allLines = allLines[len(allLines)-limit:]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   len(allLines),
		"lines":   allLines,
		"raw":     rawText,
	})
}

func (h *APIHandler) handlePurgeLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	daysStr := r.URL.Query().Get("days")
	days := 0
	if daysStr != "" {
		days, _ = strconv.Atoi(daysStr)
	}

	if days <= 0 {
		cfg := h.cfgManager.Get()
		days = cfg.Firebase.AutoDeleteDays
		if days <= 0 {
			days = 7 // Default 7 days if unspecified
		}
	}

	fb := h.storage.GetFirebaseClient()
	fbPurged := 0
	if fb != nil {
		fbPurged, _ = fb.PurgeOldLogsFromFirebase(days)
	}

	localPurged, _ := h.storage.PurgeOldLogs(days)
	totalPurged := fbPurged
	if localPurged > totalPurged {
		totalPurged = localPurged
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"purged_count": totalPurged,
		"days":         days,
		"message":      fmt.Sprintf("Successfully purged %d log(s) older than %d day(s) from Firebase DB", totalPurged, days),
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
