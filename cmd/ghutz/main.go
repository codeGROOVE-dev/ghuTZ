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
	forceOffset  = flag.Int("force-offset", 99, "Force a specific UTC offset for visualization (-12 to +14)")
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

	// Removed - Gemini info now shown after activity pattern

	// Print results in CLI format
	printResult(result)

	// Show histogram by default if activity data is available
	if result.HourlyActivityUTC != nil {
		// Use forced offset if specified, otherwise use detected timezone
		displayTimezone := result.Timezone
		displayResult := result

		if *forceOffset >= -12 && *forceOffset <= 14 {
			// Convert forced offset to UTC+/- format
			if *forceOffset >= 0 {
				displayTimezone = fmt.Sprintf("UTC+%d", *forceOffset)
			} else {
				displayTimezone = fmt.Sprintf("UTC%d", *forceOffset)
			}

			// Check if this offset matches one of our analyzed candidates
			foundCandidate := false
			for _, candidate := range result.TimezoneCandidates {
				if int(candidate.Offset) != *forceOffset {
					continue
				}
				// We have data for this timezone! Use the pre-calculated values
				// Find the rank of this candidate (1-based)
				rank := 0
				for i, c := range result.TimezoneCandidates {
					if c.Offset == candidate.Offset {
						rank = i + 1
						break
					}
				}
				fmt.Printf("\n🔧 Using forced offset %s for visualization (analyzed candidate #%d)\n",
					displayTimezone, rank)

				// Create a modified result with the candidate's lunch data
				modifiedResult := *result
				modifiedResult.LunchHoursUTC = ghutz.LunchBreak{
					Start:      candidate.LunchStartUTC,
					End:        candidate.LunchEndUTC,
					Confidence: candidate.LunchConfidence,
				}
				// Keep existing peak and quiet markers as they're still valid
				displayResult = &modifiedResult
				foundCandidate = true
				break
			}

			if !foundCandidate {
				// Not in our candidates - clear all markers as they would be misleading
				fmt.Printf("\n🔧 Using forced offset %s for visualization\n", displayTimezone)
				fmt.Println("    (Note: No lunch/peak markers - offset not in analyzed candidates)")

				modifiedResult := *result
				modifiedResult.LunchHoursUTC = ghutz.LunchBreak{}  // Clear lunch markers
				modifiedResult.PeakProductivity = ghutz.PeakTime{} // Clear peak markers
				modifiedResult.SleepHoursUTC = nil                 // Clear sleep hour markers
				modifiedResult.SleepBucketsUTC = nil               // Clear sleep markers
				displayResult = &modifiedResult
			}
		}

		histogramOutput := ghutz.GenerateHistogram(displayResult, displayResult.HourlyActivityUTC, displayTimezone)
		fmt.Print(histogramOutput)
	}

	// Show Gemini information after activity pattern when verbose
	if *verbose {
		printGeminiInfo(result)
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
	default:
		// No location information available
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

// Removed - no longer needed since we use UTC throughout.

func printWorkSchedule(result *ghutz.Result) {
	if result.ActiveHoursLocal.Start == 0 && result.ActiveHoursLocal.End == 0 {
		return
	}

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
			fmt.Print(" (uncertain)")
		}
	}

	// Add peak productivity time
	if result.PeakProductivity.Count > 0 {
		fmt.Printf("\n🔥 Activity Peak: %s → %s (%s)",
			formatHour(convertUTCToLocal(result.PeakProductivity.Start, result.Timezone)),
			formatHour(convertUTCToLocal(result.PeakProductivity.End, result.Timezone)),
			result.Timezone)
	}

	// Add sleep hours
	if len(result.SleepHoursUTC) > 0 {
		// Convert UTC sleep hours to local hours
		var localSleepHours []int
		for _, utcHour := range result.SleepHoursUTC {
			localHour := int(convertUTCToLocal(float64(utcHour), result.Timezone))
			localSleepHours = append(localSleepHours, localHour)
		}

		// Group consecutive sleep hours into ranges
		type sleepRange struct {
			start, end int
		}

		var ranges []sleepRange
		if len(localSleepHours) > 0 {
			currentStart := localSleepHours[0]
			currentEnd := localSleepHours[0]

			for i := 1; i < len(localSleepHours); i++ {
				hour := localSleepHours[i]

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
					ranges = append(ranges, sleepRange{currentStart, (currentEnd + 1) % 24})
					currentStart = hour
					currentEnd = hour
				}
			}

			// Add the final range
			ranges = append(ranges, sleepRange{currentStart, (currentEnd + 1) % 24})
		}

		// Format and display ranges with timezone
		// Only show sleep periods that are between 4-12 hours (reasonable sleep duration)
		if len(ranges) > 0 {
			var rangeStrings []string
			for _, r := range ranges {
				// Calculate duration of this sleep range
				duration := r.end - r.start
				if duration <= 0 {
					// Handle wraparound (e.g., 22:00 - 6:00)
					duration = (24 - r.start) + r.end
				}
				
				// Only include ranges that are 4-12 hours (reasonable sleep periods)
				if duration >= 4 && duration <= 12 {
					rangeStrings = append(rangeStrings, fmt.Sprintf("%s - %s",
						formatHour(float64(r.start)),
						formatHour(float64(r.end))))
				}
			}
			
			// Only print if we have valid sleep ranges
			if len(rangeStrings) > 0 {
				fmt.Printf("\n💤 Sleep Time:    %s (%s)", strings.Join(rangeStrings, ", "), result.Timezone)
			}
		}
	}

	fmt.Println()
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
	
	// Display data sources inline with method if available
	if len(result.DataSources) > 0 && result.Method == "gemini_analysis" {
		fmt.Printf("✨ Detection:     %s (%.0f%% confidence) using:\n",
			formatMethodName(result.Method),
			displayConfidence)
		for _, source := range result.DataSources {
			fmt.Printf("                  • %s\n", source)
		}
	} else {
		fmt.Printf("✨ Detection:     %s (%.0f%% confidence)\n",
			formatMethodName(result.Method),
			displayConfidence)
	}

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
		"gemini_analysis":         "Activity + AI Context Analysis",
	}
	if name, exists := methodNames[method]; exists {
		return name
	}
	return method
}

