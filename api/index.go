package handler

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
	"serverMonitoring/handlers"
	"serverMonitoring/monitor"
	"serverMonitoring/storage"
)

var (
	initOnce sync.Once
	apiMux   *http.ServeMux
)

func initVercelApp() {
	// Writable data directory (/tmp on Vercel serverless environment)
	dataDir := filepath.Join(os.TempDir(), "server-monitor-data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		dataDir = os.TempDir()
	}

	// Console & application log file in /tmp
	logFilePath := filepath.Join(dataDir, "app.log")
	if logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666); err == nil {
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
	}

	// Locate config.json
	configPath := "config.json"
	cfgMgr, err := config.InitConfig(configPath)
	if err != nil {
		log.Printf("[VERCEL WARN] Config manager initialized with default fallback: %v", err)
	}

	cfg := cfgMgr.Get()

	// Initialize Firebase & Local storage
	fbClient := storage.NewFirebaseClient(cfg.Firebase)
	store, err := storage.NewLocalStorage(filepath.Join(dataDir, "logs.json"), 5000, fbClient)
	if err != nil {
		log.Printf("[VERCEL WARN] Local storage init error: %v", err)
	}

	// Initialize Scheduler
	scheduler := monitor.InitScheduler(cfgMgr, store)

	// Register API Routes
	apiHandler := handlers.NewAPIHandler(cfgMgr, store, scheduler)
	apiMux = http.NewServeMux()
	apiHandler.RegisterRoutes(apiMux)

	log.Printf("[VERCEL API] Serverless Function initialized successfully")
}

// Handler is the entrypoint for Vercel Go Serverless Functions
func Handler(w http.ResponseWriter, r *http.Request) {
	// Add global panic recovery to prevent generic Vercel 500 crashes
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[VERCEL PANIC RECOVERED] %v", rec)
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

	initOnce.Do(initVercelApp)

	// Always set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if apiMux != nil {
		// Resolve target API path on Vercel
		reqPath := r.URL.Path

		// Check x-forwarded-uri or RequestURI if available
		if fwdURI := r.Header.Get("x-forwarded-uri"); fwdURI != "" {
			if u, err := url.Parse(fwdURI); err == nil && u.Path != "" {
				reqPath = u.Path
			}
		} else if r.RequestURI != "" {
			if u, err := url.Parse(r.RequestURI); err == nil && u.Path != "" {
				reqPath = u.Path
			}
		}

		// Ensure request URL path matches registered handler routes (e.g. /api/status, /api/check-now)
		if reqPath != "" && strings.HasPrefix(reqPath, "/api") {
			r.URL.Path = reqPath
		}

		apiMux.ServeHTTP(w, r)
		return
	}

	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   "API Handler failed to initialize",
	})
}
