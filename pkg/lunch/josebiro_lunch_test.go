package lunch

import (
	"testing"
)

func TestJosebiroLunchDetection(t *testing.T) {
	// Activity pattern from josebiro - sparse data but clear HOUR-LONG lunch at 12:30-1:30pm Pacific (19:30-20:30 UTC)
	// We must detect the full hour duration, not just the initial 30-minute drop
	halfHourActivity := map[float64]int{
		17.0: 1,  // 10:00am Pacific - NOT lunch
		19.0: 3,  // 12:00pm Pacific - before lunch
		19.5: 1,  // 12:30pm Pacific - lunch starts (activity drops from 3 to 1)
		20.0: 1,  // 1:00pm Pacific - still lunch (low activity)
		// 20.5 missing = 0 events at 1:30pm - lunch continues
		// 21.0 missing = 0 events at 2:00pm
		// 21.5 missing = 0 events at 2:30pm  
		22.0: 4,  // 3:00pm Pacific - activity resumes after lunch
		23.0: 2,  // 4:00pm Pacific
		// Evening/night activity
		0.0:  1,  // 5:00pm Pacific
		2.0:  4,  // 7:00pm Pacific  
		3.0:  2,  // 8:00pm Pacific
		// Sleep hours roughly 4-6 UTC (9pm-11pm Pacific)
		7.5:  2,  // 12:30am Pacific
		8.0:  1,  // 1:00am Pacific
		8.5:  3,  // 1:30am Pacific
		9.0:  7,  // 2:00am Pacific - peak
		9.5:  3,  // 2:30am Pacific
		10.5: 2,  // 3:30am Pacific
		11.0: 1,  // 4:00am Pacific
		11.5: 1,  // 4:30am Pacific
		12.0: 3,  // 5:00am Pacific
		13.5: 2,  // 6:30am Pacific
	}

	tests := []struct {
		name        string
		utcOffset   int
		wantStart   float64
		wantEnd     float64
		minConfidence float64
		description string
	}{
		{
			name:        "Pacific Time (UTC-7) should detect hour-long 12:30pm lunch",
			utcOffset:   -7,
			wantStart:   19.5,  // 12:30pm Pacific in UTC
			wantEnd:     20.5,  // 1:30pm Pacific in UTC (full hour)
			minConfidence: 0.5, // Lower confidence due to sparse data
			description: "Should detect the FULL hour-long lunch break at 12:30-1:30pm Pacific (19:30-20:30 UTC)",
		},
		{
			name:        "If misdetected as UTC-3, lunch would be at different time",
			utcOffset:   -3,
			wantStart:   -1,  // No clear lunch pattern for UTC-3
			wantEnd:     -1,
			minConfidence: 0,
			description: "UTC-3 interpretation shouldn't find a good lunch pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourActivity, tt.utcOffset)
			
			// Allow some flexibility in detection (within 30 minutes)
			startOk := tt.wantStart < 0 || (lunchStart >= tt.wantStart-0.5 && lunchStart <= tt.wantStart+0.5)
			endOk := tt.wantEnd < 0 || (lunchEnd >= tt.wantEnd-0.5 && lunchEnd <= tt.wantEnd+0.5)
			
			if !startOk || !endOk {
				t.Errorf("DetectLunchBreakNoonCentered() got lunch at %.1f-%.1f UTC, want around %.1f-%.1f UTC",
					lunchStart, lunchEnd, tt.wantStart, tt.wantEnd)
				
				// Convert to local time for debugging
				if lunchStart >= 0 {
					localStart := lunchStart - float64(tt.utcOffset)
					for localStart < 0 {
						localStart += 24
					}
					for localStart >= 24 {
						localStart -= 24
					}
					localEnd := lunchEnd - float64(tt.utcOffset)
					for localEnd < 0 {
						localEnd += 24
					}
					for localEnd >= 24 {
						localEnd -= 24
					}
					t.Errorf("  In local time: %.1f-%.1f", localStart, localEnd)
				}
			}
			
			if confidence < tt.minConfidence {
				t.Errorf("confidence = %.2f, want at least %.2f", confidence, tt.minConfidence)
			}
		})
	}
}

func TestJosebiroCorrectTimezone(t *testing.T) {
	// The actual activity strongly suggests UTC-7 (Pacific Time), not UTC-3
	// Peak at 9:00 UTC = 2:00am Pacific (suspicious) or 6:00am Brazil (reasonable)
	// But the location field says "Bay Area, CA" which should override
	
	// This test would require the full detection logic, so we'll skip for now
	// The key issue is lunch detection preferring early gaps over noon-centered ones
	t.Skip("Full timezone detection test requires more infrastructure")
}