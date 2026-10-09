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
	handleWithCORS("/api/users", h.handleUsers)
	handleWithCORS("/api/status", h.handleStatus)
	handleWithCORS("/api/logs", h.handleLogs)
	handleWithCORS("/api/console-logs", h.handleConsoleLogs)
	handleWithCORS("/api/check-now", h.handleCheckNow)
	handleWithCORS("/api/targets", h.handleTargets)
	handleWithCORS("/api/targets/", h.handleTargetByID)
	handleWithCORS("/api/config", h.handleConfig)
	handleWithCORS("/api/test-email", h.handleTestEmail)
	handleWithCORS("/api/export-logs", h.handleExportLogs)
	handleWithCORS("/api/firebase-status", h.handleFirebaseStatus)
	handleWithCORS("/api/firebase-logs", h.handleFirebaseLogs)
	handleWithCORS("/api/firebase-sync", h.handleFirebaseSync)
	handleWithCORS("/api/firebase-sync-all", h.handleFirebaseSyncAll)
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

func (h *APIHandler) handleUsers(w http.ResponseWriter, r *http.Request) {
	fb := h.storage.GetFirebaseClient()

	switch r.Method {
	case http.MethodGet:
		var users []models.User
		if fb != nil {
			if fbUsers, err := fb.FetchUsers(); err == nil && len(fbUsers) > 0 {
				users = fbUsers
			}
		}

		if len(users) == 0 {
			cfg := h.cfgManager.Get()
			users = []models.User{
				{
					Username:  cfg.Auth.Username,
					Role:      "admin",
					UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
				},
			}
		}

		// Mask passwords in output
		safeUsers := make([]models.User, len(users))
		for i, u := range users {
			safeUsers[i] = models.User{
				Username:  u.Username,
				Password:  "••••••••",
				Role:      u.Role,
				Email:     u.Email,
				UpdatedAt: u.UpdatedAt,
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"users":   safeUsers,
		})

	case http.MethodPost:
		var newUser models.User
		if err := json.NewDecoder(r.Body).Decode(&newUser); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid user JSON"})
			return
		}

		if newUser.Username == "" || newUser.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Username and password are required"})
			return
		}

		if newUser.Role == "" {
			newUser.Role = "admin"
		}
		newUser.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")

		// Update in ConfigManager
		cfg := h.cfgManager.Get()
		cfg.Auth.Username = newUser.Username
		cfg.Auth.Password = newUser.Password
		_ = h.cfgManager.Save(cfg)

		// Sync directly to Firebase
		if fb != nil {
			_ = fb.SaveUser(newUser)
			_ = fb.SaveAuthConfig(models.AuthConfig{Username: newUser.Username, Password: newUser.Password})
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("User '%s' saved to Firebase and local storage", newUser.Username),
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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

		if err := h.cfgManager.AddTarget(target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"success": true,
			"target":  target,
			"message": "Target saved locally and synced to Firebase",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleTargetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/targets/")
	if id == "" {
		http.Error(w, "Missing target ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var target models.Target
		if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid target JSON"})
			return
		}
		target.ID = id

		if err := h.cfgManager.UpdateTarget(target); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"target":  target,
			"message": "Target updated locally and synced to Firebase",
		})

	case http.MethodDelete:
		if err := h.cfgManager.DeleteTarget(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Target deleted locally and removed from Firebase",
		})

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
		LastSyncedAt: time.Now().Format("2006-01-02 15:04:05"),
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

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
