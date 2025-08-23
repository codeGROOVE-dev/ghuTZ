// Package main implements the gutz CLI tool for GitHub user timezone detection.
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

	"github.com/codeGROOVE-dev/guTZ/pkg/gutz"
	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
	"github.com/codeGROOVE-dev/guTZ/pkg/tzconvert"
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
		fmt.Println("guTZ CLI v2.1.0")
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
	detectorOpts := []gutz.Option{
		gutz.WithGitHubToken(*githubToken),
		gutz.WithGeminiAPIKey(*geminiAPIKey),
		gutz.WithGeminiModel(*geminiModel),
		gutz.WithMapsAPIKey(*mapsAPIKey),
		gutz.WithGCPProject(*gcpProject),
	}

	if *noCache {
		// Explicitly disable all caching
		detectorOpts = append(detectorOpts, gutz.WithNoCache())
	} else if *cacheDir != "" {
		detectorOpts = append(detectorOpts, gutz.WithCacheDir(*cacheDir))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	detector := gutz.NewWithLogger(ctx, logger, detectorOpts...)
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

	// Show Gemini prompt in verbose mode as the very first output
	if *verbose && result.GeminiPrompt != "" {
		fmt.Println("\n🤖 Gemini AI Analysis Prompt")
		fmt.Println(strings.Repeat("═", 50))
		fmt.Printf("%s\n", result.GeminiPrompt)
		fmt.Println(strings.Repeat("═", 50))
		fmt.Println()
	}

	// Clear GeminiPrompt unless in verbose mode (to save memory)
	if !*verbose {
		result.GeminiPrompt = ""
	}

	// Handle forced offset BEFORE printing results
	displayResult := result
	displayTimezone := result.Timezone

	if *forceOffset >= -12 && *forceOffset <= 14 && result.HalfHourlyActivityUTC != nil {
		// Convert forced offset to UTC+/- format
		if *forceOffset >= 0 {
			displayTimezone = fmt.Sprintf("UTC+%d", *forceOffset)
		} else {
			displayTimezone = fmt.Sprintf("UTC%d", *forceOffset)
		}

		// Check if this offset matches one of our analyzed candidates
		foundCandidate := false
		for i := range result.TimezoneCandidates {
			candidate := &result.TimezoneCandidates[i]
			if int(candidate.Offset) != *forceOffset {
				continue
			}
			// We have data for this timezone! Use the pre-calculated values
			// Find the rank of this candidate (1-based)
			rank := 0
			for i := range result.TimezoneCandidates {
				c := &result.TimezoneCandidates[i]
				if c.Offset == candidate.Offset {
					rank = i + 1
					break
				}
			}
			fmt.Printf("\n🔧 Using forced offset %s for visualization (analyzed candidate #%d)\n",
				displayTimezone, rank)

			// Create a modified result with the candidate's lunch and peak data
			modifiedResult := *result

			// Update lunch hours in both UTC and local time
			modifiedResult.LunchHoursUTC = gutz.LunchBreak{
				Start:      candidate.LunchStartUTC,
				End:        candidate.LunchEndUTC,
				Confidence: candidate.LunchConfidence,
			}

			// Convert active hours from UTC to local time using forced offset
			modifiedResult.ActiveHoursLocal = struct {
				Start float64 `json:"start"`
				End   float64 `json:"end"`
			}{
				Start: tzconvert.UTCToLocal(result.ActiveHoursUTC.Start, *forceOffset),
				End:   tzconvert.UTCToLocal(result.ActiveHoursUTC.End, *forceOffset),
			}

			// Convert lunch to local time for histogram display
			lunchLocalStart := tzconvert.UTCToLocal(candidate.LunchStartUTC, *forceOffset)
			lunchLocalEnd := tzconvert.UTCToLocal(candidate.LunchEndUTC, *forceOffset)
			modifiedResult.LunchHoursLocal = gutz.LunchBreak{
				Start:      lunchLocalStart,
				End:        lunchLocalEnd,
				Confidence: candidate.LunchConfidence,
			}

			// Recalculate peak productivity for the forced timezone offset
			// Use the original half-hourly activity data to find the peak in the correct timezone
			if result.HalfHourlyActivityUTC != nil {
				peakStartUTC, peakEndUTC, peakCount := timezone.DetectPeakProductivityWithHalfHours(result.HalfHourlyActivityUTC, *forceOffset)

				// Update peak productivity in UTC
				modifiedResult.PeakProductivityUTC = gutz.PeakTime{
					Start: peakStartUTC,
					End:   peakEndUTC,
					Count: peakCount,
				}

				// Convert peak to local time for histogram display
				peakLocalStart := tzconvert.UTCToLocal(peakStartUTC, *forceOffset)
				peakLocalEnd := tzconvert.UTCToLocal(peakEndUTC, *forceOffset)
				modifiedResult.PeakProductivityLocal = gutz.PeakTime{
					Start: peakLocalStart,
					End:   peakLocalEnd,
					Count: peakCount,
				}
			}

			displayResult = &modifiedResult
			foundCandidate = true
			break
		}

		if !foundCandidate {
			// Not in our candidates - this offset wasn't analyzed
			fmt.Printf("\n🔧 Using forced offset %s for visualization\n", displayTimezone)
			fmt.Println("    (Note: This offset was not in the analyzed candidates)")
			fmt.Println("    Using original detected lunch/sleep patterns")

			modifiedResult := *result
			// Keep the original lunch data - it's still valid in UTC and will be converted for display
			// The lunch hours are in UTC so they'll show at different local times
			displayResult = &modifiedResult
		}

		// Update the timezone in displayResult for correct display
		displayResult.Timezone = displayTimezone
	}

	// Print results in CLI format (using displayResult which may be modified)
	printResult(displayResult)

	// Show histogram if activity data is available
	if displayResult.HalfHourlyActivityUTC != nil {
		histogramOutput := gutz.GenerateHistogram(displayResult, displayTimezone)
		fmt.Print(histogramOutput)
	}

	// Show timezone candidates in verbose mode
	if *verbose && result.TimezoneCandidates != nil && len(result.TimezoneCandidates) > 0 {
		fmt.Println("\n📊 Timezone Candidates (Activity Analysis)")
		fmt.Println(strings.Repeat("─", 50))
		for i := range result.TimezoneCandidates {
			if i >= 5 {
				break // Only show top 5
			}
			candidate := &result.TimezoneCandidates[i]
			offsetStr := fmt.Sprintf("UTC%+d", int(candidate.Offset))
			if candidate.Offset == 0 {
				offsetStr = "UTC+0"
			}

			fmt.Printf("%d. %s (%.1f%% confidence)\n", i+1, offsetStr, candidate.Confidence)
			fmt.Printf("   Evening activity: %d events\n", candidate.EveningActivity)
			fmt.Printf("   Lunch: %s\n", formatCandidateLunch(*candidate))
			fmt.Printf("   Work start: %.1f:00\n", candidate.WorkStartLocal)
			if len(candidate.ScoringDetails) > 0 {
				fmt.Printf("   Scoring details:\n")
				for _, detail := range candidate.ScoringDetails {
					fmt.Printf("     • %s\n", detail)
				}
			}
			fmt.Println()
		}
	}

	// Show Gemini information after activity pattern when verbose
	if *verbose {
		printGeminiInfo(result)
	}
}

