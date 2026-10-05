package monitor

import (
	"log"
	"sync"
	"time"

	"serverMonitoring/config"
	"serverMonitoring/models"
	"serverMonitoring/notifier"
	"serverMonitoring/storage"
)

type Scheduler struct {
	cfgManager *config.ConfigManager
	storage    storage.Storage
	notifier   *notifier.EmailNotifier
	ticker     *time.Ticker
	stopChan   chan struct{}
	runningMu  sync.Mutex
	isChecking bool
}

var (
	schedInstance *Scheduler
	schedOnce     sync.Once
)

// InitScheduler initializes the singleton scheduler
func InitScheduler(cfgMgr *config.ConfigManager, store storage.Storage) *Scheduler {
	schedOnce.Do(func() {
		schedInstance = &Scheduler{
			cfgManager: cfgMgr,
			storage:    store,
			notifier:   notifier.GetNotifier(),
			stopChan:   make(chan struct{}),
		}
	})
	return schedInstance
}

// GetScheduler returns the existing scheduler instance
func GetScheduler() *Scheduler {
	return schedInstance
}

// Start begins periodic monitoring
func (s *Scheduler) Start() {
	cfg := s.cfgManager.Get()
	interval := time.Duration(cfg.Monitoring.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	log.Printf("[SCHEDULER] Starting monitoring scheduler with interval: %v", interval)

	// Run immediate initial check in background
	go func() {
		time.Sleep(1 * time.Second)
		s.RunChecksNow()
	}()

	s.ticker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-s.ticker.C:
				log.Printf("[SCHEDULER] Triggering scheduled health check cycle...")
				s.RunChecksNow()
			case <-s.stopChan:
				log.Printf("[SCHEDULER] Monitoring scheduler stopped.")
				return
			}
		}
	}()
}

// Stop stops periodic monitoring
func (s *Scheduler) Stop() {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.stopChan)
}

// RunChecksNow triggers an immediate health check across all enabled targets
func (s *Scheduler) RunChecksNow() []models.HealthCheckResult {
	s.runningMu.Lock()
	if s.isChecking {
		s.runningMu.Unlock()
		log.Printf("[SCHEDULER] Health check cycle already in progress, skipping duplicate.")
		return nil
	}
	s.isChecking = true
	s.runningMu.Unlock()

	defer func() {
		s.runningMu.Lock()
		s.isChecking = false
		s.runningMu.Unlock()
	}()

	cfg := s.cfgManager.Get()
	targets := s.cfgManager.GetTargets()
	timeout := time.Duration(cfg.Monitoring.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	var wg sync.WaitGroup
	resultsChan := make(chan models.HealthCheckResult, len(targets))

	for _, target := range targets {
		if !target.Enabled {
			continue
		}

		wg.Add(1)
		go func(t models.Target) {
			defer wg.Done()
			var res models.HealthCheckResult
			if t.Type == "backend" {
				res = CheckBackend(t, timeout)
			} else {
				res = CheckFrontend(t, timeout)
			}

			// Store result in storage (which also handles Firebase DB push)
			if err := s.storage.SaveLog(res); err != nil {
				log.Printf("[STORAGE ERROR] Failed to save log for %s: %v", t.Name, err)
			}

			// If status is false (failed), send alert email
			if !res.Status {
				log.Printf("[ALERT] Target %s (%s) is DOWN! Error: %s", t.Name, t.URL, res.ErrorDetail)
				go func(alertRes models.HealthCheckResult) {
					if err := s.notifier.SendFailureAlert(cfg.Email, alertRes); err != nil {
						log.Printf("[EMAIL ALERT ERROR] Failed sending alert for %s: %v", alertRes.ServiceName, err)
					}
				}(res)
			} else {
				log.Printf("[HEALTHY] Target %s (%s) is OK (%dms)", t.Name, t.URL, res.ResponseTimeMs)
			}

			resultsChan <- res
		}(target)
	}

	wg.Wait()
	close(resultsChan)

	var results []models.HealthCheckResult
	for res := range resultsChan {
		results = append(results, res)
	}

	return results
}
