package gutz

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
	"github.com/codeGROOVE-dev/guTZ/pkg/sleep"
	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
	"github.com/codeGROOVE-dev/guTZ/pkg/tzconvert"
)

// GlobalLunchPattern represents the best lunch pattern found globally in UTC.

// refineHourlySleepFromBuckets uses half-hour resolution data to create accurate sleep hours.
// Since we always have half-hour data, we should use it for precise sleep detection.
func refineHourlySleepFromBuckets(quietHours []int, sleepBuckets []float64, halfHourCounts map[float64]int) []int {
	// If we have good sleep bucket data, use it exclusively
	if len(sleepBuckets) >= 8 { // At least 4 hours of sleep
		// Convert sleep buckets to hours
		sleepHourMap := make(map[int]bool)
		for _, bucket := range sleepBuckets {
			hour := int(bucket) // Get the hour part
			sleepHourMap[hour] = true
		}

		// Convert to sorted slice
		var refinedHours []int
		for hour := range sleepHourMap {
			refinedHours = append(refinedHours, hour)
		}
		sort.Ints(refinedHours)
		return refinedHours
	}

	// Otherwise, refine the hourly quiet hours by checking half-hour activity
	var refinedHours []int
	for _, hour := range quietHours {
		// Check both half-hour buckets in this hour
		firstHalf := float64(hour)
		secondHalf := float64(hour) + 0.5

		firstActivity := halfHourCounts[firstHalf]
		secondActivity := halfHourCounts[secondHalf]

		// Only include hour if both halves are very quiet
		if firstActivity <= 1 && secondActivity <= 1 {
			refinedHours = append(refinedHours, hour)
		}
	}

	// If we refined away too much, fall back to original
	if len(refinedHours) < 4 {
		return quietHours
	}

	return refinedHours
}

// tryActivityPatternsWithContext performs activity pattern analysis using UserContext.
func (d *Detector) tryActivityPatternsWithContext(ctx context.Context, userCtx *UserContext) *Result {
	d.logger.Info("🔍 Starting activity pattern analysis with UserContext",
		"username", userCtx.Username,
		"has_ssh_keys", len(userCtx.SSHKeys) > 0,
		"has_repos", len(userCtx.Repositories) > 0)

	// Get implied timezone from location if not already set
	if userCtx.ProfileLocationTimezone == "" && userCtx.User != nil && userCtx.User.Location != "" {
		// Try to geocode and get timezone from the location
		if coords, err := d.geocodeLocation(ctx, userCtx.User.Location); err == nil {
			if tz, err := d.timezoneForCoordinates(ctx, coords.Latitude, coords.Longitude); err == nil {
				// Convert to UTC offset using current time for DST-aware calculation
				if loc, err := time.LoadLocation(tz); err == nil {
					now := time.Now()
					_, offset := now.In(loc).Zone()
					offsetHours := offset / 3600
					switch {
					case offsetHours == 0:
						userCtx.ProfileLocationTimezone = "UTC"
					case offsetHours > 0:
						userCtx.ProfileLocationTimezone = fmt.Sprintf("UTC+%d", offsetHours)
					default:
						userCtx.ProfileLocationTimezone = fmt.Sprintf("UTC%d", offsetHours)
					}
					d.logger.Debug("derived profile location timezone from location",
						"username", userCtx.Username,
						"location", userCtx.User.Location,
						"timezone_name", tz,
						"profile_location_offset", userCtx.ProfileLocationTimezone)
				}
			}
		}
	}

	// Log both profile and profile location timezones for debugging
	d.logger.Info("timezone context for activity analysis",
		"username", userCtx.Username,
		"profile_timezone", userCtx.GitHubTimezone,
		"profile_location_timezone", userCtx.ProfileLocationTimezone,
		"location", userCtx.User.Location)

	// For candidate evaluation, prefer claimed timezone if available, otherwise use implied
	timezoneForCandidates := userCtx.GitHubTimezone
	if timezoneForCandidates == "" {
		timezoneForCandidates = userCtx.ProfileLocationTimezone
	}
	d.logger.Debug("using timezone for candidates",
		"username", userCtx.Username,
		"timezone_for_candidates", timezoneForCandidates)

	// Collect all timestamps from various sources, including SSH keys and repositories from userCtx
	allTimestamps, orgCounts := d.collectActivityTimestampsWithContext(ctx, userCtx)

	// Since we have the full UserContext, we don't need to fetch supplemental data again
	// Just continue with the analysis directly
	return d.analyzeActivityTimestampsWithoutSupplemental(ctx, userCtx.Username, allTimestamps, orgCounts, timezoneForCandidates)
}

//nolint:revive // Complex timezone detection logic requires detailed analysis
func (d *Detector) analyzeActivityTimestampsWithoutSupplemental(ctx context.Context, username string, allTimestamps []timestampEntry, orgCounts map[string]int, claimedTimezone string) *Result {
	// Track the oldest event to check data coverage
	var oldestEventTime time.Time
	if len(allTimestamps) > 0 {
		oldestEventTime = allTimestamps[0].time
		for _, ts := range allTimestamps {
			if ts.time.Before(oldestEventTime) {
				oldestEventTime = ts.time
			}
		}
	}

	// When we have UserContext, we already have all the data including supplemental activity
	// So we can skip the supplemental collection step

	return d.analyzeTimestampsCore(ctx, username, allTimestamps, orgCounts, claimedTimezone, oldestEventTime)
}

