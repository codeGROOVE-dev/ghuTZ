package ghutz

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// GlobalLunchPattern represents the best lunch pattern found globally in UTC
type GlobalLunchPattern struct {
	startUTC    float64
	endUTC      float64
	confidence  float64
	dropPercent float64
}

// aggregateHalfHoursToHours converts 30-minute buckets back to hourly buckets for display
// This keeps CLI output and Gemini communication simple while using 30-min precision internally
func aggregateHalfHoursToHours(halfHourCounts map[float64]int) map[int]int {
	hourCounts := make(map[int]int)
	
	for bucket, count := range halfHourCounts {
		hour := int(bucket) // Convert 12.0 and 12.5 both to hour 12
		hourCounts[hour] += count
	}
	
	return hourCounts
}

func (d *Detector) tryActivityPatternsWithEvents(ctx context.Context, username string, events []PublicEvent) *Result {
	// Collect all timestamps first (we'll deduplicate and limit later)
	type timestampEntry struct {
		time   time.Time
		source string // for debugging
		org    string // organization/owner name
	}
	allTimestamps := []timestampEntry{}
	orgCounts := make(map[string]int) // Track organization activity

	// Add events
	eventOldest := time.Now()
	eventNewest := time.Time{}
	for _, event := range events {
		// Extract organization from repo name (e.g., "kubernetes/kubernetes" -> "kubernetes")
		org := ""
		if event.Repo.Name != "" {
			if idx := strings.Index(event.Repo.Name, "/"); idx > 0 {
				org = event.Repo.Name[:idx]
			}
		}
		allTimestamps = append(allTimestamps, timestampEntry{
			time:   event.CreatedAt,
			source: "event",
			org:    org,
		})
		if event.CreatedAt.Before(eventOldest) {
			eventOldest = event.CreatedAt
		}
		if event.CreatedAt.After(eventNewest) {
			eventNewest = event.CreatedAt
		}
	}

	if len(events) > 0 {
		d.logger.Debug("GitHub Events data", "username", username,
			"count", len(events),
			"oldest", eventOldest.Format("2006-01-02"),
			"newest", eventNewest.Format("2006-01-02"),
			"days_covered", int(eventNewest.Sub(eventOldest).Hours()/24))
	}

	// Also fetch gist timestamps
	if gistTimestamps, err := d.fetchUserGists(ctx, username); err == nil && len(gistTimestamps) > 0 {
		for _, ts := range gistTimestamps {
			allTimestamps = append(allTimestamps, timestampEntry{
				time:   ts,
				source: "gist",
				org:    username, // gists are owned by the user
			})
		}
		d.logger.Debug("added gist timestamps", "username", username, "count", len(gistTimestamps))
	}
	
	// Track the oldest event to check data coverage
	var oldestEventTime time.Time
	if len(events) > 0 {
		oldestEventTime = events[len(events)-1].CreatedAt // Assuming sorted by recency
		for _, event := range events {
			if event.CreatedAt.Before(oldestEventTime) {
				oldestEventTime = event.CreatedAt
			}
		}
	}

	// Implement adaptive data collection strategy:
	// Target 200 data points minimum, expand time window progressively
	const targetDataPoints = 200
	needSupplemental := len(allTimestamps) < targetDataPoints

	if needSupplemental {
		d.logger.Debug("need more data points, fetching supplemental data", "username", username, 
			"current_count", len(allTimestamps), "target", targetDataPoints)

		// Fetch ALL additional data from all sources
		additionalData := d.fetchSupplementalActivity(ctx, username)

		// Add all timestamps from supplemental data
		prOldest := time.Now()
		prNewest := time.Time{}
		for _, pr := range additionalData.PullRequests {
			// Extract organization from repository
			org := ""
			if pr.Repository != "" {
				if idx := strings.Index(pr.Repository, "/"); idx > 0 {
					org = pr.Repository[:idx]
				}
			}
			allTimestamps = append(allTimestamps, timestampEntry{
				time:   pr.CreatedAt,
				source: "pr",
				org:    org,
			})
			if pr.CreatedAt.Before(prOldest) {
				prOldest = pr.CreatedAt
			}
			if pr.CreatedAt.After(prNewest) {
				prNewest = pr.CreatedAt
			}
		}

		if len(additionalData.PullRequests) > 0 {
			d.logger.Debug("Pull Requests data", "username", username,
				"count", len(additionalData.PullRequests),
				"oldest", prOldest.Format("2006-01-02"),
				"newest", prNewest.Format("2006-01-02"),
				"days_covered", int(prNewest.Sub(prOldest).Hours()/24))
		}

		issueOldest := time.Now()
		issueNewest := time.Time{}
		for _, issue := range additionalData.Issues {
			// Extract organization from repository
			org := ""
			if issue.Repository != "" {
				if idx := strings.Index(issue.Repository, "/"); idx > 0 {
					org = issue.Repository[:idx]
				}
			}
			allTimestamps = append(allTimestamps, timestampEntry{
				time:   issue.CreatedAt,
				source: "issue",
				org:    org,
			})
			if issue.CreatedAt.Before(issueOldest) {
				issueOldest = issue.CreatedAt
			}
			if issue.CreatedAt.After(issueNewest) {
				issueNewest = issue.CreatedAt
			}
		}

		if len(additionalData.Issues) > 0 {
			d.logger.Debug("Issues data", "username", username,
				"count", len(additionalData.Issues),
				"oldest", issueOldest.Format("2006-01-02"),
				"newest", issueNewest.Format("2006-01-02"),
				"days_covered", int(issueNewest.Sub(issueOldest).Hours()/24))
		}

		commentOldest := time.Now()
		commentNewest := time.Time{}
		for _, comment := range additionalData.Comments {
			// Comments don't have repository info directly
			allTimestamps = append(allTimestamps, timestampEntry{
				time:   comment.CreatedAt,
				source: "comment",
				org:    "",
			})
			if comment.CreatedAt.Before(commentOldest) {
				commentOldest = comment.CreatedAt
			}
			if comment.CreatedAt.After(commentNewest) {
				commentNewest = comment.CreatedAt
			}
		}

		if len(additionalData.Comments) > 0 {
			d.logger.Debug("Comments data", "username", username,
				"count", len(additionalData.Comments),
				"oldest", commentOldest.Format("2006-01-02"),
				"newest", commentNewest.Format("2006-01-02"),
				"days_covered", int(commentNewest.Sub(commentOldest).Hours()/24))
		}

		d.logger.Debug("collected all timestamps", "username", username,
			"total_before_dedup", len(allTimestamps),
			"prs", len(additionalData.PullRequests),
			"issues", len(additionalData.Issues),
			"comments", len(additionalData.Comments))
	}

	// Sort timestamps by recency (newest first) before applying time windows
	sort.Slice(allTimestamps, func(i, j int) bool {
		return allTimestamps[i].time.After(allTimestamps[j].time)
	})
	
	// Filter out events older than 5 years to avoid stale patterns
	now := time.Now()
	fiveYearsAgo := now.AddDate(-5, 0, 0)
	var recentTimestamps []timestampEntry
	for _, entry := range allTimestamps {
		if entry.time.After(fiveYearsAgo) {
			recentTimestamps = append(recentTimestamps, entry)
		}
	}
	allTimestamps = recentTimestamps

	// Progressive time window strategy: start with 1 month, increase by 1.25x until we get target events or hit 5 years
	var finalTimestamps []timestampEntry
	monthsBack := 1.0
	const maxMonths = 60 // 5 years maximum
	
	for monthsBack <= maxMonths {
		cutoffTime := now.AddDate(0, -int(monthsBack), 0)
		var windowTimestamps []timestampEntry
		
		for _, entry := range allTimestamps {
			if entry.time.After(cutoffTime) {
				windowTimestamps = append(windowTimestamps, entry)
			}
		}
		
		d.logger.Debug("progressive time window", "username", username, 
			"months_back", monthsBack, "events_in_window", len(windowTimestamps), "target", targetDataPoints)
		
		// Always update finalTimestamps with the current window
		finalTimestamps = windowTimestamps
		
		if len(windowTimestamps) >= targetDataPoints || monthsBack >= maxMonths {
			d.logger.Debug("selected time window", "username", username, 
				"final_months_back", monthsBack, "final_events", len(finalTimestamps))
			break
		}
		
		// Increase time window by 1.25x for more granular progression
		monthsBack = monthsBack * 1.25
	}
	
	allTimestamps = finalTimestamps

	// Deduplicate all unique timestamps (no cap with adaptive collection)
	uniqueTimestamps := make(map[time.Time]bool)
	hourCounts := make(map[int]int)
	halfHourCounts := make(map[float64]int) // 30-minute buckets: 0.0, 0.5, 1.0, 1.5, etc.
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
	// This keeps output simple and readable while using 30-min precision for internal calculations
	displayHourCounts := aggregateHalfHoursToHours(halfHourCounts)

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

	d.logger.Debug("activity data summary", "username", username,
		"total_timestamps_collected", len(allTimestamps),
		"duplicates_removed", duplicates,
		"unique_timestamps_used", totalActivity,
		"oldest_activity", oldestActivity.Format("2006-01-02"),
		"newest_activity", newestActivity.Format("2006-01-02"),
		"total_days", totalDays)

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

	quietHours := findSleepHours(hourCounts)
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

	d.logger.Debug("activity pattern summary", "username", username,
		"total_activity", totalActivity,
		"sleep_hours", quietHours,
		"most_active_hours", mostActiveHours,
		"max_activity_count", maxActivity)

	hourlyActivity := make([]int, 24)
	for hour := range 24 {
		hourlyActivity[hour] = hourCounts[hour]
	}
	d.logger.Debug("hourly activity distribution", "username", username, "hours_utc", hourlyActivity)

	// Find the middle of sleep hours, handling wrap-around
	start := quietHours[0]
	end := quietHours[len(quietHours)-1]
	var midQuiet float64

	// Check if quiet hours wrap around midnight
	if end < start || (start == 0 && end == 23) {
		// Wraps around (e.g., 22-3)
		totalHours := (24 - start) + end + 1
		midQuiet = float64(start) + float64(totalHours)/2.0
		if midQuiet >= 24 {
			midQuiet -= 24
		}
	} else {
		// Normal case (e.g., 3-8)
		// For a 6-hour window from start to end, the middle is (start + end) / 2
		midQuiet = (float64(start) + float64(end)) / 2.0
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
	if float64(europeanActivity) > float64(americanActivity)*1.2 {
		// Strong European pattern
		// Europeans typically have earlier sleep patterns, midpoint around 2am
		assumedSleepMidpoint = 2.0
		d.logger.Debug("detected European activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	} else if float64(americanActivity) > float64(europeanActivity)*1.2 {
		// Strong American pattern
		// Americans typically sleep midnight-5am, midpoint around 2.5am
		// Using 2.5 instead of 3.5 to better match Eastern Time patterns
		assumedSleepMidpoint = 2.5
		d.logger.Debug("detected American activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	} else {
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
	var candidates []TimezoneCandidate // Store timezone candidates for Gemini
	
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
			
			// Make a simple initial guess
			offsetFromUTC = -5.0 // Start with a middle ground
			if midQuiet <= 5 {
				offsetFromUTC = -4.0 // Likely Eastern (EDT)
			} else if midQuiet <= 7 {
				offsetFromUTC = -5.0 // Likely Eastern (EST) or Central (CDT)
			} else if midQuiet <= 9 {
				offsetFromUTC = -6.0 // Likely Central (CST) or Mountain (MDT)
			} else if midQuiet <= 11 {
				offsetFromUTC = -7.0 // Likely Mountain (MST) or Pacific (PDT)
			} else {
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

	timezone := timezoneFromOffset(offsetInt)
	confidence := 0.5 // Default confidence, will be updated if we have candidates
	d.logger.Debug("Activity-based UTC offset", "username", username, "offset", offsetInt, "timezone", timezone)

	// Log the detected offset for verification
	if timezone != "" {
		now := time.Now().UTC()
		// Calculate what the local time would be with this offset
		localTime := now.Add(time.Duration(offsetInt) * time.Hour)
		d.logger.Debug("timezone verification", "username", username, "timezone", timezone,
			"utc_time", now.Format("15:04 MST"),
			"estimated_local_time", localTime.Format("15:04"),
			"offset_hours", offsetInt)
	}

	// Calculate typical active hours in local time (excluding outliers)
	// We need to convert from UTC to local time
	activeStart, activeEnd := calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)
	
	// STEP 1: Find the best global lunch pattern in UTC (timezone-independent)
	bestGlobalLunch := findBestGlobalLunchPattern(halfHourCounts)
	d.logger.Debug("best global lunch pattern", "username", username, 
		"start_utc", bestGlobalLunch.startUTC, 
		"confidence", bestGlobalLunch.confidence,
		"drop_percent", bestGlobalLunch.dropPercent)
	
	// DEBUG: Let's see what the half-hour data looks like around the detected lunch time
	if bestGlobalLunch.startUTC > 0 {
		debugStart := bestGlobalLunch.startUTC - 1.0
		debugEnd := bestGlobalLunch.startUTC + 2.0
		d.logger.Debug("lunch pattern context", "username", username,
			fmt.Sprintf("%.1f", debugStart), halfHourCounts[debugStart],
			fmt.Sprintf("%.1f", bestGlobalLunch.startUTC-0.5), halfHourCounts[bestGlobalLunch.startUTC-0.5],
			fmt.Sprintf("%.1f", bestGlobalLunch.startUTC), halfHourCounts[bestGlobalLunch.startUTC],
			fmt.Sprintf("%.1f", bestGlobalLunch.startUTC+0.5), halfHourCounts[bestGlobalLunch.startUTC+0.5],
			fmt.Sprintf("%.1f", debugEnd), halfHourCounts[debugEnd])
	}
	
	
	// Evaluate multiple timezone offsets to find the best candidates
	// Test a wider range (±5 hours) to avoid missing the correct timezone
	// when initial detection is influenced by evening activity patterns
	minOffset := offsetInt - 5
	maxOffset := offsetInt + 5
	
	// But still respect global bounds
	if minOffset < -12 {
		minOffset = -12
	}
	if maxOffset > 14 {
		maxOffset = 14
	}
	
	for testOffset := minOffset; testOffset <= maxOffset; testOffset++ {
		
		// Calculate metrics for this offset
		// 1. Lunch timing analysis
		testLunchStart, testLunchEnd, testLunchConf := detectLunchBreakNoonCentered(halfHourCounts, testOffset)
		lunchLocalStart := math.Mod(testLunchStart+float64(testOffset)+24, 24)
		
		// Work start time (needed to validate lunch)
		testWorkStart := (activeStart + testOffset + 24) % 24
		
		// Find first activity in this timezone (more accurate than activeStart which uses initial offset)
		firstActivityLocal := 24.0
		for utcHour := 0; utcHour < 24; utcHour++ {
			if hourCounts[utcHour] > 0 {
				localHour := float64((utcHour + testOffset + 24) % 24)
				if localHour < firstActivityLocal {
					firstActivityLocal = localHour
				}
			}
		}
		
		// Lunch is only reasonable if:
		// 1. Detected in the 10am-2:30pm window
		// 2. At least 1 hour after first activity (can't have lunch right after arriving)
		lunchReasonable := testLunchStart >= 0 && 
			lunchLocalStart >= 10.0 && 
			lunchLocalStart <= 14.5 && 
			testLunchConf >= 0.3 &&
			lunchLocalStart >= firstActivityLocal + 1.0 // At least 1 hour after first activity
		
		// Debug for UTC-4
		if testOffset == -4 && testLunchStart >= 0 {
			fmt.Printf("UTC-4 lunch found: start=%.1f UTC (%.1f local), conf=%.2f, reasonable=%v\n", 
				testLunchStart, lunchLocalStart, testLunchConf, lunchReasonable)
		} else if testOffset == -4 {
			fmt.Printf("UTC-4 NO lunch found (returned -1)\n")
		}
		
		// Calculate lunch dip strength
		lunchDipStrength := 0.0
		if testLunchConf > 0 && testLunchStart >= 0 {
			// Get activity before and during lunch to calculate dip percentage
			beforeLunchBucket := testLunchStart - 1.0
			if beforeLunchBucket < 0 {
				beforeLunchBucket += 24
			}
			beforeActivity := float64(halfHourCounts[beforeLunchBucket])
			lunchActivity := float64(halfHourCounts[testLunchStart])
			if beforeActivity > 0 {
				lunchDipStrength = (beforeActivity - lunchActivity) / beforeActivity
			}
		}
		
		// 2. Sleep timing analysis  
		// Calculate what local sleep time would be for this offset
		sleepLocalMid := math.Mod(midQuiet+float64(testOffset)+24, 24)
		// Sleep is reasonable if mid-sleep is between 10pm and 5am
		sleepReasonable := (sleepLocalMid >= 0 && sleepLocalMid <= 5) || sleepLocalMid >= 22
		
		// 3. Work hours analysis
		// testWorkStart already calculated above for lunch validation
		workReasonable := testWorkStart >= 6 && testWorkStart <= 10 // Allow 6am starts for early risers
		
		// 4. Evening activity (7-11pm local)
		// To convert local hour to UTC: UTC = local - offset
		// For UTC-5: 7pm local = 19:00 local = 19 - (-5) = 24 = 0 UTC
		eveningActivity := 0
		for localHour := 19; localHour <= 23; localHour++ {
			utcHour := (localHour - testOffset + 24) % 24
			eveningActivity += hourCounts[utcHour]
		}
		
		// 5. European timezone validation - Europeans should have morning activity by 10am latest
		europeanMorningActivityCheck := true
		if testOffset >= 0 && testOffset <= 3 { // European timezones (UTC+0 to UTC+3)
			// Check for activity between 8am-10am local time
			morningActivity := 0
			for localHour := 8; localHour <= 10; localHour++ {
				utcHour := (localHour - testOffset + 24) % 24
				morningActivity += hourCounts[utcHour]
			}
			// If no morning activity, this is likely not a real European timezone
			if morningActivity == 0 {
				europeanMorningActivityCheck = false
			}
		}
		
		// Calculate overall confidence score on 0-100 scale
		// Prioritize lunch and sleep times over evening activity
		testConfidence := 0.0
		adjustments := []string{} // Track all adjustments for debugging
		adjustments = append(adjustments, fmt.Sprintf("base score for UTC%+d", testOffset))
		
		// Sleep timing (most reliable) - 15 points max (increased weight)
		// Early sleep (9-10pm) is a strong Pacific Time indicator
		if sleepReasonable {
			// Check for early sleep pattern (strong Pacific indicator)
			// Find the first quiet hour to determine sleep start
			sleepStartUTC := -1
			if len(quietHours) > 0 {
				sleepStartUTC = quietHours[0]
			}
			
			if sleepLocalMid >= 1 && sleepLocalMid <= 4 {
				testConfidence += 12 // Perfect sleep timing (1-4am)
				adjustments = append(adjustments, fmt.Sprintf("+12 (perfect sleep 1-4am, mid=%.1f)", sleepLocalMid))
				// Bonus for early sleep (9-11pm start) - Pacific pattern
				if sleepStartUTC >= 0 {
					sleepStartLocal := float64((sleepStartUTC + testOffset + 24) % 24)
					if sleepStartLocal >= 21 && sleepStartLocal <= 23 {
						testConfidence += 3 // Early sleep bonus (Pacific indicator)
						adjustments = append(adjustments, fmt.Sprintf("+3 (early sleep bonus, start=%.0fpm)", sleepStartLocal-12))
					}
				}
			} else if sleepLocalMid >= 0 && sleepLocalMid <= 5 {
				testConfidence += 8 // Good sleep timing
				adjustments = append(adjustments, fmt.Sprintf("+8 (good sleep, mid=%.1f)", sleepLocalMid))
				// Still give bonus for early sleep
				if sleepStartUTC >= 0 {
					sleepStartLocal := float64((sleepStartUTC + testOffset + 24) % 24)
					if sleepStartLocal >= 21 && sleepStartLocal <= 23 {
						testConfidence += 2 // Early sleep bonus
						adjustments = append(adjustments, fmt.Sprintf("+2 (early sleep, start=%.0fpm)", sleepStartLocal-12))
					}
				}
			} else if sleepLocalMid >= 22 || sleepLocalMid <= 6 {
				testConfidence += 4 // Sleep detected but unusual timing
				adjustments = append(adjustments, fmt.Sprintf("+4 (unusual sleep, mid=%.1f)", sleepLocalMid))
			}
		} else {
			adjustments = append(adjustments, "0 (no reasonable sleep pattern)")
		}
		
		// STEP 2: Distance-from-noon lunch bonus system
		noonDistanceBonus := 0.0
		if bestGlobalLunch.confidence > 0 {
			// Calculate what local time the global lunch would be for this timezone
			globalLunchLocalTime := math.Mod(bestGlobalLunch.startUTC+float64(testOffset)+24, 24)
			
			// Calculate distance from noon (12:00) in 30-minute buckets
			distanceFromNoon := math.Abs(globalLunchLocalTime - 12.0)
			
			// Convert distance to number of 30-minute buckets
			bucketsFromNoon := distanceFromNoon / 0.5
			
			// Moderate bonus based on proximity to noon  
			// Perfect noon (0 buckets away) = 10 points
			// 1 bucket away (30 min) = 6 points  
			// 2 buckets away (1 hour) = 3 points
			// 3 buckets away (1.5 hours) = 1 point
			// 4+ buckets away (2+ hours) = minimal points
			if bucketsFromNoon == 0 {
				noonDistanceBonus = 10 * bestGlobalLunch.confidence // Perfect noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (perfect noon lunch)", noonDistanceBonus))
			} else if bucketsFromNoon <= 1 {
				noonDistanceBonus = 6 * bestGlobalLunch.confidence // Within 30 min of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 30min of noon)", noonDistanceBonus))
			} else if bucketsFromNoon <= 2 {
				noonDistanceBonus = 3 * bestGlobalLunch.confidence // Within 1 hour of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 1hr of noon)", noonDistanceBonus))
			} else if bucketsFromNoon <= 3 {
				noonDistanceBonus = 1 * bestGlobalLunch.confidence // Within 1.5 hours of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 1.5hr of noon)", noonDistanceBonus))
			} else if bucketsFromNoon <= 4 {
				noonDistanceBonus = 0.5 * bestGlobalLunch.confidence // Within 2 hours of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 2hr of noon)", noonDistanceBonus))
			} else {
				noonDistanceBonus = 0.2 * bestGlobalLunch.confidence // Too far from reasonable lunch time
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch >2hr from noon)", noonDistanceBonus))
			}
			
			testConfidence += noonDistanceBonus
			
			
		}
		
		// Lunch timing - 15 points max (strong signal when clear)
		if lunchReasonable {
			lunchScore := 0.0
			if lunchLocalStart >= 11.5 && lunchLocalStart <= 13.5 {
				// 11:30am to 1:30pm are all equally good lunch times
				lunchScore = 12 // Perfect lunch timing (includes 1:30pm for IdlePhysicist)
				adjustments = append(adjustments, fmt.Sprintf("+12 (perfect lunch 11:30am-1:30pm, at %.1f)", lunchLocalStart))
			} else if lunchLocalStart >= 11.0 && lunchLocalStart <= 14.0 {
				lunchScore = 10 // Good lunch timing (11am-2pm)
				adjustments = append(adjustments, fmt.Sprintf("+10 (good lunch 11am-2pm, at %.1f)", lunchLocalStart))
			} else if lunchLocalStart >= 10.5 && lunchLocalStart <= 14.5 {
				lunchScore = 6 // Acceptable lunch timing
				adjustments = append(adjustments, fmt.Sprintf("+6 (acceptable lunch 10:30am-2:30pm, at %.1f)", lunchLocalStart))
			} else {
				lunchScore = 2 // Lunch detected but unusual timing
				adjustments = append(adjustments, fmt.Sprintf("+2 (unusual lunch timing at %.1f)", lunchLocalStart))
			}
			// Boost score based on dip strength (up to 3 bonus points for strong drops)
			// A 50%+ drop like IdlePhysicist's 58.3% is a very strong signal
			dipBonus := math.Min(3, lunchDipStrength*6)
			if dipBonus > 0 {
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch dip strength %.1f%%)", dipBonus, lunchDipStrength*100))
			}
			lunchScore += dipBonus
			
			// PENALTY for weak lunch signals after 1:30pm
			// A 25% drop at 2pm is likely not a real lunch
			if lunchLocalStart > 13.5 && lunchDipStrength < 0.4 {
				oldScore := lunchScore
				lunchScore *= 0.3 // Severely reduce score for weak late lunches
				adjustments = append(adjustments, fmt.Sprintf("-%.1f (weak late lunch penalty)", oldScore-lunchScore))
			}
			
			finalLunchScore := math.Min(15, lunchScore)
			testConfidence += finalLunchScore
		} else if testLunchStart < 0 {
			// No detectable lunch break - apply penalty
			// This might indicate invalid timezone or unusual work pattern
			testConfidence -= 5
			adjustments = append(adjustments, "-5 (no lunch detected)")
		}
		
		// Work hours - 8 points max, with penalties for too-early starts
		if workReasonable {
			if testWorkStart >= 7 && testWorkStart <= 9 {
				testConfidence += 8 // Good work start (7-9am)
				adjustments = append(adjustments, fmt.Sprintf("+8 (good work start %dam)", testWorkStart))
			} else if testWorkStart == 6 {
				testConfidence += 4 // 6am is early but some people do it
				adjustments = append(adjustments, "+4 (early 6am work start)")
			} else if testWorkStart >= 5 && testWorkStart <= 10 {
				testConfidence += 2 // Acceptable but unusual
				adjustments = append(adjustments, fmt.Sprintf("+2 (unusual work start %dam)", testWorkStart))
			} else {
				testConfidence += 1 // Work hours detected but very unusual
				adjustments = append(adjustments, fmt.Sprintf("+1 (very unusual work start %dam)", testWorkStart))
			}
		} else {
			// Apply STRONG penalty for unreasonable work hours
			if testWorkStart >= 0 && testWorkStart < 6 {
				// Starting work before 6am is highly suspicious
				testConfidence -= 10 // Strong penalty for pre-6am starts
				adjustments = append(adjustments, fmt.Sprintf("-10 (suspicious pre-6am start %dam)", testWorkStart))
				if testWorkStart < 5 {
					testConfidence -= 5 // Extra penalty for pre-5am starts
					adjustments = append(adjustments, fmt.Sprintf("-5 (extra penalty for pre-5am start %dam)", testWorkStart))
				}
			}
		}
		
		// Evening activity - up to 1 point (very weak signal, many users only use GitHub for work)
		// NOT ALL GITHUB USERS CODE IN THE EVENING
		// What looks like "evening" in one timezone might be afternoon work in another
		// Example: 19:00-23:00 UTC is 7-11pm for UTC+1 but 3-7pm for UTC-4 (normal work hours)
		if eveningActivity > 0 {
			eveningRatio := float64(eveningActivity) / float64(totalActivity)
			
			// Only give points if evening activity is VERY SUBSTANTIAL (>30% of total)
			// This helps avoid misinterpreting afternoon work as evening coding
			if eveningRatio > 0.3 {
				// Ultra conservative scoring - max 1 point, only if >30% evening
				eveningPoints := 1 * math.Min(1.0, (eveningRatio-0.3)*3.33)
				testConfidence += eveningPoints
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (evening activity %.1f%%)", eveningPoints, eveningRatio*100))
			}
			// No points for <30% evening activity - likely just late afternoon work
			
			// PENALTY if low evening activity but interpreted as high due to timezone
			// If <10% evening activity, this timezone might be wrong
			if eveningRatio < 0.1 && testOffset >= -5 && testOffset <= -3 {
				// Eastern/Atlantic timezones with low evening = probably wrong
				testConfidence -= 2
				adjustments = append(adjustments, fmt.Sprintf("-2 (low evening %.1f%% for Eastern)", eveningRatio*100))
			}
		}
		
		// Work hours activity bonus - 2 points max
		// Check activity during expected work hours (9am-5pm local) for this timezone
		workHoursActivity := 0
		for localHour := 9; localHour <= 17; localHour++ {
			utcHour := (localHour - testOffset + 24) % 24
			workHoursActivity += hourCounts[utcHour]
		}
		if workHoursActivity > 0 && totalActivity > 0 {
			workRatio := float64(workHoursActivity) / float64(totalActivity)
			// Bonus for having high work hours activity (up to 2 points)
			workHoursBonus := 2 * math.Min(1.0, workRatio*1.5)
			testConfidence += workHoursBonus
			if workHoursBonus > 0 {
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (work hours activity %.1f%%)", workHoursBonus, workRatio*100))
			}
		}
		
		// Peak activity timing bonus - 5 points max
		// Find the hour with maximum activity and check if it occurs during ideal work hours (10am-4pm local)
		maxActivity := 0
		maxActivityHour := -1
		for hour, count := range hourCounts {
			if count > maxActivity {
				maxActivity = count
				maxActivityHour = hour
			}
		}
		if maxActivityHour >= 0 {
			// Convert peak activity UTC hour to local time for this timezone
			peakLocalHour := (maxActivityHour + testOffset + 24) % 24
			// Bonus for peak activity during ideal work hours (10am-4pm = 10-16 local)
			if peakLocalHour >= 10 && peakLocalHour <= 16 {
				// Perfect timing: 5 points for peak during 1-3pm, 3 points for 12-2pm or 3-4pm
				peakBonus := 3.0
				if peakLocalHour >= 13 && peakLocalHour <= 15 {
					// 1-3pm is ideal afternoon work time  
					peakBonus = 5.0
					adjustments = append(adjustments, fmt.Sprintf("+5 (peak at %dpm ideal)", peakLocalHour-12))
				} else if peakLocalHour >= 12 && peakLocalHour <= 16 {
					// 12-4pm is good work time
					peakBonus = 3.0
					adjustments = append(adjustments, fmt.Sprintf("+3 (peak at %d good work time)", peakLocalHour))
				} else {
					// 10-12pm is morning work time  
					peakBonus = 2.0
					adjustments = append(adjustments, fmt.Sprintf("+2 (peak at %dam morning)", peakLocalHour))
				}
				testConfidence += peakBonus
			}
			
			// PENALTY for late peak activity (after 5pm)
			// Peak productivity after 5pm is unusual for any timezone
			if peakLocalHour >= 17 {
				testConfidence -= 10 // Strong penalty for late peak
				adjustments = append(adjustments, fmt.Sprintf("-10 (late peak at %dpm)", peakLocalHour-12))
			}
			
			// PENALTY for very late work start (after 2pm)
			// Nobody regularly starts work at 2:30pm regardless of timezone
			if firstActivityLocal >= 14 {
				testConfidence -= 20 // Massive penalty for starting after 2pm
				adjustments = append(adjustments, fmt.Sprintf("-20 (work starts after 2pm at %.1f)", firstActivityLocal))
			} else if firstActivityLocal >= 12 {
				testConfidence -= 10 // Moderate penalty for starting after noon
				adjustments = append(adjustments, fmt.Sprintf("-10 (work starts after noon at %.1f)", firstActivityLocal))
			}
		}
		
		// Pacific timezone pattern recognition - 25 points max (increased)
		// Check for classic Pacific timezone indicators
		if testOffset == -8 || testOffset == -7 { // PST or PDT
			// Strong Pacific indicators
			pacificBonus := 0.0
			
			// astrojerms pattern: Peak at 18-19 UTC (10-11am Pacific)
			morning18 := hourCounts[18] // 10am Pacific
			morning19 := hourCounts[19] // 11am Pacific
			if morning18 > 30 || morning19 > 15 {
				pacificBonus += 10.0 // Strong morning peak (astrojerms has 43 at 18:00)
				adjustments = append(adjustments, fmt.Sprintf("+10 (Pacific strong morning peak %d/%d)", morning18, morning19))
			} else if morning18 > 20 || morning19 > 10 {
				pacificBonus += 6.0 // Good morning activity
				adjustments = append(adjustments, fmt.Sprintf("+6 (Pacific good morning %d/%d)", morning18, morning19))
			}
			
			// Lunch dip at 20 UTC (noon Pacific) - astrojerms pattern
			lunch20 := hourCounts[20] // 12pm Pacific
			beforeLunch := hourCounts[19] // 11am Pacific
			if beforeLunch > 0 && lunch20 > 0 && float64(lunch20) < float64(beforeLunch)*0.8 {
				pacificBonus += 5.0 // Clear lunch dip at noon
				adjustments = append(adjustments, fmt.Sprintf("+5 (Pacific noon lunch dip %d->%d)", beforeLunch, lunch20))
			}
			
			// Work starts early: 14-16 UTC (6-8am Pacific)
			early14 := hourCounts[14] // 6am Pacific
			early15 := hourCounts[15] // 7am Pacific
			early16 := hourCounts[16] // 8am Pacific
			if early14 > 0 || early15 > 5 || early16 > 10 {
				pacificBonus += 5.0 // Early morning start (6-8am Pacific)
				adjustments = append(adjustments, fmt.Sprintf("+5 (Pacific early start %d/%d/%d)", early14, early15, early16))
			}
			
			// Low late evening activity: 0-4 UTC (4-8pm Pacific)
			late0 := hourCounts[0]  // 4pm Pacific
			late1 := hourCounts[1]  // 5pm Pacific
			late2 := hourCounts[2]  // 6pm Pacific
			late3 := hourCounts[3]  // 7pm Pacific
			late4 := hourCounts[4]  // 8pm Pacific
			lateTotal := late0 + late1 + late2 + late3 + late4
			if lateTotal > 0 && lateTotal < 50 {
				pacificBonus += 3.0 // Moderate late afternoon/evening activity
				adjustments = append(adjustments, fmt.Sprintf("+3 (Pacific moderate evening %d total)", lateTotal))
			}
			
			// Early sleep pattern: quiet at 5-9 UTC (9pm-1am Pacific)
			if hourCounts[5] == 0 && hourCounts[6] == 0 && hourCounts[7] == 0 {
				pacificBonus += 2.0 // Clear early sleep pattern
				adjustments = append(adjustments, "+2 (Pacific early sleep 9pm-1am)")
			}
			
			if pacificBonus > 0 {
				testConfidence += pacificBonus
			}
		}
		
		// Mountain timezone pattern recognition - 6 points max
		// Check for classic Mountain timezone indicators (UTC-6/UTC-7)
		if testOffset == -6 || testOffset == -7 {
			mountainBonus := 0.0
			
			// Strong 1:30pm lunch pattern like IdlePhysicist
			// For UTC-6: 13:30 local = 19:30 UTC
			// For UTC-7: 13:30 local = 20:30 UTC
			lunchBucket := 19.5
			if testOffset == -7 {
				lunchBucket = 20.5
			}
			beforeLunchBucket := lunchBucket - 0.5
			
			// Check for significant drop at 1:30pm (like IdlePhysicist's 58% drop)
			// But also check that it's actually the strongest lunch signal
			if beforeCount, exists := halfHourCounts[beforeLunchBucket]; exists && beforeCount > 10 {
				if lunchCount, exists := halfHourCounts[lunchBucket]; exists {
					dropRatio := float64(beforeCount-lunchCount) / float64(beforeCount)
					// Only apply bonus if this is a strong drop AND lunch was detected here
					if dropRatio > 0.5 && lunchLocalStart >= 13.0 && lunchLocalStart <= 14.0 { 
						// 50%+ drop at 1:30pm AND lunch was actually detected in that range
						mountainBonus += 6.0 // Mountain lunch pattern (reduced from 8)
						adjustments = append(adjustments, fmt.Sprintf("+6 (Mountain 1:30pm lunch %.1f%% drop)", dropRatio*100))
					}
				}
			}
			
			// Check for caving/outdoor activity patterns (weekend mornings)
			// Mountain timezone folks often have outdoor hobbies
			// This would show as activity patterns different on weekends
			// but we don't have day-of-week data yet
			
			if mountainBonus > 0 {
				testConfidence += mountainBonus
			}
		}
		
		// Eastern timezone pattern recognition - 8 points max
		// Check for classic Eastern timezone indicators (UTC-4/UTC-5)
		if testOffset == -4 || testOffset == -5 {
			easternBonus := 0.0
			
			// Early lunch pattern (11:30am-12:00pm) common in East Coast
			// For UTC-4: 11:30 local = 15:30 UTC
			// For UTC-5: 11:30 local = 16:30 UTC
			earlyLunchBucket := 15.5
			if testOffset == -5 {
				earlyLunchBucket = 16.5
			}
			beforeEarlyLunchBucket := earlyLunchBucket - 0.5
			
			// Check for significant drop at 11:30am (tstromberg pattern)
			if beforeCount, exists := halfHourCounts[beforeEarlyLunchBucket]; exists && beforeCount > 20 {
				if lunchCount, exists := halfHourCounts[earlyLunchBucket]; exists {
					dropRatio := float64(beforeCount-lunchCount) / float64(beforeCount)
					// Strong early lunch signal
					if dropRatio > 0.7 && lunchLocalStart >= 11.0 && lunchLocalStart <= 12.0 {
						// 70%+ drop at 11:30am AND lunch detected in that range
						easternBonus += 8.0 // Strong Eastern early lunch pattern
						adjustments = append(adjustments, fmt.Sprintf("+8 (Eastern 11:30am lunch %.1f%% drop)", dropRatio*100))
					} else if dropRatio > 0.5 && lunchLocalStart >= 11.0 && lunchLocalStart <= 12.5 {
						// 50%+ drop around 11:30am-12:30pm  
						easternBonus += 5.0 // Moderate Eastern lunch pattern
						adjustments = append(adjustments, fmt.Sprintf("+5 (Eastern lunch %.1f%% drop)", dropRatio*100))
					}
				}
			}
			
			// High morning productivity (9-11am) typical of East Coast
			morningActivity := 0
			for localHour := 9.0; localHour <= 11.0; localHour += 0.5 {
				utcHour := localHour - float64(testOffset)
				for utcHour < 0 {
					utcHour += 24
				}
				for utcHour >= 24 {
					utcHour -= 24
				}
				morningActivity += halfHourCounts[utcHour]
			}
			// If strong morning activity, add small bonus
			if morningActivity > 50 {
				easternBonus += 2.0
				adjustments = append(adjustments, fmt.Sprintf("+2 (Eastern high morning activity %d)", morningActivity))
			}
			
			// CRITICAL Eastern pattern: 5pm end-of-day peak (very common)
			// For UTC-4: 17:00 local = 21:00 UTC
			// For UTC-5: 17:00 local = 22:00 UTC
			endOfDayUTC := 21
			if testOffset == -5 {
				endOfDayUTC = 22
			}
			
			// Check if 5pm is the peak hour or near-peak
			if hourCounts[endOfDayUTC] >= 20 {
				// Strong 5pm activity is classic Eastern pattern
				easternBonus += 10.0
				adjustments = append(adjustments, fmt.Sprintf("+10 (Eastern 5pm peak with %d events)", hourCounts[endOfDayUTC]))
				
				// Extra bonus if it's THE peak hour
				isPeak := true
				for h := 0; h < 24; h++ {
					if h != endOfDayUTC && hourCounts[h] > hourCounts[endOfDayUTC] {
						isPeak = false
						break
					}
				}
				if isPeak {
					easternBonus += 5.0
					adjustments = append(adjustments, "+5 (5pm is absolute peak - classic Eastern)")
				}
			}
			
			// Check for 12pm lunch dip pattern (even if weak)
			// For UTC-4: 12:00 local = 16:00 UTC
			// For UTC-5: 12:00 local = 17:00 UTC
			noonUTC := 16
			if testOffset == -5 {
				noonUTC = 17
			}
			beforeNoon := noonUTC - 1
			afterNoon := noonUTC + 1
			
			// Even a small dip at noon is meaningful for Eastern
			if hourCounts[beforeNoon] > hourCounts[noonUTC] && hourCounts[afterNoon] > hourCounts[noonUTC] {
				dropPercent := float64(hourCounts[beforeNoon] - hourCounts[noonUTC]) / float64(hourCounts[beforeNoon]) * 100
				easternBonus += 3.0
				adjustments = append(adjustments, fmt.Sprintf("+3 (Eastern noon dip pattern %.1f%%)", dropPercent))
			}
			
			if easternBonus > 0 {
				testConfidence += easternBonus
			}
		}
		
		// Apply penalties for unlikely scenarios (subtract points)
		
		// Small penalty if no lunch can be detected at all (suggests wrong timezone)
		if testLunchStart < 0 || testLunchConf < 0.2 {
			testConfidence -= 1.0 // Small penalty for no detectable lunch pattern
			adjustments = append(adjustments, "-1 (weak/no lunch pattern)")
		}
		
		// Penalize if lunch is at an unusual time despite having a dip
		if testLunchStart >= 0 && testLunchConf > 0.3 {
			if lunchLocalStart < 10.5 || lunchLocalStart > 14.5 {
				testConfidence -= 10 // Subtract points for very unusual lunch time
				adjustments = append(adjustments, fmt.Sprintf("-10 (unusual lunch time %.1f)", lunchLocalStart))
			}
			// Extra penalty for pre-11am lunch (too early for most regions)
			// BUT reduced penalty for UTC+10/+11 as these could be valid morning break patterns
			if lunchLocalStart < 11.0 {
				if testOffset >= 10 && testOffset <= 11 {
					testConfidence -= 2 // Reduced penalty for Pacific timezones
					adjustments = append(adjustments, fmt.Sprintf("-2 (early lunch at %.1f for UTC+%d)", lunchLocalStart, testOffset))
				} else {
					testConfidence -= 5 // Full penalty for other timezones
					adjustments = append(adjustments, fmt.Sprintf("-5 (lunch before 11am at %.1f)", lunchLocalStart))
				}
			}
			// EXTRA penalty for extremely late lunch (after 3pm)
			// This catches cases like UTC+1 for AmberArcadia where lunch would be at 5pm
			if lunchLocalStart > 15.0 {
				testConfidence -= 20 // Massive penalty for lunch after 3pm
				adjustments = append(adjustments, fmt.Sprintf("-20 (lunch after 3pm at %.1f)", lunchLocalStart))
			}
		}
		
		// Note: Work start penalty is now applied globally based on firstActivityLocal
		// in the peak activity timing section above
		
		// Apply population-based adjustments for timezone likelihood (reduced)
		switch testOffset {
		case -8, -7: // Pacific Time (many tech companies)
			testConfidence += 3 // Good boost for Pacific
			adjustments = append(adjustments, "+3 (Pacific population boost)")
		case -4, -5: // Eastern Time (large population)
			testConfidence += 2 // Moderate boost for Eastern
			adjustments = append(adjustments, "+2 (Eastern population boost)")
		case -6: // Central Time
			testConfidence += 1 // Small boost for Central
			adjustments = append(adjustments, "+1 (Central population boost)")
		case -3: // Atlantic Time (parts of Canada, Brazil, Argentina)
			// No adjustment - reasonable population
		case -2, -1: // Atlantic ocean timezones (Azores, Cape Verde, etc.)
			testConfidence -= 10 // Significant penalty - very few developers
			adjustments = append(adjustments, "-10 (Atlantic ocean low population)")
		case 0: // UTC/GMT (UK, Portugal, Iceland, West Africa)
			// No penalty - significant population
		case 1, 2: // European timezones
			testConfidence += 0.5 // Small boost for European developers
			adjustments = append(adjustments, "+0.5 (European population boost)")
		case 8, 9: // China, Singapore, Australia
			testConfidence += 0.5 // Small boost for Asian developers
			adjustments = append(adjustments, "+0.5 (Asian population boost)")
		case 11: // Pacific ocean
			testConfidence -= 6 // Moderate penalty - mostly ocean
			adjustments = append(adjustments, "-6 (Pacific ocean low population)")
		case 12, 13: // Could be New Zealand
			testConfidence -= 2 // Small penalty - NZ is possible
			adjustments = append(adjustments, "-2 (NZ/Pacific low population)")
		case -11, -12: // Pacific ocean (almost no land)
			testConfidence -= 12 // Large penalty - almost no population
			adjustments = append(adjustments, "-12 (Pacific ocean almost no land)")
		}
		
		// Special bonus for UTC+10 when there are clear evening activity patterns
		// This helps identify Sydney/Brisbane over Japan/Korea time
		if testOffset == 10 && eveningActivity > 50 {
			australiaBonus := 8.0 // Boost to compete with UTC+9
			testConfidence += australiaBonus
			adjustments = append(adjustments, fmt.Sprintf("+%.1f (strong evening activity suggests Australia UTC+10)", australiaBonus))
		}
		
		// Apply European morning activity penalty 
		if !europeanMorningActivityCheck {
			testConfidence -= 15 // Heavy penalty for European timezones with no morning activity
			adjustments = append(adjustments, "-15 (European timezone but no morning activity)")
		}
		
		// Ensure confidence stays above 0 (no upper cap - let real scores determine ranking)
		testConfidence = math.Max(0, testConfidence)
		
		// Always log when verbose (debug logging enabled)
		d.logger.Debug("timezone candidate scoring", "username", username, "offset", testOffset, 
			"confidence", testConfidence, "evening_activity", eveningActivity, "sleep_reasonable", sleepReasonable,
			"work_reasonable", workReasonable, "lunch_reasonable", lunchReasonable)
		
		// Log all scoring adjustments for debugging (always when verbose)
		if len(adjustments) > 0 {
			d.logger.Debug("timezone scoring adjustments", "username", username, "offset", testOffset,
				"adjustments", strings.Join(adjustments, ", "), "final_score", testConfidence)
		} else {
			d.logger.Debug("no scoring adjustments recorded", "username", username, "offset", testOffset, 
				"final_score", testConfidence)
		}
		
		// Add to candidates if confidence is reasonable (at least 10%)
		if testConfidence >= 10 {
			
			candidate := TimezoneCandidate{
				Timezone:         fmt.Sprintf("UTC%+d", testOffset),
				Offset:           float64(testOffset),
				Confidence:       testConfidence,
				EveningActivity:  eveningActivity,
				LunchReasonable:  lunchReasonable,
				WorkHoursNormal:  workReasonable,
				LunchLocalTime:   lunchLocalStart,
				WorkStartLocal:   testWorkStart,
				SleepMidLocal:    sleepLocalMid,
				LunchDipStrength: lunchDipStrength,
				LunchStartUTC:    testLunchStart,  // Store for reuse
				LunchEndUTC:      testLunchEnd,    // Store for reuse
				LunchConfidence:  testLunchConf,   // Store for reuse
			}
			candidates = append(candidates, candidate)
		}
	}
	
	// Sort candidates by confidence score
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Confidence > candidates[i].Confidence {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	
	// Keep only top 3 candidates
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	
	// Log the top candidates and use the best one
	if len(candidates) > 0 {
		d.logger.Debug("top timezone candidates", "username", username,
			"count", len(candidates),
			"top_offset", candidates[0].Offset,
			"top_confidence", candidates[0].Confidence)
		
		for i, c := range candidates {
			if i >= 3 {
				break
			}
			d.logger.Debug("timezone candidate", "username", username,
				"rank", i+1,
				"offset", c.Offset,
				"confidence", c.Confidence,
				"evening_activity", c.EveningActivity,
				"lunch_reasonable", c.LunchReasonable,
				"work_hours_normal", c.WorkHoursNormal)
		}
		
		// Use the top candidate's offset instead of the initial detection
		// This gives us better lunch/peak detection
		if candidates[0].Confidence > confidence {
			offsetInt = int(candidates[0].Offset)
			timezone = timezoneFromOffset(offsetInt)
			confidence = candidates[0].Confidence
			d.logger.Info("using top candidate offset", "username", username,
				"new_offset", offsetInt,
				"new_timezone", timezone,
				"new_confidence", confidence,
				"initial_offset", int(offsetFromUTC))
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
		lunchStart, lunchEnd, lunchConfidence = detectLunchBreakNoonCentered(halfHourCounts, offsetInt)
		d.logger.Debug("calculated new lunch", "username", username,
			"offset", offsetInt, "lunch_start_utc", lunchStart, "lunch_end_utc", lunchEnd, "confidence", lunchConfidence)
	}
	
	// Only use global lunch pattern if we don't already have a high-confidence lunch
	// Global patterns can be misleading - a consistent 2pm drop might be meetings, not lunch
	if bestGlobalLunch.confidence > 0.5 && bestGlobalLunch.startUTC >= 0 && lunchConfidence < 0.7 {
		// Calculate what local time the global lunch would be
		globalLunchLocal := math.Mod(bestGlobalLunch.startUTC+float64(offsetInt)+24, 24)
		
		// Only use if it's in a more typical lunch range (11:30am-1:30pm)
		// 2pm is too late and likely represents something else
		if globalLunchLocal >= 11.5 && globalLunchLocal <= 13.5 {
			// Don't completely override - average with the detected lunch if both exist
			if lunchStart >= 0 {
				// Weight the individual detection more than the global pattern
				lunchStart = (lunchStart*0.7 + bestGlobalLunch.startUTC*0.3)
				lunchEnd = (lunchEnd*0.7 + bestGlobalLunch.endUTC*0.3)
				lunchConfidence = math.Max(lunchConfidence, bestGlobalLunch.confidence*0.8)
			} else {
				// No individual lunch detected, use global
				lunchStart = bestGlobalLunch.startUTC
				lunchEnd = bestGlobalLunch.endUTC
				lunchConfidence = bestGlobalLunch.confidence * 0.8 // Reduce confidence a bit
			}
		}
	}
	d.logger.Debug("lunch detection attempt", "username", username,
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence,
		"work_start", activeStart, "work_end", activeEnd, "utc_offset", offsetInt)

	// Detect peak productivity window using 30-minute buckets for better precision
	peakStart, peakEnd, peakCount := detectPeakProductivityWithHalfHours(halfHourCounts, offsetInt)
	d.logger.Debug("peak productivity detected", "username", username,
		"peak_start", peakStart, "peak_end", peakEnd, "activity_count", peakCount)

	// DISABLED: Work schedule validation corrections were causing more harm than good
	// The corrections were sometimes moving people further from their actual timezone
	// For example, egibs in Kansas (UTC-6) was being detected as UTC-7 (close!)
	// but then "corrected" to UTC-9 based on lunch timing, which is worse.
	//
	// The sleep-based detection is generally more reliable than trying to correct
	// based on work/lunch schedules, as people have varying work patterns.
	//
	// Log the work schedule for debugging but don't apply corrections
	d.logger.Debug("work schedule detected", "username", username,
		"work_start", activeStart, "work_end", activeEnd,
		"lunch_start", lunchStart, "lunch_end", lunchEnd,
		"detected_offset", offsetInt)

	// Process top organizations
	type orgActivity struct {
		name  string
		count int
	}
	var orgs []orgActivity
	for name, count := range orgCounts {
		orgs = append(orgs, orgActivity{name: name, count: count})
	}
	// Sort by count descending
	sort.Slice(orgs, func(i, j int) bool {
		return orgs[i].count > orgs[j].count
	})
	// Take top 5 organizations, but only those with more than 1 contribution
	var topOrgs []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	for i := 0; i < len(orgs) && len(topOrgs) < 5; i++ {
		// Skip organizations with only 1 contribution
		if orgs[i].count <= 1 {
			continue
		}
		topOrgs = append(topOrgs, struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}{
			Name:  orgs[i].name,
			Count: orgs[i].count,
		})
	}

	// Check if work hours are suspicious (starting before 6am or after 11am)
	// This often indicates we've detected the wrong timezone
	suspiciousWorkHours := false
	alternativeTimezone := ""

	d.logger.Info("checking work hours for suspicion", "username", username,
		"activeStart", activeStart, "midQuiet", midQuiet, "offsetInt", offsetInt)

	if activeStart < 6 {
		// Work starting before 6am is very unusual
		suspiciousWorkHours = true
		confidence = 0.4 // Lower confidence

		d.logger.Info("suspicious early work detected", "username", username,
			"work_start", activeStart, "midQuiet", midQuiet)

		// If sleep is around 19-23 UTC, could be:
		// - UTC+8 (China) - would make work start at 11am instead of 3am
		// - UTC-8 (Pacific) - would make work start at 7pm (night shift)
		if midQuiet >= 19 && midQuiet <= 23 {
			// Most likely China if work starts very early in Europe
			alternativeTimezone = "UTC+8"
			d.logger.Info("suspicious work hours detected - suggesting Asia", "username", username,
				"work_start", activeStart, "detected_tz", timezone, "alternative", alternativeTimezone,
				"midQuiet", midQuiet)
		}
	} else {
		// Check local work start time (convert from UTC to local)
		localWorkStart := (activeStart + offsetInt + 24) % 24
		if localWorkStart > 11 {
			// Work starting after 11am local time is unusual (unless part-time)
			suspiciousWorkHours = true
			confidence = 0.5
			d.logger.Debug("late work start detected", "username", username,
				"work_start_utc", activeStart, "work_start_local", localWorkStart, "detected_tz", timezone)
		}
	}

	// Lunch timing confidence validation
	// Use lunch patterns as an additional signal to validate timezone detection
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
				timezone = timezoneFromOffset(offsetInt)

				// Recalculate work hours with corrected offset
				activeStart, activeEnd = calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)

				// Recalculate lunch with corrected offset
				newLunchStart, newLunchEnd, newLunchConfidence := detectLunchBreakNoonCentered(halfHourCounts, offsetInt)

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
					timezone = timezoneFromOffset(offsetInt)
					activeStart, activeEnd = calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)
					d.logger.Debug("lunch-based correction didn't improve, reverting", "username", username)
				}

				// Recalculate peak with final offset
				peakStart, peakEnd, peakCount = detectPeakProductivityWithHalfHours(halfHourCounts, offsetInt)
			}
		}
	}

	// If we have suspicious work hours and detected European timezone,
	// but sleep pattern could fit Asia, consider adjusting to Asia
	// UNLESS we have strong evidence for Europe (e.g., Polish name)
	if suspiciousWorkHours && alternativeTimezone == "UTC+8" && offsetInt <= 3 {
		// Get user's full name to check for regional indicators
		user := d.fetchUser(ctx, username)
		isLikelyEuropean := false

		if user != nil && user.Name != "" {
			// Check for Polish name indicators
			if isPolishName(user.Name) {
				isLikelyEuropean = true
				d.logger.Info("Polish name detected, keeping European timezone despite unusual hours",
					"username", username, "name", user.Name, "timezone", timezone)
			}
		}

		// Also check for activity in European projects/organizations
		for orgName := range orgCounts {
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
				"username", username, "original", timezone, "adjusted", alternativeTimezone,
				"work_start", activeStart)
			timezone = alternativeTimezone
			offsetInt = 8

			// Recalculate work hours with new offset
			activeStart, activeEnd = calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)

			// Recalculate lunch with new offset
			lunchStart, lunchEnd, lunchConfidence = detectLunchBreakNoonCentered(halfHourCounts, offsetInt)
		}

		// Recalculate peak with new offset
		peakStart, peakEnd, peakCount = detectPeakProductivityWithHalfHours(halfHourCounts, offsetInt)

		confidence = 0.7 // Moderate confidence after adjustment

		d.logger.Info("recalculated work hours after timezone adjustment", "username", username,
			"new_work_start", activeStart, "new_work_end", activeEnd, "new_offset", offsetInt)
	}

	// Active hours are already in UTC from calculateTypicalActiveHours
	// No conversion needed - just use them directly
	activeStartUTC := float64(activeStart)
	activeEndUTC := float64(activeEnd)

	// Detect sleep periods using 30-minute resolution with buffer
	sleepBuckets := detectSleepPeriodsWithHalfHours(halfHourCounts)
	
	result := &Result{
		Username:         username,
		Timezone:         timezone,
		ActivityTimezone: timezone, // Pure activity-based result
		QuietHoursUTC:    quietHours,
		SleepBucketsUTC:  sleepBuckets, // 30-minute resolution sleep periods
		ActiveHoursLocal: struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		}{
			Start: activeStartUTC, // NOTE: Despite field name "Local", storing UTC for consistency
			End:   activeEndUTC,   // Frontend converts to local for display
		},
		TopOrganizations:           topOrgs,
		Confidence:                 confidence,
		Method:                     "activity_patterns",
		HourlyActivityUTC:          displayHourCounts, // Store aggregated hourly data for histogram generation
		HalfHourlyActivityUTC:      halfHourCounts,   // Store 30-minute resolution data
		HourlyOrganizationActivity: hourOrgActivity, // Store org-specific activity
		TimezoneCandidates:         candidates, // Top 3 timezone candidates with analysis
	}

	// Add activity date range information
	result.ActivityDateRange.OldestActivity = oldestActivity
	result.ActivityDateRange.NewestActivity = newestActivity
	result.ActivityDateRange.TotalDays = totalDays
	result.ActivityDateRange.SpansDSTTransitions = spansDSTTransitions

	// Note: detectLunchBreakNoonCentered and detectPeakProductivityWithHalfHours already return UTC hours
	// so no conversion is needed

	// Store lunch hours in UTC
	result.LunchHoursUTC = struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Confidence float64 `json:"confidence"`
	}{
		Start:      lunchStart,  // Already in UTC
		End:        lunchEnd,    // Already in UTC
		Confidence: lunchConfidence,
	}

	// Store peak productivity window in UTC
	result.PeakProductivity = struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Count int     `json:"count"`
	}{
		Start: peakStart,  // Already in UTC
		End:   peakEnd,    // Already in UTC
		Count: peakCount,
	}

	d.logger.Debug("detected lunch break", "username", username,
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence)

	return result
}

