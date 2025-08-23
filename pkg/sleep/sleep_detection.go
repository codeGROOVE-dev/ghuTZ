// Package sleep provides detection of sleep patterns in activity data.
package sleep

import (
	"sort"
)

// quietPeriod represents a continuous period of low activity.
type quietPeriod struct {
	start  float64
	end    float64
	length int // number of buckets
}

// DetectSleepPeriodsWithHalfHours identifies sleep periods using 30-minute resolution data.
// A rest period is defined as:
// - 4+ hours of continuous buckets with 0-2 events
// - Ends when we hit two consecutive buckets with 3+ events
// - Maximum 12 hours per rest period.
// - Strongly prefers nighttime periods (starting search at 9pm local time).
func DetectSleepPeriodsWithHalfHours(halfHourCounts map[float64]int) []float64 {
	// Find all potential rest periods
	type restPeriod struct {
		buckets []float64
		start   float64
		length  int
		score   float64 // preference score (higher is better)
	}
	var allPeriods []restPeriod

	// Start search at 21:00 (9pm) and scan through all 48 half-hour buckets
	// This gives preference to nighttime sleep periods
	searchOrder := make([]float64, 0, 48)
	// First add 21:00 to 09:00 (nighttime hours)
	for h := 21.0; h < 24.0; h += 0.5 {
		searchOrder = append(searchOrder, h)
	}
	for h := 0.0; h < 9.0; h += 0.5 {
		searchOrder = append(searchOrder, h)
	}
	// Then add daytime hours (9:00 to 21:00) - less preferred
	for h := 9.0; h < 21.0; h += 0.5 {
		searchOrder = append(searchOrder, h)
	}

	// Scan through buckets in our preferred order
	for _, startBucket := range searchOrder {
		// Skip if this bucket is active
		if halfHourCounts[startBucket] > 2 {
			continue
		}

		var currentPeriod []float64
		bucket := startBucket

		// Build a continuous quiet period starting from this bucket
		previousCount := -1
		for len(currentPeriod) < 24 { // Max 12 hours (24 half-hour buckets)
			count := halfHourCounts[bucket]

			// Never include high-activity buckets (>5 events) in sleep
			// These are clearly active periods, not sleep
			if count > 5 {
				// If we have a decent sleep period already, stop here
				if len(currentPeriod) >= 8 {
					break
				}
				// Otherwise reset - this breaks the quiet period
				currentPeriod = []float64{}
				previousCount = -1
			} else {
				// Halting conditions for end of sleep:
				// 1. Two consecutive active buckets (2+ followed by 3+)
				// 2. A burst of activity (3+ followed by 2+) indicating wake-up
				if (previousCount >= 2 && count >= 3) || (previousCount >= 3 && count >= 2) {
					// Don't include the previous bucket if it's too active
					if len(currentPeriod) > 0 && previousCount >= 2 {
						currentPeriod = currentPeriod[:len(currentPeriod)-1]
					}
					break
				}

				// Include this bucket in the period (it has ≤5 events)
				currentPeriod = append(currentPeriod, bucket)
				previousCount = count
			}

			// Move to next bucket (with wraparound)
			bucket += 0.5
			if bucket >= 24.0 {
				bucket -= 24.0
			}

			// Stop if we've circled back to where we started
			if bucket == startBucket {
				break
			}
		}

		// If we found a rest period of 4+ hours (8+ buckets), save it
		if len(currentPeriod) >= 8 {
			// Calculate preference score based on how "nighttime" this period is
			score := calculateNighttimeScore(currentPeriod)
			allPeriods = append(allPeriods, restPeriod{
				buckets: currentPeriod,
				start:   startBucket,
				length:  len(currentPeriod),
				score:   score,
			})
		}
	}

	// Sort periods by preference: nighttime score first, then length
	sort.Slice(allPeriods, func(i, j int) bool {
		// Strongly prefer nighttime periods
		if allPeriods[i].score != allPeriods[j].score {
			return allPeriods[i].score > allPeriods[j].score
		}
		// For same nighttime score, prefer longer periods
		return allPeriods[i].length > allPeriods[j].length
	})

	// Take the best period (nighttime preferred, longest)
	var finalBuckets []float64
	if len(allPeriods) > 0 {
		bestPeriod := allPeriods[0]
		finalBuckets = bestPeriod.buckets
	}

	// Trim any buckets from the end that have 3+ activities
	// Sleep shouldn't end with active buckets
	for len(finalBuckets) > 0 {
		lastBucket := finalBuckets[len(finalBuckets)-1]
		if halfHourCounts[lastBucket] >= 3 {
			// Remove this bucket from sleep period
			finalBuckets = finalBuckets[:len(finalBuckets)-1]
		} else {
			// Stop trimming once we hit a quiet bucket
			break
		}
	}

	// Sort the final buckets for consistent output
	sort.Float64s(finalBuckets)

	// Trim to find the first continuous quiet sequence
	// Sleep should start with at least 2 consecutive quiet buckets (0-2 activities)
	for len(finalBuckets) > 1 {
		firstBucket := finalBuckets[0]
		secondBucket := finalBuckets[1]

		// Check if we have a good sleep start (two consecutive quiet buckets)
		if halfHourCounts[firstBucket] <= 2 && halfHourCounts[secondBucket] <= 2 {
			// Good start for sleep
			break
		}

		// Otherwise, trim the first bucket and keep looking
		finalBuckets = finalBuckets[1:]
	}

	// Group consecutive buckets to identify separate quiet periods
	if len(finalBuckets) == 0 {
		return finalBuckets
	}

	var periods [][]float64
	currentPeriod := []float64{finalBuckets[0]}

	for i := 1; i < len(finalBuckets); i++ {
		// Check if this bucket is consecutive with the previous one
		if finalBuckets[i]-finalBuckets[i-1] <= 0.5 {
			currentPeriod = append(currentPeriod, finalBuckets[i])
		} else {
			// Gap found, save current period and start new one
			if len(currentPeriod) >= 7 { // Only keep periods of 3.5+ hours
				periods = append(periods, currentPeriod)
			}
			currentPeriod = []float64{finalBuckets[i]}
		}
	}

	// Don't forget the last period
	if len(currentPeriod) >= 7 { // Only keep periods of 3.5+ hours
		periods = append(periods, currentPeriod)
	}

	// Find the longest period (most likely to be actual sleep)
	var longestPeriod []float64
	for _, period := range periods {
		if len(period) > len(longestPeriod) {
			longestPeriod = period
		}
	}

	return longestPeriod
}

