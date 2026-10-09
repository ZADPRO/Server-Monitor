package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// LoginHandler handles /api/auth/login on Vercel
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
