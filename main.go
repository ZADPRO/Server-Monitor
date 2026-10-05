package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"serverMonitoring/config"
	"serverMonitoring/handlers"
	"serverMonitoring/monitor"
	"serverMonitoring/storage"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to config.json")
	flag.Parse()

	log.Println("==========================================================")
	log.Println("🚀 Starting Zadroit Server Monitoring System...")
	log.Println("==========================================================")

	// 1. Initialize configuration manager
	cfgMgr, err := config.InitConfig(*configPath)
	if err != nil {
		log.Fatalf("[FATAL] Could not load config from %s: %v", *configPath, err)
	}
	cfg := cfgMgr.Get()
	log.Printf("[CONFIG] Loaded successfully. Monitored targets: %d, Check interval: %d min(s)", len(cfg.Targets), cfg.Monitoring.IntervalMinutes)

	// 2. Initialize Firebase & Local storage
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("[FATAL] Failed to create data directory: %v", err)
	}

	fbClient := storage.NewFirebaseClient(cfg.Firebase)
	store, err := storage.NewLocalStorage(filepath.Join(dataDir, "logs.json"), 5000, fbClient)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize storage: %v", err)
	}
	defer store.Close()

	if cfg.Firebase.Enabled && cfg.Firebase.DatabaseURL != "" {
		log.Printf("[FIREBASE] Connected to Firebase DB endpoint: %s (Collection: %s)", cfg.Firebase.DatabaseURL, cfg.Firebase.Collection)
	} else {
		log.Printf("[FIREBASE] Firebase DB integration disabled or URL empty. Logs stored locally.")
	}

	// 3. Initialize & Start 5-min Monitoring Scheduler
	scheduler := monitor.InitScheduler(cfgMgr, store)
	scheduler.Start()
	defer scheduler.Stop()

	// 4. Initialize HTTP API & Static Web Server
	apiHandler := handlers.NewAPIHandler(cfgMgr, store, scheduler)

	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	// Static web UI files (served from public/ directory)
	webDir := "public"
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}
	fileServer := http.FileServer(http.Dir(webDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Avoid caching index.html for fast development and updates
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		fileServer.ServeHTTP(w, r)
	}))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 5. Start HTTP Server in background
	go func() {
		log.Printf("[HTTP] Zadroit Server Monitor Dashboard running at: http://localhost:%d", cfg.Server.Port)
		log.Printf("[AUTH] Default Login: Username: %s | Password: [PROTECTED]", cfg.Auth.Username)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] HTTP server error: %v", err)
		}
	}()

	// 6. Handle graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("\n[SYSTEM] Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[ERROR] Server forced to shutdown: %v", err)
	}

	log.Println("[SYSTEM] Server exited safely.")
}
