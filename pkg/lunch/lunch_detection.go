// Package lunch provides detection of lunch break patterns in activity data.
package lunch

import (
	"math"
)

// GlobalLunchPattern represents a detected lunch pattern in UTC time.
type GlobalLunchPattern struct {
	StartUTC    float64
	EndUTC      float64
	Confidence  float64
	DropPercent float64
}

// DetectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity.
func DetectLunchBreakNoonCentered(halfHourCounts map[float64]int, utcOffset int) (lunchStart, lunchEnd, confidence float64) {
	return detectLunchBreakNoonCentered(halfHourCounts, utcOffset)
}

// detectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity.
func detectLunchBreakNoonCentered(halfHourCounts map[float64]int, utcOffset int) (lunchStart, lunchEnd, confidence float64) {
	// Debug output disabled to reduce clutter
	// Enable with --verbose flag if needed

	// First, check what data we have in the lunch window
	// fmt.Fprintf(os.Stderr, "Activity in lunch window (10am-2:30pm local):\n")
	hasAnyData := false
	for localHour := 10.0; localHour <= 14.5; localHour += 0.5 {
		utcHour := localHour - float64(utcOffset)
		for utcHour < 0 {
			utcHour += 24
		}
		for utcHour >= 24 {
			utcHour -= 24
		}

		if _, exists := halfHourCounts[utcHour]; exists {
			hasAnyData = true
		}
	}

	if !hasAnyData {
		return -1, -1, 0
	}
	// Look for lunch between 10am and 2:30pm local time
	bestScore := 0.0
	bestDrop := 0.0 // Track actual drop for reporting
	bestStart := -1.0
	bestDuration := 1.0 // Default to 1 hour

	// Try different lunch durations: 30, 60, 90 minutes
	for duration := 0.5; duration <= 1.5; duration += 0.5 {
		// Check each possible lunch start time
		for startLocal := 10.0; startLocal <= 14.5-duration; startLocal += 0.5 {
			// Convert to UTC
			// Note: utcOffset is negative for positive UTC zones (e.g., -10 for UTC+10)
			// So startLocal - utcOffset = startLocal - (-10) = startLocal + 10
			// But for UTC+10, we want local - 10 to get UTC
			// The correct formula when offset is negative for positive zones is:
			// startUTC = startLocal + utcOffset (since offset is already negative)
			startUTC := startLocal - float64(utcOffset)
			// Normalize to 0-24 range
			// IMPORTANT: Use modulo to handle day wraparound correctly
			for startUTC < 0 {
				startUTC += 24
			}
			for startUTC >= 24 {
				startUTC -= 24
			}

			// Get activity before lunch
			beforeUTC := startUTC - 0.5
			if beforeUTC < 0 {
				beforeUTC += 24
			}
			beforeCount := halfHourCounts[beforeUTC]

			// CRITICAL: Check for sustained activity in the hour BEFORE lunch
			// Real lunch breaks happen after working for a while, not at the start of the day
			activityBeforeLunch := 0
			for t := 1.0; t <= 2.0; t += 0.5 { // Check 1-2 hours before lunch
				checkUTC := startUTC - t
				if checkUTC < 0 {
					checkUTC += 24
				}
				activityBeforeLunch += halfHourCounts[checkUTC]
			}

			// Skip if there's no meaningful work before this "lunch"
			// BUT: For very sparse data, be more lenient
			totalEvents := 0
			for _, count := range halfHourCounts {
				totalEvents += count
			}

			var minActivityThreshold int
			switch {
			case totalEvents < 50:
				// VERY sparse data (like josebiro with 37 events)
				// Just require SOME activity in the morning, even if not in the 2 hours before
				minActivityThreshold = 3
				// Also check broader window (3-4 hours before lunch)
				for t := 2.5; t <= 4.0 && activityBeforeLunch < minActivityThreshold; t += 0.5 {
					checkUTC := startUTC - t
					if checkUTC < 0 {
						checkUTC += 24
					}
					activityBeforeLunch += halfHourCounts[checkUTC]
				}
			case totalEvents < 200:
				minActivityThreshold = 10 // Lower threshold for sparse data
			case totalEvents < 500:
				minActivityThreshold = 15 // Lower threshold for moderate data
			default:
				// Use default threshold for high activity data
				minActivityThreshold = 20
			}

			// For lunch times very close to noon (12:00-12:30), be extra lenient
			// People often have light mornings before lunch
			if startLocal >= 11.5 && startLocal <= 12.5 && activityBeforeLunch > 0 {
				// If it's near noon and there's ANY morning activity, consider it
				minActivityThreshold = 1
			}

			if activityBeforeLunch < minActivityThreshold {
				continue
			}

			// Get activity during lunch (average)
			// CRITICAL: Treat missing buckets as 0 events (perfect lunch signal!)
			lunchTotal := 0
			lunchBuckets := 0
			for t := 0.0; t < duration; t += 0.5 {
				bucket := math.Mod(startUTC+t+24, 24)
				// Always count the bucket, even if it doesn't exist (0 events)
				lunchBuckets++
				if count, exists := halfHourCounts[bucket]; exists {
					lunchTotal += count
				}
				// If bucket doesn't exist, it's 0 events (no activity = lunch!)
			}

			if lunchBuckets == 0 || beforeCount == 0 {
				continue
			}

			avgLunchActivity := float64(lunchTotal) / float64(lunchBuckets)

			// Calculate drop percentage
			dropRatio := (float64(beforeCount) - avgLunchActivity) / float64(beforeCount)

			// Check for "quick lunch" pattern: brief dip followed by strong rebound
			// This is common for people who grab a quick lunch then have meetings
			afterUTC := math.Mod(startUTC+duration+24, 24)
			afterCount := halfHourCounts[afterUTC]

			// Calculate recovery ratio - compare to pre-lunch activity
			// Strong recovery means activity returns to near pre-lunch levels
			recoveryToPreLunch := float64(afterCount) / float64(beforeCount)

			// Accept ANY drop near noon, with preference for bigger drops
			// Closer to noon = lower threshold
			// Prefer 12:00 noon over other times (most common lunch time)
			lunchMidpoint := startLocal + duration/2
			distanceFromNoon := math.Abs(lunchMidpoint - 12.0)

			// Also consider 11:30am and 12:30pm as very common lunch times
			distanceFrom1130 := math.Abs(lunchMidpoint - 11.5)
			distanceFrom1230 := math.Abs(lunchMidpoint - 12.5)

			// Use the closest distance to any standard lunch time
			effectiveDistance := math.Min(distanceFromNoon, math.Min(distanceFrom1130, distanceFrom1230))

			// CRITICAL: Heavily penalize late lunches (1:30pm or later start)
			// These are often false positives from afternoon lulls
			// Also skip very early lunches (before 10:30am)
			if startLocal >= 13.5 || startLocal < 10.5 {
				continue // Skip unreasonable lunch times
			}

			// Strong recovery: activity returns to >60% of pre-lunch level
			// This indicates lunch is actually over
			// BUT only consider this a valid lunch pattern if the drop was significant
			hasStrongRecovery := recoveryToPreLunch > 0.6 && dropRatio > 0.6

			// Quick lunch detection: requires BOTH significant drop AND recovery
			// AND should be near standard lunch times to avoid false positives
			isQuickLunch := recoveryToPreLunch > 0.4 && dropRatio > 0.5 && effectiveDistance < 1.0
			minThreshold := 0.01 + effectiveDistance*0.02 // 1% at standard times, more penalty farther away

			// Lower threshold for quick lunch patterns near noon
			if isQuickLunch && effectiveDistance < 0.5 {
				minThreshold *= 0.5 // 50% lower threshold for quick lunches near noon
			}

			if dropRatio > minThreshold {
				// Debug output disabled - too verbose even for --verbose mode
				// Uncomment for debugging specific lunch detection issues

				// Adjust scoring based on lunch duration and recovery pattern
				effectiveScore := dropRatio

				// CRITICAL: Boost score if there was good work activity before lunch
				// This helps distinguish real lunch from early morning quiet periods
				if activityBeforeLunch > 40 {
					effectiveScore *= 1.5 // 50% bonus for strong pre-lunch work
				} else if activityBeforeLunch > 30 {
					effectiveScore *= 1.2 // 20% bonus for moderate pre-lunch work
				}
				// Note: < 20 events already filtered out above

				// CRITICAL: Massive preference for 100% activity drops
				// This ensures perfect lunch signals always win over partial drops
				if dropRatio >= 1.0 && effectiveDistance <= 1.0 { // 100% drop within 1 hour of standard time
					effectiveScore *= 10.0 // 900% bonus for perfect drops near lunch time
				}

				// CRITICAL: Check if lunch continues beyond current duration
				// If activity remains low/zero after the lunch period, it likely continues
				lunchContinues := false
				actualDuration := duration
				if duration < 1.5 { // Only check if not already at max duration
					nextBucket := math.Mod(startUTC+duration+24, 24)
					nextCount := halfHourCounts[nextBucket]
					// If next slot has 0 or very low activity, lunch probably continues
					if nextCount == 0 || (beforeCount > 0 && float64(nextCount)/float64(beforeCount) < 0.3) {
						lunchContinues = true
						// Automatically extend duration when lunch clearly continues
						// This fixes the josebiro case where 30min lunch should be 60min
						if duration == 0.5 && nextCount == 0 {
							// Extend to full hour since next slot is 0
							actualDuration = 1.0
						}
					}
				}

				// For 30-minute lunches with strong recovery, give a big bonus
				// This is the clearest signal of a quick lunch break
				switch {
				case duration == 0.5 && hasStrongRecovery && !lunchContinues: // 30 minutes with strong rebound
					effectiveScore *= 2.0 // 100% bonus for clear quick lunch pattern
				case duration == 0.5 && isQuickLunch && !lunchContinues: // 30 minutes with moderate rebound
					effectiveScore *= 1.3 // 30% bonus for quick lunch
				case duration == 0.5 && lunchContinues:
					// 30 minutes but activity stays low - lunch probably continues
					effectiveScore *= 0.5 // 50% penalty - incomplete lunch detection
				case duration == 1.0 && !hasStrongRecovery: // 60 minutes without strong recovery after
					effectiveScore *= 1.2 // Bonus for standard hour lunch
				case duration == 1.0 && hasStrongRecovery:
					// 60 minutes but activity rebounds strongly at 60min mark
					// This might mean lunch was actually shorter
					effectiveScore *= 0.8 // 20% penalty - lunch probably ended earlier
				case duration == 0.5: // 30 minutes without strong recovery
					effectiveScore *= 0.95 // Small penalty
				default:
					// Other duration values (e.g., 1.5 hours) - less typical lunch pattern
					effectiveScore *= 0.9 // Small penalty for atypical duration
				}

				// MASSIVE bonus for 100% drops (perfect lunch signal)
				// This ensures 100% drops always win over partial drops
				if dropRatio >= 1.0 { // 100% drop
					effectiveScore *= 5.0 // 400% bonus for perfect lunch signal
				}

				// VERY strong bonus for lunches at standard times: 12:00 (most common), 11:30, 12:30
				// This helps prefer standard lunch times over early morning breaks
				// For 30-min lunch starting at 12:00, midpoint is 12:15, distance is 0.25
				// So use <= 0.25 to include exact noon starts
				switch {
				case distanceFromNoon <= 0.25: // 11:45-12:15 range (inclusive)
					// Massive bonus for noon, especially with large drops
					switch {
					case dropRatio > 0.8:
						effectiveScore *= 3.0 // 200% bonus for massive noon drops
					case dropRatio > 0.6:
						effectiveScore *= 2.5 // 150% bonus for large noon drops
					default:
						effectiveScore *= 2.0 // 100% bonus for noon lunch (most common)
					}
					if isQuickLunch {
						effectiveScore *= 1.3 // Extra 30% bonus for quick lunch at noon
					}
				case distanceFrom1230 <= 0.25: // 12:15-12:45 range (inclusive)
					// 12:30pm is VERY common, especially in Latin America
					effectiveScore *= 2.2 // 120% bonus for 12:30 lunch (more popular than 11:30!)
				case distanceFrom1130 <= 0.25: // 11:15-11:45 range (inclusive)
					effectiveScore *= 1.5 // 50% bonus for 11:30 lunch (less common than noon/12:30)
				case effectiveDistance < 0.5: // Within 30 minutes of any standard time
					effectiveScore *= 1.5 // 50% bonus for near-standard lunch
					if isQuickLunch {
						effectiveScore *= 1.2 // Extra 20% bonus for quick lunch near standard times
					}
				case effectiveDistance < 1.0: // Within 1 hour of standard times
					effectiveScore *= 1.2 // 20% bonus
				case effectiveDistance > 2.0: // More than 2 hours from standard times
					effectiveScore *= 0.5 // 50% penalty for very early/late lunch
				case effectiveDistance > 1.5: // More than 1.5 hours from standard times
					effectiveScore *= 0.7 // 30% penalty for early/late lunch
				default:
					// Distance between 1.0 and 1.5 hours from standard times
					effectiveScore *= 1.0 // No adjustment for moderate distance
				}

				// European timezone constraint: lunch should not be before 11:30am
				// UTC offsets 0, +1, +2, +3 are typically European
				if utcOffset >= -1 && utcOffset <= 3 && lunchMidpoint < 11.5 {
					// Severe penalty for pre-11:30am lunch in Europe
					effectiveScore *= 0.3 // 70% penalty
				}

				if effectiveScore > bestScore {
					bestScore = effectiveScore
					bestDrop = dropRatio // Save actual drop ratio for reporting
					bestStart = startLocal
					bestDuration = actualDuration // Use extended duration if lunch continues
				}
			}
		}
	}

	if bestStart < 0 {
		// No lunch found at all
		return -1, -1, 0
	}

	// Convert back to UTC for return
	startUTC := bestStart - float64(utcOffset)
	for startUTC < 0 {
		startUTC += 24
	}
	for startUTC >= 24 {
		startUTC -= 24
	}

	endUTC := startUTC + bestDuration
	if endUTC >= 24 {
		endUTC -= 24
	}

	// Simple confidence based on drop strength and timing
	confidence = 0.3 // Base
	if bestDrop > 0.2 {
		confidence += 0.3
	}
	if bestStart >= 11.5 && bestStart <= 13.0 {
		confidence += 0.2
	}
	confidence = math.Min(1.0, confidence)

	return startUTC, endUTC, confidence
}

