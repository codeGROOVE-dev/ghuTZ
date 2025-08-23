package gutz

const (
	// minActivityThreshold is the minimum number of events per half-hour bucket to be considered "active".
	minActivityThreshold = 3
	// maxGapHalfHours is the maximum number of consecutive half-hour gaps allowed within an active period (90 minutes).
	maxGapHalfHours = 3
	// minActiveBuckets is the minimum number of active buckets required for a valid active period (2+ hours).
	minActiveBuckets = 4
)

// normalizeHour wraps hour values to the 0-24 range.
func normalizeHour(hour float64) float64 {
	for hour >= 24.0 {
		hour -= 24.0
	}
	for hour < 0.0 {
		hour += 24.0
	}
	return hour
}

// calculateTypicalActiveHoursUTC determines active hours based on blocks of sustained activity.
// Uses half-hourly data for precision: finds periods with 3+ contributions per half-hour bucket.
// Rule: Active hours are any block of sustained activity with 3+ contributions per bucket,
// with up to 90-minute (3 half-hour) gaps allowed within the block.
//

func calculateTypicalActiveHoursUTC(halfHourlyActivityUTC map[float64]int, _ []int) (startUTC, endUTC float64) {
	// Note: second parameter kept for backward compatibility but not used
	// Active hours are determined purely by activity patterns, not by quiet hours

	if len(halfHourlyActivityUTC) == 0 {
		return 14.0, 22.0 // Default UTC work hours
	}

	// Step 1: Find the longest active period using half-hourly data
	// - Active periods start with buckets having 3+ contributions
	// - Allow gaps up to 3 half-hour buckets (90 minutes) with <3 contributions
	// - A gap of 4+ half-hour buckets creates a separate active period

	bestStartBucket := -1.0
	bestEndBucket := -1.0
	bestDuration := 0
	bestActivity := 0

	// Try each half-hour bucket as a potential start of an active period
	// We iterate through all 48 half-hour buckets (0.0, 0.5, 1.0, ..., 23.5)
	for startBucket := 0.0; startBucket < 24.0; startBucket += 0.5 {
		if halfHourlyActivityUTC[startBucket] < minActivityThreshold {
			continue // Must start with an active bucket
		}

		// CRITICAL: Enforce "two consecutive periods with 3+ events" rule
		// Check if the next bucket also has sufficient activity
		nextBucket := normalizeHour(startBucket + 0.5)
		if halfHourlyActivityUTC[nextBucket] < minActivityThreshold {
			continue // Must have two consecutive active periods to start
		}

		// Try to extend from this start bucket (with wraparound support)
		currentEndBucket := startBucket + 0.5     // End of the first bucket
		lastNoticeableBucket := startBucket + 0.5 // Track end of last noticeable bucket
		noticeableCount := 1                      // Count of buckets with 3+ contributions in this period
		gapLength := 0                            // Current consecutive gap length (buckets with <3 contributions)

		// Search up to 48 half-hour buckets to allow wraparound
		for i := 1; i < 48; i++ {
			testBucket := normalizeHour(startBucket + float64(i)*0.5)
			hasActivity := halfHourlyActivityUTC[testBucket] >= minActivityThreshold

			if hasActivity {
				// Found active bucket, reset gap and extend period to END of this bucket
				gapLength = 0
				currentEndBucket = testBucket + 0.5     // Use bucket END time, not start
				lastNoticeableBucket = testBucket + 0.5 // Track bucket END for gap handling
				noticeableCount++
			} else {
				// Inactive bucket - this is part of a gap
				gapLength++

				// If gap exceeds maximum allowed, stop extending
				if gapLength > maxGapHalfHours {
					// End the period at the last active bucket, not the gap bucket
					currentEndBucket = lastNoticeableBucket
					break
				}
				// Gap is acceptable, continue but don't extend currentEndBucket yet
				// (we only extend to active buckets)
			}
		}

		// Check if this period qualifies (≥4 half-hour buckets = 2+ hours with active buckets)
		// Calculate duration with wraparound support
		var periodDurationBuckets int
		if currentEndBucket >= startBucket {
			periodDurationBuckets = int((currentEndBucket-startBucket)*2) + 1
		} else {
			// Wraparound case: startBucket to 23.5, then 0 to currentEndBucket
			periodDurationBuckets = int((23.5-startBucket+0.5)*2) + int((currentEndBucket+0.5)*2)
		}

		if noticeableCount >= minActiveBuckets { // At least 2 hours worth of buckets
			// Calculate total activity in this period for quality scoring
			totalActivity := 0
			if currentEndBucket >= startBucket {
				// No wraparound
				for b := startBucket; b <= currentEndBucket; b += 0.5 {
					totalActivity += halfHourlyActivityUTC[b]
				}
			} else {
				// Wraparound: startBucket to 23.5, then 0 to currentEndBucket
				for b := startBucket; b < 24.0; b += 0.5 {
					totalActivity += halfHourlyActivityUTC[b]
				}
				for b := 0.0; b <= currentEndBucket; b += 0.5 {
					totalActivity += halfHourlyActivityUTC[b]
				}
			}

			// Prefer longer periods, but also consider activity density
			score := periodDurationBuckets*1000 + totalActivity // Duration weighted heavily, activity as tiebreaker
			bestScore := bestDuration*1000 + bestActivity

			if score > bestScore {
				bestStartBucket = startBucket
				bestEndBucket = currentEndBucket
				bestDuration = periodDurationBuckets
				bestActivity = totalActivity
			}
		}
	}

	// If no valid active period found, fall back to simple approach
	if bestStartBucket == -1.0 {
		// Find first and last buckets with any activity
		var activeBuckets []float64
		for b := 0.0; b < 24.0; b += 0.5 {
			if halfHourlyActivityUTC[b] > 0 {
				activeBuckets = append(activeBuckets, b)
			}
		}
		if len(activeBuckets) >= 2 {
			// Return precise bucket times without rounding
			return activeBuckets[0], activeBuckets[len(activeBuckets)-1]
		}
		return 14.0, 22.0 // Default
	}

	// Return precise bucket times without rounding
	startUTC = bestStartBucket
	endUTC = bestEndBucket

	// Handle wraparound at midnight
	endUTC = normalizeHour(endUTC)

	return startUTC, endUTC
}

// findSleepHours identifies likely sleep hours based on activity patterns.

// detectLunchBreak identifies potential lunch break patterns in work hours.