// calculateNighttimeScore returns a score indicating how much a period overlaps with typical nighttime hours.
// Nighttime is considered 21:00-09:00. Higher scores mean more nighttime overlap.
func calculateNighttimeScore(buckets []float64) float64 {
	nighttimeCount := 0
	for _, bucket := range buckets {
		// Hours 21:00-23:30 and 00:00-08:30 are considered nighttime
		if bucket >= 21.0 || bucket < 9.0 {
			nighttimeCount++
		}
	}
	// Return percentage of buckets that are during nighttime
	return float64(nighttimeCount) / float64(len(buckets))
}

// findQuietBuckets identifies all 30-minute slots with minimal activity.
func findQuietBuckets(halfHourCounts map[float64]int) []float64 {
	var quietBuckets []float64
	for bucket := 0.0; bucket < 24.0; bucket += 0.5 {
		count, exists := halfHourCounts[bucket]
		// Include buckets with 0-2 events as "quiet"
		// 2 events in 30 minutes is still very minimal activity
		// For late evening/early morning (22:00-02:00 UTC), be even more lenient
		threshold := 2
		if bucket >= 22.0 || bucket <= 2.0 {
			threshold = 3 // Allow up to 3 events in potential sleep transition times
		}
		if !exists || count <= threshold {
			quietBuckets = append(quietBuckets, bucket)
		}
	}
	sort.Float64s(quietBuckets)

	// Filter out isolated quiet buckets that are likely light activity rather than sleep
	// This helps avoid including evening/morning activity buckets that just happen to be quiet
	filteredBuckets := filterIsolatedQuietBuckets(quietBuckets, halfHourCounts)

	return filteredBuckets
}

