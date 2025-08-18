package ghutz

import (
	"math"
)

// DetectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity  
func DetectLunchBreakNoonCentered(halfHourCounts map[float64]int, utcOffset int) (lunchStart, lunchEnd, confidence float64) {
	return detectLunchBreakNoonCentered(halfHourCounts, utcOffset)
}

// detectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity
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
			// fmt.Fprintf(os.Stderr, "  %.1f local (%.1f UTC): %d events\n", localHour, utcHour, count)
			hasAnyData = true
		}
	}
	
	if !hasAnyData {
		// fmt.Fprintf(os.Stderr, "  NO DATA in lunch window!\n")
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
			// (Less than 20 events in the 2 hours before = probably not actually working yet)
			// BUT: For sparse data (< 500 total events), lower the threshold
			totalEvents := 0
			for _, count := range halfHourCounts {
				totalEvents += count
			}
			
			minActivityThreshold := 20
			if totalEvents < 200 {
				minActivityThreshold = 10 // Lower threshold for very sparse data
			} else if totalEvents < 500 {
				minActivityThreshold = 15 // Lower threshold for sparse data
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
				
				// For 30-minute lunches with strong recovery, give a big bonus
				// This is the clearest signal of a quick lunch break
				if duration == 0.5 && hasStrongRecovery { // 30 minutes with strong rebound
					effectiveScore *= 2.0 // 100% bonus for clear quick lunch pattern
				} else if duration == 0.5 && isQuickLunch { // 30 minutes with moderate rebound
					effectiveScore *= 1.3 // 30% bonus for quick lunch
				} else if duration == 1.0 && !hasStrongRecovery { // 60 minutes without strong recovery after
					effectiveScore *= 1.1 // Small bonus for standard lunch
				} else if duration == 1.0 && hasStrongRecovery { 
					// 60 minutes but activity rebounds strongly at 60min mark
					// This might mean lunch was actually shorter
					effectiveScore *= 0.8 // 20% penalty - lunch probably ended earlier
				} else if duration == 0.5 { // 30 minutes without strong recovery
					effectiveScore *= 0.95 // Small penalty
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
				if distanceFromNoon <= 0.25 { // 11:45-12:15 range (inclusive)
					// Massive bonus for noon, especially with large drops
					if dropRatio > 0.8 {
						effectiveScore *= 3.0 // 200% bonus for massive noon drops
					} else if dropRatio > 0.6 {
						effectiveScore *= 2.5 // 150% bonus for large noon drops  
					} else {
						effectiveScore *= 2.0 // 100% bonus for noon lunch (most common)
					}
					if isQuickLunch {
						effectiveScore *= 1.3 // Extra 30% bonus for quick lunch at noon
					}
				} else if distanceFrom1230 <= 0.25 { // 12:15-12:45 range (inclusive)
					// 12:30pm is VERY common, especially in Latin America
					effectiveScore *= 2.2 // 120% bonus for 12:30 lunch (more popular than 11:30!)
				} else if distanceFrom1130 <= 0.25 { // 11:15-11:45 range (inclusive)
					effectiveScore *= 1.5 // 50% bonus for 11:30 lunch (less common than noon/12:30)
				} else if effectiveDistance < 0.5 { // Within 30 minutes of any standard time
					effectiveScore *= 1.5 // 50% bonus for near-standard lunch
					if isQuickLunch {
						effectiveScore *= 1.2 // Extra 20% bonus for quick lunch near standard times
					}
				} else if effectiveDistance < 1.0 { // Within 1 hour of standard times
					effectiveScore *= 1.2 // 20% bonus
				} else if effectiveDistance > 2.0 { // More than 2 hours from standard times
					effectiveScore *= 0.5 // 50% penalty for very early/late lunch
				} else if effectiveDistance > 1.5 { // More than 1.5 hours from standard times
					effectiveScore *= 0.7 // 30% penalty for early/late lunch
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
					bestDuration = duration
				}
			}
		}
	}
	
	if bestStart < 0 {
		// No lunch found at all
		// fmt.Fprintf(os.Stderr, "Result: NO LUNCH DETECTED\n")
		return -1, -1, 0
	}
	
	// fmt.Fprintf(os.Stderr, "Result: LUNCH DETECTED at %.1f local, %.0f min, %.1f%% drop\n", 
	//	bestStart, bestDuration*60, bestDrop*100)
	
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