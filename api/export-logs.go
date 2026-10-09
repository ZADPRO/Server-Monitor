package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// ExportLogsHandler handles /api/export-logs on Vercel
func ExportLogsHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