// filterIsolatedQuietBuckets removes isolated quiet buckets that are likely light activity rather than sleep.
func filterIsolatedQuietBuckets(quietBuckets []float64, halfHourCounts map[float64]int) []float64 {
	if len(quietBuckets) == 0 {
		return quietBuckets
	}

	var filtered []float64

	for i, bucket := range quietBuckets {
		count := halfHourCounts[bucket]

		// Always include buckets with 0 events - they're truly quiet
		if count == 0 {
			filtered = append(filtered, bucket)
			continue
		}

		// For buckets with 1-2 events, only include if part of a longer quiet period
		// This filters out isolated light activity while keeping sustained quiet periods
		consecutiveQuietCount := countConsecutiveQuietBuckets(bucket, i, quietBuckets)

		// Include if part of a group of 3+ consecutive quiet buckets (1.5+ hours)
		if consecutiveQuietCount >= 3 {
			filtered = append(filtered, bucket)
		}
	}

	return filtered
}

// countConsecutiveQuietBuckets counts how many consecutive quiet buckets surround the given bucket.
func countConsecutiveQuietBuckets(bucket float64, index int, quietBuckets []float64) int {
	count := 1

	// Count consecutive quiet buckets before this one
	for j := index - 1; j >= 0; j-- {
		if quietBuckets[j] == bucket-float64(index-j)*0.5 {
			count++
		} else {
			break
		}
	}

	// Count consecutive quiet buckets after this one
	for j := index + 1; j < len(quietBuckets); j++ {
		if quietBuckets[j] == bucket+float64(j-index)*0.5 {
			count++
		} else {
			break
		}
	}

	return count
}

// isPartOfLongQuietPeriod checks if a bucket is part of an extended quiet period with mostly 0-event buckets.
func isPartOfLongQuietPeriod(bucket float64, index int, quietBuckets []float64, halfHourCounts map[float64]int, minLength int) bool {
	consecutiveCount := countConsecutiveQuietBuckets(bucket, index, quietBuckets)

	if consecutiveCount < minLength {
		return false
	}

	// Check that most of the surrounding quiet buckets have 0 events (true quiet)
	zeroEventCount := 0
	start := index - (consecutiveCount-1)/2
	if start < 0 {
		start = 0
	}
	end := start + consecutiveCount
	if end > len(quietBuckets) {
		end = len(quietBuckets)
	}

	for i := start; i < end; i++ {
		if halfHourCounts[quietBuckets[i]] == 0 {
			zeroEventCount++
		}
	}

	// At least 70% of the period should have 0 events for it to be considered true sleep
	return float64(zeroEventCount)/float64(consecutiveCount) >= 0.7
}

// findContinuousPeriodsWithBridging groups quiet buckets into periods, bridging minor gaps.
func findContinuousPeriodsWithBridging(quietBuckets []float64, halfHourCounts map[float64]int) []quietPeriod {
	if len(quietBuckets) == 0 {
		return nil
	}

	var periods []quietPeriod
	currentPeriod := quietPeriod{start: quietBuckets[0], end: quietBuckets[0], length: 1}

	for i := 1; i < len(quietBuckets); i++ {
		bucket := quietBuckets[i]
		currentPeriod = processBucket(currentPeriod, bucket, halfHourCounts, &periods)
	}

	// Add the last period (even if short, will be filtered later)
	if currentPeriod.length >= 2 { // At least 1 hour
		periods = append(periods, currentPeriod)
	}

	return mergeWraparoundPeriods(periods)
}

