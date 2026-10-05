package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	handleWithCORS("/api/firebase-logs", h.handleFirebaseLogs)
	handleWithCORS("/api/firebase-sync", h.handleFirebaseSync)
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
	// Check static credentials (default: Zadroit / ZadGugSlm06)
	if req.Username == cfg.Auth.Username && req.Password == cfg.Auth.Password {
		// Generate simple session token
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

func (h *APIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	summary := h.storage.GetSummary()
	cfg := h.cfgManager.Get()
	summary.IntervalMins = cfg.Monitoring.IntervalMinutes
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

	logs, total, err := h.storage.GetLogs(filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
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
		summary := h.storage.GetSummary()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"results": []models.HealthCheckResult{*res},
			"summary": summary,
		})
		return
	}

	results := h.scheduler.RunChecksNow()
	summary := h.storage.GetSummary()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"results": results,
		"summary": summary,
	})
}

func (h *APIHandler) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		targets := h.cfgManager.GetTargets()
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

		if err := h.cfgManager.AddTarget(target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, target)

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
		summary := h.storage.GetSummary()
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "results": []models.HealthCheckResult{*res}, "summary": summary})
		return
	}

	id := path
	switch r.Method {
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

		if err := h.cfgManager.UpdateTarget(target); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, target)

	case http.MethodDelete:
		if err := h.cfgManager.DeleteTarget(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Target deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users := h.cfgManager.GetUsers()
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

		if err := h.cfgManager.AddUser(user); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
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

	switch r.Method {
	case http.MethodPut:
		var user models.User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid user JSON"})
			return
		}
		user.ID = id

		if err := h.cfgManager.UpdateUser(user); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, user)

	case http.MethodDelete:
		if err := h.cfgManager.DeleteUser(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "User deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := h.cfgManager.Get()
		// Mask sensitive password before returning to client
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

		if err := h.cfgManager.Save(newCfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Config updated"})

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
		Limit:       10000,
	}

	logs, _, err := h.storage.GetLogs(filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=server_logs_%s.csv", time.Now().Format("20060102_150405")))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header matching user specifications
	writer.Write([]string{"S.No", "Service Name", "Type", "URL", "Status", "Hit Time", "HTTP Code", "Response Time (ms)", "Message", "Data"})

	for i, item := range logs {
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
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": false,
			"status":  "disabled",
			"message": "Firebase client not initialized",
		})
		return
	}

	cfg := fb.GetConfig()
	if !cfg.Enabled || fb.CleanDatabaseURL() == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":      false,
			"status":       "disabled",
			"database_url": cfg.DatabaseURL,
			"message":      "Firebase is disabled or Database URL is empty",
		})
		return
	}

	// Test real-time read from Firebase
	logs, err := fb.FetchLogsFromFirebase()
	if err != nil {
		isAuthErr := strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403")
		statusStr := "error"
		if isAuthErr {
			statusStr = "permission_denied"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":      true,
			"status":       statusStr,
			"database_url": fb.CleanDatabaseURL(),
			"collection":   cfg.Collection,
			"error":        err.Error(),
			"message":      err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":      true,
		"status":       "connected",
		"database_url": fb.CleanDatabaseURL(),
		"collection":   cfg.Collection,
		"total_logs":   len(logs),
		"message":      "Connected to Firebase Realtime Database in real-time",
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

	count, err := h.storage.PurgeOldLogs(days)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"purged_count": count,
		"days":         days,
		"message":      fmt.Sprintf("Successfully purged %d log(s) older than %d day(s)", count, days),
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