func printGeminiInfo(result *ghutz.Result) {
	if result.GeminiPrompt == "" && result.GeminiReasoning == "" {
		return
	}

	fmt.Println()
	fmt.Println("🤖 Gemini Analysis")
	fmt.Println(strings.Repeat("─", 50))

	if result.GeminiPrompt != "" {
		fmt.Println("\n📝 Prompt:")
		fmt.Println(result.GeminiPrompt)
	}

	if result.GeminiReasoning != "" {
		fmt.Println("\n💭 Response:")
		fmt.Println(result.GeminiReasoning)
	}
	fmt.Println()
}

// convertUTCToLocal converts a UTC hour (float) to local time using Go's timezone database.
func convertUTCToLocal(utcHour float64, timezone string) float64 {
	if loc, err := time.LoadLocation(timezone); err == nil {
		// Use Go's native timezone conversion
		// We use a reference date in the middle of the year to get consistent DST behavior
		refDate := time.Date(2024, 8, 15, 0, 0, 0, 0, time.UTC)
		hour := int(utcHour)
		minutes := int((utcHour - float64(hour)) * 60)
		utcTime := refDate.Add(time.Duration(hour)*time.Hour + time.Duration(minutes)*time.Minute)
		localTime := utcTime.In(loc)
		return float64(localTime.Hour()) + float64(localTime.Minute())/60.0
	}
	// Fallback for UTC+/- format
	offset := calculateTimezoneOffset(timezone)
	return math.Mod(utcHour+float64(offset)+24, 24)
}