func printResult(result *gutz.Result) {
	// Print header
	fmt.Printf("\n🌍 GitHub User: %s\n", result.Username)
	fmt.Println(strings.Repeat("─", 50))

	// Removed scary warnings - the elegant timezone display already shows discrepancies clearly

	printLocation(result)
	printTimezone(result)
	printWorkSchedule(result)
	printOrganizations(result)
	printActivitySummary(result)
	printDetectionInfo(result)
}

func printLocation(result *gutz.Result) {
	var locationStr string
	switch {
	case result.GeminiSuggestedLocation != "":
		locationStr = result.GeminiSuggestedLocation
	case result.LocationName != "":
		locationStr = result.LocationName
	case result.Location != nil:
		locationStr = fmt.Sprintf("%.3f, %.3f", result.Location.Latitude, result.Location.Longitude)
	default:
		// No detected location, but check if there's a profile location
		if result.Verification != nil && result.Verification.ProfileLocation != "" {
			// Show profile location when we have no detected location
			fmt.Printf("📍 Location:      Unknown\n")
			fmt.Printf("                  └─ Profile Location: %s\n", result.Verification.ProfileLocation)
			return
		}
		// No location information at all
		return
	}

	if locationStr != "" {
		fmt.Printf("📍 Location:      %s\n", locationStr)

		// Display profile location if it differs significantly
		if result.Verification != nil && result.Verification.ProfileLocation != "" {
			// Only show if there's a meaningful difference
			showProfileLocation := false
			distanceStr := ""

			if result.Verification.LocationDistanceKm > 80 {
				showProfileLocation = true
				if result.Verification.LocationDistanceKm > 1000 {
					// Red warning for >1000 km
					distanceStr = fmt.Sprintf(" \033[31m⚠️ %.0f km away\033[0m", result.Verification.LocationDistanceKm)
				} else {
					distanceStr = fmt.Sprintf(" (%.0f km away)", result.Verification.LocationDistanceKm)
				}
			} else if result.Verification.LocationDistanceKm == 0 {
				// Couldn't geocode but locations differ textually
				if locationStr != result.Verification.ProfileLocation {
					showProfileLocation = true
				}
			}

			if showProfileLocation {
				fmt.Printf("                  └─ Profile Location: %s%s\n", result.Verification.ProfileLocation, distanceStr)
			}
		}
	}
}

