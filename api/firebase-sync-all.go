package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// FirebaseSyncAllHandler handles /api/firebase-sync-all on Vercel
func FirebaseSyncAllHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
