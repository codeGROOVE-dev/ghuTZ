package gutz

import (
	"sort"
)

// findAllActivityPeriods finds all distinct activity periods in the day.
func findAllActivityPeriods(halfHourlyActivityUTC map[float64]int) []ActivityPeriod {
	const minActivityThreshold = 3
	const maxGapBuckets = 3 // Allow up to 90 minutes gap

	var periods []ActivityPeriod

	// Try each half-hour bucket as a potential start
	processed := make(map[float64]bool)

	for startBucket := 0.0; startBucket < 24.0; startBucket += 0.5 {
		if processed[startBucket] {
			continue
		}

		if halfHourlyActivityUTC[startBucket] < minActivityThreshold {
			continue
		}

		// Check for two consecutive active periods
		nextBucket := normalizeHour(startBucket + 0.5)
		if halfHourlyActivityUTC[nextBucket] < minActivityThreshold {
			continue
		}

		// Found a potential start, trace the period
		currentBucket := startBucket
		endBucket := startBucket
		totalActivity := 0
		gapCount := 0
		duration := 0

		for {
			activity := halfHourlyActivityUTC[currentBucket]
			processed[currentBucket] = true

			if activity >= minActivityThreshold {
				totalActivity += activity
				endBucket = currentBucket
				gapCount = 0
				duration++
			} else {
				gapCount++
				if gapCount > maxGapBuckets {
					break
				}
				duration++
			}

			currentBucket = normalizeHour(currentBucket + 0.5)

			// Stop if we've come full circle
			if currentBucket == startBucket {
				break
			}
		}

		if duration >= 4 { // At least 2 hours
			periods = append(periods, ActivityPeriod{
				StartUTC:      startBucket,
				EndUTC:        endBucket,
				Activity:      totalActivity,
				DurationHours: float64(duration) * 0.5,
			})
		}
	}

	// Sort by start time
	sort.Slice(periods, func(i, j int) bool {
		return periods[i].StartUTC < periods[j].StartUTC
	})

	return periods
}

// ClassifyActivityPeriods converts UTC periods to local time and classifies them.
func ClassifyActivityPeriods(halfHourlyActivityUTC map[float64]int, offsetHours int) []ActivityPeriod {
	periods := findAllActivityPeriods(halfHourlyActivityUTC)

	classified := make([]ActivityPeriod, 0, len(periods))
	for _, p := range periods {
		localStart := normalizeHour(p.StartUTC + float64(offsetHours))
		localEnd := normalizeHour(p.EndUTC + float64(offsetHours))

		// Classify the period based on local time
		var periodType string
		switch {
		case localStart >= 6 && localStart < 12:
			periodType = "morning"
		case localStart >= 12 && localStart < 17:
			periodType = "afternoon"
		case localStart >= 17 && localStart < 21:
			periodType = "evening"
		case localStart >= 21 || localStart < 3:
			periodType = "late_night"
		default:
			periodType = "early_morning"
		}

		classified = append(classified, ActivityPeriod{
			StartLocal:    localStart,
			EndLocal:      localEnd,
			StartUTC:      p.StartUTC,
			EndUTC:        p.EndUTC,
			Activity:      p.Activity,
			DurationHours: p.DurationHours,
			Type:          periodType,
		})
	}

	return classified
}
