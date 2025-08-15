package ghutz

import (
	"math"
	"sort"
)

// calculateTypicalActiveHours determines typical work hours based on activity patterns.
func calculateTypicalActiveHours(hourCounts map[int]int, quietHours []int, utcOffset int) (start, end int) {
	// Find the first hour with significant activity after quiet hours
	maxQuiet := -1
	for _, h := range quietHours {
		if h > maxQuiet {
			maxQuiet = h
		}
	}

	// Look for work start after quiet hours end
	workStart := -1
	for h := maxQuiet + 1; h < maxQuiet+12; h++ {
		hourUTC := h % 24
		localHour := (hourUTC + utcOffset + 24) % 24
		count := hourCounts[hourUTC]

		// Work typically starts between 6 AM and 11 AM local time
		if localHour >= 6 && localHour <= 11 && count > 0 {
			// Check if this is sustained activity (not just a blip)
			nextHour := (hourUTC + 1) % 24
			nextNextHour := (hourUTC + 2) % 24
			if hourCounts[nextHour] > 0 || hourCounts[nextNextHour] > 0 {
				workStart = hourUTC
				break
			}
		}
	}

	// If no clear work start found, look for any morning activity
	if workStart == -1 {
		for h := 0; h < 24; h++ {
			localHour := (h + utcOffset + 24) % 24
			if localHour >= 6 && localHour <= 11 && hourCounts[h] > 0 {
				workStart = h
				break
			}
		}
	}

	// Default to 9 AM local if still not found
	if workStart == -1 {
		workStart = (9 - utcOffset + 24) % 24
	}

	// Find work end by looking for activity drop in evening
	workEnd := -1
	for h := workStart + 6; h < workStart+14; h++ {
		hourUTC := h % 24
		localHour := (hourUTC + utcOffset + 24) % 24
		count := hourCounts[hourUTC]
		nextHour := (hourUTC + 1) % 24
		nextCount := hourCounts[nextHour]

		// Look for significant drop in activity in evening hours (4 PM - 8 PM local)
		if localHour >= 16 && localHour <= 20 {
			if count > 0 && nextCount < count/2 {
				workEnd = hourUTC
				break
			}
		}
	}

	// Default to 6 PM local if not found
	if workEnd == -1 {
		workEnd = (18 - utcOffset + 24) % 24
	}

	// Ensure work hours are reasonable (8-12 hours)
	duration := (workEnd - workStart + 24) % 24
	if duration < 8 {
		workEnd = (workStart + 9) % 24
	} else if duration > 12 {
		workEnd = (workStart + 10) % 24
	}

	return workStart, workEnd
}

// findSleepHours identifies likely sleep hours based on activity patterns.
func findSleepHours(hourCounts map[int]int) []int {
	// Find continuous blocks of low/no activity
	type block struct {
		start  int
		length int
		total  int
	}

	var blocks []block
	inBlock := false
	currentBlock := block{}

	// Scan for quiet blocks, wrapping around midnight
	for i := 0; i < 48; i++ { // Check 48 hours to handle wraparound
		h := i % 24
		count := hourCounts[h]

		if count <= 2 { // Very low activity threshold
			if !inBlock {
				inBlock = true
				currentBlock = block{start: h, length: 1, total: count}
			} else {
				currentBlock.length++
				currentBlock.total += count
			}
		} else {
			if inBlock && currentBlock.length >= 3 { // Minimum 3 hours for sleep
				blocks = append(blocks, currentBlock)
			}
			inBlock = false
		}
	}

	// Handle wraparound case
	if inBlock && currentBlock.length >= 3 {
		// Check if this connects with the first block
		if len(blocks) > 0 && blocks[0].start < 6 {
			// Merge with first block
			firstBlock := blocks[0]
			lastBlock := currentBlock

			// Adjust for wraparound
			mergedStart := lastBlock.start
			mergedLength := lastBlock.length + firstBlock.length
			mergedTotal := lastBlock.total + firstBlock.total

			// Replace first block with merged block
			blocks[0] = block{
				start:  mergedStart,
				length: mergedLength,
				total:  mergedTotal,
			}
		} else {
			blocks = append(blocks, currentBlock)
		}
	}

	// Find the longest quiet block (most likely sleep)
	var sleepBlock block
	maxLength := 0
	for _, b := range blocks {
		// Prefer blocks that span typical sleep hours (10 PM - 8 AM UTC)
		// But adjust for the fact that we don't know timezone yet
		score := b.length
		if b.length >= 5 && b.length <= 10 { // Reasonable sleep duration
			score += 2
		}
		if score > maxLength {
			maxLength = score
			sleepBlock = b
		}
	}

	// Return hours in the sleep block
	var sleepHours []int
	if sleepBlock.length > 0 {
		for i := 0; i < sleepBlock.length && i < 24; i++ {
			sleepHours = append(sleepHours, (sleepBlock.start+i)%24)
		}
	}

	// If we found very long quiet period (>10 hours), trim it to reasonable sleep hours
	if len(sleepHours) > 10 {
		// Keep the middle portion as most likely sleep time
		start := len(sleepHours)/2 - 4
		if start < 0 {
			start = 0
		}
		sleepHours = sleepHours[start : start+8]
	}

	return sleepHours
}

