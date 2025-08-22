// Package main implements the gutz web server for GitHub user timezone detection.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
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
				s.logger.Error("Panic", "error", err, "path", r.URL.Path, "request_id", requestID)
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
	if r.Method != http.MethodGet {
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
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, struct{ Username string }{username}); err != nil {
		s.logger.Error("Template execution failed", "error", err)
	}
}

func (s *server) handleDetect(writer http.ResponseWriter, request *http.Request) {
	// Rate limit (bypass for Precache User-Agent)
	if request.Header.Get("User-Agent") != "Precache" {
		ip := strings.Split(request.RemoteAddr, ":")[0]
		if !s.limiter.allow(ip) {
			http.Error(writer, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Parse request
	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(writer, "Invalid request", http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if !gutz.IsValidGitHubUsername(req.Username) {
		http.Error(writer, "Invalid username", http.StatusBadRequest)
		return
	}

	// Check memory cache
	cacheKey := "detect:" + req.Username
	if data, found := s.cache.Get(cacheKey); found {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Cache", "memory-hit")
		if _, err := writer.Write(data); err != nil {
			s.logger.Error("Failed to write cached response", "error", err)
		}
		return
	}

	// Check disk cache
	if s.diskCache != nil {
		if data := s.diskCache.load(req.Username); data != nil {
			s.cache.Set(cacheKey, data)
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("X-Cache", "disk-hit")
			if _, err := writer.Write(data); err != nil {
				s.logger.Error("Failed to write disk cached response", "error", err)
			}
			return
		}
	}

	// Detect timezone
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()

	result, err := s.detector.Detect(ctx, req.Username)
	if err != nil {
		s.logger.Error("Detection failed", "username", req.Username, "error", err)
		http.Error(writer, "Detection failed", http.StatusInternalServerError)
		return
	}

	// Clear sensitive data
	if !*verbose {
		result.GeminiPrompt = ""
	}

	// Encode response
	data, err := json.Marshal(result)
	if err != nil {
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
		s.logger.Error("Failed to write response", "error", err)
	}
}

func (s *server) handleCleanup(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.diskCache == nil {
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "no cache"}); err != nil {
			s.logger.Error("Failed to encode response", "error", err)
		}
		return
	}

	deleted := s.diskCache.cleanup()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"deleted": deleted,
	}); err != nil {
		s.logger.Error("Failed to encode response", "error", err)
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
