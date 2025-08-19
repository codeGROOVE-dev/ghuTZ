// Package main implements the gutz web server for GitHub user timezone detection.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/gutz"
)

//go:embed templates/home.html
var homeTemplate string

//go:embed static/*
var staticFiles embed.FS

var (
	port         = flag.String("port", "8080", "Port for web server")
	githubToken  = flag.String("github-token", "", "GitHub API token (or set GITHUB_TOKEN)")
	geminiAPIKey = flag.String("gemini-key", "", "Gemini API key (or set GEMINI_API_KEY)")
	geminiModel  = flag.String("gemini-model", "gemini-2.5-flash-lite", "Gemini model to use (or set GEMINI_MODEL)")
	mapsAPIKey   = flag.String("maps-key", "", "Google Maps API key (or set GOOGLE_MAPS_API_KEY)")
	gcpProject   = flag.String("gcp-project", "", "GCP project ID (or set GCP_PROJECT)")
	cacheDir     = flag.String("cache-dir", "", "Cache directory (or set CACHE_DIR)")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	version      = flag.Bool("version", false, "Show version")
)

// Simple rate limiter for QPS control with memory protection.
type rateLimiter struct {
	requests map[string][]time.Time
	window   time.Duration
	limit    int
	maxKeys  int
	mu       sync.Mutex
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		maxKeys:  10000, // Limit to 10k unique IPs to prevent memory exhaustion
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Clean old requests for this key
	if reqs, exists := rl.requests[key]; exists {
		var filtered []time.Time
		for _, t := range reqs {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			delete(rl.requests, key) // Remove empty entries
		} else {
			rl.requests[key] = filtered
		}
	}

	// Memory exhaustion protection: if too many keys, clean up old ones
	if len(rl.requests) >= rl.maxKeys {
		rl.cleanupOldEntries(cutoff)
	}

	// Check if limit exceeded
	if len(rl.requests[key]) >= rl.limit {
		return false
	}

	// Add current request
	rl.requests[key] = append(rl.requests[key], now)
	return true
}

// cleanupOldEntries removes expired entries to prevent memory exhaustion.
func (rl *rateLimiter) cleanupOldEntries(cutoff time.Time) {
	for key, timestamps := range rl.requests {
		var filtered []time.Time
		for _, t := range timestamps {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			delete(rl.requests, key)
		} else {
			rl.requests[key] = filtered
		}
	}
}

var apiLimiter = newRateLimiter(5, time.Minute) // 5 requests per minute per IP - defense against abuse

// SECURITY: Username validation regex - GitHub usernames can only contain alphanumeric characters and hyphens.
var validUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)

// sanitizeUsername validates and sanitizes username input to prevent XSS and injection.
func sanitizeUsername(username string) string {
	// Trim whitespace
	username = strings.TrimSpace(username)

	// Length check
	if username == "" || len(username) > 39 {
		return ""
	}

	// Validate against GitHub username pattern
	if !validUsernameRegex.MatchString(username) {
		return ""
	}

	// HTML escape as additional protection
	return html.EscapeString(username)
}

// securityHeadersMiddleware adds comprehensive security headers.
func securityHeadersMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// SECURITY: Comprehensive security headers for defense in depth

		// Content Security Policy - unified policy for all environments
		w.Header().Set("Content-Security-Policy", cspPolicy())

		// Prevent clickjacking attacks
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy - limit information leakage
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// HSTS - Force HTTPS (commented out for local development)
		// w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Permissions policy - restrict browser features
		w.Header().Set("Permissions-Policy",
			"geolocation=(), microphone=(), camera=(), payment=(), usb=(), bluetooth=()")

		// Cache control for sensitive responses
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			// Prevent response sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}

		next.ServeHTTP(w, r)
	}
}