// FindBestGlobalLunchPattern finds the best lunch pattern globally in UTC time
// This is timezone-independent and looks for the strongest activity drop + recovery pattern.
func FindBestGlobalLunchPattern(halfHourCounts map[float64]int) GlobalLunchPattern {
	// Calculate average activity to establish baseline
	totalActivity := 0
	totalBuckets := 0
	for _, count := range halfHourCounts {
		totalActivity += count
		totalBuckets++
	}

	if totalBuckets == 0 {
		return GlobalLunchPattern{StartUTC: -1, Confidence: 0}
	}

	avgActivity := float64(totalActivity) / float64(totalBuckets)
	if avgActivity < 1 {
		return GlobalLunchPattern{StartUTC: -1, Confidence: 0}
	}

	bestPattern := GlobalLunchPattern{StartUTC: -1, Confidence: 0}
	bestScore := 0.0

	// Look for lunch breaks of 30, 60, or 90 minutes (1, 2, or 3 buckets)
	durations := []int{1, 2, 3}

	for _, duration := range durations {
		// Restrict search to reasonable global lunch window (15:00-21:00 UTC)
		// This covers 11am-2pm across US timezones (-4 to -8), Europe (+1 to +3), and Asia (+8 to +9)
		for startBucket := 15.0; startBucket < 21.0; startBucket += 0.5 {
			endBucket := startBucket + float64(duration)*0.5

			// Get surrounding buckets for comparison
			prevBucket := startBucket - 0.5
			if prevBucket < 0 {
				prevBucket += 24
			}
			nextBucket := endBucket
			if nextBucket >= 24 {
				nextBucket -= 24
			}

			prevCount := halfHourCounts[prevBucket]
			startCount := halfHourCounts[startBucket]
			afterCount := halfHourCounts[nextBucket]

			// Skip if no data for comparison
			if prevCount == 0 {
				continue
			}
			dropPercent := (float64(prevCount) - float64(startCount)) / float64(prevCount)

			// Calculate recovery factor
			recoveryFactor := 1.0
			if startCount > 0 && afterCount > startCount {
				recoveryFactor = float64(afterCount) / float64(startCount)
			}

			// Calculate average lunch activity
			lunchTotal := 0
			lunchBuckets := 0
			for b := startBucket; b < endBucket; b += 0.5 {
				bucket := b
				if bucket >= 24 {
					bucket -= 24
				}
				lunchTotal += halfHourCounts[bucket]
				lunchBuckets++
			}

			if lunchBuckets == 0 {
				continue
			}
			avgLunchActivity := float64(lunchTotal) / float64(lunchBuckets)

			// Check if this qualifies as a lunch pattern
			minDropThreshold := 0.25 // 25% minimum drop
			if recoveryFactor >= 2.0 {
				minDropThreshold = 0.15 // Allow smaller drops with strong recovery
			}

			if dropPercent > minDropThreshold && avgLunchActivity <= avgActivity*0.8 {
				// Score this pattern
				score := 0.0

				// Prefer stronger drops
				score += dropPercent * 100

				// Prefer lower lunch activity
				quietness := 1.0 - (avgLunchActivity / avgActivity)
				score += quietness * 50

				// Boost for strong recovery
				if recoveryFactor >= 1.5 {
					score += (recoveryFactor - 1.0) * 30
				}

				// Prefer 60-minute lunches
				if duration == 2 {
					score += 20
				}

				// Track the best pattern
				if score > bestScore {
					bestScore = score
					bestPattern = GlobalLunchPattern{
						StartUTC:    startBucket,
						EndUTC:      endBucket,
						Confidence:  math.Min(1.0, score/100.0), // Normalize to 0-1
						DropPercent: dropPercent,
					}
				}
			}
		}
	}

	return bestPattern
}