// fetchSupplementalActivity fetches additional activity data when events are insufficient.
func (d *Detector) fetchSupplementalActivity(ctx context.Context, username string) *ActivityData {
	type result struct {
		prs          []PullRequest
		issues       []Issue
		comments     []Comment
		stars        []time.Time
		starredRepos []Repository
		commits      []time.Time
	}

	ch := make(chan result, 1)

	go func() {
		var res result

		// Fetch in parallel using goroutines
		var wg sync.WaitGroup
		wg.Add(5) // PRs, issues, comments, starred repos, commits

		// Fetch PRs
		go func() {
			defer wg.Done()
			if prs, err := d.fetchPullRequests(ctx, username); err == nil {
				res.prs = prs
			} else {
				d.logger.Debug("failed to fetch PRs", "username", username, "error", err)
			}
		}()

		// Fetch Issues
		go func() {
			defer wg.Done()
			if issues, err := d.fetchIssues(ctx, username); err == nil {
				res.issues = issues
			} else {
				d.logger.Debug("failed to fetch issues", "username", username, "error", err)
			}
		}()

		// Fetch Comments via GraphQL
		go func() {
			defer wg.Done()
			if comments, err := d.fetchUserComments(ctx, username); err == nil {
				res.comments = comments
			} else {
				d.logger.Debug("failed to fetch comments", "username", username, "error", err)
			}
		}()

		// Fetch starred repositories for additional timestamps
		go func() {
			defer wg.Done()
			if stars, starredRepos, err := d.fetchStarredRepositories(ctx, username); err == nil {
				res.stars = stars
				res.starredRepos = starredRepos
			} else {
				d.logger.Debug("failed to fetch starred repos", "username", username, "error", err)
			}
		}()

		// Fetch commits
		go func() {
			defer wg.Done()
			if commits, err := d.fetchUserCommits(ctx, username); err == nil {
				res.commits = commits
			} else {
				d.logger.Debug("failed to fetch commits", "username", username, "error", err)
			}
		}()

		wg.Wait()
		ch <- res
	}()

	select {
	case res := <-ch:
		// Convert starred timestamps to comments for inclusion in activity analysis
		starComments := make([]Comment, len(res.stars))
		for i, starTime := range res.stars {
			starComments[i] = Comment{
				CreatedAt:  starTime,
				Type:       "star",
				Body:       "starred repository",
				Repository: "various",
			}
		}
		
		// Convert commit timestamps to comments for inclusion in activity analysis
		commitComments := make([]Comment, len(res.commits))
		for i, commitTime := range res.commits {
			commitComments[i] = Comment{
				CreatedAt:  commitTime,
				Type:       "commit",
				Body:       "authored commit",
				Repository: "various",
			}
		}
		
		// Combine all comment types
		allComments := append(res.comments, starComments...)
		allComments = append(allComments, commitComments...)
		
		return &ActivityData{
			PullRequests: res.prs,
			Issues:       res.issues,
			Comments:     allComments,
			StarredRepos: res.starredRepos,
		}
	case <-ctx.Done():
		return &ActivityData{}
	}
}

