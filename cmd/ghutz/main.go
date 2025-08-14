package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
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
	defer func() {
		if err := detector.Close(); err != nil {
			logger.Error("Failed to close detector", "error", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := detector.Detect(ctx, username)
	if err != nil {
		cancel() // Ensure context is cancelled before exit
		logger.Error("Detection failed", "error", err)
		os.Exit(1)
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
		// Calculate UTC offset from the FINAL detected timezone for consistent display
		// Both histogram and quiet times should be shown in the user's detected timezone
		utcOffset := 0

		if strings.HasPrefix(result.Timezone, "UTC") {
			offsetStr := strings.TrimPrefix(result.Timezone, "UTC")
			if offsetStr == "" {
				utcOffset = 0 // UTC+0
			} else if offset, err := strconv.Atoi(offsetStr); err == nil {
				utcOffset = offset
			}
		} else if loc, err := time.LoadLocation(result.Timezone); err == nil {
			now := time.Now().In(loc)
			_, offset := now.Zone()
			utcOffset = offset / 3600
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
		fmt.Printf("📍 Location:      %s\n", locationStr)
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
		fmt.Printf("🕐 Timezone:      %s (%s, now %s)", tzName, gmtOffset, currentLocal)
	} else {
		// Fallback for UTC+/- format
		fmt.Printf("🕐 Timezone:      %s", result.Timezone)
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
		fmt.Printf("\n                  └─ activity suggests %s", activityTz)
	}
	fmt.Println()

	// Work schedule with relative time indicators
	if result.ActiveHoursLocal.Start != 0 || result.ActiveHoursLocal.End != 0 {
		// Convert active hours from UTC to final detected timezone for display
		// Despite the field name "Local", these are stored as UTC values
		finalOffset := calculateTimezoneOffset(result.Timezone)

		// Convert UTC to local time
		activeStartLocal := math.Mod(result.ActiveHoursLocal.Start+float64(finalOffset)+24, 24)
		activeEndLocal := math.Mod(result.ActiveHoursLocal.End+float64(finalOffset)+24, 24)

		workStartRelative := ""
		workEndRelative := ""

		// Calculate relative times if we have a valid timezone
		if loc, err := time.LoadLocation(tzName); err == nil {
			now := time.Now().In(loc)
			currentTime := float64(now.Hour()) + float64(now.Minute())/60.0

			// Calculate relative time to work start (using converted local times)
			hoursToStart := activeStartLocal - currentTime
			if hoursToStart < 0 {
				hoursToStart += 24 // Handle next day
			}
			if hoursToStart > 12 {
				// It was yesterday/earlier today
				hoursToStart -= 24
			}

			// Calculate relative time to work end (using converted local times)
			hoursToEnd := activeEndLocal - currentTime
			if hoursToEnd < 0 && activeEndLocal > activeStartLocal {
				hoursToEnd += 24 // Handle next day
			}
			if hoursToEnd > 12 {
				// It was yesterday/earlier today
				hoursToEnd -= 24
			}

			// Format relative times tersely
			workStartRelative = formatRelativeTime(hoursToStart)
			workEndRelative = formatRelativeTime(hoursToEnd)
		}

		fmt.Printf("🏃 Active Time:   %s → %s",
			formatHour(activeStartLocal),
			formatHour(activeEndLocal))

		if workStartRelative != "" && workEndRelative != "" {
			fmt.Printf(" (%s → %s)", workStartRelative, workEndRelative)
		}

		if result.LunchHoursLocal.Confidence > 0 {
			// Convert lunch times from UTC to final detected timezone for display
			finalOffset := calculateTimezoneOffset(result.Timezone)

			lunchStart := math.Mod(result.LunchHoursLocal.Start+float64(finalOffset)+24, 24)
			lunchEnd := math.Mod(result.LunchHoursLocal.End+float64(finalOffset)+24, 24)

			fmt.Printf("\n🍽️  Lunch Break:   %s → %s",
				formatHour(lunchStart),
				formatHour(lunchEnd))
			if result.LunchHoursLocal.Confidence < 0.7 {
				fmt.Printf(" (uncertain)")
			}
		}

		// Add peak productivity time
		if result.PeakProductivity.Count > 0 {
			// Convert peak times from UTC to final detected timezone for display
			finalOffset := calculateTimezoneOffset(result.Timezone)

			peakStart := math.Mod(result.PeakProductivity.Start+float64(finalOffset)+24, 24)
			peakEnd := math.Mod(result.PeakProductivity.End+float64(finalOffset)+24, 24)

			fmt.Printf("\n🔥 Activity Peak: %s → %s",
				formatHour(peakStart),
				formatHour(peakEnd))
		}

		// Add quiet hours (sleep/family time)
		if len(result.QuietHoursUTC) > 0 {
			// Calculate UTC offset from the FINAL detected timezone for display
			// Quiet hours are stored in UTC and should be converted to the user's detected timezone
			utcOffset := calculateTimezoneOffset(tzName)

			// Convert UTC quiet hours to local hours
			localQuietHours := make([]int, len(result.QuietHoursUTC))
			for i, utcHour := range result.QuietHoursUTC {
				localQuietHours[i] = (utcHour + utcOffset + 24) % 24
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

			// Format and display ranges
			if len(ranges) > 0 {
				var rangeStrings []string
				for _, r := range ranges {
					rangeStrings = append(rangeStrings, fmt.Sprintf("%s - %s",
						formatHour(float64(r.start)),
						formatHour(float64(r.end))))
				}
				fmt.Printf("\n💤 Quiet Time:    %s", strings.Join(rangeStrings, ", "))
			}
		}

		fmt.Println()
	}

	// Show all organizations with color-coded counts
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

	// Detection method as a subtle footer
	fmt.Printf("✨ Detection:     %s (%.0f%% confidence)\n",
		formatMethodName(result.Method),
		result.Confidence*100)
	fmt.Println()
}

func formatHour(decimalHour float64) string {
	hour := int(decimalHour)
	minutes := int((decimalHour - float64(hour)) * 60)
	return fmt.Sprintf("%d:%02d", hour, minutes)
}

// calculateTimezoneOffset calculates the UTC offset in hours for a given timezone
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

