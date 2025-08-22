package gutz

import (
	"math"
	"testing"
)

// TestCalculateTypicalActiveHours tests the active hours calculation function
func TestCalculateTypicalActiveHours(t *testing.T) {
	tests := []struct {
		name               string
		halfHourlyActivity map[float64]int
		quietHours         []int
		utcOffset          int
		expectedStart      float64
		expectedEnd        float64
	}{
		{
			name: "tstromberg real data pattern (UTC-4)",
			// Based on tstromberg's actual half-hourly activity pattern
			halfHourlyActivity: map[float64]int{
				// Midnight-6am UTC (8pm-2am EDT) - evening/night activity
				0.0: 10, 0.5: 6, // 8pm EDT
				1.0: 8, 1.5: 4, // 9pm EDT
				2.0: 1, 2.5: 1, // 10pm EDT
				3.0: 0, 3.5: 0, // 11pm EDT
				4.0: 0, 4.5: 0, // Midnight EDT (sleep)
				5.0: 0, 5.5: 0, // 1am EDT (sleep)
				6.0: 0, 6.5: 0, // 2am EDT (sleep)
				7.0: 0, 7.5: 0, // 3am EDT (sleep)
				8.0: 0, 8.5: 0, // 4am EDT (sleep)
				9.0: 2, 9.5: 1, // 5am EDT (early morning)
				10.0: 2, 10.5: 2, // 6am EDT (waking up)
				11.0: 10, 11.5: 9, // 7am EDT (morning start)
				12.0: 1, 12.5: 1, // 8am EDT
				13.0: 2, 13.5: 2, // 9am EDT
				14.0: 12, 14.5: 9, // 10am EDT (work ramp up)
				15.0: 20, 15.5: 16, // 11am EDT (peak morning)
				16.0: 5, 16.5: 3, // 12pm EDT (lunch start - clear dip)
				17.0: 4, 17.5: 13, // 1pm EDT (returning from lunch)
				18.0: 13, 18.5: 9, // 2pm EDT
				19.0: 42, 19.5: 27, // 3pm EDT (peak afternoon)
				20.0: 20, 20.5: 12, // 4pm EDT
				21.0: 14, 21.5: 8, // 5pm EDT
				22.0: 14, 22.5: 9, // 6pm EDT
				23.0: 7, 23.5: 4, // 7pm EDT
			},
			quietHours:    []int{3, 4, 5, 6, 7, 8}, // 11pm-4am EDT (sleep)
			utcOffset:     -4,                      // EDT (UTC-4)
			expectedStart: 14.0,                      // 10am EDT (14 UTC) - based on sustained activity
			expectedEnd:   2.0,                       // 10pm EDT - end of bucket 1.5 which has 4 events
		},
		{
			name: "mattmoor Pacific time data (UTC-7)",
			// Based on mattmoor's actual half-hourly activity pattern
			halfHourlyActivity: map[float64]int{
				// Night hours - minimal activity
				0.0: 1, 0.5: 0, // 5pm PDT
				1.0: 0, 1.5: 0, // 6pm PDT
				2.0: 0, 2.5: 0, // 7pm PDT
				3.0: 0, 3.5: 0, // 8pm PDT
				4.0: 4, 4.5: 3, // 9pm PDT - some evening work
				5.0: 1, 5.5: 0, // 10pm PDT
				6.0: 1, 6.5: 0, // 11pm PDT
				7.0: 1, 7.5: 1, // Midnight PDT
				8.0: 2, 8.5: 2, // 1am PDT
				// Morning hours - high activity
				9.0: 8, 9.5: 7, // 2am PDT
				10.0: 5, 10.5: 4, // 3am PDT
				11.0: 3, 11.5: 3, // 4am PDT
				12.0: 8, 12.5: 8, // 5am PDT
				13.0: 2, 13.5: 2, // 6am PDT
				14.0: 2, 14.5: 1, // 7am PDT
				15.0: 1, 15.5: 1, // 8am PDT (work starts)
				16.0: 3, 16.5: 2, // 9am PDT
				17.0: 3, 17.5: 2, // 10am PDT
				18.0: 1, 18.5: 0, // 11am PDT
				19.0: 2, 19.5: 2, // Noon PDT
				20.0: 3, 20.5: 3, // 1pm PDT
				21.0: 1, 21.5: 0, // 2pm PDT
				22.0: 1, 22.5: 0, // 3pm PDT
				23.0: 1, 23.5: 1, // 4pm PDT
			},
			quietHours:    []int{1, 2, 3}, // Sleep hours in UTC
			utcOffset:     -7,             // PDT (UTC-7)
			expectedStart: 9.0,              // Algorithm finds main sustained block
			expectedEnd:   13.0,             // End of bucket 12.5 which has 8 events
		},
		{
			name: "dlorenc Central time data (UTC-6)",
			// Simulated Central time data pattern
			halfHourlyActivity: map[float64]int{
				// Early morning UTC (late night CST)
				0.0: 2, 0.5: 1, // 6pm CST previous day
				1.0: 1, 1.5: 1, // 7pm CST
				2.0: 0, 2.5: 0, // 8pm CST
				3.0: 0, 3.5: 0, // 9pm CST
				4.0: 0, 4.5: 0, // 10pm CST
				5.0: 0, 5.5: 0, // 11pm CST
				6.0: 0, 6.5: 0, // Midnight CST (sleep)
				7.0: 0, 7.5: 0, // 1am CST (sleep)
				8.0: 0, 8.5: 0, // 2am CST (sleep)
				9.0: 0, 9.5: 0, // 3am CST (sleep)
				10.0: 0, 10.5: 0, // 4am CST (sleep)
				11.0: 0, 11.5: 0, // 5am CST (sleep)
				12.0: 1, 12.5: 2, // 6am CST (wake up)
				13.0: 3, 13.5: 2, // 7am CST (morning start)
				14.0: 8, 14.5: 7, // 8am CST (work starts)
				15.0: 10, 15.5: 9, // 9am CST (morning work)
				16.0: 12, 16.5: 11, // 10am CST (peak morning)
				17.0: 14, 17.5: 13, // 11am CST (pre-lunch peak)
				18.0: 3, 18.5: 2, // Noon CST (lunch dip)
				19.0: 7, 19.5: 6, // 1pm CST (post-lunch)
				20.0: 4, 20.5: 4, // 2pm CST
				21.0: 12, 21.5: 11, // 3pm CST (afternoon peak)
				22.0: 7, 22.5: 6, // 4pm CST
				23.0: 8, 23.5: 7, // 5pm CST
			},
			quietHours:    []int{6, 7, 8, 9, 10, 11}, // Midnight-5am CST
			utcOffset:     -6,                        // CST (UTC-6)
			expectedStart: 13.0,                        // Algorithm finds 7am CST start
			expectedEnd:   0.0,                         // End of bucket 23.5 (wraps to 0.0)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := calculateTypicalActiveHoursUTC(tt.halfHourlyActivity, tt.quietHours)

			if start != tt.expectedStart {
				t.Errorf("Expected start hour %.1f UTC, got %.1f UTC", tt.expectedStart, start)

				// Convert to local time for debugging
				startLocal := math.Mod(start + float64(tt.utcOffset) + 24, 24)
				expectedLocal := math.Mod(tt.expectedStart + float64(tt.utcOffset) + 24, 24)
				t.Logf("Got start: %.1f UTC (%.1f local), expected: %.1f UTC (%.1f local)",
					start, startLocal, tt.expectedStart, expectedLocal)
			}

			if end != tt.expectedEnd {
				t.Errorf("Expected end hour %.1f UTC, got %.1f UTC", tt.expectedEnd, end)

				// Convert to local time for debugging
				endLocal := math.Mod(end + float64(tt.utcOffset) + 24, 24)
				expectedLocal := math.Mod(tt.expectedEnd + float64(tt.utcOffset) + 24, 24)
				t.Logf("Got end: %.1f UTC (%.1f local), expected: %.1f UTC (%.1f local)",
					end, endLocal, tt.expectedEnd, expectedLocal)
			}
		})
	}
}

