package monitor

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"serverMonitoring/models"
)

var (
	scriptRegex   = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+)["']`)
	linkRegex     = regexp.MustCompile(`(?i)<link[^>]+(?:rel=["']stylesheet["'][^>]+href=["']([^"']+)["']|href=["']([^"']+)["'][^>]+rel=["']stylesheet["'])`)
	errorKeywords = []string{
		"unhandled error",
		"fatal error",
		"502 Bad Gateway",
		"500 Internal Server Error",
		"503 Service Unavailable",
		"504 Gateway Time-out",
		"Uncaught SyntaxError",
		"TypeError: Cannot read properties",
	}
)

// CheckFrontend checks a frontend website URL based on target configurations
func CheckFrontend(target models.Target, timeout time.Duration) models.HealthCheckResult {
	startTime := time.Now()
	nowStr := startTime.In(models.ISTLocation).Format(models.TimeFormat)

	result := models.HealthCheckResult{
		ID:          fmt.Sprintf("%s-%d", target.ID, startTime.UnixNano()),
		TargetID:    target.ID,
		ServiceName: target.Name,
		Type:        "frontend",
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
		result.Message = "Failed to create HTTP request"
		result.ErrorDetail = err.Error()
		result.ResponseTimeMs = time.Since(startTime).Milliseconds()
		return result
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Zadroit-Monitor/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	result.ResponseTimeMs = time.Since(startTime).Milliseconds()
	if err != nil {
		result.Message = "Frontend unreachable / Network Error"
		result.ErrorDetail = err.Error()
		result.Data = map[string]interface{}{
			"reachable": false,
			"error":     err.Error(),
		}
		return result
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit
	if err != nil {
		result.Message = "Failed reading HTML response"
		result.ErrorDetail = err.Error()
		return result
	}

	bodyStr := string(bodyBytes)

	// Verify HTTP status code (200 - 399 considered valid for web frontend)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		result.Message = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		result.ErrorDetail = fmt.Sprintf("Frontend returned error status code: %d", resp.StatusCode)
		result.Data = map[string]interface{}{
			"http_status": resp.StatusCode,
			"reachable":   true,
			"page_status": "error_code",
		}
		return result
	}

	// Verify Content-Type / HTML elements
	contentType := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(strings.ToLower(contentType), "text/html") ||
		strings.Contains(bodyStr, "<html") ||
		strings.Contains(bodyStr, "<!doctype html") ||
		strings.Contains(bodyStr, "<head>") ||
		strings.Contains(bodyStr, "<body>")

	if !isHTML {
		result.Message = "Response is not valid HTML"
		result.ErrorDetail = fmt.Sprintf("Unexpected Content-Type: %s", contentType)
		result.Data = map[string]interface{}{
			"content_type": contentType,
			"is_html":      false,
		}
		return result
	}

	// Scan body for fatal crash error messages
	for _, kw := range errorKeywords {
		if strings.Contains(strings.ToLower(bodyStr), strings.ToLower(kw)) {
			result.Message = fmt.Sprintf("Page contains error indicator: '%s'", kw)
			result.ErrorDetail = fmt.Sprintf("Potential console/render error found in HTML: %s", kw)
			result.Data = map[string]interface{}{
				"error_indicator": kw,
				"page_size_bytes": len(bodyBytes),
			}
			return result
		}
	}

	// Extract asset links (scripts and CSS)
	baseURL, _ := url.Parse(target.URL)
	var assetURLs []string

	scriptMatches := scriptRegex.FindAllStringSubmatch(bodyStr, -1)
	for _, match := range scriptMatches {
		if len(match) > 1 {
			resolved := resolveURL(baseURL, match[1])
			if resolved != "" {
				assetURLs = append(assetURLs, resolved)
			}
		}
	}

	linkMatches := linkRegex.FindAllStringSubmatch(bodyStr, -1)
	for _, match := range linkMatches {
		href := match[1]
		if href == "" && len(match) > 2 {
			href = match[2]
		}
		if href != "" {
			resolved := resolveURL(baseURL, href)
			if resolved != "" {
				assetURLs = append(assetURLs, resolved)
			}
		}
	}

	// Check assets concurrently to ensure no 404/500 console errors
	brokenAssets := checkAssets(client, assetURLs, 5*time.Second)

	result.Data = map[string]interface{}{
		"http_status":        resp.StatusCode,
		"content_type":       contentType,
		"page_size_bytes":    len(bodyBytes),
		"assets_checked":     len(assetURLs),
		"broken_assets":      len(brokenAssets),
		"console_error_free": len(brokenAssets) == 0,
	}

	if len(brokenAssets) > 0 {
		result.Status = false
		result.Message = fmt.Sprintf("Frontend has %d failing asset(s) (Console Error risk)", len(brokenAssets))
		result.ErrorDetail = fmt.Sprintf("Assets returning error: %s", strings.Join(brokenAssets, ", "))
		result.Data["failed_asset_list"] = brokenAssets
		return result
	}

	result.Status = true
	result.Message = "Frontend is healthy, accessible, and all assets loaded"
	return result
}

func resolveURL(base *url.URL, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}

	// Handle protocol-relative URLs (e.g. //cdn.example.com/lib.js)
	if strings.HasPrefix(rel, "//") {
		scheme := "https:"
		if base != nil && base.Scheme != "" {
			scheme = base.Scheme + ":"
		}
		return scheme + rel
	}

	if base == nil {
		return rel
	}
	u, err := url.Parse(rel)
	if err != nil {
		return rel
	}
	return base.ResolveReference(u).String()
}

func checkAssets(client *http.Client, urls []string, timeout time.Duration) []string {
	if len(urls) == 0 {
		return nil
	}

	// Deduplicate URLs
	uniqueMap := make(map[string]bool)
	var uniqueURLs []string
	for _, u := range urls {
		if u != "" && !uniqueMap[u] {
			uniqueMap[u] = true;
			uniqueURLs = append(uniqueURLs, u)
		}
	}

	// Limit to 8 assets to maintain high check speed
	if len(uniqueURLs) > 8 {
		uniqueURLs = uniqueURLs[:8]
	}

	var broken []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	assetClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	for _, rawURL := range uniqueURLs {
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			continue
		}

		wg.Add(1)
		go func(targetURL string) {
			defer wg.Done()
			req, err := http.NewRequest("HEAD", targetURL, nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", "Zadroit-Monitor-AssetCheck/1.0")
			resp, err := assetClient.Do(req)
			if err != nil || resp.StatusCode >= 400 {
				// Retry with GET if HEAD is refused or returns non-200
				getReq, _ := http.NewRequest("GET", targetURL, nil)
				getResp, getErr := assetClient.Do(getReq)
				if getErr != nil || (getResp != nil && getResp.StatusCode >= 400) {
					mu.Lock()
					broken = append(broken, targetURL)
					mu.Unlock()
				}
				if getResp != nil {
					getResp.Body.Close()
				}
				return
			}
			resp.Body.Close()
		}(rawURL)
	}

	wg.Wait()
	return broken
}
