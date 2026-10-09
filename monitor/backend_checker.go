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

// CheckBackend checks a backend API endpoint with 1 automatic retry on failure
func CheckBackend(target models.Target, timeout time.Duration) models.HealthCheckResult {
	res := checkBackendOnce(target, timeout)
	if res.Status {
		return res
	}

	// Retry once after 1s before marking as failure
	time.Sleep(1 * time.Second)
	retryRes := checkBackendOnce(target, timeout)
	if retryRes.Status {
		return retryRes
	}

	return retryRes
}

// checkBackendOnce performs a single probe of a backend API endpoint
func checkBackendOnce(target models.Target, timeout time.Duration) models.HealthCheckResult {
	startTime := time.Now()
	nowStr := startTime.In(models.ISTLocation).Format(models.TimeFormat)

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

	method := strings.ToUpper(strings.TrimSpace(target.Method))
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

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit
	if err != nil {
		result.Message = "Failed to read response body"
		result.ErrorDetail = err.Error()
		return result
	}

	// 1. Verify HTTP Status Code (must be 2xx OK)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Message = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		result.ErrorDetail = fmt.Sprintf("Server returned error status code: %d", resp.StatusCode)

		// Attempt to parse JSON error payload if present
		var rawMap map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &rawMap); err == nil {
			result.Data = rawMap
		} else {
			cleanBody := strings.TrimSpace(string(bodyBytes))
			if len(cleanBody) > 300 {
				cleanBody = cleanBody[:300] + "..."
			}
			result.Data = map[string]interface{}{"raw_response": cleanBody}
		}
		return result
	}

	cleanBody := strings.TrimSpace(string(bodyBytes))

	// 2. Try parsing response body as JSON map
	var rawMap map[string]interface{}
	jsonErr := json.Unmarshal(bodyBytes, &rawMap)

	if jsonErr != nil {
		// Check if it's a JSON array
		var rawArray []interface{}
		if arrayErr := json.Unmarshal(bodyBytes, &rawArray); arrayErr == nil {
			result.Data = map[string]interface{}{
				"item_count":   len(rawArray),
				"raw_array":    rawArray,
				"json_type":    "array",
			}
			if len(target.ExpectedKeys) > 0 {
				result.Status = false
				result.Message = "Response is JSON Array (Expected object with keys)"
				result.ErrorDetail = fmt.Sprintf("Expected keys %v but received JSON array", target.ExpectedKeys)
				return result
			}
			result.Status = true
			result.Message = fmt.Sprintf("HTTP 200 OK (JSON Array with %d items)", len(rawArray))
			return result
		}

		// Not a JSON object or array (could be plain text like "OK", "PONG", or HTML)
		truncated := cleanBody
		if len(truncated) > 300 {
			truncated = truncated[:300] + "..."
		}
		result.Data = map[string]interface{}{"raw_response": truncated}

		if len(target.ExpectedKeys) > 0 {
			result.Status = false
			result.Message = "Response is not valid JSON"
			result.ErrorDetail = fmt.Sprintf("Expected JSON keys %v, but response was not JSON: %s", target.ExpectedKeys, jsonErr.Error())
			return result
		}

		// Plain text response check (e.g. "OK", "PONG", "HEALTHY", "UP")
		lowerText := strings.ToLower(cleanBody)
		if strings.Contains(lowerText, "error") || strings.Contains(lowerText, "fatal") || strings.Contains(lowerText, "down") || strings.Contains(lowerText, "failed") {
			result.Status = false
			result.Message = "Response text indicates failure"
			result.ErrorDetail = fmt.Sprintf("Response text contained failure keyword: %s", truncated)
			return result
		}

		result.Status = true
		result.Message = "HTTP 200 OK"
		return result
	}

	// 3. We have a valid JSON object map!
	result.Data = rawMap

	// Extract message if present
	if msgVal, exists := rawMap["message"]; exists && msgVal != nil {
		result.Message = fmt.Sprintf("%v", msgVal)
	} else if msgVal, exists := rawMap["msg"]; exists && msgVal != nil {
		result.Message = fmt.Sprintf("%v", msgVal)
	} else {
		result.Message = "OK"
	}

	// Merge keys from nested "data", "result", or "payload" maps for key lookup
	mergedKeys := make(map[string]interface{})
	for k, v := range rawMap {
		mergedKeys[k] = v
	}
	for _, nestedName := range []string{"data", "result", "payload"} {
		if nested, ok := rawMap[nestedName].(map[string]interface{}); ok {
			for k, v := range nested {
				mergedKeys[k] = v
			}
		}
	}

	// 4. Validate top-level status/success fields if present
	hasStatusField := false
	statusBool := true

	for _, statusKey := range []string{"status", "success", "ok", "healthy", "service"} {
		if val, ok := mergedKeys[statusKey]; ok && val != nil {
			hasStatusField = true
			switch v := val.(type) {
			case bool:
				if !v {
					statusBool = false
				}
			case string:
				lowerV := strings.ToLower(strings.TrimSpace(v))
				if lowerV == "false" || lowerV == "down" || lowerV == "failed" || lowerV == "error" || lowerV == "0" || lowerV == "unhealthy" {
					statusBool = false
				}
			case float64:
				if v == 0 {
					statusBool = false
				}
			}
			if !statusBool {
				break
			}
		}
	}

	if hasStatusField && !statusBool {
		result.Status = false
		result.ErrorDetail = fmt.Sprintf("API status indicator is false/down (Message: %s)", result.Message)
		return result
	}

	// 5. Validate configured ExpectedKeys
	var falseFields []string
	if len(target.ExpectedKeys) > 0 {
		for _, key := range target.ExpectedKeys {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}

			// Case-insensitive key match in mergedKeys
			val, found := findKeyCaseInsensitive(mergedKeys, trimmedKey)
			if !found {
				falseFields = append(falseFields, fmt.Sprintf("missing expected key '%s'", trimmedKey))
				continue
			}

			// Check key value status
			if isValueFailure(val) {
				falseFields = append(falseFields, fmt.Sprintf("key '%s': %v", trimmedKey, val))
			}
		}
	}

	// 6. Scan all boolean & sub-service fields in merged keys for failure values
	for k, v := range mergedKeys {
		// Skip status/message fields already checked
		lowerK := strings.ToLower(k)
		if lowerK == "status" || lowerK == "message" || lowerK == "msg" || lowerK == "raw_response" {
			continue
		}

		if isValueFailure(v) {
			// Avoid duplicate error logging if already captured in ExpectedKeys
			msg := fmt.Sprintf("%s: %v", k, v)
			alreadyCaptured := false
			for _, ff := range falseFields {
				if strings.Contains(ff, k) {
					alreadyCaptured = true
					break
				}
			}
			if !alreadyCaptured {
				falseFields = append(falseFields, msg)
			}
		}
	}

	if len(falseFields) > 0 {
		result.Status = false
		result.ErrorDetail = fmt.Sprintf("Sub-service check failed: %s", strings.Join(falseFields, ", "))
		return result
	}

	// All checks passed!
	result.Status = true
	if result.Message == "" {
		result.Message = "OK"
	}
	result.ErrorDetail = ""
	return result
}

func findKeyCaseInsensitive(m map[string]interface{}, searchKey string) (interface{}, bool) {
	if val, ok := m[searchKey]; ok {
		return val, true
	}
	searchLower := strings.ToLower(searchKey)
	for k, v := range m {
		if strings.ToLower(k) == searchLower {
			return v, true
		}
	}
	return nil, false
}

func isValueFailure(val interface{}) bool {
	if val == nil {
		return true
	}
	switch v := val.(type) {
	case bool:
		return !v
	case string:
		lowerV := strings.ToLower(strings.TrimSpace(v))
		return lowerV == "false" || lowerV == "down" || lowerV == "failed" || lowerV == "error" || lowerV == "unhealthy" || lowerV == "0"
	case float64:
		return v == 0
	}
	return false
}