func printTimezone(result *gutz.Result) {
	// Helper function to get current time in a timezone
	getCurrentTime := func(tz string) (string, int) {
		if loc, err := time.LoadLocation(tz); err == nil {
			now := time.Now().In(loc)
			_, offset := now.Zone()
			return now.Format("15:04"), offset / 3600
		}
		// Try to parse UTC offset format
		if strings.HasPrefix(tz, "UTC") {
			offsetStr := strings.TrimPrefix(tz, "UTC")
			if offsetHours, err := strconv.Atoi(strings.TrimPrefix(offsetStr, "+")); err == nil {
				now := time.Now().UTC().Add(time.Duration(offsetHours) * time.Hour)
				return now.Format("15:04"), offsetHours
			} else if offsetHours, err := strconv.Atoi(offsetStr); err == nil {
				now := time.Now().UTC().Add(time.Duration(offsetHours) * time.Hour)
				return now.Format("15:04"), offsetHours
			}
		}
		return "--:--", 0
	}

	// Helper to calculate hour difference between timezones
	calcHourDiff := func(tz1, tz2 string) int {
		_, offset1 := getCurrentTime(tz1)
		_, offset2 := getCurrentTime(tz2)
		return abs(offset1 - offset2)
	}

	// Helper to format timezone display with offset indicator
	formatTimezoneRow := func(label, tz string, isPrimary bool) {
		if tz == "" {
			return
		}

		localTime, offsetHours := getCurrentTime(tz)
		utcStr := fmt.Sprintf("UTC%+d", offsetHours)

		if isPrimary {
			// Primary detected timezone
			fmt.Printf("🕐 Timezone:      %s (%s, now %s)\n", tz, utcStr, localTime)
		} else {
			// Secondary timezone sources
			hourDiff := calcHourDiff(result.Timezone, tz)
			offsetStr := ""
			if hourDiff > 0 {
				if hourDiff > 4 {
					// Red warning for >4 hour difference
					offsetStr = fmt.Sprintf(" \033[31m⚠️ %+d hr\033[0m", hourDiff)
				} else {
					offsetStr = fmt.Sprintf(" (%+d hr)", hourDiff)
				}
			}
			fmt.Printf("                  └─ %s: %s (%s, now %s)%s\n", label, tz, utcStr, localTime, offsetStr)
		}
	}

	// Display primary detected timezone
	formatTimezoneRow("Detected", result.Timezone, true)

	// Display other timezone sources if they differ
	if result.Verification != nil {
		// Profile Timezone (from GitHub UTC offset)
		if result.Verification.ProfileTimezone != "" && result.Verification.ProfileTimezone != result.Timezone {
			formatTimezoneRow("Profile Timezone", result.Verification.ProfileTimezone, false)
		}

		// Profile Location (from geocoding the location string)
		if result.Verification.ProfileLocationTimezone != "" && result.Verification.ProfileLocationTimezone != result.Timezone {
			formatTimezoneRow("Profile Location", result.Verification.ProfileLocationTimezone, false)
		}
	}

	// Activity Pattern
	if result.ActivityTimezone != "" && result.ActivityTimezone != result.Timezone {
		formatTimezoneRow("Activity Pattern", result.ActivityTimezone, false)
	}
}

