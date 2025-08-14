package ghutz

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// calculateTypicalActiveHours determines typical work hours based on activity patterns
// It uses percentiles to exclude outliers (e.g., occasional early starts or late nights)
func calculateTypicalActiveHours(hourCounts map[int]int, quietHours []int, utcOffset int) (start, end int) {
	// Create a map for easy lookup of quiet hours
	quietMap := make(map[int]bool)
	for _, h := range quietHours {
		quietMap[h] = true
	}

	// Find hours with meaningful activity (>10% of max activity)
	maxActivity := 0
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	threshold := maxActivity / 10

	// Collect active hours (not in quiet period and above threshold)
	var activeHours []int
	for hour := 0; hour < 24; hour++ {
		if !quietMap[hour] && hourCounts[hour] > threshold {
			activeHours = append(activeHours, hour)
		}
	}

	if len(activeHours) == 0 {
		// Default to 9am-5pm if no clear pattern
		return 9, 17
	}

	// Find the continuous block of active hours
	// Handle wrap-around (e.g., activity from 22-02)
	sort.Ints(activeHours)

	// Find the largest gap to determine where the active period starts/ends
	maxGap := 0
	gapStart := activeHours[len(activeHours)-1]
	for i := 0; i < len(activeHours); i++ {
		gap := activeHours[i] - gapStart
		if gap < 0 {
			gap += 24
		}
		if gap > maxGap {
			maxGap = gap
			start = activeHours[i]
		}
		gapStart = activeHours[i]
	}

	// Find the end of the active period
	end = start
	for i := 0; i < len(activeHours); i++ {
		hour := activeHours[i]
		// Check if this hour is part of the continuous block
		diff := hour - start
		if diff < 0 {
			diff += 24
		}
		if diff < 16 { // Maximum 16-hour workday
			end = hour
		}
	}

	// Apply smart filtering: use 10th and 90th percentiles to exclude outliers
	// This prevents occasional early/late activity from skewing the results
	activityInRange := make([]int, 0)
	for h := start; ; h = (h + 1) % 24 {
		if hourCounts[h] > 0 {
			// Add this hour's count multiple times to weight the calculation
			for i := 0; i < hourCounts[h]; i++ {
				activityInRange = append(activityInRange, h)
			}
		}
		if h == end {
			break
		}
	}

	if len(activityInRange) > 10 {
		sort.Ints(activityInRange)
		// Use 5th percentile for start (ignore occasional early starts)
		percentile5 := len(activityInRange) / 20
		// Use 95th percentile for end (capture more of the workday, only exclude true outliers)
		percentile95 := len(activityInRange) * 19 / 20
		
		// Ensure we don't have invalid indices
		if percentile5 < 0 {
			percentile5 = 0
		}
		if percentile95 >= len(activityInRange) {
			percentile95 = len(activityInRange) - 1
		}

		start = activityInRange[percentile5]
		end = activityInRange[percentile95]
	}

	// Convert from UTC to local time
	start = (start + utcOffset + 24) % 24
	end = (end + utcOffset + 24) % 24

	return start, end
}

// findSleepHours looks for extended periods of zero or near-zero activity
// This is more reliable than finding "quiet" hours which might just be evening time
func findSleepHours(hourCounts map[int]int) []int {
	// First, find all hours with zero or minimal activity
	zeroHours := []int{}
	for hour := 0; hour < 24; hour++ {
		if hourCounts[hour] <= 1 { // Allow for 1 random event
			zeroHours = append(zeroHours, hour)
		}
	}

	// If we have a good stretch of zero activity, use that
	if len(zeroHours) >= 5 {
		// Find the longest consecutive sequence
		maxLen := 0
		maxStart := 0
		currentStart := zeroHours[0]
		currentLen := 1

		for i := 1; i < len(zeroHours); i++ {
			if zeroHours[i] == zeroHours[i-1]+1 || (zeroHours[i-1] == 23 && zeroHours[i] == 0) {
				currentLen++
			} else {
				if currentLen > maxLen {
					maxLen = currentLen
					maxStart = currentStart
				}
				currentStart = zeroHours[i]
				currentLen = 1
			}
		}
		if currentLen > maxLen {
			maxLen = currentLen
			maxStart = currentStart
		}

		// Extract the core sleep hours from the zero-activity period
		// Skip early evening hours and wake-up hours to focus on deep sleep
		result := []int{}
		sleepStart := maxStart
		sleepLength := maxLen

		// If we have a long zero period (8+ hours), it likely includes evening time
		// Skip the first 2-3 hours to avoid evening time, and the last hour for wake-up
		if maxLen >= 8 {
			sleepStart = (maxStart + 3) % 24 // Skip first 3 hours (evening)
			sleepLength = maxLen - 4         // Also skip last hour (wake-up)
		} else if maxLen >= 6 {
			sleepStart = (maxStart + 1) % 24 // Skip first hour
			sleepLength = maxLen - 2         // Also skip last hour
		}

		// Limit to reasonable sleep duration (4-7 hours)
		if sleepLength > 7 {
			sleepLength = 7
		}
		if sleepLength < 4 {
			sleepLength = maxLen // Use original if adjustment made it too short
			sleepStart = maxStart
		}

		for i := 0; i < sleepLength; i++ {
			hour := (sleepStart + i) % 24
			result = append(result, hour)
		}

		if len(result) >= 4 {
			return result
		}
	}

	// Fall back to the old method if we don't have clear zero periods
	return findQuietHours(hourCounts)
}

