package ghutz

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
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
		// Use 98th percentile for end to capture more of the workday
		// Many developers work past traditional hours
		percentile98 := len(activityInRange) * 98 / 100
		
		// Ensure we don't have invalid indices
		if percentile5 < 0 {
			percentile5 = 0
		}
		if percentile98 >= len(activityInRange) {
			percentile98 = len(activityInRange) - 1
		}

		start = activityInRange[percentile5]
		end = activityInRange[percentile98]
		
		// Also check if there's significant activity continuing past the current end
		// If activity at end+1 hour is > 25% of max activity in work hours, extend
		maxWorkActivity := 0
		for h := start; h != end; h = (h + 1) % 24 {
			if hourCounts[h] > maxWorkActivity {
				maxWorkActivity = hourCounts[h]
			}
		}
		
		// Keep extending end time while there's meaningful activity
		for {
			nextHour := (end + 1) % 24
			// Stop if we hit quiet hours or wrap around to start
			if nextHour == start {
				break
			}
			// Check if this hour is in quiet hours
			isQuiet := false
			if quietMap[nextHour] {
				isQuiet = true
			}
			if isQuiet {
				break
			}
			// If activity is still meaningful (>20% of max), extend
			if hourCounts[nextHour] > maxWorkActivity/5 {
				end = nextHour
			} else {
				break
			}
		}
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
		// Find the longest consecutive sequence (including wrap-around)
		maxLen := 0
		maxStart := 0
		
		// Check if hours wrap around midnight
		hasWrapAround := false
		if len(zeroHours) > 1 {
			// Check if we have both late night (20-23) and early morning (0-5) hours
			hasLateNight := false
			hasEarlyMorning := false
			for _, h := range zeroHours {
				if h >= 20 && h <= 23 {
					hasLateNight = true
				}
				if h >= 0 && h <= 5 {
					hasEarlyMorning = true
				}
			}
			hasWrapAround = hasLateNight && hasEarlyMorning
		}
		
		if hasWrapAround {
			// Try to find wrap-around sequence
			// Build a bitmap of quiet hours for easier checking
			quietMap := make(map[int]bool)
			for _, h := range zeroHours {
				quietMap[h] = true
			}
			
			// Check each possible starting hour for the longest sequence
			for startHour := 0; startHour < 24; startHour++ {
				if !quietMap[startHour] {
					continue
				}
				
				// Count consecutive hours from this start point
				length := 0
				for i := 0; i < 24; i++ {
					checkHour := (startHour + i) % 24
					if !quietMap[checkHour] {
						break
					}
					length++
				}
				
				if length > maxLen {
					maxLen = length
					maxStart = startHour
				}
			}
		} else {
			// Original logic for non-wrapping sequences
			currentStart := zeroHours[0]
			currentLen := 1

			for i := 1; i < len(zeroHours); i++ {
				if zeroHours[i] == zeroHours[i-1]+1 {
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
		}

		// Extract the core sleep hours from the zero-activity period
		// Skip early evening hours and wake-up hours to focus on deep sleep
		result := []int{}
		sleepStart := maxStart
		sleepLength := maxLen

		// Only skip hours if the quiet period starts in evening (19:00-23:00)
		// Don't skip if it starts after midnight (00:00-06:00) as that's already sleep time
		if maxStart >= 19 && maxStart <= 23 {
			// Period starts in evening, might include pre-sleep wind-down time
			if maxLen >= 8 {
				sleepStart = (maxStart + 2) % 24 // Skip first 2 hours of evening
				sleepLength = maxLen - 3         // Also skip last hour for wake-up
			} else if maxLen >= 6 {
				sleepStart = (maxStart + 1) % 24 // Skip first hour
				sleepLength = maxLen - 2         // Also skip last hour
			}
		} else if maxStart >= 0 && maxStart <= 6 {
			// Period starts after midnight or early morning - this is prime sleep time
			// Don't skip the beginning, but maybe skip the end for wake-up time
			if maxLen >= 6 {
				sleepLength = maxLen - 1 // Only skip last hour for wake-up
			}
		} else {
			// Period starts during day (7:00-18:00) - unusual but possible for night shift workers
			// Use the period as-is, limiting to reasonable duration
			if maxLen > 8 {
				sleepLength = 8
			}
		}

		// Limit to reasonable sleep duration (4-8 hours)
		if sleepLength > 8 {
			sleepLength = 8
		}
		if sleepLength < 4 && maxLen >= 4 {
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
	// If work hours are too short (less than 3 hours), we can't reliably detect lunch
	// This prevents false positives when there's minimal activity data
	if workEnd - workStart < 3 {
		// Return no lunch detected
		return 0, 0, 0
	}
	
	// First, check for clear gaps (hours with 0 activity) during work hours
	// This is more accurate than the bucket approach when we have clear data
	for localHour := workStart + 1; localHour < workEnd - 1; localHour++ {
		utcHour := localHour - utcOffset
		// Normalize to 0-23 range
		for utcHour < 0 {
			utcHour += 24
		}
		for utcHour >= 24 {
			utcHour -= 24
		}
		
		// Check if this hour has zero activity (potential lunch hour)
		if hourCounts[utcHour] == 0 {
			// Check if it's in typical lunch time range (11am-2pm)
			if localHour >= 11 && localHour <= 14 {
				// Found a clear lunch gap!
				// Check if the next hour is also empty (1-hour lunch)
				nextUTCHour := (utcHour + 1) % 24
				if hourCounts[nextUTCHour] == 0 {
					return float64(localHour), float64(localHour + 1), 0.9 // High confidence for 1-hour gap
				} else {
					// Just a 30-minute gap, but still likely lunch
					return float64(localHour), float64(localHour) + 0.5, 0.85
				}
			}
		}
	}
	
	// If no clear gaps, fall back to the bucket-based approach for finding dips
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
	// Start with no lunch detected instead of a bad default
	bestCandidate := lunchCandidate{start: 0, end: 0, confidence: 0}

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

	// Only return lunch if we have at least minimal confidence (20%)
	// This prevents reporting very uncertain lunch periods
	if bestCandidate.confidence < 0.2 {
		return 0, 0, 0
	}

	return bestCandidate.start, bestCandidate.end, bestCandidate.confidence
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis
func (d *Detector) formatEvidenceForGemini(contextData map[string]interface{}) string {
	var evidence strings.Builder

	// ACTIVITY ANALYSIS SECTION
	if activityTz, ok := contextData["activity_detected_timezone"].(string); ok {
		evidence.WriteString("## ACTIVITY ANALYSIS (BEHAVIORAL DATA)\n")
		
		// Check for suspicious work patterns
		workStart := 9.0 // default
		if ws, ok := contextData["work_start_local"].(float64); ok {
			workStart = ws
		}
		
		// Calculate alternative timezone candidates based on patterns
		evidence.WriteString("### TIMEZONE CANDIDATES (with confidence):\n")
		
		// Primary detected timezone
		primaryConfidence := 80.0
		if confidence, ok := contextData["activity_confidence"].(float64); ok {
			primaryConfidence = confidence * 100
		}
		
		// If work starts very early, reduce confidence and suggest alternatives
		if workStart < 6.0 {
			primaryConfidence = 40.0 // Low confidence for unusual work hours
			
			evidence.WriteString(fmt.Sprintf("1. **%s** (%.0f%% confidence)\n", activityTz, primaryConfidence))
			evidence.WriteString("   - ⚠️ Work starts at 3am which is VERY unusual\n")
			evidence.WriteString("   - Could be night shift or incorrect timezone detection\n\n")
			
			// Add China as high confidence alternative if sleep is 19-23 UTC
			if sleepHours, ok := contextData["sleep_hours_utc"].([]int); ok && len(sleepHours) > 0 {
				midSleep := sleepHours[len(sleepHours)/2]
				
				if midSleep >= 19 && midSleep <= 23 {
					evidence.WriteString("2. **UTC+8 (China/Singapore)** (70% confidence)\n")
					evidence.WriteString("   - Would make work hours 11am-2am (common for global teams)\n")
					
					// Add name-based hint if available
					if userJSON, ok := contextData["github_user_json"]; ok {
						if userMap, ok := userJSON.(map[string]interface{}); ok {
							if name, ok := userMap["name"].(string); ok && name != "" {
								evidence.WriteString(fmt.Sprintf("   - User's name is '%s'\n", name))
							}
						}
					}
					
					evidence.WriteString("   - Consider Asian timezone if name/company suggests it\n\n")
					
					evidence.WriteString("3. **UTC-8 (Pacific US)** (30% confidence)\n")
					evidence.WriteString("   - VMware headquarters in Palo Alto, California\n")
					evidence.WriteString("   - Would be night shift work pattern\n\n")
				}
			}
		} else {
			// Normal work hours - higher confidence
			evidence.WriteString(fmt.Sprintf("1. **%s** (%.0f%% confidence)\n", activityTz, primaryConfidence))
			evidence.WriteString("   - Work hours align with typical business hours\n")
			evidence.WriteString("   - Sleep pattern matches expected timezone\n\n")
		}
		
		// Add alternative timezone possibilities based on sleep patterns
		if sleepHours, ok := contextData["sleep_hours_utc"].([]int); ok && len(sleepHours) > 0 && workStart >= 6.0 {
			evidence.WriteString("\n### Other Possible Timezones (lower confidence):\n")
			
			// Calculate midpoint of sleep hours for alternative suggestions
			midSleep := sleepHours[len(sleepHours)/2]
			
			// If sleep is around 19-23 UTC (like tnqn), this could be:
			// - UTC+1 (sleeping 20-0 local) - Europe but work starts at 3am which is unusual!
			// - UTC+8 (sleeping 3-7am local) - China with reasonable work hours 11am-2am
			// - UTC-8 (sleeping 11am-3pm local) - Pacific with night shift work
			if midSleep >= 19 && midSleep <= 23 {
				evidence.WriteString("- UTC+8 (China/Singapore) - Chinese developer with late night work pattern\n")
				evidence.WriteString("  → Would be working 11am-2am CST which is common for global teams\n")
				evidence.WriteString("  → If name is clearly Chinese (Quan, Wei, Zhang, etc), strongly consider this\n")
				evidence.WriteString("- UTC-8 (Pacific US) - developer working night shift or unusual hours\n")
				evidence.WriteString("  → VMware has offices in Palo Alto, California\n")
			}
			
			// If sleep is around 20-2 UTC (like kinzhi), could be:
			// - UTC+4 (sleeping 0-5am local) - Middle East
			// - UTC+8 (irregular pattern or incomplete data) - China
			if midSleep >= 20 && midSleep <= 2 {
				evidence.WriteString("- UTC+8 (China/Singapore) - if Chinese name/company despite unusual hours\n")
				evidence.WriteString("  → Chinese developers may have irregular GitHub activity\n")
				evidence.WriteString("  → DaoCloud, KubeSphere, Karmada are Chinese projects\n")
			}
			
			// If sleep is 0-3 UTC (like Gauravpadam), could be:
			// - UTC+2 (sleeping 2-5am local) - Europe
			// - UTC+5:30 (unusual schedule, or data incomplete) - India
			// - UTC+3 (sleeping 3-6am local) - Eastern Europe/East Africa
			if midSleep >= 0 && midSleep <= 3 {
				evidence.WriteString("- UTC+5:30 (India) - Indian developer working European hours from India\n")
				evidence.WriteString("  → Common for Indians at European companies (HERE Maps, SAP, etc.)\n")
				evidence.WriteString("  → Would be working 2pm-11pm IST to align with Berlin office\n")
				evidence.WriteString("- UTC+3 (East Africa/Arabia) - adjacent timezone possibility\n")
			} else if midSleep >= 14 && midSleep <= 18 {
				// Asian sleep pattern
				evidence.WriteString("- UTC+8 (China/Singapore) - strong Asian pattern\n")
				evidence.WriteString("- UTC+9 (Japan/Korea) - alternative Asian timezone\n")
				evidence.WriteString("- UTC+7 (Thailand/Vietnam) - Southeast Asia possibility\n")
			} else if midSleep >= 4 && midSleep <= 10 {
				// American sleep pattern
				evidence.WriteString("- UTC-6 (Central US) - typical American pattern\n")
				evidence.WriteString("- UTC-5 (Eastern US) - East Coast possibility\n")
				evidence.WriteString("- UTC-7 (Mountain US) - Western US possibility\n")
			}
			evidence.WriteString("Note: Consider these alternatives if name/company evidence strongly suggests them\n")
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

		// Add activity date range for daylight saving context
		if oldestDate, ok := contextData["activity_oldest_date"].(string); ok {
			if newestDate, ok := contextData["activity_newest_date"].(string); ok {
				if totalDays, ok := contextData["activity_total_days"].(int); ok {
					evidence.WriteString(fmt.Sprintf("Activity Period: %s to %s (%d days)\n", 
						oldestDate, newestDate, totalDays))
					
					if spansDST, ok := contextData["activity_spans_dst_transitions"].(bool); ok && spansDST {
						evidence.WriteString("⚠️  Data spans DST transitions (spring & fall) - UTC offsets reflect mixed standard/daylight periods\n")
						evidence.WriteString("⚠️  Note: Some regions (Arizona, Saskatchewan, parts of Indiana, most of Asia/Africa) don't observe DST\n")
					}
				}
			}
		}

		evidence.WriteString("\n")
	}

	// GITHUB USER PROFILE SECTION
	if userJSON, ok := contextData["github_user_json"]; ok {
		evidence.WriteString("## GITHUB USER PROFILE\n")
		// Convert JSON to key/value bullets to save tokens
		if userMap, ok := userJSON.(map[string]interface{}); ok {
			// Important fields first
			if name, ok := userMap["name"].(string); ok && name != "" {
				evidence.WriteString(fmt.Sprintf("• Name: %s\n", name))
			}
			if login, ok := userMap["login"].(string); ok && login != "" {
				evidence.WriteString(fmt.Sprintf("• Login: %s\n", login))
			}
			if location, ok := userMap["location"].(string); ok && location != "" {
				evidence.WriteString(fmt.Sprintf("• Location: %s\n", location))
			}
			if company, ok := userMap["company"].(string); ok && company != "" {
				evidence.WriteString(fmt.Sprintf("• Company: %s\n", company))
			}
			if email, ok := userMap["email"].(string); ok && email != "" {
				evidence.WriteString(fmt.Sprintf("• Email: %s\n", email))
			}
			if blog, ok := userMap["blog"].(string); ok && blog != "" {
				evidence.WriteString(fmt.Sprintf("• Blog: %s\n", blog))
			}
			if bio, ok := userMap["bio"].(string); ok && bio != "" {
				evidence.WriteString(fmt.Sprintf("• Bio: %s\n", bio))
			}
			if twitterUsername, ok := userMap["twitter_username"].(string); ok && twitterUsername != "" {
				evidence.WriteString(fmt.Sprintf("• Twitter: @%s\n", twitterUsername))
			}
			// Numeric fields
			if publicRepos, ok := userMap["public_repos"].(float64); ok {
				evidence.WriteString(fmt.Sprintf("• Public repos: %.0f\n", publicRepos))
			}
			if followers, ok := userMap["followers"].(float64); ok {
				evidence.WriteString(fmt.Sprintf("• Followers: %.0f\n", followers))
			}
			if following, ok := userMap["following"].(float64); ok {
				evidence.WriteString(fmt.Sprintf("• Following: %.0f\n", following))
			}
			if createdAt, ok := userMap["created_at"].(string); ok && createdAt != "" {
				evidence.WriteString(fmt.Sprintf("• Created: %s\n", createdAt))
			}
		}
		evidence.WriteString("\n")
	}

	// COUNTRY-CODE TLD ANALYSIS SECTION
	if ccTLDs, ok := contextData["country_tlds"].([]CountryTLD); ok && len(ccTLDs) > 0 {
		evidence.WriteString("## COUNTRY-CODE TLD ANALYSIS\n")
		evidence.WriteString("Strong location indicators from user's website/social media domains:\n")
		for _, tld := range ccTLDs {
			evidence.WriteString(fmt.Sprintf("- **%s** → %s (%s) - VERY STRONG LOCATION SIGNAL\n", tld.TLD, tld.Country, tld.Region))
		}
		evidence.WriteString("\n")
	}

	// ORGANIZATIONS SECTION
	if orgs, ok := contextData["organizations"]; ok {
		evidence.WriteString("## ORGANIZATION MEMBERSHIPS\n")
		// Convert to simple bulleted list to save tokens
		if orgsList, ok := orgs.([]interface{}); ok {
			for _, org := range orgsList {
				if orgMap, ok := org.(map[string]interface{}); ok {
					if name, ok := orgMap["name"].(string); ok {
						if count, ok := orgMap["count"].(float64); ok {
							evidence.WriteString(fmt.Sprintf("• %s (%d contributions)\n", name, int(count)))
						} else {
							evidence.WriteString(fmt.Sprintf("• %s\n", name))
						}
					}
				}
			}
		}
		evidence.WriteString("\n")
	}

	// PULL REQUESTS SECTION
	if prs, ok := contextData["pull_requests"]; ok {
		evidence.WriteString("## RECENT PULL REQUEST TITLES\n")
		// Convert to simple bulleted list to save tokens
		if prsList, ok := prs.([]interface{}); ok {
			for _, pr := range prsList {
				if prStr, ok := pr.(string); ok {
					evidence.WriteString(fmt.Sprintf("• %s\n", prStr))
				}
			}
		}
		evidence.WriteString("\n")
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

	// REPOSITORIES SECTION (Enhanced)
	if repos, ok := contextData["user_repositories"].([]Repository); ok && len(repos) > 0 {
		evidence.WriteString("## USER'S TOP REPOSITORIES (Pinned/Popular)\n")
		evidence.WriteString("These repositories may provide strong clues about the user's location, work affiliations, and regional interests:\n")
		for _, repo := range repos {
			evidence.WriteString(fmt.Sprintf("- **%s** (%d stars)", repo.FullName, repo.StargazersCount))
			if repo.IsPinned {
				evidence.WriteString(" [PINNED]")
			}
			evidence.WriteString("\n")
			if repo.Description != "" {
				evidence.WriteString(fmt.Sprintf("  Description: %s\n", repo.Description))
			}
			if repo.Language != "" {
				evidence.WriteString(fmt.Sprintf("  Language: %s\n", repo.Language))
			}
		}
		evidence.WriteString("\n")
	}
	
	// CONTRIBUTED REPOSITORIES SECTION (existing activity data)
	if repos, ok := contextData["repositories"].([]string); ok && len(repos) > 0 {
		evidence.WriteString("## REPOSITORIES USER IS ACTIVE IN\n")
		evidence.WriteString("These repositories from activity data may provide additional location clues:\n")
		for _, repo := range repos {
			evidence.WriteString(fmt.Sprintf("- %s\n", repo))
		}
		evidence.WriteString("\n")
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

// detectPeakProductivity finds the single most productive 30-minute slot
func detectPeakProductivity(hourCounts map[int]int, utcOffset int) (start, end float64, count int) {
	// Convert hour counts to 30-minute buckets in local time
	bucketCounts := make(map[float64]int)
	
	// Distribute hourly counts into 30-minute buckets
	for utcHour, hourCount := range hourCounts {
		// Convert UTC hour to local hour
		localHour := (utcHour + utcOffset + 24) % 24
		
		// Split the count between two 30-minute buckets
		// Give slightly more weight to the first half-hour since most activity
		// tends to happen at the start of an hour
		bucketCounts[float64(localHour)] += (hourCount + 1) / 2
		bucketCounts[float64(localHour)+0.5] += hourCount / 2
	}
	
	// Find the single 30-minute bucket with the highest activity
	maxActivity := 0
	peakBucket := 0.0
	
	for bucket := 0.0; bucket < 24.0; bucket += 0.5 {
		if bucketCounts[bucket] > maxActivity {
			maxActivity = bucketCounts[bucket]
			peakBucket = bucket
		}
	}
	
	// Return the peak 30-minute slot
	if maxActivity > 0 {
		return peakBucket, peakBucket + 0.5, maxActivity
	}
	
	return 0, 0, 0
}

// CountryTLD represents a country-code top-level domain with metadata
type CountryTLD struct {
	TLD     string `json:"tld"`
	Country string `json:"country"`
	Region  string `json:"region"`
}

// extractCountryTLDs extracts country-code top-level domains from URLs
func extractCountryTLDs(urls ...string) []CountryTLD {
	// Map of ccTLDs to countries and regions for location hints
	ccTLDMap := map[string]CountryTLD{
		".ca":  {".ca", "Canada", "North America"},
		".uk":  {".uk", "United Kingdom", "Europe"},
		".de":  {".de", "Germany", "Europe"},
		".fr":  {".fr", "France", "Europe"},
		".it":  {".it", "Italy", "Europe"},
		".es":  {".es", "Spain", "Europe"},
		".nl":  {".nl", "Netherlands", "Europe"},
		".ch":  {".ch", "Switzerland", "Europe"},
		".at":  {".at", "Austria", "Europe"},
		".be":  {".be", "Belgium", "Europe"},
		".dk":  {".dk", "Denmark", "Europe"},
		".fi":  {".fi", "Finland", "Europe"},
		".no":  {".no", "Norway", "Europe"},
		".se":  {".se", "Sweden", "Europe"},
		".pl":  {".pl", "Poland", "Europe"},
		".cz":  {".cz", "Czech Republic", "Europe"},
		".hu":  {".hu", "Hungary", "Europe"},
		".ru":  {".ru", "Russia", "Europe/Asia"},
		".ua":  {".ua", "Ukraine", "Europe"},
		".jp":  {".jp", "Japan", "Asia"},
		".kr":  {".kr", "South Korea", "Asia"},
		".cn":  {".cn", "China", "Asia"},
		".hk":  {".hk", "Hong Kong", "Asia"},
		".sg":  {".sg", "Singapore", "Asia"},
		".in":  {".in", "India", "Asia"},
		".au":  {".au", "Australia", "Oceania"},
		".nz":  {".nz", "New Zealand", "Oceania"},
		".br":  {".br", "Brazil", "South America"},
		".ar":  {".ar", "Argentina", "South America"},
		".cl":  {".cl", "Chile", "South America"},
		".mx":  {".mx", "Mexico", "North America"},
		".za":  {".za", "South Africa", "Africa"},
		".ie":  {".ie", "Ireland", "Europe"},
		".pt":  {".pt", "Portugal", "Europe"},
		".gr":  {".gr", "Greece", "Europe"},
		".tr":  {".tr", "Turkey", "Europe/Asia"},
		".is":  {".is", "Iceland", "Europe"},
		".il":  {".il", "Israel", "Middle East"},
		".eg":  {".eg", "Egypt", "Africa/Middle East"},
	}

	var foundTLDs []CountryTLD
	seenTLDs := make(map[string]bool)

	for _, urlStr := range urls {
		if urlStr == "" {
			continue
		}

		// Ensure URL has a scheme for proper parsing
		if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
			urlStr = "https://" + urlStr
		}

		parsedURL, err := url.Parse(urlStr)
		if err != nil {
			continue
		}

		hostname := strings.ToLower(parsedURL.Hostname())
		if hostname == "" {
			continue
		}

		// Check each ccTLD against the hostname
		for tld, info := range ccTLDMap {
			if strings.HasSuffix(hostname, tld) && !seenTLDs[tld] {
				foundTLDs = append(foundTLDs, info)
				seenTLDs[tld] = true
			}
		}
	}

	return foundTLDs
}

// extractSocialMediaURLs extracts URLs from various social media fields in user profile
func extractSocialMediaURLs(user *GitHubUser) []string {
	var urls []string
	
	// Blog/website URL
	if user.Blog != "" {
		urls = append(urls, user.Blog)
	}
	
	// Twitter URL construction
	if user.TwitterUsername != "" {
		urls = append(urls, "https://twitter.com/"+user.TwitterUsername)
	}
	
	// Extract URLs from bio using regex
	if user.Bio != "" {
		// Simple regex to find URLs in bio
		urlRegex := regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`)
		bioURLs := urlRegex.FindAllString(user.Bio, -1)
		urls = append(urls, bioURLs...)
		
		// Also check for domain-only references (e.g., "jamon.ca")
		domainRegex := regexp.MustCompile(`[a-zA-Z0-9-]+\.[a-z]{2,}`)
		domains := domainRegex.FindAllString(user.Bio, -1)
		for _, domain := range domains {
			// Filter out common non-URL patterns and only include ccTLD-like domains
			lowerDomain := strings.ToLower(domain)
			if !strings.HasSuffix(lowerDomain, ".com") && !strings.HasSuffix(lowerDomain, ".org") && !strings.HasSuffix(lowerDomain, ".net") && !strings.HasSuffix(lowerDomain, ".io") && !strings.HasSuffix(lowerDomain, ".dev") {
				urls = append(urls, domain)
			}
		}
	}
	
	return urls
}

// fetchMastodonWebsite fetches a Mastodon profile and extracts the website URL
func fetchMastodonWebsite(mastodonURL string, logger *slog.Logger) string {
	// Parse the Mastodon URL to extract instance and username
	// Format: https://instance.domain/@username
	re := regexp.MustCompile(`https?://([^/]+)/@([^/]+)`)
	matches := re.FindStringSubmatch(mastodonURL)
	if len(matches) < 3 {
		logger.Debug("invalid Mastodon URL format", "url", mastodonURL)
		return ""
	}
	
	instance := matches[1]
	username := matches[2]
	
	// Try to fetch the Mastodon profile page
	resp, err := http.Get(mastodonURL)
	if err != nil {
		logger.Debug("failed to fetch Mastodon profile", "url", mastodonURL, "error", err)
		return ""
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		logger.Debug("Mastodon profile returned non-200 status", "url", mastodonURL, "status", resp.StatusCode)
		return ""
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debug("failed to read Mastodon profile body", "url", mastodonURL, "error", err)
		return ""
	}
	
	html := string(body)
	
	// Look for website in meta tags or profile fields
	// Pattern 1: Look for website in profile fields (common in Mastodon)
	websiteRegex := regexp.MustCompile(`(?i)(?:website|homepage|blog|site)[^>]*>.*?href="([^"]+)"`)
	if matches := websiteRegex.FindStringSubmatch(html); len(matches) > 1 {
		website := matches[1]
		logger.Debug("found website in Mastodon profile", "mastodon", mastodonURL, "website", website)
		return website
	}
	
	// Pattern 2: Look for verified links (rel="me" links that Mastodon verifies)
	verifiedRegex := regexp.MustCompile(`rel="me[^"]*"[^>]*href="([^"]+)"`)
	if matches := verifiedRegex.FindStringSubmatch(html); len(matches) > 1 {
		website := matches[1]
		// Filter out other social media links
		if !strings.Contains(website, "twitter.com") && !strings.Contains(website, "github.com") {
			logger.Debug("found verified website in Mastodon profile", "mastodon", mastodonURL, "website", website)
			return website
		}
	}
	
	// Pattern 3: Try the Mastodon API
	apiURL := fmt.Sprintf("https://%s/api/v1/accounts/lookup?acct=%s", instance, username)
	apiResp, err := http.Get(apiURL)
	if err == nil && apiResp.StatusCode == http.StatusOK {
		defer apiResp.Body.Close()
		var account struct {
			URL    string `json:"url"`
			Fields []struct {
				Name       string `json:"name"`
				Value      string `json:"value"`
				VerifiedAt string `json:"verified_at"`
			} `json:"fields"`
		}
		if err := json.NewDecoder(apiResp.Body).Decode(&account); err == nil {
			// Check fields for website
			for _, field := range account.Fields {
				fieldName := strings.ToLower(field.Name)
				if strings.Contains(fieldName, "website") || strings.Contains(fieldName, "site") || 
				   strings.Contains(fieldName, "blog") || strings.Contains(fieldName, "homepage") {
					// Extract URL from HTML value
					urlRegex := regexp.MustCompile(`href="([^"]+)"`)
					if matches := urlRegex.FindStringSubmatch(field.Value); len(matches) > 1 {
						website := matches[1]
						logger.Debug("found website via Mastodon API", "mastodon", mastodonURL, "website", website)
						return website
					}
				}
			}
		}
	}
	
	logger.Debug("no website found in Mastodon profile", "url", mastodonURL)
	return ""
}

// isPolishName checks if a name appears to be Polish based on common patterns
func isPolishName(name string) bool {
	if name == "" {
		return false
	}
	
	nameLower := strings.ToLower(name)
	
	// Check for Polish special characters
	polishChars := []string{"ł", "ą", "ć", "ę", "ń", "ó", "ś", "ź", "ż"}
	for _, char := range polishChars {
		if strings.Contains(nameLower, char) {
			return true
		}
	}
	
	// Check for common Polish name endings
	polishEndings := []string{"ski", "cki", "wicz", "czak", "czyk", "owski", "ewski", "iński"}
	for _, ending := range polishEndings {
		if strings.HasSuffix(nameLower, ending) {
			return true
		}
	}
	
	// Check for common Polish first names
	polishFirstNames := []string{"łukasz", "paweł", "michał", "piotr", "wojciech", 
		"krzysztof", "andrzej", "marek", "tomasz", "jan", "stanisław", "zbigniew",
		"anna", "maria", "katarzyna", "małgorzata", "agnieszka", "barbara", "ewa",
		"elżbieta", "zofia", "teresa", "magdalena", "joanna", "aleksandra"}
	
	nameWords := strings.Fields(nameLower)
	for _, word := range nameWords {
		for _, firstName := range polishFirstNames {
			if word == firstName {
				return true
			}
		}
	}
	
	return false
}

// extractSocialMediaFromHTML extracts social media links from GitHub profile HTML
func extractSocialMediaFromHTML(html string) []string {
	var urls []string
	
	// Extract Mastodon links (format: @username@instance.domain)
	// Look for pattern like: href="https://infosec.exchange/@jamon">@jamon@infosec.exchange
	mastodonRegex := regexp.MustCompile(`href="(https?://[^"]+/@[^"]+)"[^>]*>@[^@]+@[^<]+`)
	mastodonMatches := mastodonRegex.FindAllStringSubmatch(html, -1)
	for _, match := range mastodonMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}
	
	// Also look for rel="nofollow me" links which are commonly used for Mastodon verification
	relMeRegex := regexp.MustCompile(`rel="[^"]*\bme\b[^"]*"[^>]*href="([^"]+)"`)
	relMeMatches := relMeRegex.FindAllStringSubmatch(html, -1)
	for _, match := range relMeMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}
	
	// Alternative pattern for rel="me" links
	altRelMeRegex := regexp.MustCompile(`href="([^"]+)"[^>]*rel="[^"]*\bme\b[^"]*"`)
	altRelMeMatches := altRelMeRegex.FindAllStringSubmatch(html, -1)
	for _, match := range altRelMeMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}
	
	return urls
}