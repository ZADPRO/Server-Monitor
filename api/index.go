package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	// Console & application log file
	logFilePath := filepath.Join(dataDir, "app.log")
	if logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666); err == nil {
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
	}

	// Locate config.json
	configPath := "config.json"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(dataDir, "config.json")
	}

	cfgMgr, err := config.InitConfig(configPath)
	if err != nil {
		log.Printf("[VERCEL WARN] Config manager init error: %v", err)
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
	initOnce.Do(initVercelApp)

	if apiMux != nil {
		apiMux.ServeHTTP(w, r)
		return
	}

	http.Error(w, fmt.Sprintf("API Handler failed to initialize"), http.StatusInternalServerError)
}