func findQuietHours(hourCounts map[int]int) []int {
	minSum := 999999
	minStart := 0
	windowSize := 6

	for start := 0; start < 24; start++ {
		sum := 0
		for i := 0; i < windowSize; i++ {
			hour := (start + i) % 24
			sum += hourCounts[hour]
		}
		if sum < minSum {
			minSum = sum
			minStart = start
		}
	}

	quietHours := make([]int, windowSize)
	for i := 0; i < windowSize; i++ {
		quietHours[i] = (minStart + i) % 24
	}

	return quietHours
}

func timezoneFromOffset(offsetHours int) string {
	// Return generic UTC offset format since we don't know the country at this stage
	// This is used for activity-only detection where location is unknown
	if offsetHours >= 0 {
		return fmt.Sprintf("UTC+%d", offsetHours)
	}
	return fmt.Sprintf("UTC%d", offsetHours) // Negative sign is already included
}

func detectLunchBreak(hourCounts map[int]int, utcOffset int, workStart, workEnd int) (lunchStart, lunchEnd, confidence float64) {
	// Convert hour counts to 30-minute buckets for better precision
	bucketCounts := make(map[float64]int)
	for hour, count := range hourCounts {
		// Distribute the count evenly between two 30-minute buckets
		bucketCounts[float64(hour)] += count / 2
		bucketCounts[float64(hour)+0.5] += count / 2
		// Handle odd counts
		if count%2 == 1 {
			bucketCounts[float64(hour)] += 1
		}
	}

	// Look for activity dips during typical lunch hours (10am-3pm local for broader search)
	typicalLunchStart := 10.0
	typicalLunchEnd := 15.0

	// Convert local lunch hours to UTC
	lunchStartUTC := typicalLunchStart - float64(utcOffset)
	lunchEndUTC := typicalLunchEnd - float64(utcOffset)

	// Normalize to 0-24 range
	for lunchStartUTC < 0 {
		lunchStartUTC += 24
	}
	for lunchEndUTC < 0 {
		lunchEndUTC += 24
	}
	for lunchStartUTC >= 24 {
		lunchStartUTC -= 24
	}
	for lunchEndUTC >= 24 {
		lunchEndUTC -= 24
	}

	// Calculate average activity during work hours for comparison
	totalActivity := 0
	bucketCount := 0
	workHourBuckets := make([]float64, 0)
	for bucket := float64(workStart); bucket < float64(workEnd); bucket += 0.5 {
		utcBucket := bucket - float64(utcOffset)
		for utcBucket < 0 {
			utcBucket += 24
		}
		for utcBucket >= 24 {
			utcBucket -= 24
		}
		totalActivity += bucketCounts[utcBucket]
		bucketCount++
		workHourBuckets = append(workHourBuckets, utcBucket)
	}

	avgActivity := 0.0
	if bucketCount > 0 {
		avgActivity = float64(totalActivity) / float64(bucketCount)
	}

	// Find all candidate lunch periods (30-minute and 1-hour windows)
	type lunchCandidate struct {
		start      float64
		end        float64
		avgDip     float64
		distFrom12 float64
		confidence float64
	}

	candidates := make([]lunchCandidate, 0)

	// Check all possible 30-minute and 1-hour windows in the lunch timeframe
	for windowStart := lunchStartUTC; ; windowStart += 0.5 {
		if windowStart >= 24 {
			windowStart -= 24
		}

		// Try both 30-minute (1-bucket) and 60-minute (2-bucket) windows
		for windowSize := 1; windowSize <= 2; windowSize++ {
			windowEnd := windowStart + float64(windowSize)*0.5
			if windowEnd >= 24 {
				windowEnd -= 24
			}

			// Calculate average activity in this window
			windowActivity := 0.0
			windowBuckets := 0
			for bucket := windowStart; windowBuckets < windowSize; bucket += 0.5 {
				if bucket >= 24 {
					bucket -= 24
				}
				windowActivity += float64(bucketCounts[bucket])
				windowBuckets++
				if windowBuckets >= windowSize {
					break
				}
			}

			if windowBuckets > 0 {
				avgWindowActivity := windowActivity / float64(windowBuckets)

				// Calculate the dip relative to average work activity
				var dipPercentage float64
				if avgActivity > 0 {
					dipPercentage = (avgActivity - avgWindowActivity) / avgActivity
				}

				// Convert window center to local time to check distance from 12pm
				windowCenter := windowStart + float64(windowSize)*0.25
				localCenter := windowCenter + float64(utcOffset)
				for localCenter < 0 {
					localCenter += 24
				}
				for localCenter >= 24 {
					localCenter -= 24
				}
				distanceFrom12 := math.Abs(localCenter - 12.0)
				if distanceFrom12 > 12 {
					distanceFrom12 = 24 - distanceFrom12
				}

				// Calculate confidence based on dip size and proximity to 12pm
				confidence := 0.1 // Base confidence - always show something

				// Dip size component (max 0.6)
				dipComponent := 0.0
				if dipPercentage > 0.1 {
					dipComponent += 0.2 // Small dip
				}
				if dipPercentage > 0.25 {
					dipComponent += 0.2 // Significant dip
				}
				if dipPercentage > 0.5 {
					dipComponent += 0.2 // Very large dip
				}

				// Proximity to 12pm component (max 0.2)
				proximityComponent := 0.0
				if distanceFrom12 <= 2.0 {
					proximityComponent = (2.0 - distanceFrom12) / 2.0 * 0.2
				}

				// Duration appropriateness component (max 0.1)
				durationComponent := 0.0
				if windowSize == 1 && dipPercentage > 0.3 {
					// 30-minute breaks with strong dip pattern
					durationComponent = 0.1
				} else if windowSize == 2 && dipPercentage > 0.2 {
					// 1-hour breaks with reasonable dip pattern
					durationComponent = 0.1
				}

				// Combine components and cap at 1.0
				confidence = confidence + dipComponent + proximityComponent + durationComponent
				if confidence > 1.0 {
					confidence = 1.0
				}

				candidates = append(candidates, lunchCandidate{
					start:      windowStart + float64(utcOffset),
					end:        windowEnd + float64(utcOffset),
					avgDip:     dipPercentage,
					distFrom12: distanceFrom12,
					confidence: confidence,
				})
			}
		}

		// Stop when we've covered the lunch window
		if (lunchEndUTC > lunchStartUTC && windowStart >= lunchEndUTC) ||
			(lunchEndUTC < lunchStartUTC && windowStart >= lunchEndUTC && windowStart < lunchStartUTC) {
			break
		}
	}

	// Find the best candidate (highest confidence, prefer closer to 12pm for ties)
	bestCandidate := lunchCandidate{start: 12.0, end: 13.0, confidence: 0.1} // Default fallback

	for _, candidate := range candidates {
		// Normalize candidate times to 0-24 range
		for candidate.start < 0 {
			candidate.start += 24
		}
		for candidate.start >= 24 {
			candidate.start -= 24
		}
		for candidate.end < 0 {
			candidate.end += 24
		}
		for candidate.end >= 24 {
			candidate.end -= 24
		}

		// IMPORTANT: Lunch must be at least 1 hour after work starts
		// This prevents detecting early morning dips as lunch
		if candidate.start < float64(workStart)+1.0 {
			continue // Skip this candidate - too early to be lunch
		}
		
		// Also ensure lunch ends before work ends (with some buffer)
		if candidate.end > float64(workEnd)-0.5 {
			continue // Skip this candidate - too late to be lunch
		}

		// Prefer higher confidence, with proximity to 12pm as tiebreaker
		if candidate.confidence > bestCandidate.confidence ||
			(candidate.confidence == bestCandidate.confidence && candidate.distFrom12 < bestCandidate.distFrom12) {
			bestCandidate = candidate
		}
	}

	return bestCandidate.start, bestCandidate.end, bestCandidate.confidence
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis
func (d *Detector) formatEvidenceForGemini(contextData map[string]interface{}) string {
	var evidence strings.Builder

	// ACTIVITY ANALYSIS SECTION
	if activityTz, ok := contextData["activity_detected_timezone"].(string); ok {
		evidence.WriteString("## ACTIVITY ANALYSIS (HIGHLY RELIABLE)\n")
		evidence.WriteString(fmt.Sprintf("Detected Timezone: %s\n", activityTz))

		if confidence, ok := contextData["activity_confidence"].(float64); ok {
			evidence.WriteString(fmt.Sprintf("Activity Confidence: %.1f%%\n", confidence*100))
		}

		if workStart, ok := contextData["work_start_local"].(float64); ok {
			if workEnd, ok := contextData["work_end_local"].(float64); ok {
				evidence.WriteString(fmt.Sprintf("Work Hours: %.1f-%.1f local time\n", workStart, workEnd))
			}
		}

		if lunchStart, ok := contextData["lunch_start_local"].(float64); ok {
			if lunchEnd, ok := contextData["lunch_end_local"].(float64); ok {
				if lunchConf, ok := contextData["lunch_confidence"].(float64); ok {
					evidence.WriteString(fmt.Sprintf("Lunch Hours: %.1f-%.1f local time (%.1f%% confidence)\n",
						lunchStart, lunchEnd, lunchConf*100))
				}
			}
		}

		if sleepHours, ok := contextData["sleep_hours_utc"].([]int); ok && len(sleepHours) > 0 {
			evidence.WriteString(fmt.Sprintf("Sleep Hours UTC: %v\n", sleepHours))
		}

		if offset, ok := contextData["detected_gmt_offset"].(string); ok {
			evidence.WriteString(fmt.Sprintf("GMT Offset: %s\n", offset))
		}

		evidence.WriteString("\n")
	}

	// GITHUB USER PROFILE SECTION
	if userJSON, ok := contextData["github_user_json"]; ok {
		evidence.WriteString("## GITHUB USER PROFILE\n")
		if userBytes, err := json.MarshalIndent(userJSON, "", "  "); err == nil {
			evidence.WriteString(string(userBytes))
		}
		evidence.WriteString("\n\n")
	}

	// ORGANIZATIONS SECTION
	if orgs, ok := contextData["organizations"]; ok {
		evidence.WriteString("## ORGANIZATION MEMBERSHIPS\n")
		if orgBytes, err := json.MarshalIndent(orgs, "", "  "); err == nil {
			evidence.WriteString(string(orgBytes))
		}
		evidence.WriteString("\n\n")
	}

	// PULL REQUESTS SECTION
	if prs, ok := contextData["pull_requests"]; ok {
		evidence.WriteString("## RECENT PULL REQUEST TITLES\n")
		if prBytes, err := json.MarshalIndent(prs, "", "  "); err == nil {
			evidence.WriteString(string(prBytes))
		}
		evidence.WriteString("\n\n")
	}

	// LONGEST PR/ISSUE CONTENT SECTION (inline, not JSON)
	if title, ok := contextData["longest_pr_issue_title"].(string); ok && title != "" {
		evidence.WriteString("## LONGEST PR/ISSUE CONTENT\n")
		evidence.WriteString(fmt.Sprintf("Title: %s\n\n", title))

		if body, ok := contextData["longest_pr_issue_body"].(string); ok && body != "" {
			evidence.WriteString("Body:\n")
			evidence.WriteString(body)
			evidence.WriteString("\n\n")
		}
	}

	// WEBSITE CONTENT SECTION
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		evidence.WriteString("## WEBSITE/BLOG CONTENT\n")
		evidence.WriteString(websiteContent)
		evidence.WriteString("\n\n")
	}

	// ISSUE COUNT
	if issueCount, ok := contextData["issue_count"].(int); ok {
		evidence.WriteString(fmt.Sprintf("## ADDITIONAL METRICS\n"))
		evidence.WriteString(fmt.Sprintf("Issue Count: %d\n", issueCount))
	}

	return evidence.String()
}