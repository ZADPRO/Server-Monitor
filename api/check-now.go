package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// CheckNowHandler handles /api/check-now on Vercel
func CheckNowHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
