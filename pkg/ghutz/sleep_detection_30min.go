package ghutz

import (
	"sort"
)

// detectSleepPeriodsWithHalfHours identifies sleep periods using 30-minute resolution data.
// It requires at least 30 minutes of buffer between activity and sleep.
func detectSleepPeriodsWithHalfHours(halfHourCounts map[float64]int) []float64 {
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
		if quietBuckets[i] == currentPeriod.end + 0.5 {
			currentPeriod.end = quietBuckets[i]
			currentPeriod.length++
		} else if currentPeriod.end == 23.5 && quietBuckets[i] == 0.0 {
			// Wraparound case
			currentPeriod.end = quietBuckets[i]
			currentPeriod.length++
		} else {
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
			if count, exists := halfHourCounts[bufferStart]; exists && count > 1 {
				period.start += 0.5
				if period.start >= 24 {
					period.start -= 24
				}
				period.length--
				
				// Stop if we've adjusted too much
				if period.length < 4 {
					break
				}
			} else {
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
			if count, exists := halfHourCounts[bufferEnd]; exists && count > 1 {
				period.end -= 0.5
				if period.end < 0 {
					period.end += 24
				}
				period.length--
				
				// Stop if we've adjusted too much
				if period.length < 4 {
					break
				}
			} else {
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