// findBestGlobalLunchPattern finds the best lunch pattern globally in UTC time
// This is timezone-independent and looks for the strongest activity drop + recovery pattern
func findBestGlobalLunchPattern(halfHourCounts map[float64]int) GlobalLunchPattern {
	// Calculate average activity to establish baseline
	totalActivity := 0
	totalBuckets := 0
	for _, count := range halfHourCounts {
		totalActivity += count
		totalBuckets++
	}
	
	if totalBuckets == 0 {
		return GlobalLunchPattern{startUTC: -1, confidence: 0}
	}
	
	avgActivity := float64(totalActivity) / float64(totalBuckets)
	if avgActivity < 1 {
		return GlobalLunchPattern{startUTC: -1, confidence: 0}
	}
	
	bestPattern := GlobalLunchPattern{startUTC: -1, confidence: 0}
	bestScore := 0.0
	
	// Look for lunch breaks of 30, 60, or 90 minutes (1, 2, or 3 buckets)
	durations := []int{1, 2, 3}
	
	for _, duration := range durations {
		// Restrict search to reasonable global lunch window (15:00-21:00 UTC)
		// This covers 11am-2pm across US timezones (-4 to -8), Europe (+1 to +3), and Asia (+8 to +9)
		for startBucket := 15.0; startBucket < 21.0; startBucket += 0.5 {
			endBucket := startBucket + float64(duration)*0.5
			
			// Get surrounding buckets for comparison
			prevBucket := startBucket - 0.5
			if prevBucket < 0 {
				prevBucket += 24
			}
			nextBucket := endBucket
			if nextBucket >= 24 {
				nextBucket -= 24
			}
			
			prevCount := halfHourCounts[prevBucket]
			startCount := halfHourCounts[startBucket]
			afterCount := halfHourCounts[nextBucket]
			
			// Skip if no data for comparison
			if prevCount == 0 {
				continue
			}
			dropPercent := (float64(prevCount) - float64(startCount)) / float64(prevCount)
			
			// Calculate recovery factor
			recoveryFactor := 1.0
			if startCount > 0 && afterCount > startCount {
				recoveryFactor = float64(afterCount) / float64(startCount)
			}
			
			// Calculate average lunch activity
			lunchTotal := 0
			lunchBuckets := 0
			for b := startBucket; b < endBucket; b += 0.5 {
				bucket := b
				if bucket >= 24 {
					bucket -= 24
				}
				lunchTotal += halfHourCounts[bucket]
				lunchBuckets++
			}
			
			if lunchBuckets == 0 {
				continue
			}
			avgLunchActivity := float64(lunchTotal) / float64(lunchBuckets)
			
			// Check if this qualifies as a lunch pattern
			minDropThreshold := 0.25 // 25% minimum drop
			if recoveryFactor >= 2.0 {
				minDropThreshold = 0.15 // Allow smaller drops with strong recovery
			}
			
			
			if dropPercent > minDropThreshold && avgLunchActivity <= avgActivity*0.8 {
				// Score this pattern
				score := 0.0
				
				// Prefer stronger drops
				score += dropPercent * 100
				
				// Prefer lower lunch activity
				quietness := 1.0 - (avgLunchActivity / avgActivity)
				score += quietness * 50
				
				// Boost for strong recovery
				if recoveryFactor >= 1.5 {
					score += (recoveryFactor - 1.0) * 30
				}
				
				// Prefer 60-minute lunches
				if duration == 2 {
					score += 20
				}
				
				// Track the best pattern
				if score > bestScore {
					bestScore = score
					bestPattern = GlobalLunchPattern{
						startUTC:    startBucket,
						endUTC:      endBucket,
						confidence:  math.Min(1.0, score/100.0), // Normalize to 0-1
						dropPercent: dropPercent,
					}
				}
				
			}
		}
	}
	
	return bestPattern
}
