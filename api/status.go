package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// StatusHandler handles /api/status on Vercel
func StatusHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
