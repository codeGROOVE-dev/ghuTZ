// Package main implements the ghutz CLI tool for GitHub user timezone detection.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
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
	noCache      = flag.Bool("no-cache", false, "Disable caching")
	verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	version      = flag.Bool("version", false, "Show version")
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
	level := slog.LevelError
	if *verbose {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))

	// Get tokens from environment if not provided as flags
	if *githubToken == "" {
		*githubToken = os.Getenv("GITHUB_TOKEN")
		// If still empty, try to get from gh CLI
		if *githubToken == "" {
			if token, err := exec.Command("gh", "auth", "token").Output(); err == nil {
				*githubToken = strings.TrimSpace(string(token))
			}
		}
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

	if *noCache {
		// Disable cache by setting an empty cache dir
		detectorOpts = append(detectorOpts, ghutz.WithCacheDir(""))
	} else if *cacheDir != "" {
		detectorOpts = append(detectorOpts, ghutz.WithCacheDir(*cacheDir))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	detector := ghutz.NewWithLogger(ctx, logger, detectorOpts...)
	defer func() {
		if err := detector.Close(); err != nil {
			logger.Error("Failed to close detector", "error", err)
		}
	}()

	result, err := detector.Detect(ctx, username)
	if err != nil {
		cancel() // Ensure context is cancelled before exit
		logger.Error("Detection failed", "error", err)
		return
	}

	// When verbose, show Gemini prompt and reasoning before results
	if *verbose && result.GeminiPrompt != "" {
		fmt.Printf("\n🤖 Gemini Prompt:\n")
		fmt.Println(strings.Repeat("─", 50))
		fmt.Printf("%s\n\n", result.GeminiPrompt)

		if result.GeminiReasoning != "" {
			fmt.Printf("🧠 Gemini Reasoning:\n")
			fmt.Println(strings.Repeat("─", 50))
			fmt.Printf("%s\n\n", result.GeminiReasoning)
		}
	}

	// Print results in CLI format
	printResult(result)

	// Show histogram by default if activity data is available
	if result.HourlyActivityUTC != nil {
		histogramOutput := ghutz.GenerateHistogram(result, result.HourlyActivityUTC, result.Timezone)
		fmt.Print(histogramOutput)
	}
}

func printResult(result *ghutz.Result) {
	printHeader(result)
	printLocation(result)
	printTimezone(result)
	printWorkSchedule(result)
	printOrganizations(result)
	printActivitySummary(result)
	printDetectionInfo(result)
}

func printHeader(result *ghutz.Result) {
	fmt.Printf("\n🌍 GitHub User: %s\n", result.Username)
	fmt.Println(strings.Repeat("─", 50))
}

func printLocation(result *ghutz.Result) {
	locationStr := ""
	switch {
	case result.GeminiSuggestedLocation != "":
		locationStr = result.GeminiSuggestedLocation
	case result.LocationName != "":
		locationStr = result.LocationName
	case result.Location != nil:
		locationStr = fmt.Sprintf("%.3f, %.3f", result.Location.Latitude, result.Location.Longitude)
	}

	if locationStr != "" {
		fmt.Printf("📍 Location:      %s\n", locationStr)
	}
}

func printTimezone(result *ghutz.Result) {
	tzName := result.Timezone
	
	// Try to load the timezone
	if loc, err := time.LoadLocation(tzName); err == nil {
		now := time.Now().In(loc)
		_, offset := now.Zone()
		offsetHours := offset / 3600
		var utcOffset string
		if offsetHours >= 0 {
			utcOffset = fmt.Sprintf("UTC+%d", offsetHours)
		} else {
			utcOffset = fmt.Sprintf("UTC%d", offsetHours)
		}
		currentLocal := now.Format("15:04")
		fmt.Printf("🕐 Timezone:      %s (%s, now %s)", tzName, utcOffset, currentLocal)
	} else {
		// Fallback for UTC+/- format
		fmt.Printf("🕐 Timezone:      %s", result.Timezone)
	}

	if result.ActivityTimezone != "" && result.ActivityTimezone != result.Timezone {
		fmt.Printf("\n                  └─ activity suggests %s", result.ActivityTimezone)
	}
	fmt.Println()
}

// Removed - no longer needed since we use UTC throughout

func printWorkSchedule(result *ghutz.Result) {
	if result.ActiveHoursLocal.Start != 0 || result.ActiveHoursLocal.End != 0 {
		fmt.Printf("🏃 Active Time:   %s → %s (%s)",
			formatHour(convertUTCToLocal(result.ActiveHoursLocal.Start, result.Timezone)),
			formatHour(convertUTCToLocal(result.ActiveHoursLocal.End, result.Timezone)),
			result.Timezone)

		if result.LunchHoursUTC.Confidence > 0 {
			fmt.Printf("\n🍽️  Lunch Break:   %s → %s (%s)",
				formatHour(convertUTCToLocal(result.LunchHoursUTC.Start, result.Timezone)),
				formatHour(convertUTCToLocal(result.LunchHoursUTC.End, result.Timezone)),
				result.Timezone)
			if result.LunchHoursUTC.Confidence < 0.7 {
				fmt.Printf(" (uncertain)")
			}
		}

		// Add peak productivity time
		if result.PeakProductivity.Count > 0 {
			fmt.Printf("\n🔥 Activity Peak: %s → %s (%s)",
				formatHour(convertUTCToLocal(result.PeakProductivity.Start, result.Timezone)),
				formatHour(convertUTCToLocal(result.PeakProductivity.End, result.Timezone)),
				result.Timezone)
		}

		// Add quiet hours (sleep/family time)
		if len(result.QuietHoursUTC) > 0 {
			// Convert UTC quiet hours to local hours
			var localQuietHours []int
			for _, utcHour := range result.QuietHoursUTC {
				localHour := int(convertUTCToLocal(float64(utcHour), result.Timezone))
				localQuietHours = append(localQuietHours, localHour)
			}

			// Group consecutive quiet hours into ranges
			type quietRange struct {
				start, end int
			}

			var ranges []quietRange
			if len(localQuietHours) > 0 {
				currentStart := localQuietHours[0]
				currentEnd := localQuietHours[0]

				for i := 1; i < len(localQuietHours); i++ {
					hour := localQuietHours[i]

					// Check if this hour is consecutive (handle day wraparound)
					isConsecutive := false
					if hour == (currentEnd+1)%24 {
						isConsecutive = true
					}
					// Special case: if current end is 23 and next hour is 0
					if currentEnd == 23 && hour == 0 {
						isConsecutive = true
					}

					if isConsecutive {
						currentEnd = hour
					} else {
						// End current range and start new one
						ranges = append(ranges, quietRange{currentStart, (currentEnd + 1) % 24})
						currentStart = hour
						currentEnd = hour
					}
				}

				// Add the final range
				ranges = append(ranges, quietRange{currentStart, (currentEnd + 1) % 24})
			}

			// Format and display ranges with timezone
			if len(ranges) > 0 {
				var rangeStrings []string
				for _, r := range ranges {
					rangeStrings = append(rangeStrings, fmt.Sprintf("%s - %s",
						formatHour(float64(r.start)),
						formatHour(float64(r.end))))
				}
				fmt.Printf("\n💤 Quiet Time:    %s (%s)", strings.Join(rangeStrings, ", "), result.Timezone)
			}
		}

		fmt.Println()
	}
}

func printOrganizations(result *ghutz.Result) {
	if len(result.TopOrganizations) > 0 {
		colors := []string{
			"\033[34m", // Blue for 1st
			"\033[33m", // Yellow for 2nd
			"\033[31m", // Red for 3rd
		}
		grey := "\033[90m" // Grey for others

		// Build the organizations string with colors and counts
		var orgsStr strings.Builder
		for i, org := range result.TopOrganizations {
			if i > 0 {
				orgsStr.WriteString(", ")
			}

			// Choose color based on rank
			color := grey
			if i < 3 {
				color = colors[i]
			}

			// Format: name (colorized count)
			orgsStr.WriteString(fmt.Sprintf("%s (%s%d\033[0m)", org.Name, color, org.Count))
		}

		// Always display organizations on a single line
		fmt.Printf("🏢 Orgs:          %s\n", orgsStr.String())
	}
}

func printActivitySummary(result *ghutz.Result) {
	if !result.ActivityDateRange.OldestActivity.IsZero() && !result.ActivityDateRange.NewestActivity.IsZero() {
		// Calculate total events from hourly activity
		totalEvents := 0
		if result.HourlyActivityUTC != nil {
			for _, count := range result.HourlyActivityUTC {
				totalEvents += count
			}
		}

		// Format the date range
		oldestStr := result.ActivityDateRange.OldestActivity.Format("2006-01-02")
		newestStr := result.ActivityDateRange.NewestActivity.Format("2006-01-02")
		
		if totalEvents > 0 {
			fmt.Printf("📊 Activity:      %d events from %s to %s",
				totalEvents,
				oldestStr,
				newestStr)
		} else {
			fmt.Printf("📊 Activity:      %s to %s",
				oldestStr,
				newestStr)
		}

		if result.ActivityDateRange.TotalDays > 0 {
			fmt.Printf(" (%d days)", result.ActivityDateRange.TotalDays)
		}
		fmt.Println()
	}
}

func printDetectionInfo(result *ghutz.Result) {
	// Our confidence scores are now in a 0-50 range typically
	// For the winning candidate, show it as 85-95% range
	// This matches how we display to Gemini
	displayConfidence := 85.0 + math.Min(10, result.Confidence/4.0)
	displayConfidence = math.Min(95, displayConfidence)
	fmt.Printf("✨ Detection:     %s (%.0f%% confidence)\n",
		formatMethodName(result.Method),
		displayConfidence)
	fmt.Println()
}

func formatHour(decimalHour float64) string {
	hour := int(decimalHour)
	minutes := int((decimalHour - float64(hour)) * 60)
	return fmt.Sprintf("%d:%02d", hour, minutes)
}

// calculateTimezoneOffset calculates the UTC offset in hours for a given timezone.
func calculateTimezoneOffset(timezone string) int {
	if strings.HasPrefix(timezone, "UTC") {
		offsetStr := strings.TrimPrefix(timezone, "UTC")
		if offsetStr == "" {
			return 0
		}
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			return offset
		}
	} else if loc, err := time.LoadLocation(timezone); err == nil {
		now := time.Now().In(loc)
		_, offset := now.Zone()
		return offset / 3600
	}
	return 0
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
		switch {
		case h == 0:
			return fmt.Sprintf("%dm ago", m)
		case m == 0:
			return fmt.Sprintf("%dh ago", h)
		default:
			return fmt.Sprintf("%dh%dm ago", h, m)
		}
	} else {
		// In the future
		switch {
		case h == 0:
			return fmt.Sprintf("in %dm", m)
		case m == 0:
			return fmt.Sprintf("in %dh", h)
		default:
			// For future times, keep it simple
			return fmt.Sprintf("in %dh", h+(m+29)/60) // Round to nearest hour
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

// convertUTCToLocal converts a UTC hour (float) to local time using Go's timezone database
func convertUTCToLocal(utcHour float64, timezone string) float64 {
	if loc, err := time.LoadLocation(timezone); err == nil {
		// Use Go's native timezone conversion
		// We use a reference date in the middle of the year to get consistent DST behavior
		refDate := time.Date(2024, 8, 15, 0, 0, 0, 0, time.UTC)
		hour := int(utcHour)
		min := int((utcHour - float64(hour)) * 60)
		utcTime := refDate.Add(time.Duration(hour)*time.Hour + time.Duration(min)*time.Minute)
		localTime := utcTime.In(loc)
		return float64(localTime.Hour()) + float64(localTime.Minute())/60.0
	}
	// Fallback for UTC+/- format
	offset := calculateTimezoneOffset(timezone)
	return math.Mod(utcHour+float64(offset)+24, 24)
}
