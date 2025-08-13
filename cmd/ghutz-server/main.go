package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ghutz/ghutz/pkg/ghutz"
)

var (
	port         = flag.String("port", "8080", "Port for web server")
	githubToken  = flag.String("github-token", "", "GitHub API token (or set GITHUB_TOKEN)")
	geminiAPIKey = flag.String("gemini-key", "", "Gemini API key (or set GEMINI_API_KEY)")
	mapsAPIKey   = flag.String("maps-key", "", "Google Maps API key (or set GOOGLE_MAPS_API_KEY)")
	gcpProject   = flag.String("gcp-project", "", "GCP project ID (or set GCP_PROJECT)")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	version      = flag.Bool("version", false, "Show version")
)

// Simple rate limiter for QPS control
type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int           // requests per window
	window   time.Duration // time window
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Clean old requests
	if reqs, exists := rl.requests[key]; exists {
		var filtered []time.Time
		for _, t := range reqs {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		rl.requests[key] = filtered
	}

	// Check if limit exceeded
	if len(rl.requests[key]) >= rl.limit {
		return false
	}

	// Add current request
	rl.requests[key] = append(rl.requests[key], now)
	return true
}

var apiLimiter = newRateLimiter(10, time.Minute) // 10 requests per minute per IP

func main() {
	flag.Parse()

	if *version {
		fmt.Println("ghuTZ Server v2.1.0")
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
	if *mapsAPIKey == "" {
		*mapsAPIKey = os.Getenv("GOOGLE_MAPS_API_KEY")
	}
	if *gcpProject == "" {
		*gcpProject = os.Getenv("GCP_PROJECT")
	}

	// Create detector with options
	detector := ghutz.NewWithLogger(
		logger,
		ghutz.WithGitHubToken(*githubToken),
		ghutz.WithGeminiAPIKey(*geminiAPIKey),
		ghutz.WithMapsAPIKey(*mapsAPIKey),
		ghutz.WithGCPProject(*gcpProject),
	)

	runServer(detector, logger)
}

func runServer(detector *ghutz.Detector, logger *slog.Logger) {
	http.HandleFunc("/", handleHome)
	http.HandleFunc("/u/", handleUserPage)        // For shareable URLs like /u/username
	http.HandleFunc("/api/v1/detect", rateLimitMiddleware(handleAPIDetect(detector, logger)))

	addr := ":" + *port
	logger.Info("Starting ghuTZ server", "addr", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP (handle proxy headers)
		clientIP := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			clientIP = strings.Split(forwarded, ",")[0]
		} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			clientIP = realIP
		}

		if !apiLimiter.allow(clientIP) {
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if URL has username parameter for direct sharing
	username := r.URL.Query().Get("u")

	tmpl, err := template.ParseFiles("templates/home.html")
	if err != nil {
		http.Error(w, "Template not found", http.StatusInternalServerError)
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

func handleUserPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract username from URL path /u/username
	username := strings.TrimPrefix(r.URL.Path, "/u/")
	if username == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Redirect to home with username parameter for frontend to handle
	http.Redirect(w, r, "/?u="+username, http.StatusFound)
}

func handleAPIDetect(detector *ghutz.Detector, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string `json:"username"`
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("Invalid request", "error", err, "remote_addr", r.RemoteAddr)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || len(req.Username) > 100 {
			logger.Warn("Invalid username", "username", req.Username, "remote_addr", r.RemoteAddr)
			http.Error(w, "Invalid username", http.StatusBadRequest)
			return
		}

		logger.Info("Processing detection request", "username", req.Username, "remote_addr", r.RemoteAddr)

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		result, err := detector.Detect(ctx, req.Username)
		if err != nil {
			logger.Error("Detection failed", "username", req.Username, "error", err)
			http.Error(w, "Detection failed", http.StatusInternalServerError)
			return
		}

		logger.Info("Detection successful", "username", req.Username, "timezone", result.Timezone, "method", result.Method)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		_ = json.NewEncoder(w).Encode(result)
	}
}