// Package sleep provides detection of sleep patterns in activity data.
package sleep

import (
	"sort"
)

// DetectSleepPeriodsWithHalfHours identifies sleep periods using 30-minute resolution data.
// A rest period is defined as:
// - 4+ hours of continuous buckets with 0-2 events
// - Ends when we hit two consecutive buckets with 3+ events
// - Maximum 12 hours per rest period.
// - Strongly prefers nighttime periods (starting search at 9pm local time).
func DetectSleepPeriodsWithHalfHours(halfHourCounts map[float64]int) []float64 {
	return DetectSleepPeriodsWithOffset(halfHourCounts, 0)
}

// DetectSleepPeriodsWithOffset identifies sleep periods for a specific timezone offset.
// The offset parameter adjusts the search to look for sleep at appropriate local times.
//
//nolint:gocognit,revive,maintidx // Complex sleep detection algorithm
func DetectSleepPeriodsWithOffset(halfHourCounts map[float64]int, timezoneOffset int) []float64 {
	// Find all potential rest periods
	type restPeriod struct {
		buckets []float64
		start   float64
		length  int
		score   float64 // preference score (higher is better)
	}
	var allPeriods []restPeriod

	// Calculate the UTC hour that corresponds to 21:00 (9pm) in the target timezone
	// For example, if timezoneOffset is +8 (China), 21:00 local = 13:00 UTC
	localNightStart := 21.0 // 9pm local time
	utcNightStart := localNightStart - float64(timezoneOffset)
	// Normalize to 0-24 range
	for utcNightStart < 0 {
		utcNightStart += 24
	}
	for utcNightStart >= 24 {
		utcNightStart -= 24
	}

	// Start search at the UTC time that corresponds to 9pm local
	// This gives preference to nighttime sleep periods in the target timezone
	searchOrder := make([]float64, 0, 48)

	// Add 12 hours starting from utcNightStart (covers 9pm to 9am local)
	for i := range 24 { // 24 half-hour buckets = 12 hours
		h := utcNightStart + float64(i)*0.5
		// Wrap around at 24
		for h >= 24.0 {
			h -= 24.0
		}
		searchOrder = append(searchOrder, h)
	}

	// Then add the remaining daytime hours (less preferred)
	for i := 24; i < 48; i++ { // Remaining 24 half-hour buckets
		h := utcNightStart + float64(i)*0.5
		// Wrap around at 24
		for h >= 24.0 {
			h -= 24.0
		}
		searchOrder = append(searchOrder, h)
	}

	// Calculate total activity to determine if we have sparse data
	totalActivity := 0
	for _, count := range halfHourCounts {
		totalActivity += count
	}

	// For sparse data (< 50 events total), use stricter criteria
	quietThreshold := 2
	if totalActivity < 50 {
		quietThreshold = 0 // Only consider buckets with NO activity as quiet
	}

	// Scan through buckets in our preferred order
	for _, startBucket := range searchOrder {
		// Skip if this bucket is active
		if halfHourCounts[startBucket] > quietThreshold {
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
				// For sparse data: any activity ends sleep
				// For normal data: two consecutive active buckets
				if totalActivity < 50 {
					// Sparse data: any activity > 0 ends sleep
					if count > 0 && previousCount >= 0 {
						break
					}
				} else {
					// Normal data: original logic
					if (previousCount >= 2 && count >= 3) || (previousCount >= 3 && count >= 2) {
						// Don't include the previous bucket if it's too active
						if len(currentPeriod) > 0 && previousCount >= 2 {
							currentPeriod = currentPeriod[:len(currentPeriod)-1]
						}
						break
					}
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
			score := calculateNighttimeScore(currentPeriod, timezoneOffset)
			// For sparse data, skip periods with zero nighttime score (likely work hours)
			if totalActivity < 50 && score == 0.0 {
				continue
			}
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
		// For sparse data, VERY strongly prefer nighttime periods
		if totalActivity < 50 {
			// Only consider periods with >50% nighttime overlap
			iIsNight := allPeriods[i].score > 0.5
			jIsNight := allPeriods[j].score > 0.5
			if iIsNight != jIsNight {
				return iIsNight
			}
		}

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
		if halfHourCounts[lastBucket] < 3 {
			// Stop trimming once we hit a quiet bucket
			break
		}
		// Remove this bucket from sleep period
		finalBuckets = finalBuckets[:len(finalBuckets)-1]
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
// Nighttime is considered 21:00-09:00 LOCAL time. Higher scores mean more nighttime overlap.
// Periods during work hours (9:00-17:00 local) get heavily penalized.
func calculateNighttimeScore(buckets []float64, timezoneOffset int) float64 {
	nighttimeCount := 0
	worktimeCount := 0

	for _, bucket := range buckets {
		// Convert UTC bucket to local time
		localHour := bucket + float64(timezoneOffset)
		// Normalize to 0-24 range
		for localHour >= 24.0 {
			localHour -= 24.0
		}
		for localHour < 0.0 {
			localHour += 24.0
		}

		// Hours 21:00-23:30 and 00:00-08:30 LOCAL are considered nighttime
		if localHour >= 21.0 || localHour < 9.0 {
			nighttimeCount++
		}

		// Hours 9:00-17:00 LOCAL are work hours - sleep shouldn't be here
		if localHour >= 9.0 && localHour < 17.0 {
			worktimeCount++
		}
	}

	// If more than 30% of the period is during work hours, heavily penalize
	if float64(worktimeCount)/float64(len(buckets)) > 0.3 {
		return 0.0 // This can't be a sleep period
	}

	// Return percentage of buckets that are during nighttime
	return float64(nighttimeCount) / float64(len(buckets))
}
