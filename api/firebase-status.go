package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// FirebaseStatusHandler handles /api/firebase-status on Vercel
func FirebaseStatusHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
