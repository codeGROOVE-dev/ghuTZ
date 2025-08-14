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
	"strings"
)

// calculateTypicalActiveHours determines typical work hours based on activity patterns
// It finds the longest continuous block of substantial activity, ignoring gaps > 2 hours
func calculateTypicalActiveHours(hourCounts map[int]int, quietHours []int, utcOffset int) (start, end int) {
	// Create a map for easy lookup of quiet hours
	quietMap := make(map[int]bool)
	for _, h := range quietHours {
		quietMap[h] = true
	}

	// Find hours with meaningful activity (>5% of max activity)
	maxActivity := 0
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	threshold := maxActivity / 20 // Lower threshold to catch more activity

	if maxActivity == 0 {
		// Default to 9am-5pm if no activity
		return 9, 17
	}

	// Find all continuous work blocks (gaps <= 2 hours allowed, except for lunch)
	type workBlock struct {
		start, end    int
		totalActivity int
		hourCount     int
	}

	var blocks []workBlock
	currentBlock := workBlock{start: -1}

	for hour := 0; hour < 24; hour++ {
		hasActivity := !quietMap[hour] && hourCounts[hour] > threshold

		if hasActivity {
			if currentBlock.start == -1 {
				// Start new block
				currentBlock.start = hour
				currentBlock.end = hour
				currentBlock.totalActivity = hourCounts[hour]
				currentBlock.hourCount = 1
			} else {
				// Extend current block
				currentBlock.end = hour
				currentBlock.totalActivity += hourCounts[hour]
				currentBlock.hourCount++
			}
		} else {
			if currentBlock.start != -1 {
				// Check if we should end the current block
				// Allow gaps of up to 1 hour (for lunch breaks)
				foundActivity := false

				// Look ahead up to 1 hour for continuation (serious lunch breaks!)
				for lookahead := 1; lookahead <= 1; lookahead++ {
					nextHour := (hour + lookahead) % 24
					if !quietMap[nextHour] && hourCounts[nextHour] > threshold {
						foundActivity = true
						break
					}
				}

				if !foundActivity {
					// End current block - no meaningful activity found within 1 hour
					blocks = append(blocks, currentBlock)
					currentBlock = workBlock{start: -1}
				}
			}
		}
	}

	// Don't forget to add the last block if we ended on activity
	if currentBlock.start != -1 {
		blocks = append(blocks, currentBlock)
	}

	if len(blocks) == 0 {
		// Default to 9am-5pm if no clear pattern
		return 9, 17
	}

	// Find the longest block by total activity (not just hour count)
	bestBlock := blocks[0]
	for _, block := range blocks[1:] {
		// Prefer blocks with more total activity and longer duration
		score := block.totalActivity * block.hourCount
		bestScore := bestBlock.totalActivity * bestBlock.hourCount

		if score > bestScore {
			bestBlock = block
		}
	}

	start = bestBlock.start
	end = bestBlock.end

	// Convert UTC hours to local hours for final result
	start = (start + utcOffset) % 24
	if start < 0 {
		start += 24
	}
	end = (end + utcOffset) % 24
	if end < 0 {
		end += 24
	}

	return start, end
}

