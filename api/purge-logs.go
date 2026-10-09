package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// PurgeLogsHandler handles /api/purge-logs on Vercel
func PurgeLogsHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
