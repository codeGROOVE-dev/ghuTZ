// Package sleep provides detection of sleep patterns in activity data.
package sleep

import (
	"sort"
)

// DetectSleepPeriodsWithHalfHours identifies sleep periods using 30-minute resolution data.
// It requires at least 30 minutes of buffer between activity and sleep.
func DetectSleepPeriodsWithHalfHours(halfHourCounts map[float64]int) []float64 {
	// Find all quiet buckets (truly quiet - 0 activity only)
	type quietPeriod struct {
		start  float64
		end    float64
		length int // number of buckets
	}

	var quietBuckets []float64
	for bucket := 0.0; bucket < 24.0; bucket += 0.5 {
		if count, exists := halfHourCounts[bucket]; exists && count == 0 {
			quietBuckets = append(quietBuckets, bucket)
		} else if !exists {
			// No data means no activity
			quietBuckets = append(quietBuckets, bucket)
		}
	}

	if len(quietBuckets) == 0 {
		return []float64{}
	}

	// Sort buckets
	sort.Float64s(quietBuckets)

	// Find continuous quiet periods (with wraparound handling)
	var periods []quietPeriod
	currentPeriod := quietPeriod{start: quietBuckets[0], end: quietBuckets[0], length: 1}

	for i := 1; i < len(quietBuckets); i++ {
		// Check if consecutive (0.5 apart) or wraparound (23.5 to 0.0)
		switch {
		case quietBuckets[i] == currentPeriod.end+0.5:
			currentPeriod.end = quietBuckets[i]
			currentPeriod.length++
		case currentPeriod.end == 23.5 && quietBuckets[i] == 0.0:
			// Wraparound case
			currentPeriod.end = quietBuckets[i]
			currentPeriod.length++
		default:
			// End of current period
			if currentPeriod.length >= 6 { // At least 3 hours
				periods = append(periods, currentPeriod)
			}
			currentPeriod = quietPeriod{start: quietBuckets[i], end: quietBuckets[i], length: 1}
		}
	}

	// Don't forget the last period
	if currentPeriod.length >= 6 {
		periods = append(periods, currentPeriod)
	}

	// Check for wraparound merge (if first and last periods connect)
	if len(periods) >= 2 {
		first := periods[0]
		last := periods[len(periods)-1]

		// If last ends at 23.5 and first starts at 0.0, merge them
		if last.end == 23.5 && first.start == 0.0 {
			merged := quietPeriod{
				start:  last.start,
				end:    first.end,
				length: last.length + first.length,
			}
			// Replace with merged period
			newPeriods := []quietPeriod{merged}
			if len(periods) > 2 {
				newPeriods = append(newPeriods, periods[1:len(periods)-1]...)
			}
			periods = newPeriods
		}
	}

	// Apply 30-minute buffer to all periods: sleep doesn't start until 30 minutes after last activity
	// and ends 30 minutes before first activity
	var adjustedPeriods []quietPeriod
	for _, period := range periods {
		// Adjust start: keep moving forward while there's activity in the previous bucket
		for {
			bufferStart := period.start - 0.5
			if bufferStart < 0 {
				bufferStart += 24
			}

			// If there's any activity in the buffer bucket, move start forward
			count, exists := halfHourCounts[bufferStart]
			if !exists || count <= 1 {
				break
			}
			period.start += 0.5
			if period.start >= 24 {
				period.start -= 24
			}
			period.length--

			// Stop if we've adjusted too much
			if period.length < 4 {
				break
			}
		}

		// Adjust end: keep moving backward while there's activity in the next bucket
		for {
			bufferEnd := period.end + 0.5
			if bufferEnd >= 24 {
				bufferEnd -= 24
			}

			// If there's any activity in the buffer bucket, move end backward
			count, exists := halfHourCounts[bufferEnd]
			if !exists || count <= 1 {
				break
			}
			period.end -= 0.5
			if period.end < 0 {
				period.end += 24
			}
			period.length--

			// Stop if we've adjusted too much
			if period.length < 4 {
				break
			}
		}

		// Only keep periods that are still at least 2 hours after buffer adjustment
		if period.length >= 4 {
			adjustedPeriods = append(adjustedPeriods, period)
		}
	}

	// Convert all quiet periods to list of buckets
	var sleepBuckets []float64
	for _, period := range adjustedPeriods {
		if period.start <= period.end {
			// Normal case
			for b := period.start; b <= period.end; b += 0.5 {
				sleepBuckets = append(sleepBuckets, b)
			}
		} else {
			// Wraparound case
			for b := period.start; b < 24.0; b += 0.5 {
				sleepBuckets = append(sleepBuckets, b)
			}
			for b := 0.0; b <= period.end; b += 0.5 {
				sleepBuckets = append(sleepBuckets, b)
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
