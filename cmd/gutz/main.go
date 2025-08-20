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
	
	if *forceOffset >= -12 && *forceOffset <= 14 && result.HourlyActivityUTC != nil {
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

			// Create a modified result with the candidate's lunch data
			modifiedResult := *result
			modifiedResult.LunchHoursUTC = gutz.LunchBreak{
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
	if displayResult.HourlyActivityUTC != nil {
		histogramOutput := gutz.GenerateHistogram(displayResult, displayResult.HourlyActivityUTC, displayTimezone)
		fmt.Print(histogramOutput)
	}

	// Show timezone candidates in verbose mode  
	if *verbose && result.TimezoneCandidates != nil && len(result.TimezoneCandidates) > 0 {
		fmt.Println("\n📊 Timezone Candidates (Activity Analysis)")
		fmt.Println(strings.Repeat("─", 50))
		for i, candidate := range result.TimezoneCandidates {
			if i >= 5 {
				break // Only show top 5
			}
			offsetStr := fmt.Sprintf("UTC%+d", int(candidate.Offset))
			if candidate.Offset == 0 {
				offsetStr = "UTC+0"
			}
			
			fmt.Printf("%d. %s (%.1f%% confidence)\n", i+1, offsetStr, candidate.Confidence)
			fmt.Printf("   Evening activity: %d events\n", candidate.EveningActivity)
			fmt.Printf("   Lunch: %s\n", formatCandidateLunch(candidate))
			fmt.Printf("   Work start: %d:00\n", candidate.WorkStartLocal)
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

	// Show combined analysis warning when both systems detect issues
	hasSuspiciousLocation := result.Verification != nil && result.Verification.LocationMismatch == "major"
	hasSuspiciousTimezone := result.Verification != nil && result.Verification.TimezoneMismatch == "major"
	hasGeminiSuspicion := result.GeminiSuspiciousMismatch
	
	if (hasSuspiciousLocation || hasSuspiciousTimezone) && hasGeminiSuspicion {
		// Both detection systems agree something is off
		fmt.Printf("\033[91m🔍 DETECTION ALERT: Multiple anomalies detected\033[0m\n")
		fmt.Println(strings.Repeat("─", 50))
	} else if hasGeminiSuspicion && result.GeminiMismatchReason != "" &&
	          (result.Verification == nil || (result.Verification.ClaimedLocation == "" && result.Verification.ClaimedTimezone == "")) {
		// Only Gemini detected something (no claims to verify against)
		fmt.Printf("\033[33m⚠️  AI ANALYSIS: %s\033[0m\n", result.GeminiMismatchReason)
		fmt.Println(strings.Repeat("─", 50))
	}

	printLocation(result)
	printTimezone(result)
	printWorkSchedule(result)
	printOrganizations(result)
	printActivitySummary(result)
	printDetectionInfo(result)
}

// extractMainLocation extracts the main city/state from a location string
// Examples: 
//   "Raleigh, NC, United States" -> "Raleigh, NC"
//   "Raleigh, NC" -> "Raleigh, NC"
//   "London, United Kingdom" -> "London"
func extractMainLocation(location string) string {
	if location == "" {
		return ""
	}
	
	parts := strings.Split(location, ",")
	if len(parts) == 0 {
		return location
	}
	
	// Trim spaces from all parts
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	
	// If last part is a country name, remove it
	if len(parts) > 1 {
		lastPart := parts[len(parts)-1]
		// Common country names to strip
		countries := []string{
			"United States", "USA", "US", "United Kingdom", "UK", 
			"Canada", "Australia", "Germany", "France", "India",
			"China", "Japan", "Brazil", "Mexico", "Spain", "Italy",
		}
		for _, country := range countries {
			if strings.EqualFold(lastPart, country) {
				parts = parts[:len(parts)-1]
				break
			}
		}
	}
	
	// For US locations, keep city and state (first 2 parts)
	// For others, keep just the city (first part)
	if len(parts) >= 2 {
		// Check if second part looks like a US state code
		if len(parts[1]) == 2 && isUSStateCode(parts[1]) {
			return parts[0] + ", " + parts[1]
		}
	}
	
	return parts[0]
}

// isUSStateCode checks if a string is a valid US state code
func isUSStateCode(code string) bool {
	states := []string{
		"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA",
		"HI", "ID", "IL", "IN", "IA", "KS", "KY", "LA", "ME", "MD",
		"MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ",
		"NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC",
		"SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY",
		"DC",
	}
	upperCode := strings.ToUpper(code)
	for _, state := range states {
		if upperCode == state {
			return true
		}
	}
	return false
}

func printLocation(result *gutz.Result) {
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
		fmt.Printf("📍 Location:      %s", locationStr)
		
		// Check for discrepancies from both our verification and Gemini
		if result.Verification != nil && result.Verification.ClaimedLocation != "" {
			// Check if we should show the claim
			// 1. If distance > 50 miles, definitely show
			// 2. If LocationMismatch is flagged, show
			// 3. If Gemini says it's suspicious, show
			// 4. If location strings differ but distance is 0, check if cities match
			showClaim := false
			
			if result.Verification.LocationDistanceMiles > 50 {
				showClaim = true
			} else if result.Verification.LocationMismatch != "" {
				showClaim = true
			} else if result.GeminiSuspiciousMismatch {
				showClaim = true
			} else if locationStr != result.Verification.ClaimedLocation {
				// Strings differ but no distance/mismatch flags
				// Check if it's just formatting differences (e.g., "Raleigh, NC" vs "Raleigh, NC, United States")
				detectedCity := extractMainLocation(locationStr)
				claimedCity := extractMainLocation(result.Verification.ClaimedLocation)
				showClaim = detectedCity != claimedCity
			}
			
			if showClaim {
				claimStr := fmt.Sprintf(" — claims %s", result.Verification.ClaimedLocation)
				if result.Verification.LocationDistanceMiles > 0 {
					distStr := fmt.Sprintf(" (%.0f mi away)", result.Verification.LocationDistanceMiles)
					claimStr += distStr
				}
				
				// Determine severity based on both detectors
				isMajor := result.Verification.LocationMismatch == "major" || result.GeminiSuspiciousMismatch
				isMinor := result.Verification.LocationMismatch == "minor"
				
				if isMajor {
					// Red for major discrepancy
					fmt.Printf("\033[31m%s\033[0m", claimStr)
				} else if isMinor {
					// Normal for minor discrepancy
					fmt.Printf("%s", claimStr)
				} else {
					fmt.Printf("%s", claimStr)
				}
			}
		}
		fmt.Println()
		
		// Show analysis from both detectors working together
		if result.GeminiSuspiciousMismatch && result.GeminiMismatchReason != "" {
			// Gemini AI analysis
			fmt.Printf("                  └─ AI: \033[33m%s\033[0m\n", result.GeminiMismatchReason)
		}
		if result.Verification != nil && result.Verification.LocationMismatch != "" {
			// Distance-based analysis
			severity := "suspicious"
			if result.Verification.LocationMismatch == "major" {
				severity = "highly suspicious"
			}
			fmt.Printf("                  └─ Distance: %s discrepancy detected\n", severity)
		}
	}
}

func printTimezone(result *gutz.Result) {
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

	// Check for verification discrepancy (claimed location's timezone vs activity)
	if result.Verification != nil && result.Verification.TimezoneMismatch != "" {
		// When detected from location field, show that the location implies wrong timezone
		claimStr := ""
		if result.Method == "location_field" && result.ActivityTimezone != "" {
			claimStr = fmt.Sprintf(" — location implies this")
			if result.Verification.TimezoneOffsetDiff > 0 {
				diffStr := fmt.Sprintf(" (%d hr off from activity)", result.Verification.TimezoneOffsetDiff)
				claimStr += diffStr
			}
		} else if result.Verification.ClaimedTimezone != "" {
			claimStr = fmt.Sprintf(" — user claims %s", result.Verification.ClaimedTimezone)
			if result.Verification.TimezoneOffsetDiff > 0 {
				diffStr := fmt.Sprintf(" (%d hours off)", result.Verification.TimezoneOffsetDiff)
				claimStr += diffStr
			}
		}
		
		if claimStr != "" {
			switch result.Verification.TimezoneMismatch {
			case "major":
				// Red for >3 timezone difference
				fmt.Printf("\033[31m%s\033[0m", claimStr)
			case "minor":
				// Normal color for >1 timezone difference
				fmt.Printf("%s", claimStr)
			}
		}
	}

	if result.ActivityTimezone != "" && result.ActivityTimezone != result.Timezone {
		fmt.Printf("\n                  └─ activity suggests %s", result.ActivityTimezone)
	}
	fmt.Println()
}

// Removed - no longer needed since we use UTC throughout.

func printWorkSchedule(result *gutz.Result) {
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
		printSleepHours(result)
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

type sleepRange struct {
	start, end int
}

func printSleepHours(result *gutz.Result) {
	// Convert UTC sleep hours to local hours
	localSleepHours := convertSleepHoursToLocal(result.SleepHoursUTC, result.Timezone)

	// Group consecutive sleep hours into ranges
	ranges := groupSleepHours(localSleepHours)

	// Format and display valid sleep ranges
	displaySleepRanges(ranges, result.Timezone)
}

func convertSleepHoursToLocal(sleepHoursUTC []int, timezone string) []int {
	var localSleepHours []int
	for _, utcHour := range sleepHoursUTC {
		localHour := int(convertUTCToLocal(float64(utcHour), timezone))
		localSleepHours = append(localSleepHours, localHour)
	}
	return localSleepHours
}

func groupSleepHours(localSleepHours []int) []sleepRange {
	var ranges []sleepRange
	if len(localSleepHours) == 0 {
		return ranges
	}

	currentStart := localSleepHours[0]
	currentEnd := localSleepHours[0]

	for i := 1; i < len(localSleepHours); i++ {
		hour := localSleepHours[i]

		if isConsecutiveHour(currentEnd, hour) {
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
	return ranges
}

func isConsecutiveHour(currentEnd, hour int) bool {
	// Check if this hour is consecutive (handle day wraparound)
	return hour == (currentEnd+1)%24 || (currentEnd == 23 && hour == 0)
}

func displaySleepRanges(ranges []sleepRange, timezone string) {
	if len(ranges) == 0 {
		return
	}

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
		fmt.Printf("\n💤 Sleep Time:    %s (%s)", strings.Join(rangeStrings, ", "), timezone)
	}
}
