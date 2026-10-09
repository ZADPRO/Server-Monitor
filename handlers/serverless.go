package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"serverMonitoring/config"
	"serverMonitoring/monitor"
	"serverMonitoring/storage"
)

var (
	serverlessOnce sync.Once
	serverlessMux  *http.ServeMux
)

// GetServerlessMux initializes the shared API handler and mux for Vercel Serverless Functions
func GetServerlessMux() *http.ServeMux {
	serverlessOnce.Do(func() {
		dataDir := filepath.Join(os.TempDir(), "server-monitor-data")
		_ = os.MkdirAll(dataDir, 0755)

		logFilePath := filepath.Join(dataDir, "app.log")
		if logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666); err == nil {
			multiWriter := io.MultiWriter(os.Stdout, logFile)
			log.SetOutput(multiWriter)
		}

		cfgMgr, _ := config.InitConfig("config.json")
		cfg := cfgMgr.Get()

		fbClient := storage.NewFirebaseClient(cfg.Firebase)
		cfgMgr.SetFirebaseClient(fbClient)

		store, _ := storage.NewLocalStorage(filepath.Join(dataDir, "logs.json"), 5000, fbClient)
		scheduler := monitor.InitScheduler(cfgMgr, store)

		apiHandler := NewAPIHandler(cfgMgr, store, scheduler)
		serverlessMux = http.NewServeMux()

		// Register routes with /api prefix
		apiHandler.RegisterRoutes(serverlessMux)

		// Also register routes without /api prefix for maximum Vercel compatibility
		registerRawRoutes(serverlessMux, apiHandler)
	})
	return serverlessMux
}

func registerRawRoutes(mux *http.ServeMux, h *APIHandler) {
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

	handleWithCORS("/auth/login", h.handleLogin)
	handleWithCORS("/status", h.handleStatus)
	handleWithCORS("/logs", h.handleLogs)
	handleWithCORS("/console-logs", h.handleConsoleLogs)
	handleWithCORS("/check-now", h.handleCheckNow)
	handleWithCORS("/targets", h.handleTargets)
	handleWithCORS("/targets/", h.handleTargetByID)
	handleWithCORS("/users", h.handleUsers)
	handleWithCORS("/users/", h.handleUserByID)
	handleWithCORS("/config", h.handleConfig)
	handleWithCORS("/test-email", h.handleTestEmail)
	handleWithCORS("/export-logs", h.handleExportLogs)
	handleWithCORS("/firebase-status", h.handleFirebaseStatus)
	handleWithCORS("/firebase-sync", h.handleFirebaseSyncAll)
	handleWithCORS("/firebase-sync-all", h.handleFirebaseSyncAll)
	handleWithCORS("/purge-logs", h.handlePurgeLogs)
}

// ServeServerless processes incoming Vercel Serverless Function requests
func ServeServerless(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[SERVERLESS PANIC RECOVERED] %v", rec)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Serverless execution error: %v", rec),
			})
		}
	}()

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Resolve request path if forwarded by Vercel
	reqPath := r.URL.Path
	if fwdURI := r.Header.Get("x-forwarded-uri"); fwdURI != "" {
		if u, err := url.Parse(fwdURI); err == nil && u.Path != "" {
			reqPath = u.Path
		}
	} else if r.RequestURI != "" {
		if u, err := url.Parse(r.RequestURI); err == nil && u.Path != "" {
			reqPath = u.Path
		}
	}

	if reqPath != "" && strings.HasPrefix(reqPath, "/api") {
		r.URL.Path = reqPath
	}

	mux := GetServerlessMux()
	mux.ServeHTTP(w, r)
}
