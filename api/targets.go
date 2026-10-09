package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// TargetsHandler handles /api/targets on Vercel
func TargetsHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
