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
// It requires at least 30 minutes of buffer between activity and sleep.
func DetectSleepPeriodsWithHalfHours(halfHourCounts map[float64]int) []float64 {
	// First pass: find truly quiet buckets (0-1 events)
	quietBuckets := findQuietBuckets(halfHourCounts)
	if len(quietBuckets) == 0 {
		return []float64{}
	}

	// Second pass: find continuous periods, allowing bridging of small gaps
	periods := findContinuousPeriodsWithBridging(quietBuckets, halfHourCounts)

	// Third pass: adjust boundaries to avoid activity
	adjustedPeriods := applyActivityBuffers(periods, halfHourCounts)

	return convertPeriodsToSleepBuckets(adjustedPeriods)
}

// findQuietBuckets identifies all 30-minute slots with minimal activity.
func findQuietBuckets(halfHourCounts map[float64]int) []float64 {
	var quietBuckets []float64
	for bucket := 0.0; bucket < 24.0; bucket += 0.5 {
		count, exists := halfHourCounts[bucket]
		// Include buckets with 0-1 events as "quiet"
		// We'll handle minor blips (2 events) separately in period detection
		if !exists || count <= 1 {
			quietBuckets = append(quietBuckets, bucket)
		}
	}
	sort.Float64s(quietBuckets)
	return quietBuckets
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

	// Only try to bridge if gap is reasonable (up to 1 hour = 2 buckets)
	if actualGap <= 0 || actualGap > 1.0 {
		return bridgeResult{shouldBridge: false}
	}

	// Check all buckets in the gap
	gapSize := 0
	for gb := gapStart; float64(gapSize)*0.5 < actualGap; gb += 0.5 {
		if gb >= 24.0 {
			gb -= 24.0
		}
		count := halfHourCounts[gb]
		// Allow bridging if gap has ≤2 events per bucket
		if count > 2 {
			return bridgeResult{shouldBridge: false}
		}
		gapSize++
	}

	// Bridge if conditions are met and we have substantial sleep already
	shouldBridge := currentPeriod.length >= 4
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

// applyActivityBuffers adjusts period boundaries to avoid nearby activity.
func applyActivityBuffers(periods []quietPeriod, halfHourCounts map[float64]int) []quietPeriod {
	var adjustedPeriods []quietPeriod
	for _, period := range periods {
		adjusted := adjustPeriodStart(period, halfHourCounts)
		adjusted = adjustPeriodEnd(adjusted, halfHourCounts)

		// Only keep periods that are still at least 2 hours after buffer adjustment
		if adjusted.length >= 4 {
			adjustedPeriods = append(adjustedPeriods, adjusted)
		}
	}
	return adjustedPeriods
}

// adjustPeriodStart moves start forward if there's activity in preceding buckets.
func adjustPeriodStart(period quietPeriod, halfHourCounts map[float64]int) quietPeriod {
	for period.length >= 4 {
		bufferStart := period.start - 0.5
		if bufferStart < 0 {
			bufferStart += 24
		}

		count, exists := halfHourCounts[bufferStart]
		// Increased threshold from 1 to 2 to tolerate minor blips
		if !exists || count <= 2 {
			break
		}

		period.start += 0.5
		if period.start >= 24 {
			period.start -= 24
		}
		period.length--
	}
	return period
}

// adjustPeriodEnd moves end backward if there's activity in following buckets.
func adjustPeriodEnd(period quietPeriod, halfHourCounts map[float64]int) quietPeriod {
	// Trim back if we're approaching sustained activity
	// Look for the start of the "wake up" period where activity doesn't drop back to 0

	for period.length >= 4 {
		nextBucket := period.end + 0.5
		if nextBucket >= 24 {
			nextBucket -= 24
		}
		nextNextBucket := nextBucket + 0.5
		if nextNextBucket >= 24 {
			nextNextBucket -= 24
		}

		nextCount := halfHourCounts[nextBucket]
		nextNextCount := halfHourCounts[nextNextBucket]

		// If we see sustained activity (both buckets have events), trim back
		// This catches the pattern at 7:00 (2 events) followed by 7:30 (1 event)
		// which leads into the day's activity
		if nextCount <= 0 || nextNextCount <= 0 {
			break
		}
		period.end -= 0.5
		if period.end < 0 {
			period.end += 24
		}
		period.length--
	}

	return period
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
			// Reorder to maintain wrap-around: move the end hours to the beginning
			// e.g., [2,3,4,5,6,7,8,9,10,22] -> [22,2,3,4,5,6,7,8,9,10]
			quietHours = append(quietHours[wrapPoint:], quietHours[:wrapPoint]...)
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