// TestWorkDayBoundaries tests edge cases for work day boundaries
func TestWorkDayBoundaries(t *testing.T) {
	tests := []struct {
		name               string
		halfHourlyActivity map[float64]int
		quietHours         []int
		expectedStart      float64
		expectedEnd        float64
	}{
		{
			name:               "No activity",
			halfHourlyActivity: map[float64]int{},
			quietHours:         []int{},
			expectedStart:      14.0, // Default fallback
			expectedEnd:        22.0,
		},
		{
			name: "Single burst of activity",
			halfHourlyActivity: map[float64]int{
				15.0: 5, 15.5: 4,
			},
			quietHours:    []int{},
			expectedStart: 15.0,
			expectedEnd:   15.5,
		},
		{
			name: "Activity wrapping around midnight",
			halfHourlyActivity: map[float64]int{
				22.0: 5, 22.5: 4,
				23.0: 4, 23.5: 3,
				0.0: 3, 0.5: 3,
				1.0: 4, 1.5: 3,
			},
			quietHours:    []int{3, 4, 5, 6, 7, 8},
			expectedStart: 22.0,
			expectedEnd:   2.0, // Bucket 1.5 has >=3 events, so end extends to 2.0 (bucket end)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := calculateTypicalActiveHoursUTC(tt.halfHourlyActivity, tt.quietHours)

			if start != tt.expectedStart {
				t.Errorf("Expected start hour %.1f UTC, got %.1f UTC", tt.expectedStart, start)
			}

			if end != tt.expectedEnd {
				t.Errorf("Expected end hour %.1f UTC, got %.1f UTC", tt.expectedEnd, end)
			}
		})
	}
}
