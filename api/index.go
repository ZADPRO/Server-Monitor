package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// Handler handles /api root and general fallback
func Handler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