// isConsecutiveBucket checks if two buckets are consecutive, including wraparound.
func isConsecutiveBucket(current, next float64) bool {
	return next == current+0.5 || (current == 23.5 && next == 0.0)
}

// processBucket processes a single bucket and determines whether to extend current period or start new one.
func processBucket(currentPeriod quietPeriod, bucket float64, halfHourCounts map[float64]int, periods *[]quietPeriod) quietPeriod {
	if isConsecutiveBucket(currentPeriod.end, bucket) {
		// Directly consecutive
		currentPeriod.end = bucket
		currentPeriod.length++
		return currentPeriod
	}

	// Try to bridge gap
	if canBridgeGap := tryBridgeGap(currentPeriod, bucket, halfHourCounts); canBridgeGap.shouldBridge {
		currentPeriod.end = bucket
		currentPeriod.length += canBridgeGap.gapSize + 1
		return currentPeriod
	}

	// Save current period if long enough and start new one
	return startNewPeriod(currentPeriod, bucket, periods)
}

type bridgeResult struct {
	shouldBridge bool
	gapSize      int
}

// tryBridgeGap determines if a gap between periods can be bridged.
func tryBridgeGap(currentPeriod quietPeriod, bucket float64, halfHourCounts map[float64]int) bridgeResult {
	gapStart := currentPeriod.end + 0.5
	if gapStart >= 24.0 {
		gapStart -= 24.0
	}

	// Calculate actual gap size considering wraparound
	var actualGap float64
	if bucket > currentPeriod.end {
		actualGap = bucket - currentPeriod.end - 0.5
	} else {
		// Wraparound case
		actualGap = (24.0 - currentPeriod.end) + bucket - 0.5
	}

	// Allow bridging gaps up to 2 hours (4 buckets) if activity is minimal
	if actualGap <= 0 || actualGap > 2.0 {
		return bridgeResult{shouldBridge: false}
	}

	// Check all buckets in the gap
	gapSize := 0
	totalGapActivity := 0
	for gb := gapStart; float64(gapSize)*0.5 < actualGap; gb += 0.5 {
		if gb >= 24.0 {
			gb -= 24.0
		}
		count := halfHourCounts[gb]
		totalGapActivity += count
		// Don't bridge if any single bucket has heavy activity (>5 events)
		if count > 5 {
			return bridgeResult{shouldBridge: false}
		}
		gapSize++
	}

	// Bridge if:
	// 1. We have at least 2 hours of quiet already (currentPeriod.length >= 4)
	// 2. The gap has minimal total activity (avg < 3 events per bucket)
	avgActivityPerBucket := float64(totalGapActivity) / float64(gapSize)
	shouldBridge := currentPeriod.length >= 4 && avgActivityPerBucket < 3.0
	return bridgeResult{shouldBridge: shouldBridge, gapSize: gapSize}
}

// startNewPeriod saves current period if long enough and starts a new period.
func startNewPeriod(currentPeriod quietPeriod, bucket float64, periods *[]quietPeriod) quietPeriod {
	// Save current period if long enough
	if currentPeriod.length >= 6 { // At least 3 hours
		*periods = append(*periods, currentPeriod)
	}
	// Start new period
	return quietPeriod{start: bucket, end: bucket, length: 1}
}

// mergeWraparoundPeriods merges periods that connect across midnight.
func mergeWraparoundPeriods(periods []quietPeriod) []quietPeriod {
	if len(periods) < 2 {
		return periods
	}

	first := periods[0]
	last := periods[len(periods)-1]

	// Check if periods wrap around midnight
	// They should be close (within 1 hour) across the boundary
	wrapsAround := false
	if last.end >= 23.0 && first.start <= 1.0 {
		// Check the gap size
		gap := (24.0 - last.end) + first.start
		if gap <= 1.0 { // Within 30 minutes on each side of midnight
			wrapsAround = true
		}
	}

	if wrapsAround {
		merged := quietPeriod{
			start:  last.start,
			end:    first.end,
			length: last.length + first.length + int(((24.0-last.end)+first.start)/0.5),
		}
		// Replace with merged period
		newPeriods := []quietPeriod{merged}
		if len(periods) > 2 {
			newPeriods = append(newPeriods, periods[1:len(periods)-1]...)
		}
		return newPeriods
	}

	return periods
}