// panicRecoveryMiddleware prevents crashes from panics - critical for nation-state attack resilience.
func panicRecoveryMiddleware(logger *slog.Logger, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Generate request ID for tracing
		requestID := fmt.Sprintf("%d-%d", time.Now().Unix(), time.Now().Nanosecond())
		w.Header().Set("X-Request-ID", requestID)

		defer func() {
			if err := recover(); err != nil {
				logger.Error("SECURITY ALERT: Panic recovered",
					"error", err,
					"path", r.URL.Path,
					"method", r.Method,
					"remote_addr", r.RemoteAddr,
					"user_agent", r.Header.Get("User-Agent"),
					"request_id", requestID)

				// Don't leak internal details to potential attackers
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	}
}

func main() {
	flag.Parse()

	if *version {
		fmt.Println("guTZ Server v2.1.0")
		return
	}

	// Configure logging
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))

	// Get tokens from environment if not provided as flags
	if *githubToken == "" {
		*githubToken = os.Getenv("GITHUB_TOKEN")
	}
	if *geminiAPIKey == "" {
		*geminiAPIKey = os.Getenv("GEMINI_API_KEY")
	}
	if *geminiModel == "gemini-2.5-flash-lite" && os.Getenv("GEMINI_MODEL") != "" {
		*geminiModel = os.Getenv("GEMINI_MODEL")
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

	// Create detector with options
	detectorOpts := []gutz.Option{
		gutz.WithGitHubToken(*githubToken),
		gutz.WithGeminiAPIKey(*geminiAPIKey),
		gutz.WithGeminiModel(*geminiModel),
		gutz.WithMapsAPIKey(*mapsAPIKey),
		gutz.WithGCPProject(*gcpProject),
	}

	if *cacheDir != "" {
		detectorOpts = append(detectorOpts, gutz.WithCacheDir(*cacheDir))
		logger.Info("using custom cache directory", "cache_dir", *cacheDir)
	}

	// Create a context for the server lifetime
	ctx := context.Background()
	detector := gutz.NewWithLogger(ctx, logger, detectorOpts...)

	if err := runServer(detector, logger); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}

func runServer(detector *gutz.Detector, logger *slog.Logger) error {
	// Create ServeMux for routing
	mux := http.NewServeMux()

	// Register handlers with security middleware
	mux.HandleFunc("POST /api/v1/detect",
		panicRecoveryMiddleware(logger,
			securityHeadersMiddleware(
				rateLimitMiddleware(
					handleAPIDetect(detector, logger)))))
	// Static file server using embedded files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to create static file system: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	staticHandler := http.StripPrefix("/static/", fileServer)
	mux.Handle("/static/", panicRecoveryMiddleware(logger, securityHeadersMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// Set proper MIME types for static files
		switch {
		case strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "application/javascript")
		case strings.HasSuffix(r.URL.Path, ".css"):
			w.Header().Set("Content-Type", "text/css")
		case strings.HasSuffix(r.URL.Path, ".png"):
			w.Header().Set("Content-Type", "image/png")
		case strings.HasSuffix(r.URL.Path, ".jpg"), strings.HasSuffix(r.URL.Path, ".jpeg"):
			w.Header().Set("Content-Type", "image/jpeg")
		default:
			// No special Content-Type needed for other files
		}

		// Additional security for static files
		w.Header().Set("Cache-Control", "public, max-age=3600, immutable")
		staticHandler.ServeHTTP(w, r)
	})))
	mux.HandleFunc("/", panicRecoveryMiddleware(logger, securityHeadersMiddleware(handleHomeOrUser)))

	// Configure CSRF protection using Go 1.25's CrossOriginProtection
	antiCSRF := http.NewCrossOriginProtection()

	// Add the protected mux to a handler with CSRF protection
	handler := antiCSRF.Handler(mux)

	addr := ":" + *port

	// Create server with proper timeouts
	server := &http.Server{
		Addr:           addr,
		Handler:        handler, // Use the CSRF-protected handler
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// Set up graceful shutdown
	serverErrors := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		logger.Info("Starting guTZ server", "addr", addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Set up signal handling for graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error: %w", err)
		}
	case sig := <-shutdown:
		logger.Info("Shutdown signal received", "signal", sig.String())

		// Give outstanding requests 30 seconds to complete
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown server gracefully
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("Server shutdown error", "error", err)
			// Force close
			if closeErr := server.Close(); closeErr != nil {
				logger.Error("Failed to force close server", "error", closeErr)
			}
		}

		// Close detector (saves cache to disk)
		if err := detector.Close(); err != nil {
			logger.Error("Detector close error", "error", err)
		}

		logger.Info("Server shutdown complete")
	}
	return nil
}