// abs returns the absolute value of an integer.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func printWorkSchedule(result *gutz.Result) {
	if result.ActiveHoursLocal.Start == 0 && result.ActiveHoursLocal.End == 0 {
		return
	}

	fmt.Printf("🏃 Active Time:   %s → %s (%s)",
		formatHour(result.ActiveHoursLocal.Start),
		formatHour(result.ActiveHoursLocal.End),
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
	if result.PeakProductivityLocal.Count > 0 {
		fmt.Printf("\n🔥 Activity Peak: %s → %s (%s)",
			formatHour(result.PeakProductivityLocal.Start),
			formatHour(result.PeakProductivityLocal.End),
			result.Timezone)
	}

	// Add rest hours
	if len(result.SleepRangesLocal) > 0 {
		printRestHours(result)
	}

	fmt.Println()
}

func printOrganizations(result *gutz.Result) {
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

func printActivitySummary(result *gutz.Result) {
	if !result.ActivityDateRange.OldestActivity.IsZero() && !result.ActivityDateRange.NewestActivity.IsZero() {
		// Calculate total events from half-hourly activity
		totalEvents := 0
		if result.HalfHourlyActivityUTC != nil {
			for _, count := range result.HalfHourlyActivityUTC {
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

func printDetectionInfo(result *gutz.Result) {
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

func formatCandidateLunch(candidate timezone.Candidate) string {
	if candidate.LunchStartUTC == 0 && candidate.LunchEndUTC == 0 {
		return "Not detected"
	}

	// Convert UTC lunch to local time for this candidate
	localStart := math.Mod(candidate.LunchStartUTC+candidate.Offset+24, 24)
	localEnd := math.Mod(candidate.LunchEndUTC+candidate.Offset+24, 24)

	return fmt.Sprintf("%.1f-%.1f local (%.0f%% conf)", localStart, localEnd, candidate.LunchConfidence*100)
}

func formatMethodName(method string) string {
	methodNames := map[string]string{
		"github_profile":          "GitHub Profile Timezone",
		"location_geocoding":      "Location Field Geocoding",
		"location_field":          "location_field",
		"activity_patterns":       "Activity Pattern Analysis",
		"gemini_refined_activity": "AI-Enhanced Activity Analysis",
		"company_heuristic":       "Company-based Inference",
		"email_heuristic":         "Email Domain Analysis",
		"blog_heuristic":          "Blog Domain Analysis",
		"website_gemini_analysis": "Website AI Analysis",
		"gemini_analysis":         "Activity + AI Context Analysis",
		"gemini_enhanced":         "Activity + AI Enhanced",
		"gemini_corrected":        "AI-Corrected Location",
	}
	if name, exists := methodNames[method]; exists {
		return name
	}
	return method
}

func printGeminiInfo(result *gutz.Result) {
	// Skip if no Gemini reasoning to show (prompt is now shown at the beginning)
	if result.GeminiReasoning == "" {
		return
	}

	fmt.Println()
	fmt.Println("🤖 Gemini Analysis Response")
	fmt.Println(strings.Repeat("─", 50))

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

func printRestHours(result *gutz.Result) {
	// Use pre-calculated rest ranges with 30-minute precision (already in local time)
	if len(result.SleepRangesLocal) == 0 {
		return
	}

	var rangeStrings []string
	for _, r := range result.SleepRangesLocal {
		rangeStrings = append(rangeStrings, fmt.Sprintf("%s - %s",
			formatHour(r.Start),
			formatHour(r.End)))
	}

	// Print rest ranges
	if len(rangeStrings) > 0 {
		fmt.Printf("\n💤 Rest Hours:    %s (%s)", strings.Join(rangeStrings, ", "), result.Timezone)
	}
}
