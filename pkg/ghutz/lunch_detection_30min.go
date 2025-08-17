package ghutz

import (
	"fmt"
	"math"
	"os"
)

// detectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity
func detectLunchBreakNoonCentered(halfHourCounts map[float64]int, utcOffset int) (lunchStart, lunchEnd, confidence float64) {
	// Write to stderr so it shows up
	fmt.Fprintf(os.Stderr, "\n=== Lunch Detection for UTC%+d ===\n", utcOffset)
	
	// First, check what data we have in the lunch window
	fmt.Fprintf(os.Stderr, "Activity in lunch window (10am-2:30pm local):\n")
	hasAnyData := false
	for localHour := 10.0; localHour <= 14.5; localHour += 0.5 {
		utcHour := localHour - float64(utcOffset)
		for utcHour < 0 {
			utcHour += 24
		}
		for utcHour >= 24 {
			utcHour -= 24
		}
		
		if count, exists := halfHourCounts[utcHour]; exists {
			fmt.Fprintf(os.Stderr, "  %.1f local (%.1f UTC): %d events\n", localHour, utcHour, count)
			hasAnyData = true
		}
	}
	
	if !hasAnyData {
		fmt.Fprintf(os.Stderr, "  NO DATA in lunch window!\n")
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
			startUTC := startLocal - float64(utcOffset)
			// Normalize to 0-24 range
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
			
			// Get activity during lunch (average)
			lunchTotal := 0
			lunchBuckets := 0
			for t := 0.0; t < duration; t += 0.5 {
				bucket := math.Mod(startUTC+t+24, 24)
				if count, exists := halfHourCounts[bucket]; exists {
					lunchTotal += count
					lunchBuckets++
				}
			}
			
			if lunchBuckets == 0 || beforeCount == 0 {
				continue
			}
			
			avgLunchActivity := float64(lunchTotal) / float64(lunchBuckets)
			
			// Calculate drop percentage
			dropRatio := (float64(beforeCount) - avgLunchActivity) / float64(beforeCount)
			
			// Check for "quick lunch" pattern: brief dip followed by rebound
			// This is common for people who grab a quick lunch then have meetings
			afterUTC := math.Mod(startUTC+duration+24, 24)
			afterCount := halfHourCounts[afterUTC]
			isQuickLunch := afterCount > int(avgLunchActivity*1.2) && dropRatio > 0.1
			
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
			minThreshold := 0.01 + effectiveDistance*0.02 // 1% at standard times, more penalty farther away
			
			// Lower threshold for quick lunch patterns near noon
			if isQuickLunch && effectiveDistance < 0.5 {
				minThreshold *= 0.5 // 50% lower threshold for quick lunches near noon
			}
			
			if dropRatio > minThreshold {
				fmt.Fprintf(os.Stderr, "  Found drop: %.1f local for %.0f min, %.1f%% drop (threshold %.1f%%)\n", 
					startLocal, duration*60, dropRatio*100, minThreshold*100)
				
				// Prefer 60-minute lunches when drops are similar
				// A 60-minute lunch with 80% drop is better than 30-minute with 85% drop
				effectiveScore := dropRatio
				if duration == 1.0 { // 60 minutes
					effectiveScore *= 1.15 // 15% bonus for standard lunch duration
				} else if duration == 0.5 { // 30 minutes
					effectiveScore *= 0.95 // 5% penalty for short lunch
				}
				
				// VERY strong bonus for lunches at standard times: 12:00 (most common), 11:30, 12:30
				// This helps prefer standard lunch times over early morning breaks
				if distanceFromNoon < 0.25 { // 11:45-12:15 range
					effectiveScore *= 2.0 // 100% bonus for noon lunch (most common)
					if isQuickLunch {
						effectiveScore *= 1.3 // Extra 30% bonus for quick lunch at noon
					}
				} else if distanceFrom1130 < 0.25 { // 11:15-11:45 range
					effectiveScore *= 1.8 // 80% bonus for 11:30 lunch
				} else if distanceFrom1230 < 0.25 { // 12:15-12:45 range
					effectiveScore *= 1.8 // 80% bonus for 12:30 lunch
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
		fmt.Fprintf(os.Stderr, "Result: NO LUNCH DETECTED\n")
		return -1, -1, 0
	}
	
	fmt.Fprintf(os.Stderr, "Result: LUNCH DETECTED at %.1f local, %.0f min, %.1f%% drop\n", 
		bestStart, bestDuration*60, bestDrop*100)
	
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