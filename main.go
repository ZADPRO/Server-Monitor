package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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

	// Ensure data directory exists (with fallback for read-only serverless environments like Vercel)
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		dataDir = filepath.Join(os.TempDir(), "server-monitor-data")
		_ = os.MkdirAll(dataDir, 0755)
	}

	// Set up continuous log file for console logs / printfs
	logFilePath := filepath.Join(dataDir, "app.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		// If read-only filesystem, try opening in /tmp
		dataDir = filepath.Join(os.TempDir(), "server-monitor-data")
		_ = os.MkdirAll(dataDir, 0755)
		logFilePath = filepath.Join(dataDir, "app.log")
		logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	}

	if err == nil {
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
		defer logFile.Close()
	}

	log.Println("==========================================================")
	log.Println("🚀 Starting Zadroit Server Monitoring System...")
	log.Printf("📝 Live Console & Execution logs saving to: %s", logFilePath)
	log.Println("==========================================================")

	// 1. Initialize configuration manager
	cfgMgr, _ := config.InitConfig(*configPath)
	cfg := cfgMgr.Get()
	log.Printf("[CONFIG] Active targets: %d, Check interval: %d min(s)", len(cfg.Targets), cfg.Monitoring.IntervalMinutes)

	// 2. Initialize Firebase & Local storage
	fbClient := storage.NewFirebaseClient(cfg.Firebase)
	cfgMgr.SetFirebaseClient(fbClient)

	store, err := storage.NewLocalStorage(filepath.Join(dataDir, "logs.json"), 5000, fbClient)
	if err != nil {
		tmpDataDir := filepath.Join(os.TempDir(), "server-monitor-data")
		_ = os.MkdirAll(tmpDataDir, 0755)
		store, _ = storage.NewLocalStorage(filepath.Join(tmpDataDir, "logs.json"), 5000, fbClient)
	}
	defer store.Close()

	if cfg.Firebase.Enabled && cfg.Firebase.DatabaseURL != "" {
		log.Printf("[FIREBASE] Connected to Firebase DB endpoint: %s", cfg.Firebase.DatabaseURL)
		// Perform initial bidirectional sync of Targets, Users, and Email settings
		go func() {
			time.Sleep(500 * time.Millisecond)
			if err := cfgMgr.SyncWithFirebase(); err != nil {
				log.Printf("[FIREBASE SYNC NOTICE] Initial sync completed with notice: %v", err)
			}
		}()
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
		// Avoid caching index.html, JS, and CSS for instant dev updates
		ext := filepath.Ext(r.URL.Path)
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || ext == ".js" || ext == ".css" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		fileServer.ServeHTTP(w, r)
	}))

	// Determine port (check PORT environment variable for Vercel / Cloud deployments)
	port := cfg.Server.Port
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			port = p
		}
	}

	host := cfg.Server.Host
	if os.Getenv("PORT") != "" || host == "" {
		host = "0.0.0.0"
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  180 * time.Second,
	}

	// 5. Start HTTP Server
	go func() {
		log.Printf("[HTTP] Zadroit Server Monitor Dashboard listening on %s (Port: %d)", addr, port)
		log.Printf("[AUTH] Default Login: Username: %s | Password: [PROTECTED]", cfg.Auth.Username)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[HTTP ERROR] Server error: %v", err)
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
