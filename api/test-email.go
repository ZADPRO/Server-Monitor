package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// TestEmailHandler handles /api/test-email on Vercel
func TestEmailHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