// findSleepHours looks for continuous quiet periods of at least 3 hours
// Only returns quiet periods that are 3+ hours continuous
func findSleepHours(hourCounts map[int]int) []int {
	// Find threshold for quiet hours (≤5% of max activity or ≤1 event)
	maxActivity := 0
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}

	threshold := maxActivity / 20
	if threshold < 1 {
		threshold = 1
	}

	// Find all quiet hours
	quietHours := make([]bool, 24)
	for hour := 0; hour < 24; hour++ {
		quietHours[hour] = hourCounts[hour] <= threshold
	}

	// If very few quiet hours, be more strict (only zero activity)
	totalQuietHours := 0
	for _, isQuiet := range quietHours {
		if isQuiet {
			totalQuietHours++
		}
	}

	if totalQuietHours < 4 {
		// Be more strict - only use hours with zero activity
		for hour := 0; hour < 24; hour++ {
			quietHours[hour] = hourCounts[hour] == 0
		}
	}

	// Find continuous blocks of quiet hours that are at least 3 hours long
	var result []int
	var blocks [][]int

	// First pass: Find all continuous blocks
	var currentBlock []int
	for hour := 0; hour < 24; hour++ {
		if quietHours[hour] {
			currentBlock = append(currentBlock, hour)
		} else {
			if len(currentBlock) >= 3 {
				blocks = append(blocks, currentBlock)
			}
			currentBlock = nil
		}
	}

	// Handle wraparound case: check if first and last blocks can be combined
	// (e.g., [22, 23] and [0, 1, 2] should become [22, 23, 0, 1, 2])
	if len(currentBlock) > 0 && len(blocks) > 0 {
		firstBlock := blocks[0]
		lastBlock := currentBlock

		// Check if they can be combined (last hour of currentBlock is 23, first hour of firstBlock is 0)
		if len(lastBlock) > 0 && len(firstBlock) > 0 &&
			lastBlock[len(lastBlock)-1] == 23 && firstBlock[0] == 0 {
			// Combine the blocks: [22, 23] + [0, 1, 2] = [22, 23, 0, 1, 2]
			combined := append(lastBlock, firstBlock...)
			if len(combined) >= 3 {
				// Replace the first block with the combined block
				blocks[0] = combined
			}
		} else {
			// They can't be combined, add the last block if it's long enough
			if len(lastBlock) >= 3 {
				blocks = append(blocks, lastBlock)
			}
		}
	} else if len(currentBlock) >= 3 {
		// No existing blocks, just add the current one if it's long enough
		blocks = append(blocks, currentBlock)
	}

	// Flatten all valid blocks into the result
	for _, block := range blocks {
		result = append(result, block...)
	}

	// If no continuous blocks of 3+ hours found, fall back to old method
	if len(result) == 0 {
		return findQuietHours(hourCounts)
	}

	return result
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
	if workEnd-workStart < 3 {
		// Return no lunch detected
		return 0, 0, 0
	}

	// First, check for clear gaps (hours with 0 activity) during work hours
	// This is more accurate than the bucket approach when we have clear data
	for localHour := workStart + 1; localHour < workEnd-1; localHour++ {
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
			// Check if it's in typical lunch time range (10am-2pm) - expanded to include early lunch
			if localHour >= 10 && localHour <= 14 {
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

		// Generate timezone candidates sorted by confidence (all times in UTC)
		evidence.WriteString("### TIMEZONE CANDIDATES (sorted by confidence):\n")

		// Primary detected timezone
		primaryConfidence := 80.0
		if confidence, ok := contextData["activity_confidence"].(float64); ok {
			primaryConfidence = confidence * 100
		}

		// If work starts very early, reduce confidence but still list it first
		if workStart < 6.0 {
			primaryConfidence = 60.0 // Moderate confidence - unusual pattern
		}

		// Convert local times to UTC for consistency
		var workStartUTC, workEndUTC, lunchStartUTC, lunchEndUTC float64
		var lunchConfidence float64
		var utcOffset int

		if offsetStr, ok := contextData["detected_gmt_offset"].(string); ok {
			if offsetStr == "GMT+0" || offsetStr == "GMT-0" {
				utcOffset = 0
			} else if strings.HasPrefix(offsetStr, "GMT+") {
				fmt.Sscanf(offsetStr, "GMT+%d", &utcOffset)
			} else if strings.HasPrefix(offsetStr, "GMT-") {
				fmt.Sscanf(offsetStr, "GMT-%d", &utcOffset)
				utcOffset = -utcOffset
			}
		}

		if ws, ok := contextData["work_start_local"].(float64); ok {
			if we, ok := contextData["work_end_local"].(float64); ok {
				workStartUTC = math.Mod(ws-float64(utcOffset)+24, 24)
				workEndUTC = math.Mod(we-float64(utcOffset)+24, 24)
			}
		}

		if ls, ok := contextData["lunch_start_local"].(float64); ok {
			if le, ok := contextData["lunch_end_local"].(float64); ok {
				if lc, ok := contextData["lunch_confidence"].(float64); ok {
					lunchStartUTC = math.Mod(ls-float64(utcOffset)+24, 24)
					lunchEndUTC = math.Mod(le-float64(utcOffset)+24, 24)
					lunchConfidence = lc * 100
				}
			}
		}

		// Primary timezone candidate
		evidence.WriteString(fmt.Sprintf("1. **%s** (%.0f%% confidence)\n", activityTz, primaryConfidence))
		if workStartUTC != 0 || workEndUTC != 0 {
			evidence.WriteString(fmt.Sprintf("   - Work Hours UTC: %.1f-%.1f\n", workStartUTC, workEndUTC))
		}
		if lunchStartUTC != 0 || lunchEndUTC != 0 {
			evidence.WriteString(fmt.Sprintf("   - Lunch Hours UTC: %.1f-%.1f (%.0f%% confidence)\n",
				lunchStartUTC, lunchEndUTC, lunchConfidence))
		}
		if sleepHours, ok := contextData["sleep_hours_utc"].([]int); ok && len(sleepHours) > 0 {
			evidence.WriteString(fmt.Sprintf("   - Sleep Hours UTC: %v\n", sleepHours))
		}
		if workStart < 6.0 {
			evidence.WriteString("   - ⚠️ Work starts very early (unusual pattern - possible night shift/remote work)\n")
		} else {
			evidence.WriteString("   - Work hours align with typical business patterns\n")
		}
		evidence.WriteString("\n")

		// Generate 2-3 alternative timezone candidates based on context
		altCandidates := generateAlternativeTimezones(activityTz, workStart)
		for i, alt := range altCandidates {
			evidence.WriteString(fmt.Sprintf("%d. **%s** (%.0f%% confidence)\n", i+2, alt.Timezone, alt.Confidence*100))
			for _, reason := range alt.Evidence {
				evidence.WriteString(fmt.Sprintf("   - %s\n", reason))
			}
			evidence.WriteString("\n")
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

		// Handle GitHubUser struct directly
		if user, ok := userJSON.(*GitHubUser); ok && user != nil {
			// Important fields first
			if user.Name != "" {
				evidence.WriteString(fmt.Sprintf("• Name: %s\n", user.Name))
			}
			if user.Login != "" {
				evidence.WriteString(fmt.Sprintf("• Login: %s\n", user.Login))
			}
			if user.Location != "" {
				evidence.WriteString(fmt.Sprintf("• Location: %s\n", user.Location))
			}
			if user.Company != "" {
				evidence.WriteString(fmt.Sprintf("• Company: %s\n", user.Company))
			}
			if user.Email != "" {
				evidence.WriteString(fmt.Sprintf("• Email: %s\n", user.Email))
			}
			if user.Blog != "" {
				evidence.WriteString(fmt.Sprintf("• Blog: %s\n", user.Blog))
			}
			if user.Bio != "" {
				evidence.WriteString(fmt.Sprintf("• Bio: %s\n", user.Bio))
			}
			if user.TwitterUsername != "" {
				evidence.WriteString(fmt.Sprintf("• Twitter: @%s\n", user.TwitterUsername))
			}
			if user.CreatedAt != "" {
				evidence.WriteString(fmt.Sprintf("• Created: %s\n", user.CreatedAt))
			}
		} else if userMap, ok := userJSON.(map[string]interface{}); ok {
			// Fallback: handle map[string]interface{} format
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
			if createdAt, ok := userMap["created_at"].(string); ok && createdAt != "" {
				evidence.WriteString(fmt.Sprintf("• Created: %s\n", createdAt))
			}
		}
		evidence.WriteString("\n")
	}

	// SOCIAL MEDIA PROFILES SECTION
	if twitterURLs, ok := contextData["twitter_urls"].([]string); ok && len(twitterURLs) > 0 {
		evidence.WriteString("## SOCIAL MEDIA PROFILES\n")
		evidence.WriteString("Twitter/X profiles that may contain location information:\n")
		for _, url := range twitterURLs {
			evidence.WriteString(fmt.Sprintf("• %s\n", url))
		}
		evidence.WriteString("Note: Check these profiles manually for location data in bio or profile fields\n")
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

		// Handle []Organization slice directly
		if orgsList, ok := orgs.([]Organization); ok {
			for _, org := range orgsList {
				if org.Login != "" {
					if org.Description != "" {
						evidence.WriteString(fmt.Sprintf("• %s - %s\n", org.Login, org.Description))
					} else {
						evidence.WriteString(fmt.Sprintf("• %s\n", org.Login))
					}
					if org.Location != "" {
						evidence.WriteString(fmt.Sprintf("  → Location: %s\n", org.Location))
					}
				}
			}
		} else if orgsList, ok := orgs.([]interface{}); ok {
			// Fallback: handle []interface{} format
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

		// Handle []map[string]interface{} format (actual format from detector.go)
		if prsList, ok := prs.([]map[string]interface{}); ok {
			for _, pr := range prsList {
				if title, ok := pr["title"].(string); ok && title != "" {
					evidence.WriteString(fmt.Sprintf("• %s\n", title))
				}
			}
		} else if prsList, ok := prs.([]interface{}); ok {
			// Fallback: handle []interface{} format
			for _, pr := range prsList {
				if prMap, ok := pr.(map[string]interface{}); ok {
					if title, ok := prMap["title"].(string); ok && title != "" {
						evidence.WriteString(fmt.Sprintf("• %s\n", title))
					}
				} else if prStr, ok := pr.(string); ok {
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
			if repo.Description != "" {
				evidence.WriteString(fmt.Sprintf("• %s: %s\n", repo.FullName, repo.Description))
			} else {
				evidence.WriteString(fmt.Sprintf("• %s\n", repo.FullName))
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

// generateAlternativeTimezones creates 2-3 plausible alternative timezone candidates
func generateAlternativeTimezones(primaryTz string, workStart float64) []TimezoneCandidate {
	var candidates []TimezoneCandidate

	switch primaryTz {
	case "UTC+1":
		if workStart < 6.0 {
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "UTC-5",
				Confidence: 0.25,
				Evidence:   []string{"Early hours could indicate US East Coast remote work alignment"},
			})
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "UTC+3",
				Confidence: 0.20,
				Evidence:   []string{"Night shift worker in Eastern Europe/Middle East"},
			})
		} else {
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "UTC+5:30",
				Confidence: 0.30,
				Evidence:   []string{"Indian developer working European hours (common in global companies)"},
			})
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "UTC+3",
				Confidence: 0.20,
				Evidence:   []string{"Adjacent Eastern European timezone possibility"},
			})
		}
	case "UTC+2":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC+1",
			Confidence: 0.25,
			Evidence:   []string{"One hour west (Central Europe) possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC+3",
			Confidence: 0.20,
			Evidence:   []string{"One hour east (Eastern Europe/Middle East) possibility"},
		})
	case "UTC-5":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-6",
			Confidence: 0.25,
			Evidence:   []string{"Central US timezone possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-4",
			Confidence: 0.20,
			Evidence:   []string{"Atlantic timezone or Eastern Daylight Time"},
		})
	case "UTC-6":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-5",
			Confidence: 0.25,
			Evidence:   []string{"Eastern US timezone possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-7",
			Confidence: 0.20,
			Evidence:   []string{"Mountain US timezone possibility"},
		})
	case "UTC-7":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-8",
			Confidence: 0.25,
			Evidence:   []string{"Pacific US timezone possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-6",
			Confidence: 0.20,
			Evidence:   []string{"Central US timezone possibility"},
		})
	case "UTC-8":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-7",
			Confidence: 0.25,
			Evidence:   []string{"Mountain US timezone possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC-9",
			Confidence: 0.15,
			Evidence:   []string{"Alaska timezone possibility"},
		})
	case "UTC+8":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC+9",
			Confidence: 0.25,
			Evidence:   []string{"Japan/Korea timezone possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC+7",
			Confidence: 0.20,
			Evidence:   []string{"Southeast Asia (Thailand/Vietnam) possibility"},
		})
	case "UTC+9":
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC+8",
			Confidence: 0.25,
			Evidence:   []string{"China/Singapore timezone possibility"},
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "UTC+10",
			Confidence: 0.20,
			Evidence:   []string{"Eastern Australia timezone possibility"},
		})
	default:
		// Fallback for other timezones - adjacent possibilities
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Adjacent timezone",
			Confidence: 0.20,
			Evidence:   []string{"Adjacent timezone possibility based on geographic proximity"},
		})
	}

	return candidates
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
		".ca": {".ca", "Canada", "North America"},
		".uk": {".uk", "United Kingdom", "Europe"},
		".de": {".de", "Germany", "Europe"},
		".fr": {".fr", "France", "Europe"},
		".it": {".it", "Italy", "Europe"},
		".es": {".es", "Spain", "Europe"},
		".nl": {".nl", "Netherlands", "Europe"},
		".ch": {".ch", "Switzerland", "Europe"},
		".at": {".at", "Austria", "Europe"},
		".be": {".be", "Belgium", "Europe"},
		".dk": {".dk", "Denmark", "Europe"},
		".fi": {".fi", "Finland", "Europe"},
		".no": {".no", "Norway", "Europe"},
		".se": {".se", "Sweden", "Europe"},
		".pl": {".pl", "Poland", "Europe"},
		".cz": {".cz", "Czech Republic", "Europe"},
		".hu": {".hu", "Hungary", "Europe"},
		".ru": {".ru", "Russia", "Europe/Asia"},
		".ua": {".ua", "Ukraine", "Europe"},
		".jp": {".jp", "Japan", "Asia"},
		".kr": {".kr", "South Korea", "Asia"},
		".cn": {".cn", "China", "Asia"},
		".hk": {".hk", "Hong Kong", "Asia"},
		".sg": {".sg", "Singapore", "Asia"},
		".in": {".in", "India", "Asia"},
		".au": {".au", "Australia", "Oceania"},
		".nz": {".nz", "New Zealand", "Oceania"},
		".br": {".br", "Brazil", "South America"},
		".ar": {".ar", "Argentina", "South America"},
		".cl": {".cl", "Chile", "South America"},
		".mx": {".mx", "Mexico", "North America"},
		".za": {".za", "South Africa", "Africa"},
		".ie": {".ie", "Ireland", "Europe"},
		".pt": {".pt", "Portugal", "Europe"},
		".gr": {".gr", "Greece", "Europe"},
		".tr": {".tr", "Turkey", "Europe/Asia"},
		".is": {".is", "Iceland", "Europe"},
		".il": {".il", "Israel", "Middle East"},
		".eg": {".eg", "Egypt", "Africa/Middle East"},
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

	// Extract Twitter/X links
	// Look for patterns like: href="https://twitter.com/username" or href="https://x.com/username"
	twitterRegex := regexp.MustCompile(`href="(https?://(?:twitter\.com|x\.com)/[^"/?]+)"`)
	twitterMatches := twitterRegex.FindAllStringSubmatch(html, -1)
	for _, match := range twitterMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}

	return urls
}
