package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// UsersHandler handles /api/users on Vercel
func UsersHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
