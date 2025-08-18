package ghutz

import (
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
	// Calculate total activity to determine thresholds
	totalActivity := 0
	maxActivity := 0
	for _, count := range hourCounts {
		totalActivity += count
		if count > maxActivity {
			maxActivity = count
		}
	}
	
	// If very little data, use default sleep hours
	if totalActivity < 50 {
		return []int{2, 3, 4, 5, 6} // Default UTC sleep hours
	}
	
	// First, find all hours with very low activity (less than 10% of max hour)
	threshold := float64(maxActivity) * 0.1
	if threshold < 2 {
		threshold = 2 // At least 2 events to not be considered quiet
	}
	
	var quietHours []int
	for hour := 0; hour < 24; hour++ {
		if float64(hourCounts[hour]) <= threshold {
			quietHours = append(quietHours, hour)
		}
	}
	
	// If we found 4-12 quiet hours (sleep time), use them
	// User specified 4-9 hours of sleep is most likely
	if len(quietHours) >= 4 && len(quietHours) <= 12 {
		return quietHours
	}
	
	// If we found more than 12 quiet hours, cap at 12
	if len(quietHours) > 12 {
		// Find the quietest consecutive 12-hour period
		bestSum := totalActivity
		bestStart := 0
		for start := 0; start < len(quietHours); start++ {
			sum := 0
			count := 0
			for i := start; i < start+12 && i < len(quietHours); i++ {
				sum += hourCounts[quietHours[i]]
				count++
			}
			if count == 12 && sum < bestSum {
				bestSum = sum
				bestStart = start
			}
		}
		return quietHours[bestStart:min(bestStart+12, len(quietHours))]
	}
	
	// Otherwise, find the quietest consecutive period using a sliding window
	// Try different window sizes from 4 to 12 hours (sleep time)
	bestWindowSize := 4
	bestSum := totalActivity
	bestStart := 0
	
	for windowSize := 4; windowSize <= 12; windowSize++ {
		for start := 0; start < 24; start++ {
			sum := 0
			for i := 0; i < windowSize; i++ {
				hour := (start + i) % 24
				sum += hourCounts[hour]
			}
			
			// Prefer longer windows if the activity per hour is similar
			avgPerHour := float64(sum) / float64(windowSize)
			bestAvgPerHour := float64(bestSum) / float64(bestWindowSize)
			
			// Accept longer window if avg activity per hour is within 20% of best
			if sum < bestSum || (avgPerHour <= bestAvgPerHour*1.2 && windowSize > bestWindowSize) {
				bestSum = sum
				bestStart = start
				bestWindowSize = windowSize
			}
		}
	}
	
	// Build the sleep hours array
	var sleepHours []int
	for i := 0; i < bestWindowSize; i++ {
		sleepHours = append(sleepHours, (bestStart+i)%24)
	}
	
	// Check if we should extend the quiet period further
	// If the average activity during quiet hours is very low, we found good sleep hours
	// Variables removed as they were unused - validation happens through the window selection above
	
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
