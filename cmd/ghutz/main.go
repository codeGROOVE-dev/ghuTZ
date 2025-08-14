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
	
	// Timezone with GMT offset and current local time
	tzName := result.Timezone
	gmtOffset := ""
	currentLocal := ""
	
	// Try to load the timezone
	if loc, err := time.LoadLocation(tzName); err == nil {
		now := time.Now().In(loc)
		_, offset := now.Zone()
		offsetHours := offset / 3600
		if offsetHours >= 0 {
			gmtOffset = fmt.Sprintf("GMT+%d", offsetHours)
		} else {
			gmtOffset = fmt.Sprintf("GMT%d", offsetHours)
		}
		currentLocal = now.Format("15:04")
		fmt.Printf("🕐 Timezone: %s (%s, now %s)", tzName, gmtOffset, currentLocal)
	} else {
		// Fallback for UTC+/- format
		fmt.Printf("🕐 Timezone: %s", result.Timezone)
	}
	
	if result.ActivityTimezone != "" && result.ActivityTimezone != result.Timezone {
		// Convert UTC format to GMT format for consistency
		activityTz := result.ActivityTimezone
		if strings.HasPrefix(activityTz, "UTC") {
			offset := strings.TrimPrefix(activityTz, "UTC")
			if offset == "" {
				activityTz = "GMT"
			} else if strings.HasPrefix(offset, "+") {
				activityTz = "GMT" + offset
			} else {
				// Negative offset already has minus sign
				activityTz = "GMT" + offset
			}
		}
		fmt.Printf("\n   └─ activity suggests %s", activityTz)
	}
	fmt.Println()
	
	// Work schedule with relative time indicators
	if result.ActiveHoursLocal.Start != 0 || result.ActiveHoursLocal.End != 0 {
		workStartRelative := ""
		workEndRelative := ""
		
		// Calculate relative times if we have a valid timezone
		if loc, err := time.LoadLocation(tzName); err == nil {
			now := time.Now().In(loc)
			currentTime := float64(now.Hour()) + float64(now.Minute())/60.0
			
			// Calculate relative time to work start
			hoursToStart := result.ActiveHoursLocal.Start - currentTime
			if hoursToStart < 0 {
				hoursToStart += 24 // Handle next day
			}
			if hoursToStart > 12 {
				// It was yesterday/earlier today
				hoursToStart = hoursToStart - 24
			}
			
			// Calculate relative time to work end
			hoursToEnd := result.ActiveHoursLocal.End - currentTime
			if hoursToEnd < 0 && result.ActiveHoursLocal.End > result.ActiveHoursLocal.Start {
				hoursToEnd += 24 // Handle next day
			}
			if hoursToEnd > 12 {
				// It was yesterday/earlier today
				hoursToEnd = hoursToEnd - 24
			}
			
			// Format relative times tersely
			workStartRelative = formatRelativeTime(hoursToStart)
			workEndRelative = formatRelativeTime(hoursToEnd)
		}
		
		fmt.Printf("🏃 Active Time: %s → %s", 
			formatHour(result.ActiveHoursLocal.Start), 
			formatHour(result.ActiveHoursLocal.End))
		
		if workStartRelative != "" && workEndRelative != "" {
			fmt.Printf(" (%s → %s)", workStartRelative, workEndRelative)
		}
		
		if result.LunchHoursLocal.Confidence > 0 {
			fmt.Printf("\n🍽️  Lunch Break: %s → %s", 
				formatHour(result.LunchHoursLocal.Start),
				formatHour(result.LunchHoursLocal.End))
			if result.LunchHoursLocal.Confidence < 0.7 {
				fmt.Printf(" (uncertain)")
			}
		}
		
		// Add peak productivity time
		if result.PeakProductivity.Count > 0 {
			fmt.Printf("\n🔥 Peak Time: %s → %s",
				formatHour(result.PeakProductivity.Start),
				formatHour(result.PeakProductivity.End))
		}
		
		// Add quiet hours (sleep/family time)
		if len(result.QuietHoursUTC) > 0 {
			// Convert quiet hours from UTC to local
			quietStart := -1
			quietEnd := -1
			
			// Find the continuous range of quiet hours
			if len(result.QuietHoursUTC) > 0 {
				// Calculate UTC offset from timezone
				utcOffset := 0
				if loc, err := time.LoadLocation(tzName); err == nil {
					now := time.Now().In(loc)
					_, offset := now.Zone()
					utcOffset = offset / 3600
				}
				
				// Convert first and last quiet hour to local time
				quietStart = (result.QuietHoursUTC[0] + utcOffset + 24) % 24
				quietEnd = (result.QuietHoursUTC[len(result.QuietHoursUTC)-1] + utcOffset + 24) % 24
				
				// Add one hour to end to show the end of the quiet period
				quietEnd = (quietEnd + 1) % 24
			}
			
			if quietStart >= 0 && quietEnd >= 0 {
				fmt.Printf("\n💤 Quiet Time: %s → %s",
					formatHour(float64(quietStart)),
					formatHour(float64(quietEnd)))
			}
		}
		
		fmt.Println()
	}
	
	// Show top organizations if available
	if len(result.TopOrganizations) > 0 {
		fmt.Print("🏢 Organizations: ")
		for i, org := range result.TopOrganizations {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s (%d)", org.Name, org.Count)
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

func formatRelativeTime(hours float64) string {
	if hours < -12 || hours > 12 {
		return "" // Too far in past/future
	}
	
	absHours := hours
	if absHours < 0 {
		absHours = -absHours
	}
	
	// Convert to minutes for better precision
	totalMinutes := int(absHours * 60)
	h := totalMinutes / 60
	m := totalMinutes % 60
	
	// Format tersely
	if hours < 0 {
		// In the past
		if h == 0 {
			return fmt.Sprintf("%dm ago", m)
		} else if m == 0 {
			return fmt.Sprintf("%dh ago", h)
		} else {
			return fmt.Sprintf("%dh%dm ago", h, m)
		}
	} else {
		// In the future
		if h == 0 {
			return fmt.Sprintf("in %dm", m)
		} else if m == 0 {
			return fmt.Sprintf("in %dh", h)
		} else {
			// For future times, keep it simple
			return fmt.Sprintf("in %dh", h + (m + 29) / 60) // Round to nearest hour
		}
	}
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