// convertPeriodsToSleepBuckets converts quiet periods to a list of sleep buckets.
func convertPeriodsToSleepBuckets(periods []quietPeriod) []float64 {
	var sleepBuckets []float64
	for _, period := range periods {
		// Cap at 9 hours (18 half-hour buckets)
		maxBuckets := 18
		bucketCount := period.length

		// If period is too long, trim from the start (keep the morning end)
		// People are more likely to check GitHub first thing in the morning
		adjustedStart := period.start
		if bucketCount > maxBuckets {
			// Move the start forward to limit to 9 hours
			excessBuckets := bucketCount - maxBuckets
			adjustedStart = period.start + float64(excessBuckets)*0.5
			if adjustedStart >= 24.0 {
				adjustedStart -= 24.0
			}
		}

		if adjustedStart <= period.end {
			// Normal case
			count := 0
			for b := adjustedStart; b <= period.end && count < maxBuckets; b += 0.5 {
				sleepBuckets = append(sleepBuckets, b)
				count++
			}
		} else {
			// Wraparound case
			count := 0
			for b := adjustedStart; b < 24.0 && count < maxBuckets; b += 0.5 {
				sleepBuckets = append(sleepBuckets, b)
				count++
			}
			for b := 0.0; b <= period.end && count < maxBuckets; b += 0.5 {
				sleepBuckets = append(sleepBuckets, b)
				count++
			}
		}
	}
	return sleepBuckets
}

// FindSleepHours identifies sleep hours from hourly activity data.
func FindSleepHours(hourCounts map[int]int) []int {
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
	for hour := range 24 {
		if float64(hourCounts[hour]) <= threshold {
			quietHours = append(quietHours, hour)
		}
	}

	// If we found 4-12 quiet hours (sleep time), use them
	// User specified 4-9 hours of sleep is most likely
	if len(quietHours) >= 4 && len(quietHours) <= 12 {
		// Check if hours wrap around midnight by looking for a gap
		// If there's a gap larger than 1 hour, it indicates wrap-around
		hasWrapAround := false
		wrapPoint := -1
		for i := 1; i < len(quietHours); i++ {
			if quietHours[i] != quietHours[i-1]+1 {
				// Found a gap - this might be wrap-around
				// e.g., [2,3,4,5,6,7,8,9,10,22] has gap between 10 and 22
				hasWrapAround = true
				wrapPoint = i
				break
			}
		}

		if hasWrapAround && wrapPoint > 0 {
			// Return hours in their natural order, not wrapped
			// The wrap-around hours (e.g., 22,23) should stay at the end
			// e.g., [2,3,4,5,6,7,8,9,22,23] stays as is
			// The caller should handle the wraparound properly
			return quietHours
		}

		return quietHours
	}

	// If we found more than 12 quiet hours, cap at 12
	if len(quietHours) > 12 {
		// Find the quietest consecutive 12-hour period
		bestSum := totalActivity
		bestStart := 0
		for start := range quietHours {
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
		for start := range 24 {
			sum := 0
			for i := range windowSize {
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
	for i := range bestWindowSize {
		sleepHours = append(sleepHours, (bestStart+i)%24)
	}

	// Check if we should extend the quiet period further
	// If the average activity during quiet hours is very low, we found good sleep hours
	// Variables removed as they were unused - validation happens through the window selection above

	return sleepHours
}

// FindQuietHours identifies hours with minimal activity.
func FindQuietHours(hourCounts map[int]int) []int {
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
