package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// LogsHandler handles /api/logs on Vercel
func LogsHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
