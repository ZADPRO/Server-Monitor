package handler

import (
	"net/http"
	"serverMonitoring/handlers"
)

// ConsoleLogsHandler handles /api/console-logs on Vercel
func ConsoleLogsHandler(w http.ResponseWriter, r *http.Request) {
	handlers.ServeServerless(w, r)
}
