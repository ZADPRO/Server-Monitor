package monitor

import (
	"fmt"
	"log"
	"sync"
	"time"

	"serverMonitoring/config"
	"serverMonitoring/models"
	"serverMonitoring/notifier"
	"serverMonitoring/storage"
)

type Scheduler struct {
	cfgManager     *config.ConfigManager
	storage        storage.Storage
	notifier       *notifier.EmailNotifier
	ticker         *time.Ticker
	stopChan       chan struct{}
	runningMu      sync.Mutex
	isChecking     bool
	lastCheckedMap map[string]time.Time
	lastCheckMu    sync.Mutex
}

var (
	schedInstance *Scheduler
	schedOnce     sync.Once
)

// InitScheduler initializes the singleton scheduler
func InitScheduler(cfgMgr *config.ConfigManager, store storage.Storage) *Scheduler {
	schedOnce.Do(func() {
		schedInstance = &Scheduler{
			cfgManager:     cfgMgr,
			storage:        store,
			notifier:       notifier.GetNotifier(),
			stopChan:       make(chan struct{}),
			lastCheckedMap: make(map[string]time.Time),
		}
	})
	return schedInstance
}

// GetScheduler returns the existing scheduler instance
func GetScheduler() *Scheduler {
	return schedInstance
}

// Start begins periodic monitoring ticker (runs strictly every 5 minutes for all targets)
func (s *Scheduler) Start() {
	log.Printf("[SCHEDULER] Starting monitoring scheduler (strictly every 5 mins)...")

	// Run immediate initial check in background
	go func() {
		time.Sleep(1 * time.Second)
		s.RunChecksNow()
	}()

	s.ticker = time.NewTicker(5 * time.Minute)

	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.checkDueTargets()
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

func (s *Scheduler) getTargets() []models.Target {
	fb := s.storage.GetFirebaseClient()
	if fb != nil {
		targets, err := fb.FetchTargets()
		if err == nil && targets != nil {
			return targets
		}
	}
	return []models.Target{}
}

// checkDueTargets checks all enabled targets strictly every 5 minutes
func (s *Scheduler) checkDueTargets() {
	targets := s.getTargets()
	now := time.Now()

	s.lastCheckMu.Lock()
	var dueTargets []models.Target
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		s.lastCheckedMap[target.ID] = now
		dueTargets = append(dueTargets, target)
	}
	s.lastCheckMu.Unlock()

	if len(dueTargets) == 0 {
		return
	}

	log.Printf("[SCHEDULER] Executing 5-minute health check cycle for %d enabled target(s)...", len(dueTargets))
	s.executeTargets(dueTargets)

	// Check if auto deletion is enabled and purge old logs
	cfg := s.cfgManager.Get()
	if cfg.Firebase.AutoDeleteEnabled && cfg.Firebase.AutoDeleteDays > 0 {
		fb := s.storage.GetFirebaseClient()
		if fb != nil {
			_, _ = fb.PurgeOldLogsFromFirebase(cfg.Firebase.AutoDeleteDays)
		}
		_, _ = s.storage.PurgeOldLogs(cfg.Firebase.AutoDeleteDays)
	}
}

// RunChecksNow triggers an immediate health check across ALL enabled targets
func (s *Scheduler) RunChecksNow() []models.HealthCheckResult {
	targets := s.getTargets()
	var enabledTargets []models.Target
	now := time.Now()

	s.lastCheckMu.Lock()
	for _, t := range targets {
		if t.Enabled {
			s.lastCheckedMap[t.ID] = now
			enabledTargets = append(enabledTargets, t)
		}
	}
	s.lastCheckMu.Unlock()

	return s.executeTargets(enabledTargets)
}

// RunCheckForSingleTarget triggers an immediate health check for a single target by ID
func (s *Scheduler) RunCheckForSingleTarget(targetID string) (*models.HealthCheckResult, error) {
	targets := s.getTargets()
	var foundTarget *models.Target
	for _, t := range targets {
		if t.ID == targetID {
			foundTarget = &t
			break
		}
	}

	if foundTarget == nil {
		return nil, fmt.Errorf("target with ID '%s' not found", targetID)
	}

	s.lastCheckMu.Lock()
	s.lastCheckedMap[foundTarget.ID] = time.Now()
	s.lastCheckMu.Unlock()

	cfg := s.cfgManager.Get()
	timeout := time.Duration(cfg.Monitoring.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	var res models.HealthCheckResult
	if foundTarget.Type == "backend" {
		res = CheckBackend(*foundTarget, timeout)
	} else {
		res = CheckFrontend(*foundTarget, timeout)
	}

	if err := s.storage.SaveLog(res); err != nil {
		log.Printf("[STORAGE ERROR] Failed to save log for %s: %v", foundTarget.Name, err)
	}

	if !res.Status {
		log.Printf("[ALERT] Target %s (%s) is DOWN! Failure recipient(s): %v. Error: %s", foundTarget.Name, foundTarget.URL, foundTarget.RecipientEmails, res.ErrorDetail)
		go func(alertRes models.HealthCheckResult, recs []string) {
			if err := s.notifier.SendFailureAlert(cfg.Email, recs, alertRes); err != nil {
				log.Printf("[EMAIL ALERT ERROR] Failed sending alert for %s: %v", alertRes.ServiceName, err)
			}
		}(res, foundTarget.RecipientEmails)
	} else {
		log.Printf("[HEALTHY] Target %s (%s) is OK (%dms)", foundTarget.Name, foundTarget.URL, res.ResponseTimeMs)
	}

	return &res, nil
}

func (s *Scheduler) executeTargets(targets []models.Target) []models.HealthCheckResult {
	if len(targets) == 0 {
		return nil
	}

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
	timeout := time.Duration(cfg.Monitoring.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	var wg sync.WaitGroup
	resultsChan := make(chan models.HealthCheckResult, len(targets))

	for _, target := range targets {
		wg.Add(1)
		go func(t models.Target) {
			defer wg.Done()
			var res models.HealthCheckResult
			if t.Type == "backend" {
				res = CheckBackend(t, timeout)
			} else {
				res = CheckFrontend(t, timeout)
			}

			if err := s.storage.SaveLog(res); err != nil {
				log.Printf("[STORAGE ERROR] Failed to save log for %s: %v", t.Name, err)
			}

			if !res.Status {
				log.Printf("[ALERT] Target %s (%s) is DOWN! Failure recipient(s): %v. Error: %s", t.Name, t.URL, t.RecipientEmails, res.ErrorDetail)
				go func(alertRes models.HealthCheckResult, recs []string) {
					if err := s.notifier.SendFailureAlert(cfg.Email, recs, alertRes); err != nil {
						log.Printf("[EMAIL ALERT ERROR] Failed sending alert for %s: %v", alertRes.ServiceName, err)
					}
				}(res, t.RecipientEmails)
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

