package models

import "time"

// Target represents a monitored endpoint
type Target struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"` // "backend" or "frontend"
	URL          string   `json:"url"`
	Method       string   `json:"method,omitempty"`
	Enabled      bool     `json:"enabled"`
	ExpectedKeys []string `json:"expected_keys,omitempty"`
}

// HealthCheckResult represents a single check outcome
type HealthCheckResult struct {
	ID             string                 `json:"id"`
	SNo            int                    `json:"s_no,omitempty"`
	ServiceName    string                 `json:"service_name"`
	Type           string                 `json:"type"`
	URL            string                 `json:"url"`
	Status         bool                   `json:"status"`
	HitTime        string                 `json:"hit_time"`
	Timestamp      int64                  `json:"timestamp"`
	Message        string                 `json:"message"`
	Data           map[string]interface{} `json:"data"`
	ResponseTimeMs int64                  `json:"response_time_ms"`
	HTTPStatus     int                    `json:"http_status"`
	ErrorDetail    string                 `json:"error_detail,omitempty"`
	TargetID       string                 `json:"target_id"`
}

// LogFilter defines filtering criteria for query logs
type LogFilter struct {
	ServiceName string `json:"service_name"`
	Type        string `json:"type"`
	Status      string `json:"status"` // "all", "success", "failure"
	FromDate    string `json:"from_date"`
	ToDate      string `json:"to_date"`
	Search      string `json:"search"`
	Page        int    `json:"page"`
	Limit       int    `json:"limit"`
}

// ServerSummary represents high-level metrics for the dashboard
type ServerSummary struct {
	TotalTargets   int                            `json:"total_targets"`
	OnlineTargets  int                            `json:"online_targets"`
	OfflineTargets int                            `json:"offline_targets"`
	TotalChecks    int                            `json:"total_checks"`
	PassedChecks   int                            `json:"passed_checks"`
	FailedChecks   int                            `json:"failed_checks"`
	UptimePercent  float64                        `json:"uptime_percent"`
	LastCheckTime  string                         `json:"last_check_time"`
	NextCheckTime  string                         `json:"next_check_time"`
	TargetStatuses map[string]HealthCheckResult   `json:"target_statuses"`
	IntervalMins   int                            `json:"interval_minutes"`
}

// ServerConfig configuration for HTTP server
type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

// AuthConfig configuration for dashboard login
type AuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// User represents a system user stored in Firebase / Local auth
type User struct {
	Username  string `json:"username"`
	Password  string `json:"password,omitempty"`
	Role      string `json:"role,omitempty"`
	Email     string `json:"email,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// FirebaseSyncStatus represents sync status across all entities
type FirebaseSyncStatus struct {
	Enabled       bool   `json:"enabled"`
	Status        string `json:"status"`
	DatabaseURL   string `json:"database_url"`
	LogsCount     int    `json:"logs_count"`
	TargetsCount  int    `json:"targets_count"`
	UsersCount    int    `json:"users_count"`
	EmailSynced   bool   `json:"email_synced"`
	Message       string `json:"message"`
	LastSyncedAt  string `json:"last_synced_at,omitempty"`
}

// MonitoringConfig configuration for check loop
type MonitoringConfig struct {
	IntervalMinutes       int `json:"interval_minutes"`
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`
}

// EmailConfig configuration for Gmail SMTP alerts
type EmailConfig struct {
	Enabled     bool     `json:"enabled"`
	SMTPHost    string   `json:"smtp_host"`
	SMTPPort    int      `json:"smtp_port"`
	FromEmail   string   `json:"from_email"`
	AppPassword string   `json:"app_password"`
	ToEmails    []string `json:"to_emails"`
}

// FirebaseConfig configuration for Firebase storage
type FirebaseConfig struct {
	Enabled           bool   `json:"enabled"`
	Type              string `json:"type"` // "realtime" or "firestore"
	APIKey            string `json:"api_key,omitempty"`
	AuthDomain        string `json:"auth_domain,omitempty"`
	DatabaseURL       string `json:"database_url"`
	ProjectID         string `json:"project_id,omitempty"`
	StorageBucket     string `json:"storage_bucket,omitempty"`
	MessagingSenderID string `json:"messaging_sender_id,omitempty"`
	AppID             string `json:"app_id,omitempty"`
	MeasurementID     string `json:"measurement_id,omitempty"`
	AuthSecret        string `json:"auth_secret,omitempty"`
	Collection        string `json:"collection"`
}

// Config represents root config.json
type Config struct {
	Server     ServerConfig     `json:"server"`
	Auth       AuthConfig       `json:"auth"`
	Monitoring MonitoringConfig `json:"monitoring"`
	Email      EmailConfig      `json:"email"`
	Firebase   FirebaseConfig   `json:"firebase"`
	Targets    []Target         `json:"targets"`
}

// BackendResponseFormat matches the backend API response schema
type BackendResponseFormat struct {
	Status  bool                   `json:"status"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

// TimeFormat standard format for display
const (
	TimeFormat = "2006-01-02 15:04:05"
	DateFormat = "2006-01-02"
)

// NowFormatted returns current timestamp in standard format
func NowFormatted() string {
	return time.Now().Format(TimeFormat)
}
