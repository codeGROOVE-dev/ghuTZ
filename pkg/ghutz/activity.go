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
		}
		
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
		}
		
		for _, comment := range additionalData.Comments {
			// Comments don't have repository info directly
			allTimestamps = append(allTimestamps, timestampEntry{
				time:   comment.CreatedAt,
				source: "comment",
				org:    "",
			})
		}
		
		d.logger.Debug("collected all timestamps", "username", username,
			"total_before_dedup", len(allTimestamps))
	}
	
	// Sort timestamps by recency (newest first)
	sort.Slice(allTimestamps, func(i, j int) bool {
		return allTimestamps[i].time.After(allTimestamps[j].time)
	})
	
	// Deduplicate and take the most recent 480 unique timestamps
	const maxTimestamps = 480
	uniqueTimestamps := make(map[time.Time]bool)
	hourCounts := make(map[int]int)
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
	
	d.logger.Debug("activity data summary", "username", username,
		"total_timestamps_collected", len(allTimestamps),
		"duplicates_removed", duplicates,
		"unique_timestamps_used", totalActivity,
		"max_allowed", maxTimestamps)
	
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
		// American pattern - original logic works
		offsetFromUTC = assumedSleepMidpoint - midQuiet
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
	// Take top 5
	var topOrgs []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	for i := 0; i < 5 && i < len(orgs); i++ {
		topOrgs = append(topOrgs, struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}{
			Name:  orgs[i].name,
			Count: orgs[i].count,
		})
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
		TopOrganizations:  topOrgs,
		Confidence:        0.8,
		Method:            "activity_patterns",
		HourlyActivityUTC: hourCounts, // Store for histogram generation
	}

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

