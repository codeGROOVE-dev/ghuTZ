package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/ghutz"
)

var (
	githubToken   = flag.String("github-token", "", "GitHub token for API access (or set GITHUB_TOKEN)")
	geminiAPIKey  = flag.String("gemini-key", "", "Gemini API key (or set GEMINI_API_KEY)")
	geminiModel   = flag.String("gemini-model", "gemini-2.5-flash-lite", "Gemini model to use (or set GEMINI_MODEL)")
	mapsAPIKey    = flag.String("maps-key", "", "Google Maps API key (or set GOOGLE_MAPS_API_KEY)")
	gcpProject    = flag.String("gcp-project", "", "GCP project ID (or set GCP_PROJECT)")
	cacheDir      = flag.String("cache-dir", "", "Cache directory (or set CACHE_DIR)")
	verbose       = flag.Bool("verbose", false, "Enable verbose logging")
	activity      = flag.Bool("activity", false, "Force activity analysis")
	version       = flag.Bool("version", false, "Show version")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Println("ghuTZ CLI v2.1.0")
		return
	}

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <github-username>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	username := args[0]

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
	detectorOpts := []ghutz.Option{
		ghutz.WithGitHubToken(*githubToken),
		ghutz.WithGeminiAPIKey(*geminiAPIKey),
		ghutz.WithGeminiModel(*geminiModel),
		ghutz.WithMapsAPIKey(*mapsAPIKey),
		ghutz.WithGCPProject(*gcpProject),
		ghutz.WithActivityAnalysis(*activity),
	}
	
	if *cacheDir != "" {
		detectorOpts = append(detectorOpts, ghutz.WithCacheDir(*cacheDir))
	}
	
	detector := ghutz.NewWithLogger(logger, detectorOpts...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := detector.Detect(ctx, username)
	if err != nil {
		log.Fatalf("Detection failed: %v", err)
	}

	// Print results in CLI format
	printResult(result)
}

func printResult(result *ghutz.Result) {
	fmt.Printf("User: %s\n", result.Username)
	fmt.Printf("Timezone: %s\n", result.Timezone)
	
	if result.ActivityTimezone != "" && result.ActivityTimezone != result.Timezone {
		fmt.Printf("Activity TZ: %s\n", result.ActivityTimezone)
	}

	if result.ActiveHoursLocal.Start != 0 || result.ActiveHoursLocal.End != 0 {
		fmt.Printf("Active Hours: %s - %s %s\n", 
			formatHour(result.ActiveHoursLocal.Start), formatHour(result.ActiveHoursLocal.End), result.Timezone)
	}

	if result.LunchHoursLocal.Confidence > 0 {
		fmt.Printf("Lunch Break: %s - %s %s (%.0f%% confidence)\n",
			formatHour(result.LunchHoursLocal.Start), formatHour(result.LunchHoursLocal.End), 
			result.Timezone, result.LunchHoursLocal.Confidence*100)
	}

	if result.GeminiSuggestedLocation != "" {
		fmt.Printf("Location: %s (AI-suggested)\n", result.GeminiSuggestedLocation)
	} else if result.LocationName != "" {
		fmt.Printf("Location: %s\n", result.LocationName)
	} else if result.Location != nil {
		fmt.Printf("Location: %.5f, %.5f\n", result.Location.Latitude, result.Location.Longitude)
	}

	fmt.Printf("Method: %s\n", formatMethodName(result.Method))
	fmt.Printf("Confidence: %.2f\n", result.Confidence)
}

func formatHour(decimalHour float64) string {
	hour := int(decimalHour)
	minutes := int((decimalHour - float64(hour)) * 60)
	return fmt.Sprintf("%d:%02d", hour, minutes)
}

func formatMethodName(method string) string {
	methodNames := map[string]string{
		"github_profile":         "GitHub Profile Timezone",
		"location_geocoding":     "Location Field Geocoding",
		"activity_patterns":      "Activity Pattern Analysis",
		"gemini_refined_activity": "AI-Enhanced Activity Analysis",
		"company_heuristic":      "Company-based Inference",
		"email_heuristic":        "Email Domain Analysis",
		"blog_heuristic":         "Blog Domain Analysis",
		"website_gemini_analysis": "Website AI Analysis",
		"gemini_analysis":        "AI Context Analysis",
	}
	if name, exists := methodNames[method]; exists {
		return name
	}
	return method
}