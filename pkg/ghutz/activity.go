package ghutz

import (
	"context"
	"math"
	"sync"
	"time"
)

func (d *Detector) tryActivityPatterns(ctx context.Context, username string) *Result {
	// Fetch all activity data in parallel
	activity := d.fetchAllActivity(ctx, username)

	totalActivity := len(activity.PullRequests) + len(activity.Issues) + len(activity.Comments)
	if totalActivity < 20 {
		d.logger.Debug("insufficient activity data", "username", username,
			"pr_count", len(activity.PullRequests),
			"issue_count", len(activity.Issues),
			"comment_count", len(activity.Comments),
			"total", totalActivity, "minimum_required", 20)
		return nil
	}

	d.logger.Debug("analyzing activity patterns", "username", username,
		"pr_count", len(activity.PullRequests),
		"issue_count", len(activity.Issues),
		"comment_count", len(activity.Comments))

	hourCounts := make(map[int]int)

	// Count PRs
	for _, pr := range activity.PullRequests {
		hour := pr.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}

	// Count issues
	for _, issue := range activity.Issues {
		hour := issue.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}

	// Count comments
	for _, comment := range activity.Comments {
		hour := comment.CreatedAt.UTC().Hour()
		hourCounts[hour]++
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
	if len(quietHours) < 4 {
		d.logger.Debug("insufficient sleep hours", "username", username, "sleep_hours", len(quietHours))
		return nil
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

	offsetFromUTC := assumedSleepMidpoint - midQuiet

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

	// Use work schedule validation for timezone detection
	// Most people start work between 8:00am-9:30am and have lunch 11:30am-1:00pm
	var offsetCorrection int
	var correctionReason string

	// Check work start time (should be 8:00am-9:30am) - be more strict
	if float64(activeStart) < 7.5 || float64(activeStart) > 9.5 {
		expectedWorkStart := 8.5 // 8:30am average
		workCorrection := int(expectedWorkStart - float64(activeStart))
		if workCorrection != 0 && workCorrection >= -8 && workCorrection <= 8 {
			offsetCorrection = workCorrection
			correctionReason = "work_start"
			d.logger.Debug("work start timing suggests timezone correction", "username", username,
				"work_start_local", activeStart, "expected_range", "7:30-9:30",
				"suggested_correction", workCorrection)
		}
	}

	// Check lunch timing (should be 11:30am-12:30pm, much stricter)
	if lunchStart != -1 && lunchEnd != -1 {
		// Very strict validation: lunch should start between 11:30am-12:30pm
		if lunchStart < 11.5 || lunchStart > 12.5 || lunchEnd < 12.5 || lunchEnd > 13.5 {
			expectedLunchMid := 12.0 // 12:00pm
			actualLunchMid := (lunchStart + lunchEnd) / 2
			lunchCorrection := int(expectedLunchMid - actualLunchMid)

			// If we don't have a work start correction, or lunch correction is larger, use lunch correction
			if offsetCorrection == 0 || (lunchCorrection != 0 && int(math.Abs(float64(lunchCorrection))) > int(math.Abs(float64(offsetCorrection)))) {
				offsetCorrection = lunchCorrection
				correctionReason = "lunch_timing"
			}

			d.logger.Debug("lunch timing suggests timezone correction", "username", username,
				"lunch_start_local", lunchStart, "lunch_end_local", lunchEnd,
				"expected_range", "11:30-12:30 start, 12:30-13:30 end", "suggested_correction", lunchCorrection)
		}
	}

	// Check evening wind-down time (should be 5:00pm-7:00pm)
	if float64(activeEnd) < 16.0 || float64(activeEnd) > 19.0 {
		expectedWorkEnd := 17.0 // 5:00pm average
		endCorrection := int(expectedWorkEnd - float64(activeEnd))
		if endCorrection != 0 && endCorrection >= -8 && endCorrection <= 8 {
			// If we don't have other corrections, or this correction is more significant, use it
			if offsetCorrection == 0 || (int(math.Abs(float64(endCorrection))) > int(math.Abs(float64(offsetCorrection)))) {
				offsetCorrection = endCorrection
				correctionReason = "work_end"
				d.logger.Debug("work end timing suggests timezone correction", "username", username,
					"work_end_local", activeEnd, "expected_range", "16:00-19:00",
					"suggested_correction", endCorrection)
			}
		}
	}

	// Apply timezone correction if we found one
	if offsetCorrection != 0 && offsetCorrection >= -8 && offsetCorrection <= 8 {
		correctedOffset := offsetInt + offsetCorrection
		d.logger.Debug("correcting timezone based on work schedule", "username", username,
			"original_offset", offsetInt, "correction", offsetCorrection,
			"corrected_offset", correctedOffset, "reason", correctionReason)
		offsetInt = correctedOffset
		timezone = timezoneFromOffset(offsetInt)

		// Recalculate active hours and lunch with corrected offset
		activeStart, activeEnd = calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)
		lunchStart, lunchEnd, lunchConfidence = detectLunchBreak(hourCounts, offsetInt, activeStart, activeEnd)
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
		Confidence: 0.8,
		Method:     "activity_patterns",
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
	d.logger.Debug("detected lunch break", "username", username,
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence)

	return result
}

func (d *Detector) fetchAllActivity(ctx context.Context, username string) *ActivityData {
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