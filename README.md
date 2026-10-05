# 🛡️ Zadroit Server Health & API Monitoring System

A high-performance, real-time server and API monitoring watchdog built with **Golang**, **Firebase Database**, **Gmail SMTP Alerts**, and a **Modern Web Dashboard**.

---

## 📋 Features

- ⏱️ **Automated 5-Minute Watchdog**: Periodic health check probes run automatically every 5 minutes across all configured targets.
- 🔍 **Backend API Validation**:
  - Hits API endpoints (e.g., `https://nivas-appsearching-algorithm.brightoncloudtech.com/app-search-algorithm/checkserver`).
  - Validates top-level `status == true` and validates all keys inside `data` (e.g. `service == true`, `db == true`).
  - If any sub-service check is `false` or server returns an HTTP error (e.g., `502 Bad Gateway`), triggers an immediate failure alert.
- 🌐 **Frontend Website & Asset Inspection**:
  - Hits frontend web applications (e.g., `https://nivashoc.com/`).
  - Verifies HTTP 200 status and scans HTML for error indicators and broken script/CSS bundles to avoid console errors.
- 🚨 **Instant Email Alerting**:
  - Sends styled HTML alert emails via Gmail SMTP (`smtp.gmail.com:587`).
  - Default Sender: `development.zadroit@gmail.com`
  - Default Recipient: `indumathi@zadroit.com`
  - App Password configured securely.
- 🔥 **Firebase Database Real-time Sync**:
  - Automatically records every check outcome into Firebase Realtime Database / Firestore.
  - Data structure:
    - `service_name`: (e.g., "Nivas App Search Algorithm")
    - `status`: (`true` for Success / `false` for Failure)
    - `hit_time`: (Formatted timestamp e.g. `2026-10-05 11:45:00`)
    - `message`: (Status message or error summary)
    - `data`: (Parsed JSON payload e.g. `{"service": true, "db": true}`)
    - `type`, `url`, `http_status`, `response_time_ms`.
- 📊 **Modern Web Dashboard**:
  - Static Login with credentials:
    - **Username**: `Zadroit`
    - **Password**: `ZadGugSlm06`
  - Live Overview Metrics: Monitored Services, Online vs Offline, Uptime %, Total Checks.
  - Live Status Cards with color-coded UP / DOWN indicators and latency.
  - Interactive Logs Data Table:
    - Columns: `S.No | Service Name | Type | URL | Status (Green/Red) | Hit Time | Message | Data | Inspect`
    - Dynamic Filters: Service Name, Type (Backend/Frontend), Status (Success/Failure), Date Range (From Date, To Date, Quick Presets), Real-time Search.
    - One-Click Manual **"Check Now"** button.
    - Live 5-minute countdown timer.
    - Target Management Modal (Add/Edit/Delete targets directly in JSON).
    - CSV Report Export.

---

## 🚀 Quick Start

### 1. Run the Server
From the project root directory:
```bash
# Build and run
go run main.go

# Or build binary
go build -o serverMonitoring.exe .
.\serverMonitoring.exe
```

The web dashboard is available at: **[http://localhost:8080](http://localhost:8080)**

---

## 🔐 Login Credentials

| Field | Value |
|---|---|
| **Username** | `Zadroit` |
| **Password** | `ZadGugSlm06` |

*(Can also be customized in `config.json` under `"auth"`)*

---

## ⚙️ Configuration (`config.json`)

All configuration is centralized in `config.json` and can be edited anytime:

```json
{
  "server": {
    "port": 8080,
    "host": "0.0.0.0"
  },
  "auth": {
    "username": "Zadroit",
    "password": "ZadGugSlm06"
  },
  "monitoring": {
    "interval_minutes": 5,
    "request_timeout_seconds": 15
  },
  "email": {
    "enabled": true,
    "smtp_host": "smtp.gmail.com",
    "smtp_port": 587,
    "from_email": "development.zadroit@gmail.com",
    "app_password": "bfit dhmb hcxt jrvg",
    "to_emails": [
      "indumathi@zadroit.com"
    ]
  },
  "firebase": {
    "enabled": true,
    "type": "realtime",
    "database_url": "https://<your-project-id>-default-rtdb.firebaseio.com",
    "auth_secret": "",
    "collection": "server_monitoring_logs"
  },
  "targets": [
    {
      "id": "target_backend_1",
      "name": "Nivas App Search Algorithm",
      "type": "backend",
      "url": "https://nivasappproduct-wishlist.brightoncloudtech.com/checkserver",
      "method": "GET",
      "enabled": true,
      "expected_keys": ["service", "db"]
    },
    {
      "id": "target_frontend_1",
      "name": "Nivas HOC Website",
      "type": "frontend",
      "url": "https://nivashoc.com/",
      "method": "GET",
      "enabled": true
    }
  ]
}
```

---

## 🔥 Firebase Database Setup Guide

1. Go to the [Firebase Console](https://console.firebase.google.com/) and create or select a project.
2. Under **Build**, select **Realtime Database** (or Firestore).
3. Click **Create Database** and choose your region.
4. Set Rules to allow read/write or configure authentication:
   ```json
   {
     "rules": {
       ".read": true,
       ".write": true
     }
   }
   ```
5. Copy your Database URL (e.g. `https://my-monitoring-app-default-rtdb.firebaseio.com`).
6. Paste the URL into the **Settings Modal** on the Web Dashboard (or update `config.json` -> `"firebase"."database_url"`).

---

## 📡 REST API Endpoints

- `POST /api/auth/login`: Authenticate dashboard user
- `GET /api/status`: Overall system health and target snapshots
- `GET /api/logs`: Paginated & filtered audit logs
- `POST /api/check-now`: Trigger instant health check across all targets
- `GET /api/targets`: List all targets
- `POST /api/targets`: Add target
- `PUT /api/targets/:id`: Update target
- `DELETE /api/targets/:id`: Delete target
- `GET /api/config`: Read current configuration
- `POST /api/config`: Update configuration
- `POST /api/test-email`: Send a test email alert
- `GET /api/export-logs`: Export matching logs to CSV

---

## 📁 Directory Layout

```
serverMonitoring/
├── config.json             # Monitoring targets, email, auth & Firebase configuration
├── main.go                 # Server entry point & graceful shutdown
├── go.mod                  # Go module definition
├── config/
│   └── config.go           # Thread-safe config loader & target manager
├── models/
│   └── models.go           # Health check models, filter schemas, and config structs
├── monitor/
│   ├── backend_checker.go  # Backend API validator (status, service, db keys)
│   ├── frontend_checker.go # Frontend website & asset error checker
│   └── scheduler.go        # 5-minute automated ticker & manual triggers
├── notifier/
│   └── email.go            # Gmail SMTP HTML alert sender
├── storage/
│   ├── storage.go          # Storage interface
│   ├── firebase.go         # Firebase Realtime Database REST client
│   └── local.go            # Persistent local log store with ring buffer & filtering
├── handlers/
│   └── api.go              # HTTP REST API & static file handlers
└── web/
    ├── index.html          # Modern Single Page Application
    ├── css/
    │   └── styles.css      # Dark glassmorphic design system
    └── js/
        └── app.js          # Interactive dashboard controller
```