// findQuietHours identifies hours with minimal activity.
func findQuietHours(hourCounts map[int]int) []int {
	// Calculate average activity
	total := 0
	nonZeroHours := 0
	for _, count := range hourCounts {
		if count > 0 {
			total += count
			nonZeroHours++
		}
	}

	if nonZeroHours == 0 {
		return []int{} // No activity at all
	}

	avgActivity := float64(total) / float64(nonZeroHours)
	quietThreshold := avgActivity * 0.2 // Hours with less than 20% of average activity

	var quietHours []int
	for hour, count := range hourCounts {
		if float64(count) <= quietThreshold {
			quietHours = append(quietHours, hour)
		}
	}

	// Sort hours for consistency
	sort.Ints(quietHours)
	return quietHours
}

// detectLunchBreak identifies potential lunch break patterns in work hours.
func detectLunchBreak(hourCounts map[int]int, utcOffset int, workStart, workEnd int) (lunchStart, lunchEnd, confidence float64) {
	// If work hours are too short (less than 3 hours), we can't reliably detect lunch
	workDuration := (workEnd - workStart + 24) % 24
	if workDuration < 3 {
		return -1, -1, 0
	}

	// Convert work hours to local time for analysis
	// workStartLocal and workEndLocal not currently used but kept for clarity
	// workStartLocal := (workStart + utcOffset + 24) % 24
	// workEndLocal := (workEnd + utcOffset + 24) % 24

	// Typical lunch hours are between 11 AM and 2 PM local time
	lunchWindowStart := 11
	lunchWindowEnd := 14

	// Find potential lunch break (activity dip during work hours)
	minActivity := math.MaxInt32
	minHour := -1

	// Build work hour buckets
	var workHourBuckets []int
	for h := workStart; ; h = (h + 1) % 24 {
		workHourBuckets = append(workHourBuckets, h)
		if h == workEnd {
			break
		}
		if len(workHourBuckets) > 24 { // Safety check
			break
		}
	}

	// Calculate average activity during work hours
	totalWorkActivity := 0
	workHourCount := 0
	for _, h := range workHourBuckets {
		if hourCounts[h] > 0 {
			totalWorkActivity += hourCounts[h]
			workHourCount++
		}
	}

	if workHourCount == 0 {
		return -1, -1, 0
	}

	avgWorkActivity := float64(totalWorkActivity) / float64(workHourCount)

	// Look for lunch break pattern
	for _, utcHour := range workHourBuckets {
		localHour := (utcHour + utcOffset + 24) % 24
		activity := hourCounts[utcHour]

		// Check if this hour falls within typical lunch window
		if localHour >= lunchWindowStart && localHour <= lunchWindowEnd {
			if activity < minActivity {
				minActivity = activity
				minHour = utcHour
			}
		}
	}

	// If we found a potential lunch hour
	if minHour != -1 {
		// Look for the extent of the lunch break
		lunchStart = float64(minHour)
		lunchEnd = float64(minHour)

		// Check previous hour
		prevHour := (minHour - 1 + 24) % 24
		prevLocal := (prevHour + utcOffset + 24) % 24
		if prevLocal >= lunchWindowStart-1 && hourCounts[prevHour] < int(avgWorkActivity*0.5) {
			lunchStart = float64(prevHour)
		}

		// Check next hour
		nextHour := (minHour + 1) % 24
		nextLocal := (nextHour + utcOffset + 24) % 24
		if nextLocal <= lunchWindowEnd+1 && hourCounts[nextHour] < int(avgWorkActivity*0.5) {
			lunchEnd = float64(nextHour)
		}

		// Calculate confidence based on how pronounced the dip is
		if minActivity == 0 && avgWorkActivity > 5 {
			confidence = 0.8 // Strong signal
		} else if float64(minActivity) < avgWorkActivity*0.3 {
			confidence = 0.6 // Moderate signal
		} else if float64(minActivity) < avgWorkActivity*0.5 {
			confidence = 0.4 // Weak signal
		} else {
			confidence = 0.2 // Very weak signal
		}

		// Boost confidence if lunch is at typical time (12-1 PM local)
		lunchStartLocal := (int(lunchStart) + utcOffset + 24) % 24
		if lunchStartLocal == 12 {
			confidence = math.Min(1.0, confidence*1.3)
		}

		return lunchStart, lunchEnd, confidence
	}

	// No clear lunch break found
	// As a fallback, if work hours span noon, suggest noon as potential lunch
	for _, utcHour := range workHourBuckets {
		localHour := (utcHour + utcOffset + 24) % 24
		if localHour == 12 {
			// Check if there's any activity reduction around noon
			activity := hourCounts[utcHour]
			if float64(activity) < avgWorkActivity*0.7 {
				return float64(utcHour), float64(utcHour), 0.3 // Low confidence fallback
			}
		}
	}

	return -1, -1, 0
}

