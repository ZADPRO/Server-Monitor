package storage

import (
	"serverMonitoring/models"
)

// Storage defines the interface for persisting and retrieving health check logs
type Storage interface {
	SaveLog(result models.HealthCheckResult) error
	GetLogs(filter models.LogFilter) ([]models.HealthCheckResult, int, error)
	GetLatestStatus() map[string]models.HealthCheckResult
	GetSummary() models.ServerSummary
	GetFirebaseClient() *FirebaseClient
	SyncFromFirebase() (int, error)
	PurgeOldLogs(days int) (int, error)
	Close() error
}
