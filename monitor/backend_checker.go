package monitor

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"serverMonitoring/models"
)

// CheckBackend checks a backend API endpoint and validates status and data fields
func CheckBackend(target models.Target, timeout time.Duration) models.HealthCheckResult {
	startTime := time.Now()
	nowStr := startTime.Format(models.TimeFormat)

	result := models.HealthCheckResult{
		ID:          fmt.Sprintf("%s-%d", target.ID, startTime.UnixNano()),
		TargetID:    target.ID,
		ServiceName: target.Name,
		Type:        "backend",
		URL:         target.URL,
		HitTime:     nowStr,
		Timestamp:   startTime.Unix(),
		Status:      false,
		Data:        make(map[string]interface{}),
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	method := target.Method
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequest(method, target.URL, nil)
	if err != nil {
		result.Message = "Failed to construct HTTP request"
		result.ErrorDetail = err.Error()
		result.ResponseTimeMs = time.Since(startTime).Milliseconds()
		return result
	}

	req.Header.Set("User-Agent", "Zadroit-Server-Monitor/1.0")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := client.Do(req)
	result.ResponseTimeMs = time.Since(startTime).Milliseconds()
	if err != nil {
		result.Message = "Connection failed / Timeout"
		result.ErrorDetail = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		result.Message = "Failed to read response body"
		result.ErrorDetail = err.Error()
		return result
	}

	// Check if HTTP status is not successful
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Message = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		result.ErrorDetail = fmt.Sprintf("Server returned non-200 status code: %d", resp.StatusCode)
		
		// Attempt to parse JSON error response if present
		var rawMap map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &rawMap); err == nil {
			result.Data = rawMap
		} else {
			cleanBody := strings.TrimSpace(string(bodyBytes))
			if len(cleanBody) > 200 {
				cleanBody = cleanBody[:200] + "..."
			}
			result.Data = map[string]interface{}{
				"raw_response": cleanBody,
			}
		}
		return result
	}

	// Parse JSON response
	var parsedResp struct {
		Status  interface{}            `json:"status"`
		Message interface{}            `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &parsedResp); err != nil {
		// Not JSON or schema mismatch
		result.Message = "Invalid JSON response format"
		result.ErrorDetail = fmt.Sprintf("Could not decode JSON: %s", err.Error())
		cleanBody := strings.TrimSpace(string(bodyBytes))
		if len(cleanBody) > 200 {
			cleanBody = cleanBody[:200] + "..."
		}
		result.Data = map[string]interface{}{"raw_response": cleanBody}
		return result
	}

	// Store message
	if parsedResp.Message != nil {
		result.Message = fmt.Sprintf("%v", parsedResp.Message)
	} else {
		result.Message = "OK"
	}

	// Store data map
	if parsedResp.Data != nil {
		result.Data = parsedResp.Data
	} else {
		result.Data = make(map[string]interface{})
	}

	// Validate top-level status
	statusBool, isBool := parsedResp.Status.(bool)
	if !isBool {
		// If status is string "true"/"ok" or int 1
		statusStr := strings.ToLower(fmt.Sprintf("%v", parsedResp.Status))
		statusBool = (statusStr == "true" || statusStr == "ok" || statusStr == "success" || statusStr == "1")
	}

	if !statusBool {
		result.Status = false
		result.ErrorDetail = fmt.Sprintf("Response top-level status is false (message: %s)", result.Message)
		return result
	}

	// Validate data contents: if anything is false in data, mark as failure
	var falseFields []string
	for k, v := range parsedResp.Data {
		switch val := v.(type) {
		case bool:
			if !val {
				falseFields = append(falseFields, fmt.Sprintf("%s: false", k))
			}
		case string:
			lower := strings.ToLower(strings.TrimSpace(val))
			if lower == "false" || lower == "down" || lower == "failed" || lower == "error" {
				falseFields = append(falseFields, fmt.Sprintf("%s: %s", k, val))
			}
		}
	}

	// Check expected keys if configured
	for _, expectedKey := range target.ExpectedKeys {
		val, exists := parsedResp.Data[expectedKey]
		if !exists {
			falseFields = append(falseFields, fmt.Sprintf("missing expected key: %s", expectedKey))
			continue
		}
		if bVal, ok := val.(bool); ok && !bVal {
			// Already caught above or explicit
		}
	}

	if len(falseFields) > 0 {
		result.Status = false
		result.ErrorDetail = fmt.Sprintf("Sub-service check failed: %s", strings.Join(falseFields, ", "))
		return result
	}

	// All checks passed!
	result.Status = true
	result.ErrorDetail = ""
	return result
}
