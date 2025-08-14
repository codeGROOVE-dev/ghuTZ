package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/ghutz"
)

var (
	githubToken  = flag.String("github-token", "", "GitHub token for API access (or set GITHUB_TOKEN)")
	geminiAPIKey = flag.String("gemini-key", "", "Gemini API key (or set GEMINI_API_KEY)")
	geminiModel  = flag.String("gemini-model", "gemini-2.5-flash-lite", "Gemini model to use (or set GEMINI_MODEL)")
	mapsAPIKey   = flag.String("maps-key", "", "Google Maps API key (or set GOOGLE_MAPS_API_KEY)")
	gcpProject   = flag.String("gcp-project", "", "GCP project ID (or set GCP_PROJECT)")
	cacheDir     = flag.String("cache-dir", "", "Cache directory (or set CACHE_DIR)")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	version      = flag.Bool("version", false, "Show version")
	histogram    = flag.Bool("histogram", false, "Show activity histogram")
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
	
	// Show histogram by default if activity data is available
	if result.HourlyActivityUTC != nil {
		// Calculate UTC offset from timezone string
		// Try ActivityTimezone first (it has the actual UTC offset), then fall back to Timezone
		utcOffset := 0
		offsetSource := result.ActivityTimezone
		if offsetSource == "" {
			offsetSource = result.Timezone
		}
		
		if strings.HasPrefix(offsetSource, "UTC") {
			offsetStr := strings.TrimPrefix(offsetSource, "UTC")
			if offset, err := strconv.Atoi(offsetStr); err == nil {
				utcOffset = offset
			}
		}
		
		histogramOutput := ghutz.GenerateHistogram(result, result.HourlyActivityUTC, utcOffset)
		fmt.Print(histogramOutput)
	}
}

func printResult(result *ghutz.Result) {
	// Modern header with emoji
	fmt.Printf("\n🌍 GitHub User: %s\n", result.Username)
	fmt.Println(strings.Repeat("─", 50))
	
	// Location and timezone in a cleaner format
	locationStr := ""
	if result.GeminiSuggestedLocation != "" {
		locationStr = result.GeminiSuggestedLocation
	} else if result.LocationName != "" {
		locationStr = result.LocationName
	} else if result.Location != nil {
		locationStr = fmt.Sprintf("%.3f, %.3f", result.Location.Latitude, result.Location.Longitude)
	}
	
	if locationStr != "" {
		fmt.Printf("📍 Location: %s\n", locationStr)
	}
	
	fmt.Printf("🕐 Timezone: %s", result.Timezone)
	if result.ActivityTimezone != "" && result.ActivityTimezone != result.Timezone {
		fmt.Printf("\n   └─ activity suggests %s", result.ActivityTimezone)
	}
	fmt.Println()
	
	// Work schedule with visual indicators
	if result.ActiveHoursLocal.Start != 0 || result.ActiveHoursLocal.End != 0 {
		fmt.Printf("💼 Work Hours: %s → %s", 
			formatHour(result.ActiveHoursLocal.Start), 
			formatHour(result.ActiveHoursLocal.End))
		
		if result.LunchHoursLocal.Confidence > 0 {
			fmt.Printf("\n🍽️  Lunch Break: %s → %s", 
				formatHour(result.LunchHoursLocal.Start),
				formatHour(result.LunchHoursLocal.End))
			if result.LunchHoursLocal.Confidence < 0.7 {
				fmt.Printf(" (uncertain)")
			}
		}
		fmt.Println()
	}
	
	// Detection method as a subtle footer
	fmt.Printf("✨ Detection: %s (%.0f%% confidence)\n", 
		formatMethodName(result.Method), 
		result.Confidence*100)
	fmt.Println()
}

func formatHour(decimalHour float64) string {
	hour := int(decimalHour)
	minutes := int((decimalHour - float64(hour)) * 60)
	return fmt.Sprintf("%d:%02d", hour, minutes)
}

func formatMethodName(method string) string {
	methodNames := map[string]string{
		"github_profile":          "GitHub Profile Timezone",
		"location_geocoding":      "Location Field Geocoding",
		"activity_patterns":       "Activity Pattern Analysis",
		"gemini_refined_activity": "AI-Enhanced Activity Analysis",
		"company_heuristic":       "Company-based Inference",
		"email_heuristic":         "Email Domain Analysis",
		"blog_heuristic":          "Blog Domain Analysis",
		"website_gemini_analysis": "Website AI Analysis",
		"gemini_analysis":         "AI Context Analysis",
	}
	if name, exists := methodNames[method]; exists {
		return name
	}
	return method
}