//nolint:gocognit,revive,maintidx // Complex timezone detection logic requires detailed analysis
func (d *Detector) analyzeTimestampsCore(ctx context.Context, username string, allTimestamps []timestampEntry, orgCounts map[string]int, claimedTimezone string, _ time.Time) *Result {
	// Filter and sort timestamps, then apply progressive time window
	const targetDataPoints = 160
	allTimestamps = filterAndSortTimestamps(allTimestamps, 5)
	allTimestamps = applyProgressiveTimeWindow(allTimestamps, targetDataPoints)

	// Log each timeline item for debugging
	d.logger.Debug("Final timeline assembled", "username", username, "total_items", len(allTimestamps))
	for i, entry := range allTimestamps {
		d.logger.Debug("timeline item",
			"index", i,
			"date", entry.time.Format("2006-01-02 15:04:05"),
			"source", entry.source,
			"repository", entry.repository,
			"title", entry.title,
			"org", entry.org)
	}

	// Deduplicate all unique timestamps (no cap with adaptive collection)
	uniqueTimestamps := make(map[time.Time]bool)
	hourCounts := make(map[int]int)
	halfHourCounts := make(map[float64]int)         // 30-minute buckets: 0.0, 0.5, 1.0, 1.5, etc.
	hourOrgActivity := make(map[int]map[string]int) // Track org activity by hour
	duplicates := 0

	for _, entry := range allTimestamps {
		if !uniqueTimestamps[entry.time] {
			uniqueTimestamps[entry.time] = true
			hour := entry.time.UTC().Hour()
			minute := entry.time.UTC().Minute()

			// Traditional hourly counting (keep for backwards compatibility)
			hourCounts[hour]++

			// 30-minute bucket counting: 0-29 minutes = .0, 30-59 minutes = .5
			halfHourBucket := float64(hour)
			if minute >= 30 {
				halfHourBucket += 0.5
			}
			halfHourCounts[halfHourBucket]++

			// Track organization counts
			if entry.org != "" {
				orgCounts[entry.org]++
				// Track hourly organization activity
				if hourOrgActivity[hour] == nil {
					hourOrgActivity[hour] = make(map[string]int)
				}
				hourOrgActivity[hour][entry.org]++
			}
		} else {
			duplicates++
		}
	}

	// Count total unique activities used
	totalActivity := len(uniqueTimestamps)

	// For CLI output and Gemini communication, aggregate halfHourCounts back to hourly buckets
	// Note: Removed displayHourCounts as we now use half-hourly precision everywhere
	// except for the Gemini prompt which aggregates on-demand

	// Find oldest and newest timestamps from the unique set
	var oldestActivity, newestActivity time.Time
	for timestamp := range uniqueTimestamps {
		if oldestActivity.IsZero() || timestamp.Before(oldestActivity) {
			oldestActivity = timestamp
		}
		if newestActivity.IsZero() || timestamp.After(newestActivity) {
			newestActivity = timestamp
		}
	}

	// If we have no valid timestamps, use the timestamp dates as fallback
	if oldestActivity.IsZero() && len(allTimestamps) > 0 {
		// Find oldest and newest from the timestamps
		for _, ts := range allTimestamps {
			if oldestActivity.IsZero() || ts.time.Before(oldestActivity) {
				oldestActivity = ts.time
			}
			if newestActivity.IsZero() || ts.time.After(newestActivity) {
				newestActivity = ts.time
			}
		}
	}

	// Calculate total days covered
	var totalDays int
	var spansDSTTransitions bool
	if !oldestActivity.IsZero() && !newestActivity.IsZero() {
		totalDays = int(newestActivity.Sub(oldestActivity).Hours()/24) + 1

		// Check if the activity period spans DST transitions
		// DST transitions happen in March/April (spring forward) and September/October/November (fall back)
		// We need both a spring and fall month to indicate DST transitions
		monthsPresent := make(map[int]bool)
		for timestamp := range uniqueTimestamps {
			monthsPresent[int(timestamp.Month())] = true
		}

		hasSpringDST := monthsPresent[3] || monthsPresent[4]                     // March or April
		hasFallDST := monthsPresent[9] || monthsPresent[10] || monthsPresent[11] // September, October, or November

		// Only mark as spanning DST if we have both spring AND fall transition periods
		spansDSTTransitions = hasSpringDST && hasFallDST && totalDays > 90
	}

	// Check minimum threshold
	if totalActivity < 3 {
		d.logger.Debug("insufficient activity data", "username", username,
			"total_activity", totalActivity,
			"minimum_required", 3)
		return nil
	}

	// Warn if we have limited data
	if totalActivity < 20 {
		d.logger.Debug("limited activity data - results may be less accurate", "username", username,
			"total_activity", totalActivity,
			"recommended", 20)
	}

	maxActivity := 0
	mostActiveHours := []int{}
	for hour, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
			mostActiveHours = []int{hour}
		} else if count == maxActivity {
			mostActiveHours = append(mostActiveHours, hour)
		}
	}

	// Convert half-hour sleep buckets to hour-based quiet hours
	// DetectSleepPeriodsWithHalfHours returns half-hour buckets (0.0, 0.5, 1.0, etc.)
	// We need to convert these to integer hours for the existing logic
	sleepBucketsHalf := sleep.DetectSleepPeriodsWithHalfHours(halfHourCounts)
	quietHoursMap := make(map[int]bool)
	for _, bucket := range sleepBucketsHalf {
		hour := int(bucket) // Convert to hour (0.5 becomes 0, 1.0 becomes 1, etc.)
		quietHoursMap[hour] = true
	}

	// Convert map to sorted slice
	var quietHours []int
	for hour := range quietHoursMap {
		quietHours = append(quietHours, hour)
	}
	sort.Ints(quietHours)
	// Create a string representation to see the actual order
	var sleepHoursStr []string
	for _, h := range quietHours {
		sleepHoursStr = append(sleepHoursStr, strconv.Itoa(h))
	}
	d.logger.Info("DEBUG: raw sleep hours", "username", username,
		"quiet_hours_order", strings.Join(sleepHoursStr, ","),
		"count", len(quietHours))
	// With limited data, we might not find clear sleep patterns
	// If we don't have enough quiet hours, make educated guesses based on what we have
	if len(quietHours) < 4 {
		d.logger.Debug("limited sleep hour data - making educated guess", "username", username, "sleep_hours", len(quietHours))
		// If we have very limited data, assume typical sleep hours (2am-6am UTC is common)
		if len(quietHours) == 0 {
			// Find the hours with zero activity and use those, or default to typical hours
			for hour := range 24 {
				if hourCounts[hour] == 0 {
					quietHours = append(quietHours, hour)
				}
			}
			// If still no quiet hours, use a reasonable default
			if len(quietHours) < 4 {
				quietHours = []int{2, 3, 4, 5, 6} // Default sleep hours
				d.logger.Debug("using default sleep hours due to limited data", "username", username)
			}
		}
	}

	// Find the middle of sleep hours
	// FindSleepHours returns hours that may wrap around midnight
	// e.g., [13, 2, 3, 4, 5, 6, 7, 8, 9] means sleep from 2:00 to 9:00 with 13 as an outlier
	var midQuiet float64

	if len(quietHours) == 0 { //nolint:nestif // Complex sleep midpoint calculation
		// No quiet hours found - shouldn't happen but handle gracefully
		midQuiet = 2.5 // Default to 2:30 AM UTC
	} else {
		// Find the actual continuous sleep window
		// When wrapped, the hours won't be in order, so we need to find the actual range
		minHour := 24
		maxHour := -1
		hasWrap := false

		// Check if we have a wraparound (e.g., hours like 22, 23, 0, 1, 2)
		for i := 1; i < len(quietHours); i++ {
			if quietHours[i] < quietHours[i-1] && quietHours[i-1]-quietHours[i] > 12 {
				hasWrap = true
				break
			}
		}

		if hasWrap {
			// Handle wraparound case - find the continuous range
			// Find the start of the sleep period (first hour after the gap)
			startIdx := 0
			for i := 1; i < len(quietHours); i++ {
				if quietHours[i-1] > quietHours[i] && quietHours[i-1]-quietHours[i] > 12 {
					startIdx = i
					break
				}
			}

			// Reorder the array to be continuous
			quietHours = append(quietHours[startIdx:], quietHours[:startIdx]...)
			startHour := quietHours[0]
			endHour := quietHours[len(quietHours)-1]

			// Calculate midpoint
			if endHour < startHour {
				// Still wrapped after reordering
				totalHours := (24 - startHour) + endHour + 1
				midQuiet = float64(startHour) + float64(totalHours)/2.0
			} else {
				midQuiet = float64(startHour) + float64(endHour-startHour)/2.0
			}
		} else {
			// No wraparound - simple case
			for _, h := range quietHours {
				if h < minHour {
					minHour = h
				}
				if h > maxHour {
					maxHour = h
				}
			}
			midQuiet = float64(minHour) + float64(maxHour-minHour)/2.0
		}

		if midQuiet >= 24 {
			midQuiet -= 24
		}

		d.logger.Debug("sleep midpoint calculation", "username", username,
			"quiet_hours", quietHours, "hasWrap", hasWrap, "midQuiet", midQuiet)
	}

	// Analyze the activity pattern to determine likely region
	// European pattern: more activity in hours 6-16 UTC (morning/afternoon in Europe)
	// American pattern: more activity in hours 12-22 UTC (morning/afternoon in Americas)
	europeanActivity := 0
	americanActivity := 0
	for hour := 6; hour <= 16; hour++ {
		europeanActivity += hourCounts[hour]
	}
	for hour := 12; hour <= 22; hour++ {
		americanActivity += hourCounts[hour]
	}

	// Sleep patterns are more reliable than work patterns for timezone detection
	// The middle of quiet time varies by region:
	// - Americans tend to sleep later (midpoint ~3:30am)
	// - Europeans tend to sleep earlier (midpoint ~2:30am)
	// - Asians vary widely
	var assumedSleepMidpoint float64
	switch {
	case float64(europeanActivity) > float64(americanActivity)*1.2:
		// Strong European pattern
		// Europeans typically have earlier sleep patterns, midpoint around 2am
		assumedSleepMidpoint = 2.0
		d.logger.Debug("detected European activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	case float64(americanActivity) > float64(europeanActivity)*1.2:
		// Strong American pattern
		// Americans typically sleep midnight-5am, midpoint around 2.5am
		// Using 2.5 instead of 3.5 to better match Eastern Time patterns
		assumedSleepMidpoint = 2.5
		d.logger.Debug("detected American activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	default:
		// Unclear or Asian pattern, use default
		assumedSleepMidpoint = 3.0
		d.logger.Debug("unclear activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	}

	// For European timezones (UTC+X), the UTC sleep time is EARLIER than local sleep time
	// For American timezones (UTC-X), the UTC sleep time is LATER than local sleep time
	// If someone sleeps at 2 AM local in Europe/Warsaw (UTC+1), that's 1 AM UTC
	// So: UTC_offset = local_time - utc_time
	// But we have UTC sleep time and want to find offset
	// So: UTC_offset = assumed_local_sleep - observed_utc_sleep

	// However, we need to think about this differently:
	// If midQuiet (UTC) is 2.5 and we expect local sleep at 2.0 (Europe)
	// That means UTC is AHEAD of local, which would be UTC-0.5
	// But that's wrong for Europe!

	// The correct logic:
	// For positive UTC offsets (east of Greenwich): local_time = utc_time + offset
	// So if they sleep at 2.5 UTC and we expect 3.5 local (typical for UTC+1)
	// Then offset = 3.5 - 2.5 = +1

	// We need to adjust assumed sleep midpoints based on patterns
	var offsetFromUTC float64
	var candidates []timezone.Candidate // Store timezone candidates for Gemini

	if float64(europeanActivity) > float64(americanActivity)*1.2 {
		// European/Asian pattern - need to distinguish between them
		// Asian timezones (UTC+8/+9) have sleep around 15-20 UTC (midnight-5am local)
		// European timezones (UTC+0/+1/+2) have sleep around 0-5 UTC (midnight-5am local)

		if midQuiet >= 14 && midQuiet <= 20 {
			// Asian pattern - sleep hours in the afternoon/evening UTC
			// If they sleep at 17 UTC and expect 2am local, offset = 2 + 24 - 17 = 9
			assumedSleepMidpoint = 2.0 // 2am local sleep time
			offsetFromUTC = assumedSleepMidpoint + 24 - midQuiet
			if offsetFromUTC >= 24 {
				offsetFromUTC -= 24
			}
			d.logger.Debug("Asian timezone detection", "username", username,
				"midQuiet", midQuiet, "calculated_offset", offsetFromUTC)
		} else {
			// European pattern - sleep hours in early morning UTC
			assumedSleepMidpoint = midQuiet + 1.0 // Assume UTC+1 for Europe
			offsetFromUTC = 1.0                   // Default to CET

			// Fine-tune based on exact sleep timing
			if midQuiet < 2.0 {
				offsetFromUTC = 2.0 // Eastern Europe (UTC+2)
			} else if midQuiet > 3.0 && midQuiet < 14 {
				offsetFromUTC = 0.0 // UK/Portugal (UTC+0)
			}

			d.logger.Debug("European timezone detection", "username", username,
				"midQuiet", midQuiet, "assumed_offset", offsetFromUTC)
		}
	} else {
		// American pattern - need better US timezone differentiation
		// Calculate base offset from sleep pattern
		baseOffset := assumedSleepMidpoint - midQuiet

		// For US timezones, consider multiple possibilities when sleep patterns are ambiguous
		// US sleep patterns typically map to:
		// - Eastern (UTC-5): sleep 5-10 UTC (midnight-5am local)
		// - Central (UTC-6): sleep 6-11 UTC (midnight-5am local)
		// - Mountain (UTC-7): sleep 7-12 UTC (midnight-5am local)
		// - Pacific (UTC-8): sleep 8-13 UTC (midnight-5am local)

		// Check for US timezone patterns
		// Use a simple initial guess and let the lunch+sleep scoring refine it
		if midQuiet >= 3 && midQuiet <= 13 && len(quietHours) >= 4 {
			// This sleep pattern likely matches a US timezone
			d.logger.Debug("US sleep pattern detected", "username", username,
				"midQuiet", midQuiet, "baseOffset", baseOffset, "quietHours", quietHours)

			// Simple initial guess based on sleep pattern
			// Let the lunch+sleep scoring system refine this
			//
			// For midQuiet = 5 (tstromberg's case):
			// 5am UTC = 1am EDT (UTC-4) - reasonable sleep time
			// 5am UTC = 12am CDT (UTC-5) - reasonable sleep time
			// 5am UTC = 11pm MDT (UTC-6) - early sleep
			// 5am UTC = 10pm PDT (UTC-7) - very early sleep

			// Make a simple initial guess based on midQuiet hour
			switch {
			case midQuiet <= 5:
				offsetFromUTC = -4.0 // Likely Eastern (EDT)
			case midQuiet <= 7:
				offsetFromUTC = -5.0 // Likely Eastern (EST) or Central (CDT)
			case midQuiet <= 9:
				offsetFromUTC = -6.0 // Likely Central (CST) or Mountain (MDT)
			case midQuiet <= 11:
				offsetFromUTC = -7.0 // Likely Mountain (MST) or Pacific (PDT)
			default:
				offsetFromUTC = -8.0 // Likely Pacific (PST)
			}

			d.logger.Debug("initial US timezone guess based on sleep", "username", username,
				"midQuiet", midQuiet, "initial_offset", offsetFromUTC)

			// The candidate evaluation system below will refine this using lunch+sleep patterns
		} else {
			// Clear sleep pattern, use original logic
			offsetFromUTC = baseOffset
			d.logger.Debug("clear US sleep pattern, using calculated offset", "username", username, "offset", offsetFromUTC)
		}
	}

	d.logger.Debug("offset calculation details", "username", username,
		"assumedSleepMidpoint", assumedSleepMidpoint,
		"midQuiet", midQuiet,
		"rawOffset", offsetFromUTC)

	// For US timezones, check if the pattern matches known US work/activity patterns
	// US developers often have evening activity (7-11pm local) for open source
	// This would be roughly 13-17 UTC for Central Time (UTC-6)

	// Count activity in typical US evening hours (converted to UTC for different zones)
	// For Central Time (UTC-6): 7-11pm local = 1-5am UTC (next day) or 13-17 UTC (same day DST)
	// For Eastern Time (UTC-5): 7-11pm local = 0-4am UTC (next day) or 12-16 UTC (same day DST)
	// For Pacific Time (UTC-8): 7-11pm local = 3-7am UTC (next day) or 15-19 UTC (same day DST)

	// Check if this looks like a US pattern based on quiet hours
	// US timezones typically have quiet hours (midnight-5am local) that map to:
	// - Eastern (UTC-4 DST): quiet hours 4-9 UTC
	// - Central (UTC-5 DST): quiet hours 5-10 UTC
	// - Mountain (UTC-6 DST): quiet hours 6-11 UTC
	// - Pacific (UTC-7 DST): quiet hours 7-12 UTC

	// If we detected American activity pattern and quiet hours suggest US timezone,
	// use the calculated offset as-is rather than forcing to Central
	if americanActivity > europeanActivity && midQuiet >= 4 && midQuiet <= 12 {
		// This looks like a US pattern, trust the calculated offset
		d.logger.Debug("detected US timezone pattern", "username", username,
			"mid_quiet_utc", midQuiet, "calculated_offset", offsetFromUTC)
		// Keep the calculated offset as-is
	}

	// Normalize to [-12, 12] range
	if offsetFromUTC > 12 {
		offsetFromUTC -= 24
	} else if offsetFromUTC <= -12 {
		offsetFromUTC += 24
	}

	offsetInt := int(math.Round(offsetFromUTC))

	d.logger.Debug("calculated timezone offset", "username", username,
		"sleep_hours", quietHours,
		"mid_sleep_utc", midQuiet,
		"offset_calculated", offsetFromUTC,
		"offset_rounded", offsetInt)

	detectedTimezone := timezoneFromOffset(offsetInt)
	confidence := 0.5 // Default confidence, will be updated if we have candidates
	d.logger.Debug("Activity-based UTC offset", "username", username, "offset", offsetInt, "timezone", detectedTimezone)

	// Log the detected offset for verification
	if detectedTimezone != "" {
		now := time.Now().UTC()
		// Calculate what the local time would be with this offset
		localTime := now.Add(time.Duration(offsetInt) * time.Hour)
		d.logger.Debug("timezone verification", "username", username, "timezone", detectedTimezone,
			"utc_time", now.Format("15:04 MST"),
			"estimated_local_time", localTime.Format("15:04"),
			"offset_hours", offsetInt)
	}

	// Calculate typical active hours in UTC (excluding outliers)
	// This function returns UTC hours, conversion to local happens later
	activeStartUTC, activeEndUTC := calculateTypicalActiveHoursUTC(halfHourCounts, quietHours)

	d.logger.Info("active hours calculated", "username", username,
		"activeStartUTC", activeStartUTC, "activeEndUTC", activeEndUTC, "offsetInt", offsetInt,
		"quietHours", quietHours)

	// STEP 1: Find the best global lunch pattern in UTC (timezone-independent)
	bestGlobalLunch := lunch.FindBestGlobalLunchPattern(halfHourCounts)

	// DEBUG: Let's see what the half-hour data looks like around the detected lunch time
	if bestGlobalLunch.StartUTC > 0 {
		debugStart := bestGlobalLunch.StartUTC - 1.0
		debugEnd := bestGlobalLunch.StartUTC + 2.0
		d.logger.Debug("lunch pattern context", "username", username,
			fmt.Sprintf("%.1f", debugStart), halfHourCounts[debugStart],
			fmt.Sprintf("%.1f", bestGlobalLunch.StartUTC-0.5), halfHourCounts[bestGlobalLunch.StartUTC-0.5],
			fmt.Sprintf("%.1f", bestGlobalLunch.StartUTC), halfHourCounts[bestGlobalLunch.StartUTC],
			fmt.Sprintf("%.1f", bestGlobalLunch.StartUTC+0.5), halfHourCounts[bestGlobalLunch.StartUTC+0.5],
			fmt.Sprintf("%.1f", debugEnd), halfHourCounts[debugEnd])
	}

	// Evaluate timezone candidates using the new timezone package
	candidates = timezone.EvaluateCandidates(username, hourCounts, halfHourCounts,
		totalActivity, quietHours, midQuiet, activeStartUTC, timezone.GlobalLunchPattern{
			StartUTC:    bestGlobalLunch.StartUTC,
			EndUTC:      bestGlobalLunch.EndUTC,
			Confidence:  bestGlobalLunch.Confidence,
			DropPercent: bestGlobalLunch.DropPercent,
		}, claimedTimezone, newestActivity)

	// Ensure the claimed timezone is in the candidates list
	// Check if any candidate is already marked as claimed
	claimedFound := false
	for i := range candidates {
		c := &candidates[i]
		if c.IsProfile {
			claimedFound = true
			break
		}
	}

	// If claimed timezone wasn't evaluated (shouldn't happen but just in case), add it
	if claimedTimezone != "" && !claimedFound {
		// This shouldn't happen since EvaluateCandidates evaluates all offsets
		// But if somehow the claimed timezone wasn't included, add it with minimal info
		d.logger.Warn("claimed timezone not found in candidates, adding it",
			"username", username, "claimed_timezone", claimedTimezone)
	}

	// Keep ALL candidates for --force-offset support
	// We've already analyzed all 27 possible offsets (-12 to +14)
	// so we have complete data for any forced offset the user might choose

	// Log the top candidates and use the best one
	if len(candidates) > 0 {
		d.logger.Debug("top timezone candidates", "username", username,
			"count", len(candidates),
			"top_offset", candidates[0].Offset,
			"top_confidence", candidates[0].Confidence)

		for i := range candidates {
			if i >= 5 {
				break
			}
			c := &candidates[i]
			d.logger.Debug("timezone candidate", "username", username,
				"rank", i+1,
				"offset", c.Offset,
				"confidence", c.Confidence,
				"evening_activity", c.EveningActivity,
				"lunch_reasonable", c.LunchReasonable,
				"work_hours_reasonable", c.WorkHoursReasonable)
		}

		// Use the top candidate's offset instead of the initial detection
		// This gives us better lunch/peak detection
		if candidates[0].Confidence > confidence {
			offsetInt = int(candidates[0].Offset)
			detectedTimezone = timezoneFromOffset(offsetInt)
			confidence = candidates[0].Confidence
			d.logger.Info("using top candidate offset", "username", username,
				"new_offset", offsetInt,
				"new_timezone", detectedTimezone,
				"new_confidence", confidence,
				"initial_offset", int(offsetFromUTC))

			// Active hours in UTC don't change with timezone - they're based on the actual activity pattern
			// We only need to convert them to local time differently
			d.logger.Info("using winning candidate timezone", "username", username,
				"activeStartUTC", activeStartUTC, "activeEndUTC", activeEndUTC, "new_offset", offsetInt)
		} else {
			d.logger.Info("keeping initial offset", "username", username,
				"initial_offset", offsetInt,
				"initial_confidence", confidence,
				"top_candidate_offset", candidates[0].Offset,
				"top_candidate_confidence", candidates[0].Confidence)
		}
	}

	// Use the lunch times from the winning candidate if available
	// This avoids recalculating and ensures consistency
	var lunchStart, lunchEnd, lunchConfidence float64

	// Check if we have a winning candidate with lunch data
	if len(candidates) > 0 && int(candidates[0].Offset) == offsetInt && candidates[0].LunchStartUTC >= 0 {
		// Reuse the lunch calculation from the winning candidate
		lunchStart = candidates[0].LunchStartUTC
		lunchEnd = candidates[0].LunchEndUTC
		lunchConfidence = candidates[0].LunchConfidence
		d.logger.Debug("reusing lunch from winning candidate", "username", username,
			"lunch_start_utc", lunchStart, "lunch_end_utc", lunchEnd, "confidence", lunchConfidence)
	} else {
		// Fall back to calculating lunch for the chosen offset
		lunchStart, lunchEnd, lunchConfidence = lunch.DetectLunchBreakNoonCentered(halfHourCounts, offsetInt)
		d.logger.Debug("calculated new lunch", "username", username,
			"offset", offsetInt, "lunch_start_utc", lunchStart, "lunch_end_utc", lunchEnd, "confidence", lunchConfidence)
	}

	// Only use global lunch pattern if we don't already have a high-confidence lunch
	// Global patterns can be misleading - a consistent 2pm drop might be meetings, not lunch
	if bestGlobalLunch.Confidence > 0.5 && bestGlobalLunch.StartUTC >= 0 && lunchConfidence < 0.7 {
		// Calculate what local time the global lunch would be
		globalLunchLocal := math.Mod(bestGlobalLunch.StartUTC+float64(offsetInt)+24, 24)

		// Only use if it's in a more typical lunch range (11:30am-1:30pm)
		// 2pm is too late and likely represents something else
		if globalLunchLocal >= 11.5 && globalLunchLocal <= 13.5 {
			// Don't completely override - average with the detected lunch if both exist
			if lunchStart >= 0 {
				// Weight the individual detection more than the global pattern
				lunchStart = (lunchStart*0.7 + bestGlobalLunch.StartUTC*0.3)
				lunchEnd = (lunchEnd*0.7 + bestGlobalLunch.EndUTC*0.3)
				lunchConfidence = math.Max(lunchConfidence, bestGlobalLunch.Confidence*0.8)
			} else {
				// No individual lunch detected, use global
				lunchStart = bestGlobalLunch.StartUTC
				lunchEnd = bestGlobalLunch.EndUTC
				lunchConfidence = bestGlobalLunch.Confidence * 0.8 // Reduce confidence a bit
			}
		}
	}
	// Detect peak productivity window using 30-minute buckets for better precision
	peakStart, peakEnd, peakCount := timezone.DetectPeakProductivityWithHalfHours(halfHourCounts, offsetInt)

	// DISABLED: Work schedule validation corrections were causing more harm than good
	// The corrections were sometimes moving people further from their actual timezone
	// For example, egibs in Kansas (UTC-6) was being detected as UTC-7 (close!)
	// but then "corrected" to UTC-9 based on lunch timing, which is worse.
	//
	// The sleep-based detection is generally more reliable than trying to correct
	// based on work/lunch schedules, as people have varying work patterns.
	//
	// Log the work schedule for debugging but don't apply corrections

	// Process top organizations
	type orgActivity struct {
		name  string
		count int
	}
	var orgs []orgActivity
	for name, count := range orgCounts {
		orgs = append(orgs, orgActivity{name: name, count: count})
	}
	// Sort by count descending, then by name for deterministic ordering
	sort.Slice(orgs, func(i, j int) bool {
		if orgs[i].count == orgs[j].count {
			return orgs[i].name < orgs[j].name
		}
		return orgs[i].count > orgs[j].count
	})
	// Take top 5 organizations, but only those with more than 1 contribution
	var topOrgs []OrgActivity
	for i := 0; i < len(orgs) && len(topOrgs) < 5; i++ {
		// Skip organizations with only 1 contribution
		if orgs[i].count <= 1 {
			continue
		}
		topOrgs = append(topOrgs, OrgActivity{
			Name:  orgs[i].name,
			Count: orgs[i].count,
		})
	}

	// Check if work hours are suspicious (starting before 6am or after 11am)
	// This often indicates we've detected the wrong timezone
	suspiciousWorkHours := false
	alternativeTimezone := ""
	if activeStartUTC < 6.0 {
		// Work starting before 6am UTC is very unusual
		suspiciousWorkHours = true
		confidence = 0.4 // Lower confidence

		// If sleep is around 19-23 UTC, could be:
		// - UTC+8 (China) - would make work start at 11am instead of 3am
		// - UTC-8 (Pacific) - would make work start at 7pm (night shift)
		if midQuiet >= 19 && midQuiet <= 23 {
			// Most likely China if work starts very early in Europe
			alternativeTimezone = "UTC+8"
			d.logger.Info("suspicious work hours detected - suggesting Asia", "username", username,
				"work_start_utc", activeStartUTC, "detected_tz", detectedTimezone, "alternative", alternativeTimezone,
				"midQuiet", midQuiet)
		}
	} else {
		// Check local work start time (convert from UTC to local)
		localWorkStart := int(activeStartUTC+float64(offsetInt)+24) % 24
		if localWorkStart > 11 {
			// Work starting after 11am local time is unusual (unless part-time)
			suspiciousWorkHours = true
			confidence = 0.5
			d.logger.Debug("late work start detected", "username", username,
				"work_start_utc", activeStartUTC, "work_start_local", localWorkStart, "detected_tz", detectedTimezone)
		}
	}

	// Lunch timing confidence validation
	// Use lunch patterns as an additional signal to validate timezone detection
	//nolint:nestif // Lunch pattern validation requires conditional logic
	if lunchConfidence > 0 {
		lunchStartLocal := lunchStart
		lunchEndLocal := lunchEnd

		// Check if lunch timing makes sense (typical lunch: 11:30am-2:30pm)
		reasonableLunchStart := lunchStartLocal >= 11.5 && lunchStartLocal <= 14.5                              // 11:30am-2:30pm
		reasonableLunchEnd := lunchEndLocal >= 12.0 && lunchEndLocal <= 15.0                                    // 12:00pm-3:00pm
		normalLunchDuration := (lunchEndLocal-lunchStartLocal) >= 0.5 && (lunchEndLocal-lunchStartLocal) <= 2.0 // 30min-2hr

		if lunchConfidence >= 0.75 { // High lunch confidence
			if reasonableLunchStart && reasonableLunchEnd && normalLunchDuration {
				// High confidence lunch at reasonable time = boost timezone confidence
				originalConfidence := confidence
				confidence = math.Min(confidence+0.15, 0.95) // Boost by 15%, cap at 95%
				d.logger.Debug("lunch timing boosts timezone confidence", "username", username,
					"lunch_time", fmt.Sprintf("%.1f-%.1f", lunchStartLocal, lunchEndLocal),
					"lunch_confidence", lunchConfidence,
					"original_confidence", originalConfidence, "new_confidence", confidence)
			} else {
				// High confidence lunch at weird time = reduce timezone confidence significantly
				originalConfidence := confidence
				confidence = math.Max(confidence-0.25, 0.2) // Reduce by 25%, floor at 20%
				d.logger.Info("suspicious lunch timing reduces timezone confidence", "username", username,
					"lunch_time", fmt.Sprintf("%.1f-%.1f", lunchStartLocal, lunchEndLocal),
					"lunch_confidence", lunchConfidence,
					"reasonable_start", reasonableLunchStart, "reasonable_end", reasonableLunchEnd,
					"original_confidence", originalConfidence, "new_confidence", confidence)
			}
		} else if lunchConfidence >= 0.5 { // Medium lunch confidence
			if reasonableLunchStart && reasonableLunchEnd && normalLunchDuration {
				// Medium confidence lunch at reasonable time = small boost
				originalConfidence := confidence
				confidence = math.Min(confidence+0.08, 0.9) // Boost by 8%, cap at 90%
				d.logger.Debug("reasonable lunch timing slightly boosts confidence", "username", username,
					"lunch_confidence", lunchConfidence, "original_confidence", originalConfidence, "new_confidence", confidence)
			} else {
				// Medium confidence lunch at weird time = small penalty
				originalConfidence := confidence
				confidence = math.Max(confidence-0.1, 0.3) // Reduce by 10%, floor at 30%
				d.logger.Debug("questionable lunch timing slightly reduces confidence", "username", username,
					"lunch_confidence", lunchConfidence, "original_confidence", originalConfidence, "new_confidence", confidence)
			}
		}
		// Low lunch confidence (< 0.5) doesn't affect timezone confidence much

		// Check for late lunch suggesting timezone offset error
		// If lunch is at 2pm or later, we might be off by 2-3 hours
		if lunchConfidence >= 0.5 && lunchStartLocal >= 14.0 {
			// Late lunch detected - check if adjusting timezone would normalize it
			suggestedOffsetCorrection := 0

			// If lunch is at 2pm, shifting 2 hours earlier would make it noon (normal)
			// If lunch is at 3pm, shifting 3 hours earlier would make it noon
			if lunchStartLocal >= 14.0 && lunchStartLocal < 15.0 {
				suggestedOffsetCorrection = +2 // Shift 2 hours east (less negative offset)
			} else if lunchStartLocal >= 15.0 && lunchStartLocal < 16.0 {
				suggestedOffsetCorrection = +3 // Shift 3 hours east (less negative offset)
			}

			// Apply correction if it results in a valid US timezone
			potentialOffset := offsetInt + suggestedOffsetCorrection
			if suggestedOffsetCorrection != 0 && potentialOffset >= -8 && potentialOffset <= -5 {
				d.logger.Info("late lunch suggests timezone correction", "username", username,
					"lunch_start", lunchStartLocal, "current_offset", offsetInt,
					"suggested_correction", suggestedOffsetCorrection, "new_offset", potentialOffset)

				// Apply the correction
				offsetInt = potentialOffset
				detectedTimezone = timezoneFromOffset(offsetInt)

				// Active hours in UTC don't change - no need to recalculate

				// Recalculate lunch with corrected offset
				newLunchStart, newLunchEnd, newLunchConfidence := lunch.DetectLunchBreakNoonCentered(halfHourCounts, offsetInt)

				// If the new lunch time is more reasonable, keep the correction
				if newLunchStart >= 11.5 && newLunchStart <= 13.0 {
					lunchStart = newLunchStart
					lunchEnd = newLunchEnd
					lunchConfidence = newLunchConfidence

					// Boost confidence since lunch correction worked
					confidence = math.Min(confidence+0.15, 0.85)
					d.logger.Info("lunch-based timezone correction successful", "username", username,
						"new_lunch_start", lunchStart, "new_offset", offsetInt, "new_confidence", confidence)
				} else {
					// Revert if it didn't help
					offsetInt -= suggestedOffsetCorrection
					detectedTimezone = timezoneFromOffset(offsetInt)
					// Active hours in UTC don't change - no need to recalculate
					d.logger.Debug("lunch-based correction didn't improve, reverting", "username", username)
				}

				// Recalculate peak with final offset
				peakStart, peakEnd, peakCount = timezone.DetectPeakProductivityWithHalfHours(halfHourCounts, offsetInt)
			}
		}
	}

	// If we have suspicious work hours and detected European timezone,
	// but sleep pattern could fit Asia, consider adjusting to Asia
	// UNLESS we have strong evidence for Europe (e.g., Polish name)
	if suspiciousWorkHours && alternativeTimezone == "UTC+8" && offsetInt <= 3 {
		// Get user's full name to check for regional indicators
		user, _, _, _, err := d.githubClient.FetchUserEnhancedGraphQL(ctx, username)
		if err != nil && !errors.Is(err, github.ErrNoGitHubToken) && !errors.Is(err, github.ErrUserNotFound) {
			d.logger.Debug("failed to fetch user for regional check", "username", username, "error", err)
		}
		isLikelyEuropean := false

		if user != nil && user.Name != "" {
			// Check for Polish name indicators
			if isPolishName(user.Name) {
				isLikelyEuropean = true
				d.logger.Info("Polish name detected, keeping European timezone despite unusual hours",
					"username", username, "name", user.Name, "timezone", detectedTimezone)
			}
		}

		// Also check for activity in European projects/organizations
		// Sort org names for deterministic iteration
		var orgNames []string
		for orgName := range orgCounts {
			orgNames = append(orgNames, orgName)
		}
		sort.Strings(orgNames)
		for _, orgName := range orgNames {
			orgLower := strings.ToLower(orgName)
			if strings.Contains(orgLower, "canonical") || strings.Contains(orgLower, "ubuntu") {
				// Canonical/Ubuntu has significant European presence
				isLikelyEuropean = true
				d.logger.Info("European organization activity detected, keeping European timezone",
					"username", username, "org", orgName)
				break
			}
		}

		if !isLikelyEuropean {
			d.logger.Info("adjusting timezone from Europe to Asia due to unreasonable work hours",
				"username", username, "original", detectedTimezone, "adjusted", alternativeTimezone,
				"work_start_utc", activeStartUTC)
			detectedTimezone = alternativeTimezone
			offsetInt = 8

			// Active hours in UTC don't change - no need to recalculate

			// Recalculate lunch with new offset
			lunchStart, lunchEnd, lunchConfidence = lunch.DetectLunchBreakNoonCentered(halfHourCounts, offsetInt)
		}

		// Recalculate peak with new offset
		peakStart, peakEnd, peakCount = timezone.DetectPeakProductivityWithHalfHours(halfHourCounts, offsetInt)

		confidence = 0.7 // Moderate confidence after adjustment

		d.logger.Info("recalculated work hours after timezone adjustment", "username", username,
			"new_work_start_utc", activeStartUTC, "new_work_end_utc", activeEndUTC, "new_offset", offsetInt)
	}

	// Active hours are already in UTC from calculateTypicalActiveHours
	// No conversion needed for storage

	// Detect sleep periods using 30-minute resolution with buffer
	sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(halfHourCounts)
	d.logger.Info("detected sleep buckets UTC",
		"username", username,
		"sleepBuckets", sleepBuckets,
		"numBuckets", len(sleepBuckets))

	// Log what this translates to in local time for debugging
	if len(sleepBuckets) > 0 && offsetInt != 0 {
		localStart := sleepBuckets[0] + float64(offsetInt)
		if localStart < 0 {
			localStart += 24
		} else if localStart >= 24 {
			localStart -= 24
		}
		localEnd := sleepBuckets[len(sleepBuckets)-1] + 0.5 + float64(offsetInt)
		if localEnd < 0 {
			localEnd += 24
		} else if localEnd >= 24 {
			localEnd -= 24
		}
		d.logger.Info("sleep period in local time",
			"username", username,
			"local_start", localStart,
			"local_end", localEnd,
			"offset", offsetInt)
	}

	// Refine sleep hours using the more precise half-hour data
	// If we have half-hour sleep data, use it to create more accurate hourly sleep hours
	refinedSleepHours := refineHourlySleepFromBuckets(quietHours, sleepBuckets, halfHourCounts)

	// Calculate sleep ranges from buckets for 30-minute granularity
	sleepRanges := CalculateSleepRangesFromBuckets(sleepBuckets, detectedTimezone)

	result := &Result{
		Username:         username,
		Timezone:         detectedTimezone,
		ActivityTimezone: detectedTimezone, // Pure activity-based result
		SleepHoursUTC:    refinedSleepHours,
		SleepRangesLocal: sleepRanges,   // Pre-calculated sleep ranges in local time
		SleepBucketsUTC:  sleepBuckets,  // 30-minute resolution sleep periods
		Timeline:         allTimestamps, // Store the full timeline for text sample generation
		ActiveHoursLocal: struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		}{
			Start: tzconvert.UTCToLocal(activeStartUTC, offsetInt),
			End:   tzconvert.UTCToLocal(activeEndUTC, offsetInt),
		},
		ActiveHoursUTC: struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		}{
			Start: activeStartUTC,
			End:   activeEndUTC,
		},
		TopOrganizations:           topOrgs,
		Confidence:                 confidence,
		Method:                     "activity_patterns",
		HalfHourlyActivityUTC:      halfHourCounts,  // Store 30-minute resolution data
		HourlyOrganizationActivity: hourOrgActivity, // Store org-specific activity
		TimezoneCandidates:         candidates,      // Top 3 timezone candidates with analysis
	}

	// Add activity date range information
	result.ActivityDateRange.OldestActivity = oldestActivity
	result.ActivityDateRange.NewestActivity = newestActivity
	result.ActivityDateRange.TotalDays = totalDays
	result.ActivityDateRange.SpansDSTTransitions = spansDSTTransitions

	// Note: detectLunchBreakNoonCentered and timezone.DetectPeakProductivityWithHalfHours already return UTC hours
	// so no conversion is needed

	// Store lunch hours in UTC
	result.LunchHoursUTC = struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Confidence float64 `json:"confidence"`
	}{
		Start:      lunchStart, // Already in UTC
		End:        lunchEnd,   // Already in UTC
		Confidence: lunchConfidence,
	}

	// Store lunch hours in Local (converted from UTC)
	result.LunchHoursLocal = struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Confidence float64 `json:"confidence"`
	}{
		Start:      tzconvert.UTCToLocal(lunchStart, offsetInt),
		End:        tzconvert.UTCToLocal(lunchEnd, offsetInt),
		Confidence: lunchConfidence,
	}

	// Store peak productivity window in UTC
	result.PeakProductivityUTC = struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Count int     `json:"count"`
	}{
		Start: peakStart, // Already in UTC
		End:   peakEnd,   // Already in UTC
		Count: peakCount,
	}

	// Store peak productivity window in Local (converted from UTC)
	result.PeakProductivityLocal = struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Count int     `json:"count"`
	}{
		Start: tzconvert.UTCToLocal(peakStart, offsetInt),
		End:   tzconvert.UTCToLocal(peakEnd, offsetInt),
		Count: peakCount,
	}

	d.logger.Debug("detected lunch break", "username", username,
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence)

	return result
}
