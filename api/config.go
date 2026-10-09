package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// ConfigHandler handles /api/config on Vercel
func ConfigHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
