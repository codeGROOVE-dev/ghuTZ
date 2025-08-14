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

func (d *Detector) tryActivityPatterns(ctx context.Context, username string) *Result {
	// Start with public events data
	events, err := d.fetchPublicEvents(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch public events", "username", username, "error", err)
		// Don't return nil - try other sources
		events = []PublicEvent{}
	}
	return d.tryActivityPatternsWithEvents(ctx, username, events)
}

func (d *Detector) tryActivityPatternsWithEvents(ctx context.Context, username string, events []PublicEvent) *Result {

	// Collect all timestamps first (we'll deduplicate and limit later)
	type timestampEntry struct {
		time time.Time
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
	
	twoWeeksAgo := time.Now().AddDate(0, 0, -14)
	
	// Decide if we need supplemental data:
	// 1. If we have less than 300 events, OR
	// 2. If our oldest event is newer than 2 weeks ago (insufficient time coverage)
	needSupplemental := len(events) < 300 || oldestEventTime.IsZero() || oldestEventTime.After(twoWeeksAgo)
	
	if needSupplemental {
		reason := ""
		if len(events) < 300 {
			reason = fmt.Sprintf("only %d events", len(events))
		}
		if !oldestEventTime.IsZero() && oldestEventTime.After(twoWeeksAgo) {
			daysCovered := int(time.Since(oldestEventTime).Hours() / 24)
			if reason != "" {
				reason += " and "
			}
			reason += fmt.Sprintf("only %d days of data", daysCovered)
		}
		d.logger.Debug("supplementing with additional API queries", "username", username, 
			"reason", reason)
		
		// Fetch additional data in parallel
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
	
	// Sort timestamps by recency (newest first)
	sort.Slice(allTimestamps, func(i, j int) bool {
		return allTimestamps[i].time.After(allTimestamps[j].time)
	})
	
	// Deduplicate and take the most recent 480 unique timestamps
	const maxTimestamps = 480
	uniqueTimestamps := make(map[time.Time]bool)
	hourCounts := make(map[int]int)
	hourOrgActivity := make(map[int]map[string]int) // Track org activity by hour
	duplicates := 0
	used := 0
	
	for _, entry := range allTimestamps {
		if !uniqueTimestamps[entry.time] {
			uniqueTimestamps[entry.time] = true
			hour := entry.time.UTC().Hour()
			hourCounts[hour]++
			
			// Track organization counts
			if entry.org != "" {
				orgCounts[entry.org]++
				// Track hourly organization activity
				if hourOrgActivity[hour] == nil {
					hourOrgActivity[hour] = make(map[string]int)
				}
				hourOrgActivity[hour][entry.org]++
			}
			used++
			
			// Stop after we have enough unique timestamps
			if used >= maxTimestamps {
				break
			}
		} else {
			duplicates++
		}
	}
	
	// Count total unique activities used
	totalActivity := len(uniqueTimestamps)
	
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
		
		hasSpringDST := monthsPresent[3] || monthsPresent[4]    // March or April
		hasFallDST := monthsPresent[9] || monthsPresent[10] || monthsPresent[11] // September, October, or November
		
		// Only mark as spanning DST if we have both spring AND fall transition periods
		spansDSTTransitions = hasSpringDST && hasFallDST && totalDays > 90
	}
	
	d.logger.Debug("activity data summary", "username", username,
		"total_timestamps_collected", len(allTimestamps),
		"duplicates_removed", duplicates,
		"unique_timestamps_used", totalActivity,
		"max_allowed", maxTimestamps,
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
			for hour := 0; hour < 24; hour++ {
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
	for hour := 0; hour < 24; hour++ {
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
			offsetFromUTC = 1.0 // Default to CET
			
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
		
		// Check for ambiguous sleep patterns that could match multiple US zones
		// Expanded range to catch more edge cases like early sleepers
		if midQuiet >= 4 && midQuiet <= 13 && len(quietHours) >= 4 {
			// This sleep pattern could match Eastern, Central, Mountain, or Pacific time
			d.logger.Debug("ambiguous US sleep pattern detected", "username", username,
				"midQuiet", midQuiet, "baseOffset", baseOffset, "quietHours", quietHours)
			
			// Use additional heuristics for US timezone differentiation
			
			// Check evening activity patterns (7-11pm local converted to UTC)
			// Eastern Time (UTC-5): 7-11pm local = 0-4am UTC (next day, or 12-16 UTC same day in DST)
			// Central Time (UTC-6): 7-11pm local = 1-5am UTC (next day, or 13-17 UTC same day in DST)
			// Mountain Time (UTC-7): 7-11pm local = 2-6am UTC (next day, or 14-18 UTC same day in DST)
			// Pacific Time (UTC-8): 7-11pm local = 3-7am UTC (next day, or 15-19 UTC same day in DST)
			
			eveningActivityEastern := hourCounts[0] + hourCounts[1] + hourCounts[2] + hourCounts[3] + hourCounts[12] + hourCounts[13] + hourCounts[14] + hourCounts[15]
			eveningActivityCentral := hourCounts[1] + hourCounts[2] + hourCounts[3] + hourCounts[4] + hourCounts[13] + hourCounts[14] + hourCounts[15] + hourCounts[16]
			eveningActivityMountain := hourCounts[2] + hourCounts[3] + hourCounts[4] + hourCounts[5] + hourCounts[14] + hourCounts[15] + hourCounts[16] + hourCounts[17]
			eveningActivityPacific := hourCounts[3] + hourCounts[4] + hourCounts[5] + hourCounts[6] + hourCounts[15] + hourCounts[16] + hourCounts[17] + hourCounts[18]
			
			d.logger.Debug("evening activity analysis", "username", username,
				"eastern_evening", eveningActivityEastern,
				"central_evening", eveningActivityCentral, 
				"mountain_evening", eveningActivityMountain,
				"pacific_evening", eveningActivityPacific)
			
			// Choose timezone based on which has the highest evening activity
			// Evening activity is a strong signal for timezone since people often code in the evenings
			bestTimezone := "eastern"
			bestActivity := eveningActivityEastern
			bestOffset := -5.0
			
			if eveningActivityCentral > bestActivity {
				bestTimezone = "central"
				bestActivity = eveningActivityCentral
				bestOffset = -6.0
			}
			
			if eveningActivityMountain > bestActivity {
				bestTimezone = "mountain"
				bestActivity = eveningActivityMountain  
				bestOffset = -7.0
			}
			
			if eveningActivityPacific > bestActivity {
				bestTimezone = "pacific"
				bestActivity = eveningActivityPacific
				bestOffset = -8.0
			}
			
			// Apply the best evening activity match, but add sleep pattern validation
			// If sleep pattern strongly disagrees (>2 hours off), consider alternatives
			if bestTimezone == "eastern" && midQuiet > 8.0 {
				// Eastern time but very late sleep pattern - might actually be Central
				if float64(eveningActivityCentral) > float64(eveningActivityEastern) * 0.7 { // Within 30% of eastern
					offsetFromUTC = -6.0 // Central Time
					d.logger.Debug("adjusted Eastern to Central due to late sleep pattern", "username", username,
						"midQuiet", midQuiet, "eastern_evening", eveningActivityEastern, "central_evening", eveningActivityCentral)
				} else {
					offsetFromUTC = bestOffset
					d.logger.Debug("selected Eastern Time despite late sleep (strong evening activity)", "username", username,
						"midQuiet", midQuiet, "eastern_evening", eveningActivityEastern)
				}
			} else if bestTimezone == "mountain" && midQuiet < 6.0 {
				// Mountain time but very early sleep pattern - might actually be Eastern
				if float64(eveningActivityEastern) > float64(eveningActivityMountain) * 0.7 { // Within 30% of mountain
					offsetFromUTC = -5.0 // Eastern Time  
					d.logger.Debug("adjusted Mountain to Eastern due to early sleep pattern", "username", username,
						"midQuiet", midQuiet, "mountain_evening", eveningActivityMountain, "eastern_evening", eveningActivityEastern)
				} else {
					offsetFromUTC = bestOffset
					d.logger.Debug("selected Mountain Time despite early sleep (strong evening activity)", "username", username,
						"midQuiet", midQuiet, "mountain_evening", eveningActivityMountain)
				}
			} else if bestTimezone == "pacific" && midQuiet < 8.0 {
				// Pacific time but earlier sleep pattern - might actually be Mountain or Central
				if float64(eveningActivityMountain) > float64(eveningActivityPacific) * 0.7 { // Within 30% of pacific
					offsetFromUTC = -7.0 // Mountain Time
					d.logger.Debug("adjusted Pacific to Mountain due to early sleep pattern", "username", username,
						"midQuiet", midQuiet, "pacific_evening", eveningActivityPacific, "mountain_evening", eveningActivityMountain)
				} else {
					offsetFromUTC = bestOffset
					d.logger.Debug("selected Pacific Time despite early sleep (strong evening activity)", "username", username,
						"midQuiet", midQuiet, "pacific_evening", eveningActivityPacific)
				}
			} else {
				// Sleep pattern is consistent with timezone choice
				offsetFromUTC = bestOffset
				d.logger.Debug("selected timezone based on evening activity", "username", username,
					"selected_timezone", bestTimezone, "evening_activity", bestActivity, "offset", bestOffset,
					"eastern_evening", eveningActivityEastern, "central_evening", eveningActivityCentral, 
					"mountain_evening", eveningActivityMountain, "pacific_evening", eveningActivityPacific)
			}
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

	// Detect lunch break
	lunchStart, lunchEnd, lunchConfidence := detectLunchBreak(hourCounts, offsetInt, activeStart, activeEnd)
	d.logger.Debug("lunch detection attempt", "username", username,
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence,
		"work_start", activeStart, "work_end", activeEnd, "utc_offset", offsetInt)
	
	// Detect peak productivity window
	peakStart, peakEnd, peakCount := detectPeakProductivity(hourCounts, offsetInt)
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
	confidence := 0.8
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
	} else if activeStart > 11 {
		// Work starting after 11am is also unusual (unless part-time)
		suspiciousWorkHours = true
		confidence = 0.5
		d.logger.Debug("late work start detected", "username", username,
			"work_start", activeStart, "detected_tz", timezone)
	}
	
	// Lunch timing confidence validation
	// Use lunch patterns as an additional signal to validate timezone detection
	if lunchConfidence > 0 {
		lunchStartLocal := lunchStart
		lunchEndLocal := lunchEnd
		
		// Check if lunch timing makes sense (typical lunch: 11:30am-2:30pm)
		reasonableLunchStart := lunchStartLocal >= 11.5 && lunchStartLocal <= 14.5  // 11:30am-2:30pm
		reasonableLunchEnd := lunchEndLocal >= 12.0 && lunchEndLocal <= 15.0        // 12:00pm-3:00pm
		normalLunchDuration := (lunchEndLocal - lunchStartLocal) >= 0.5 && (lunchEndLocal - lunchStartLocal) <= 2.0 // 30min-2hr
		
		if lunchConfidence >= 0.75 { // High lunch confidence
			if reasonableLunchStart && reasonableLunchEnd && normalLunchDuration {
				// High confidence lunch at reasonable time = boost timezone confidence
				originalConfidence := confidence
				confidence = math.Min(confidence + 0.15, 0.95) // Boost by 15%, cap at 95%
				d.logger.Debug("lunch timing boosts timezone confidence", "username", username,
					"lunch_time", fmt.Sprintf("%.1f-%.1f", lunchStartLocal, lunchEndLocal),
					"lunch_confidence", lunchConfidence,
					"original_confidence", originalConfidence, "new_confidence", confidence)
			} else {
				// High confidence lunch at weird time = reduce timezone confidence significantly
				originalConfidence := confidence
				confidence = math.Max(confidence - 0.25, 0.2) // Reduce by 25%, floor at 20%
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
				confidence = math.Min(confidence + 0.08, 0.9) // Boost by 8%, cap at 90%
				d.logger.Debug("reasonable lunch timing slightly boosts confidence", "username", username,
					"lunch_confidence", lunchConfidence, "original_confidence", originalConfidence, "new_confidence", confidence)
			} else {
				// Medium confidence lunch at weird time = small penalty
				originalConfidence := confidence  
				confidence = math.Max(confidence - 0.1, 0.3) // Reduce by 10%, floor at 30%
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
				suggestedOffsetCorrection = -2 // Shift 2 hours west
			} else if lunchStartLocal >= 15.0 && lunchStartLocal < 16.0 {
				suggestedOffsetCorrection = -3 // Shift 3 hours west
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
				newLunchStart, newLunchEnd, newLunchConfidence := detectLunchBreak(hourCounts, offsetInt, activeStart, activeEnd)
				
				// If the new lunch time is more reasonable, keep the correction
				if newLunchStart >= 11.5 && newLunchStart <= 13.0 {
					lunchStart = newLunchStart
					lunchEnd = newLunchEnd
					lunchConfidence = newLunchConfidence
					
					// Boost confidence since lunch correction worked
					confidence = math.Min(confidence + 0.15, 0.85)
					d.logger.Info("lunch-based timezone correction successful", "username", username,
						"new_lunch_start", lunchStart, "new_offset", offsetInt, "new_confidence", confidence)
				} else {
					// Revert if it didn't help
					offsetInt = offsetInt - suggestedOffsetCorrection
					timezone = timezoneFromOffset(offsetInt)
					activeStart, activeEnd = calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)
					d.logger.Debug("lunch-based correction didn't improve, reverting", "username", username)
				}
				
				// Recalculate peak with final offset
				peakStart, peakEnd, peakCount = detectPeakProductivity(hourCounts, offsetInt)
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
			lunchStart, lunchEnd, lunchConfidence = detectLunchBreak(hourCounts, offsetInt, activeStart, activeEnd)
		}
		
		// Recalculate peak with new offset
		peakStart, peakEnd, peakCount = detectPeakProductivity(hourCounts, offsetInt)
		
		confidence = 0.7 // Moderate confidence after adjustment
		
		d.logger.Info("recalculated work hours after timezone adjustment", "username", username,
			"new_work_start", activeStart, "new_work_end", activeEnd, "new_offset", offsetInt)
	}

	result := &Result{
		Username:         username,
		Timezone:         timezone,
		ActivityTimezone: timezone, // Pure activity-based result
		QuietHoursUTC:    quietHours,
		ActiveHoursLocal: struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		}{
			Start: float64(activeStart),
			End:   float64(activeEnd),
		},
		TopOrganizations:            topOrgs,
		Confidence:                  confidence,
		Method:                      "activity_patterns",
		HourlyActivityUTC:           hourCounts, // Store for histogram generation
		HourlyOrganizationActivity:  hourOrgActivity, // Store org-specific activity
	}
	
	// Add activity date range information
	result.ActivityDateRange.OldestActivity = oldestActivity
	result.ActivityDateRange.NewestActivity = newestActivity
	result.ActivityDateRange.TotalDays = totalDays
	result.ActivityDateRange.SpansDSTTransitions = spansDSTTransitions

	// Always add lunch hours (they're always detected now)
	result.LunchHoursLocal = struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Confidence float64 `json:"confidence"`
	}{
		Start:      lunchStart,
		End:        lunchEnd,
		Confidence: lunchConfidence,
	}
	
	// Add peak productivity window
	result.PeakProductivity = struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Count int     `json:"count"`
	}{
		Start: peakStart,
		End:   peakEnd,
		Count: peakCount,
	}
	
	d.logger.Debug("detected lunch break", "username", username,
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence)

	return result
}

// fetchSupplementalActivity fetches additional activity data when events are insufficient
func (d *Detector) fetchSupplementalActivity(ctx context.Context, username string) *ActivityData {
	type result struct {
		prs      []PullRequest
		issues   []Issue
		comments []Comment
	}

	ch := make(chan result, 1)

	go func() {
		var res result

		// Fetch in parallel using goroutines
		var wg sync.WaitGroup
		wg.Add(3)

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

		wg.Wait()
		ch <- res
	}()

	select {
	case res := <-ch:
		return &ActivityData{
			PullRequests: res.prs,
			Issues:       res.issues,
			Comments:     res.comments,
		}
	case <-ctx.Done():
		return &ActivityData{}
	}
}