func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP - SECURITY: Only trust proxy headers in known proxy environments
		clientIP := r.RemoteAddr

		// Extract IP without port if present (format "IP:port")
		if idx := strings.LastIndex(clientIP, ":"); idx != -1 {
			clientIP = clientIP[:idx]
		}

		// SECURITY: Only trust proxy headers if we're behind a known trusted proxy
		// In production, this should check against a whitelist of trusted proxy IPs
		// For now, we'll use the direct connection IP to prevent header spoofing

		if !apiLimiter.allow(clientIP) {
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func handleHomeOrUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract potential username from URL path
	path := strings.TrimPrefix(r.URL.Path, "/")

	// Check for username in path first, then fall back to query parameter
	var username string
	if path != "" {
		// SECURITY: Sanitize username from path to prevent attacks
		username = sanitizeUsername(path)
		if username == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	} else {
		// Fall back to query parameter for backwards compatibility
		username = sanitizeUsername(r.URL.Query().Get("u"))
	}

	// Render the template with the username
	tmpl, err := template.New("home").Parse(homeTemplate)
	if err != nil {
		http.Error(w, "Template parse error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Username string
	}{
		Username: username,
	}

	w.Header().Set("Content-Type", "text/html")
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Template execution failed", http.StatusInternalServerError)
		return
	}
}

func handleAPIDetect(detector *gutz.Detector, logger *slog.Logger) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		// Method check is now handled by the mux pattern "POST /api/v1/detect"
		// CSRF protection is now handled by Go 1.25's CrossOriginProtection

		var req struct {
			Username string `json:"username"`
		}

		// SECURITY: Limit request size to prevent DoS attacks
		request.Body = http.MaxBytesReader(writer, request.Body, 4096) // 4KB max - much smaller for simple username

		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields() // SECURITY: Strict JSON parsing

		if err := decoder.Decode(&req); err != nil {
			logger.Warn("Invalid request", "error", err, "remote_addr", request.RemoteAddr)
			http.Error(writer, "Invalid request", http.StatusBadRequest)
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || len(req.Username) > 100 {
			// SECURITY: Log potential attack attempts
			logger.Warn("SECURITY: Invalid username attempt",
				"username_length", len(req.Username),
				"remote_addr", request.RemoteAddr,
				"user_agent", request.Header.Get("User-Agent"))
			http.Error(writer, "Invalid username", http.StatusBadRequest)
			return
		}

		logger.Info("Processing detection request", "username", req.Username, "remote_addr", request.RemoteAddr)

		ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
		defer cancel()

		result, err := detector.Detect(ctx, req.Username)
		if err != nil {
			logger.Error("Detection failed", "username", req.Username, "error", err)
			http.Error(writer, "Detection failed", http.StatusInternalServerError)
			return
		}

		// Check if this is a "user not found" result
		if result.Method == "user_not_found" {
			logger.Info("User not found", "username", req.Username)
			http.Error(writer, "User not found", http.StatusNotFound)
			return
		}

		logger.Info("Detection successful", "username", req.Username, "timezone", result.Timezone, "method", result.Method)

		// Log detailed result information for debugging
		logger.Debug("Detection result details",
			"username", req.Username,
			"location", result.Location,
			"location_name", result.LocationName,
			"gemini_suggested_location", result.GeminiSuggestedLocation,
			"top_organizations_count", len(result.TopOrganizations),
			"confidence", result.TimezoneConfidence,
			"activity_date_range", result.ActivityDateRange,
			"hourly_activity_count", len(result.HourlyActivityUTC))

		// Log the top organizations if present
		if len(result.TopOrganizations) > 0 {
			var orgNames []string
			for _, org := range result.TopOrganizations {
				orgNames = append(orgNames, fmt.Sprintf("%s(%d)", org.Name, org.Count))
			}
			logger.Info("Top organizations detected",
				"username", req.Username,
				"organizations", strings.Join(orgNames, ", "))
		}

		// Marshal to JSON for logging the full response
		if jsonBytes, err := json.MarshalIndent(result, "", "  "); err == nil {
			logger.Debug("Full JSON response", "username", req.Username, "response", string(jsonBytes))
		}

		writer.Header().Set("Content-Type", "application/json")

		// SECURITY: Restrictive CORS - only allow same origin by default
		// No CORS headers = same-origin only (most secure default)

		if err := json.NewEncoder(writer).Encode(result); err != nil {
			logger.Error("failed to encode JSON response", "error", err)
		}
	}
}