// detectPeakProductivity identifies hours of highest activity.
func detectPeakProductivity(hourCounts map[int]int, utcOffset int) (start, end float64, count int) {
	type hourActivity struct {
		hour  int
		count int
	}

	var activities []hourActivity
	for h, c := range hourCounts {
		if c > 0 {
			activities = append(activities, hourActivity{hour: h, count: c})
		}
	}

	if len(activities) == 0 {
		return -1, -1, 0
	}

	// Sort by activity count
	sort.Slice(activities, func(i, j int) bool {
		return activities[i].count > activities[j].count
	})

	// Take top 25% most active hours
	topCount := len(activities) / 4
	if topCount < 1 {
		topCount = 1
	}

	topHours := make([]int, 0, topCount)
	totalActivity := 0
	for i := 0; i < topCount && i < len(activities); i++ {
		topHours = append(topHours, activities[i].hour)
		totalActivity += activities[i].count
	}

	// Find the continuous block within top hours
	sort.Ints(topHours)

	// Look for the longest continuous sequence
	maxStart := topHours[0]
	maxEnd := topHours[0]
	currentStart := topHours[0]
	currentEnd := topHours[0]

	for i := 1; i < len(topHours); i++ {
		if topHours[i] == currentEnd+1 || (currentEnd == 23 && topHours[i] == 0) {
			currentEnd = topHours[i]
		} else {
			// Check if current sequence is longer
			currentLen := (currentEnd - currentStart + 24) % 24
			maxLen := (maxEnd - maxStart + 24) % 24
			if currentLen > maxLen {
				maxStart = currentStart
				maxEnd = currentEnd
			}
			currentStart = topHours[i]
			currentEnd = topHours[i]
		}
	}

	// Final check
	currentLen := (currentEnd - currentStart + 24) % 24
	maxLen := (maxEnd - maxStart + 24) % 24
	if currentLen > maxLen {
		maxStart = currentStart
		maxEnd = currentEnd
	}

	// Convert to local time
	startLocal := float64((maxStart + utcOffset + 24) % 24)
	endLocal := float64((maxEnd + utcOffset + 24) % 24)

	return startLocal, endLocal, totalActivity
}