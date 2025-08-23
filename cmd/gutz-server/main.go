// Package main implements the gutz web server for GitHub user timezone detection.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/gutz"
	"github.com/maypok86/otter"
)

//go:embed templates/home.html
var homeTemplate string

//go:embed static/*
var staticFiles embed.FS

var (
	port         = flag.String("port", "8080", "Port for web server")
	githubToken  = flag.String("github-token", "", "GitHub API token (or set GITHUB_TOKEN)")
	geminiAPIKey = flag.String("gemini-key", "", "Gemini API key (or set GEMINI_API_KEY)")
	geminiModel  = flag.String("gemini-model", "gemini-2.5-flash-lite", "Gemini model to use")
	mapsAPIKey   = flag.String("maps-key", "", "Google Maps API key (or set GOOGLE_MAPS_API_KEY)")
	gcpProject   = flag.String("gcp-project", "", "GCP project ID (or set GCP_PROJECT)")
	cacheDir     = flag.String("cache-dir", "", "Cache directory (or set CACHE_DIR)")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	version      = flag.Bool("version", false, "Show version")
)

type rateLimiter struct {
	requests map[string][]time.Time
	mu       sync.Mutex
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	// Rate limit: 15 requests per minute per IP
	if len(valid) >= 15 {
		rl.requests[ip] = valid
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

func main() {
	flag.Parse()

	if *version {
		fmt.Println("guTZ Server v2.1.0")
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if *githubToken == "" {
		*githubToken = os.Getenv("GITHUB_TOKEN")
		if *githubToken == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if token, err := exec.CommandContext(ctx, "gh", "auth", "token").Output(); err == nil {
				*githubToken = strings.TrimSpace(string(token))
			}
		}
	}
	if *geminiAPIKey == "" {
		*geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	}
	if *mapsAPIKey == "" {
		*mapsAPIKey = os.Getenv("GOOGLE_MAPS_API_KEY")
	}
	if *gcpProject == "" {
		*gcpProject = os.Getenv("GCP_PROJECT")
	}
	if *cacheDir == "" {
		*cacheDir = os.Getenv("CACHE_DIR")
	}

	// Log configuration (without exposing sensitive keys)
	logger.Info("Server configuration",
		"port", *port,
		"verbose", *verbose,
		"cache_dir", *cacheDir,
		"gemini_model", *geminiModel,
		"has_github_token", *githubToken != "",
		"has_gemini_key", *geminiAPIKey != "",
		"has_maps_key", *mapsAPIKey != "",
		"has_gcp_project", *gcpProject != "")

	detector := gutz.NewWithLogger(context.Background(), logger,
		gutz.WithGitHubToken(*githubToken),
		gutz.WithGeminiAPIKey(*geminiAPIKey),
		gutz.WithGeminiModel(*geminiModel),
		gutz.WithMapsAPIKey(*mapsAPIKey),
		gutz.WithGCPProject(*gcpProject),
		gutz.WithMemoryOnlyCache(),
	)
	defer func() {
		if err := detector.Close(); err != nil {
			logger.Error("Failed to close detector", "error", err)
		}
	}()

	cache, err := otter.MustBuilder[string, []byte](10_000).
		WithTTL(12 * time.Hour).
		Build()
	if err != nil {
		logger.Error("Failed to build cache", "error", err)
		return
	}

	var diskCache *diskCacheHandler
	if *cacheDir != "" {
		diskCache = &diskCacheHandler{dir: *cacheDir, logger: logger}
	}

	server := &server{
		detector:  detector,
		cache:     cache,
		diskCache: diskCache,
		limiter:   newRateLimiter(),
		logger:    logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleHome)
	mux.HandleFunc("POST /api/v1/detect", server.handleDetect)
	mux.HandleFunc("POST /_/x-cleanup", server.handleCleanup)
	mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	antiCSRF := http.NewCrossOriginProtection()
	// Add trusted origins if needed for cross-origin access
	// antiCSRF.AddTrustedOrigin("https://example.com")
	// antiCSRF.AddTrustedOrigin("https://*.example.com")

	srv := &http.Server{
		Addr:              ":" + *port,
		Handler:           server.wrap(antiCSRF.Handler(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		logger.Info("Server starting", "port", *port)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Shutdown failed", "error", err)
	}
	logger.Info("Server stopped")
}

type server struct {
	detector  *gutz.Detector
	cache     otter.Cache[string, []byte]
	diskCache *diskCacheHandler
	limiter   *rateLimiter
	logger    *slog.Logger
}

func (s *server) wrap(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := fmt.Sprintf("%d-%d", time.Now().Unix(), time.Now().Nanosecond())
		w.Header().Set("X-Request-ID", requestID)

		defer func() {
			if err := recover(); err != nil {
				// Get stack trace
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]

				// Extract client IP for debugging
				clientIP := strings.Split(r.RemoteAddr, ":")[0]

				s.logger.Error("PANIC: Request handler crashed",
					"error", err,
					"path", r.URL.Path,
					"method", r.Method,
					"request_id", requestID,
					"client_ip", clientIP,
					"user_agent", r.Header.Get("User-Agent"),
					"username", r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:],
					"stack", string(buf))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()

		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=(), bluetooth=()")

		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://unpkg.com https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline' https://unpkg.com; "+
				"img-src 'self' data: https:; "+
				"connect-src 'self'")

		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		} else if strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
		}

		handler.ServeHTTP(w, r)
	})
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	// Get request ID from header (set by wrap middleware)
	requestID := w.Header().Get("X-Request-ID")

	if r.Method != http.MethodGet {
		s.logger.Error("Method not allowed",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"client_ip", strings.Split(r.RemoteAddr, ":")[0])
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get username from path or query
	username := strings.TrimPrefix(r.URL.Path, "/")
	if username == "" {
		username = r.URL.Query().Get("u")
	}

	// Validate username
	if username != "" && !gutz.IsValidGitHubUsername(username) {
		username = ""
	}

	// Render template
	tmpl, err := template.New("home").Parse(homeTemplate)
	if err != nil {
		s.logger.Error("Template parsing failed",
			"request_id", requestID,
			"error", err,
			"path", r.URL.Path)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, struct{ Username string }{username}); err != nil {
		s.logger.Error("Template execution failed",
			"request_id", requestID,
			"error", err,
			"username", username)
	}
}

