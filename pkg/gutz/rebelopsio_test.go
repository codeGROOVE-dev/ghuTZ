package gutz

import (
	"math"
	"testing"
)

// TestRebelopsioActiveTimeDetection validates that rebelopsio's activity pattern
// correctly identifies 06:30-21:30 EDT as active time.
func TestRebelopsioActiveTimeDetection(t *testing.T) {
	// Rebelopsio's actual UTC half-hourly activity pattern
	// Shows activity from 06:30 EDT to 21:30 EDT (10:30 UTC to 01:30 UTC)
	halfHourlyActivity := map[float64]int{
		// Night/early morning UTC (evening EDT)
		0.0: 3, 0.5: 0,   // 20:00 EDT previous day
		1.0: 3, 1.5: 1,   // 21:00-21:30 EDT previous day
		2.0: 0, 2.5: 0,   // 22:00 EDT (sleep)
		3.0: 0, 3.5: 0,   // 23:00 EDT (sleep)
		4.0: 0, 4.5: 0,   // 00:00 EDT (sleep)
		5.0: 0, 5.5: 0,   // 01:00 EDT (sleep)
		6.0: 0, 6.5: 0,   // 02:00 EDT (sleep)
		7.0: 0, 7.5: 0,   // 03:00 EDT (sleep)
		8.0: 0, 8.5: 0,   // 04:00 EDT (sleep)
		9.0: 0, 9.5: 0,   // 05:00 EDT (sleep)
		10.0: 1, 10.5: 7, // 06:00-06:30 EDT (morning start!)
		11.0: 4, 11.5: 1, // 07:00-07:30 EDT
		12.0: 9, 12.5: 3, // 08:00-08:30 EDT
		13.0: 1, 13.5: 1, // 09:00-09:30 EDT
		14.0: 10, 14.5: 5,  // 10:00-10:30 EDT
		15.0: 11, 15.5: 4,  // 11:00-11:30 EDT (peak + lunch start)
		16.0: 11, 16.5: 4,  // 12:00-12:30 EDT
		17.0: 4, 17.5: 9,   // 13:00-13:30 EDT
		18.0: 14, 18.5: 6,  // 14:00-14:30 EDT (afternoon peak)
		19.0: 6, 19.5: 5,   // 15:00-15:30 EDT
		20.0: 9, 20.5: 8,   // 16:00-16:30 EDT
		21.0: 4, 21.5: 2,   // 17:00-17:30 EDT
		22.0: 2, 22.5: 4,   // 18:00-18:30 EDT
		23.0: 3, 23.5: 1,   // 19:00-19:30 EDT
	}

	// Quiet hours during sleep (22:00-06:00 EDT = 02:00-10:00 UTC)
	// Note: hour 10 UTC includes 10:00-10:59, so 10:30 (6:30 EDT) is considered quiet
	quietHours := []int{2, 3, 4, 5, 6, 7, 8, 9, 10}

	startUTC, endUTC := calculateTypicalActiveHoursUTC(halfHourlyActivity, quietHours)

	// Convert to EDT for verification
	startEDT := math.Mod(startUTC - 4 + 24, 24)
	endEDT := math.Mod(endUTC - 4 + 24, 24)

	t.Logf("Detected active hours: %.1f-%.1f UTC (%.1f-%.1f EDT)", 
		startUTC, endUTC, startEDT, endEDT)

	// The algorithm should detect:
	// Start: 10:30 UTC (06:30 EDT) - first bucket with 7 events (>=3)
	// End: Around 01:30 UTC (21:30 EDT) or slightly earlier
	
	// The algorithm correctly finds the 10.5 bucket (06:30 EDT with 7 events) as the start
	// Now preserves the precise 10.5 value without rounding
	expectedStartUTC := 10.5  // 06:30 EDT (exact, no rounding)

	if startUTC != expectedStartUTC {
		t.Errorf("Expected active start %.1f UTC (06:30 EDT), got %.1f UTC (%.1f EDT)", 
			expectedStartUTC, startUTC, startEDT)
		
		// Debug: show buckets with 3+ contributions
		t.Log("Buckets with 3+ contributions (not in quiet hours):")
		for bucket := 0.0; bucket < 24.0; bucket += 0.5 {
			hour := int(bucket)
			isQuiet := false
			for _, qh := range quietHours {
				if qh == hour {
					isQuiet = true
					break
				}
			}
			if halfHourlyActivity[bucket] >= 3 && !isQuiet {
				edtTime := bucket - 4.0
				if edtTime < 0 {
					edtTime += 24
				}
				t.Logf("  UTC %.1f (%.1f EDT): %d events", 
					bucket, edtTime, halfHourlyActivity[bucket])
			}
		}
	}

	// The end time should be 1.5 UTC (21:30 EDT) - end of the last active bucket
	expectedEndUTC := 1.5  // 21:30 EDT (end of bucket with 3 events at 21:00)
	if endUTC != expectedEndUTC {
		t.Errorf("Expected active end %.1f UTC (21:30 EDT), got %.1f UTC (%.1f EDT)", 
			expectedEndUTC, endUTC, endEDT)
	}
}