func (s *server) handleDetect(writer http.ResponseWriter, request *http.Request) {
	start := time.Now()
	clientIP := strings.Split(request.RemoteAddr, ":")[0]
	userAgent := request.Header.Get("User-Agent")
	requestID := writer.Header().Get("X-Request-ID")

	s.logger.Info("Detection request started",
		"request_id", requestID,
		"client_ip", clientIP,
		"user_agent", userAgent,
		"method", request.Method,
		"path", request.URL.Path)

	// Rate limit (bypass for Precache User-Agent)
	if userAgent != "Precache" {
		if !s.limiter.allow(clientIP) {
			s.logger.Error("Rate limit exceeded",
				"request_id", requestID,
				"client_ip", clientIP,
				"user_agent", userAgent)
			http.Error(writer, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Parse request
	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		s.logger.Error("Invalid request body",
			"request_id", requestID,
			"error", err,
			"client_ip", clientIP,
			"duration_ms", time.Since(start).Milliseconds())
		http.Error(writer, "Invalid request", http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if !gutz.IsValidGitHubUsername(req.Username) {
		s.logger.Error("Invalid username",
			"request_id", requestID,
			"username", req.Username,
			"client_ip", clientIP,
			"duration_ms", time.Since(start).Milliseconds())
		http.Error(writer, "Invalid username", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Processing detection request",
		"request_id", requestID,
		"username", req.Username,
		"client_ip", clientIP)

	// Check memory cache
	cacheKey := "detect:" + req.Username
	if data, found := s.cache.Get(cacheKey); found {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Cache", "memory-hit")
		if _, err := writer.Write(data); err != nil {
			s.logger.Error("Failed to write cached response",
				"request_id", requestID,
				"error", err,
				"username", req.Username)
		}
		s.logger.Info("Detection request completed (memory cache)",
			"request_id", requestID,
			"username", req.Username,
			"cache", "memory-hit",
			"duration_ms", time.Since(start).Milliseconds())
		return
	}

	// Check disk cache
	if s.diskCache != nil {
		if data := s.diskCache.load(req.Username); data != nil {
			s.cache.Set(cacheKey, data)
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("X-Cache", "disk-hit")
			if _, err := writer.Write(data); err != nil {
				s.logger.Error("Failed to write disk cached response",
					"request_id", requestID,
					"error", err,
					"username", req.Username)
			}
			s.logger.Info("Detection request completed (disk cache)",
				"request_id", requestID,
				"username", req.Username,
				"cache", "disk-hit",
				"duration_ms", time.Since(start).Milliseconds())
			return
		}
	}

	// Detect timezone
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()

	s.logger.Info("Starting detection",
		"request_id", requestID,
		"username", req.Username,
		"timeout", "30s")

	detectStart := time.Now()
	result, err := s.detector.Detect(ctx, req.Username)
	detectDuration := time.Since(detectStart)

	if err != nil {
		statusCode := http.StatusInternalServerError
		var errorResponse struct {
			Error   string `json:"error"`
			Details string `json:"details,omitempty"`
			Code    string `json:"code,omitempty"`
		}

		// Check for specific error types and provide helpful messages
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			statusCode = http.StatusGatewayTimeout
			errorResponse.Error = "Detection took too long"
			errorResponse.Details = "The analysis exceeded the 30-second timeout. This usually happens with very active users. Please try again."
			errorResponse.Code = "TIMEOUT"
			s.logger.Error("Detection timeout",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		case errors.Is(err, context.Canceled):
			statusCode = http.StatusRequestTimeout
			errorResponse.Error = "Request was canceled"
			errorResponse.Details = "The request was canceled before completion. Please try again."
			errorResponse.Code = "CANCELED"
			s.logger.Error("Detection canceled",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		case strings.Contains(err.Error(), "rate limit"):
			statusCode = http.StatusTooManyRequests
			errorResponse.Error = "GitHub API rate limit exceeded"
			errorResponse.Details = "We've hit GitHub's rate limit. Please try again in a few minutes, or provide a GitHub token for higher limits."
			errorResponse.Code = "GITHUB_RATE_LIMIT"
			s.logger.Error("GitHub rate limit hit",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		case strings.Contains(err.Error(), "not found"):
			statusCode = http.StatusNotFound
			errorResponse.Error = "GitHub user not found"
			errorResponse.Details = fmt.Sprintf("The username '%s' doesn't exist on GitHub. Please check the spelling.", req.Username)
			errorResponse.Code = "USER_NOT_FOUND"
			s.logger.Error("User not found",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		case strings.Contains(err.Error(), "unable to fetch GitHub profile"):
			statusCode = http.StatusBadGateway
			errorResponse.Error = "Unable to fetch GitHub profile"
			if strings.Contains(err.Error(), "permission") || strings.Contains(err.Error(), "scope") {
				errorResponse.Details = "The GitHub token doesn't have required permissions. Please ensure the token has 'read:user' scope."
				errorResponse.Code = "INSUFFICIENT_PERMISSIONS"
			} else {
				errorResponse.Details = "GitHub's API is temporarily unavailable. Please try again in a moment."
				errorResponse.Code = "GITHUB_API_ERROR"
			}
			s.logger.Error("GitHub API error",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		case strings.Contains(err.Error(), "Gemini"):
			statusCode = http.StatusServiceUnavailable
			errorResponse.Error = "AI analysis service unavailable"
			errorResponse.Details = "The Gemini AI service is temporarily unavailable. Detection will use fallback methods."
			errorResponse.Code = "GEMINI_ERROR"
			s.logger.Error("Gemini API error",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		default:
			errorResponse.Error = "Detection failed"
			errorResponse.Details = "An unexpected error occurred during timezone detection. Please try again."
			errorResponse.Code = "INTERNAL_ERROR"
			s.logger.Error("Detection failed",
				"request_id", requestID,
				"username", req.Username,
				"error", err,
				"error_type", fmt.Sprintf("%T", err),
				"detect_duration_ms", detectDuration.Milliseconds(),
				"total_duration_ms", time.Since(start).Milliseconds())
		}

		// Send JSON error response
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(statusCode)
		if err := json.NewEncoder(writer).Encode(errorResponse); err != nil {
			s.logger.Error("Failed to encode error response",
				"request_id", requestID,
				"encode_error", err)
		}
		return
	}

	s.logger.Info("Detection completed successfully",
		"request_id", requestID,
		"username", req.Username,
		"timezone", result.Timezone,
		"detect_duration_ms", detectDuration.Milliseconds())

	// Clear sensitive data
	if !*verbose {
		result.GeminiPrompt = ""
	}

	// Convert HalfHourlyActivityUTC map[float64]int to map[string]int for JSON encoding
	// JSON doesn't support float64 as map keys
	if result.HalfHourlyActivityUTC != nil {
		stringKeyMap := make(map[string]int)
		for k, v := range result.HalfHourlyActivityUTC {
			// Format as "0.0", "0.5", "1.0", etc.
			key := fmt.Sprintf("%.1f", k)
			stringKeyMap[key] = v
		}
		// Create a temporary result with string keys for JSON encoding
		type JSONResult struct {
			*gutz.Result

			HalfHourlyActivityUTC map[string]int `json:"half_hourly_activity_utc,omitempty"`
		}
		jsonResult := JSONResult{
			Result:                result,
			HalfHourlyActivityUTC: stringKeyMap,
		}
		data, err := json.Marshal(jsonResult)
		if err != nil {
			s.logger.Error("JSON encoding failed",
				"request_id", requestID,
				"error", err,
				"username", req.Username,
				"duration_ms", time.Since(start).Milliseconds())
			http.Error(writer, "Encoding failed", http.StatusInternalServerError)
			return
		}
		// Cache result
		s.cache.Set(cacheKey, data)
		if s.diskCache != nil {
			go s.diskCache.save(req.Username, data)
		}

		// Send response
		writer.Header().Set("Content-Type", "application/json")
		if _, err := writer.Write(data); err != nil {
			s.logger.Error("Failed to write response",
				"request_id", requestID,
				"error", err,
				"username", req.Username,
				"duration_ms", time.Since(start).Milliseconds())
		}
		return
	}

	// Encode response (for cases without HalfHourlyActivityUTC)
	data, err := json.Marshal(result)
	if err != nil {
		s.logger.Error("JSON encoding failed", "error", err, "username", req.Username)
		http.Error(writer, "Encoding failed", http.StatusInternalServerError)
		return
	}

	// Cache result
	s.cache.Set(cacheKey, data)
	if s.diskCache != nil {
		go s.diskCache.save(req.Username, data)
	}

	// Send response
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Cache", "miss")
	if _, err := writer.Write(data); err != nil {
		s.logger.Error("Failed to write response",
			"request_id", requestID,
			"error", err,
			"username", req.Username,
			"response_size", len(data),
			"duration_ms", time.Since(start).Milliseconds())
	} else {
		s.logger.Info("Detection request completed",
			"request_id", requestID,
			"username", req.Username,
			"timezone", result.Timezone,
			"cache", "miss",
			"duration_ms", time.Since(start).Milliseconds())
	}
}

func (s *server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	// Get request ID from header (set by wrap middleware)
	requestID := w.Header().Get("X-Request-ID")

	w.Header().Set("Content-Type", "application/json")

	if s.diskCache == nil {
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "no cache"}); err != nil {
			s.logger.Error("Failed to encode response",
				"request_id", requestID,
				"error", err,
				"path", r.URL.Path)
		}
		return
	}

	deleted := s.diskCache.cleanup()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"deleted": deleted,
	}); err != nil {
		s.logger.Error("Failed to encode response",
			"request_id", requestID,
			"error", err,
			"deleted_count", deleted)
	}
}

type diskCacheHandler struct {
	logger *slog.Logger
	dir    string
}

func (d *diskCacheHandler) path(username string) string {
	var dir1, dir2 string
	if len(username) >= 2 {
		dir1 = username[:2]
	} else {
		dir1 = username
	}
	switch {
	case len(username) >= 4:
		dir2 = username[2:4]
	case len(username) > 2:
		dir2 = username[2:]
	default:
		dir2 = "_"
	}
	return filepath.Join(d.dir, "v1", dir1, dir2, username+".json.gz")
}

func (d *diskCacheHandler) load(username string) []byte {
	compressedData, err := os.ReadFile(d.path(username))
	if err != nil {
		return nil
	}

	info, err := os.Stat(d.path(username))
	if err != nil || time.Since(info.ModTime()) > 30*24*time.Hour {
		return nil
	}

	r, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil
	}
	defer func() {
		if err := r.Close(); err != nil {
			d.logger.Debug("Failed to close gzip reader", "error", err)
		}
	}()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil
	}

	return data
}

func (d *diskCacheHandler) save(username string, data []byte) {
	path := d.path(username)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		d.logger.Debug("Failed to create cache dir", "error", err)
		return
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		d.logger.Debug("Failed to compress", "error", err)
		return
	}
	if err := gz.Close(); err != nil {
		d.logger.Debug("Failed to close gzip", "error", err)
		return
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		d.logger.Debug("Failed to write cache", "error", err)
	}
}

func (d *diskCacheHandler) cleanup() int {
	count := 0
	cutoff := time.Now().Add(-28 * 24 * time.Hour)

	if err := filepath.Walk(d.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				d.logger.Debug("Failed to remove old cache file", "path", path, "error", err)
			} else {
				count++
			}
		}
		return nil
	}); err != nil {
		d.logger.Error("Cache cleanup walk failed", "error", err)
	}

	return count
}
