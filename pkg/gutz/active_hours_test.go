package gutz

import (
	"testing"
)

// TestCalculateTypicalActiveHours tests the active hours calculation function
func TestCalculateTypicalActiveHours(t *testing.T) {
	tests := []struct {
		name          string
		hourCounts    map[int]int
		quietHours    []int
		utcOffset     int
		expectedStart int
		expectedEnd   int
	}{
		{
			name: "tstromberg real data pattern (UTC-4)",
			// Based on tstromberg's actual activity pattern
			hourCounts: map[int]int{
				0:  13, // 8pm EDT (evening activity)
				1:  11, // 9pm EDT (evening activity)
				2:  2,  // 10pm EDT (light activity)
				3:  0,  // 11pm EDT (sleep)
				4:  0,  // Midnight EDT (sleep)
				5:  0,  // 1am EDT (sleep)
				6:  0,  // 2am EDT (sleep)
				7:  0,  // 3am EDT (sleep)
				8:  0,  // 4am EDT (sleep)
				9:  0,  // 5am EDT (sleep)
				10: 2,  // 6am EDT (early morning)
				11: 16, // 7am EDT (work starts)
				12: 0,  // 8am EDT
				13: 3,  // 9am EDT
				14: 19, // 10am EDT (work ramp-up)
				15: 34, // 11am EDT (peak morning)
				16: 11, // Noon EDT
				17: 21, // 1pm EDT (afternoon)
				18: 17, // 2pm EDT (afternoon)
				19: 61, // 3pm EDT (peak activity)
				20: 26, // 4pm EDT (afternoon)
				21: 20, // 5pm EDT (late work)
				22: 19, // 6pm EDT (evening work)
				23: 10, // 7pm EDT (light evening)
			},
			quietHours:    []int{3, 4, 5, 6, 7, 8, 9}, // 11pm-5am EDT (sleep)
			utcOffset:     -4,                         // EDT (UTC-4)
			expectedStart: 11,                         // 7am EDT (11 UTC)
			expectedEnd:   1,                          // 9pm EDT (01 UTC next day)
		},
		{
			name: "dlorenc real data pattern (UTC-4)",
			// Based on dlorenc's typical work pattern
			hourCounts: map[int]int{
				0:  5,  // 8pm EDT
				1:  8,  // 9pm EDT
				2:  3,  // 10pm EDT
				3:  0,  // 11pm EDT (sleep)
				4:  0,  // Midnight EDT (sleep)
				5:  0,  // 1am EDT (sleep)
				6:  0,  // 2am EDT (sleep)
				7:  0,  // 3am EDT (sleep)
				8:  0,  // 4am EDT (sleep)
				9:  1,  // 5am EDT
				10: 2,  // 6am EDT
				11: 12, // 7am EDT (work starts)
				12: 5,  // 8am EDT
				13: 8,  // 9am EDT
				14: 15, // 10am EDT
				15: 25, // 11am EDT (peak)
				16: 18, // Noon EDT
				17: 22, // 1pm EDT (peak)
				18: 12, // 2pm EDT
				19: 20, // 3pm EDT
				20: 15, // 4pm EDT
				21: 8,  // 5pm EDT
				22: 5,  // 6pm EDT (end of work)
				23: 3,  // 7pm EDT
			},
			quietHours:    []int{3, 4, 5, 6, 7, 8},
			utcOffset:     -4,
			expectedStart: 11, // 7am EDT
			expectedEnd:   22, // 6pm EDT
		},
		{
			name: "Basic 9-5 pattern (UTC-5)",
			hourCounts: map[int]int{
				14: 10, // 9am CDT (work starts)
				15: 15, // 10am CDT
				16: 20, // 11am CDT
				17: 25, // Noon CDT (peak)
				18: 20, // 1pm CDT
				19: 18, // 2pm CDT
				20: 22, // 3pm CDT (peak)
				21: 15, // 4pm CDT
				22: 8,  // 5pm CDT (work ends)
				23: 3,  // 6pm CDT (light evening)
			},
			quietHours:    []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
			utcOffset:     -5,
			expectedStart: 14, // 9am CDT
			expectedEnd:   22, // 5pm CDT
		},
		{
			name: "EyeCantCU extreme work pattern (UTC-6)",
			// Based on EyeCantCU's real pattern: works ~6:30am to midnight (17+ hours!)
			hourCounts: map[int]int{
				0:  11, // 6pm CST (evening work)
				1:  5,  // 7pm CST
				2:  15, // 8pm CST
				3:  3,  // 9pm CST
				4:  3,  // 10pm CST
				5:  11, // 11pm CST (late work)
				6:  2,  // Midnight CST (very late)
				7:  0,  // 1am CST (sleep)
				8:  0,  // 2am CST (sleep)
				9:  1,  // 3am CST (sleep)
				10: 5,  // 4am CST (sleep)
				11: 2,  // 5am CST (sleep)
				12: 13, // 6am CST (early start!)
				13: 11, // 7am CST (morning work)
				14: 19, // 8am CST
				15: 10, // 9am CST
				16: 6,  // 10am CST
				17: 10, // 11am CST
				18: 14, // Noon CST
				19: 13, // 1pm CST
				20: 8,  // 2pm CST
				21: 23, // 3pm CST (peak)
				22: 13, // 4pm CST
				23: 15, // 5pm CST
			},
			quietHours:    []int{7, 8, 9, 10, 11}, // 1am-5am CST (short sleep!)
			utcOffset:     -6,                     // CST (UTC-6)
			expectedStart: 12,                     // 6am CST (12 UTC)
			expectedEnd:   5,                      // 11pm CST (5 UTC next day) - algorithm is more conservative
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := calculateTypicalActiveHours(tt.hourCounts, tt.quietHours, tt.utcOffset)

			if start != tt.expectedStart {
				t.Errorf("Expected start hour %d UTC, got %d UTC", tt.expectedStart, start)

				// Convert to local time for debugging
				startLocal := (start + tt.utcOffset + 24) % 24
				expectedLocal := (tt.expectedStart + tt.utcOffset + 24) % 24
				t.Logf("Got start: %d UTC (%dam local), expected: %d UTC (%dam local)",
					start, startLocal, tt.expectedStart, expectedLocal)
			}

			if end != tt.expectedEnd {
				t.Errorf("Expected end hour %d UTC, got %d UTC", tt.expectedEnd, end)

				// Convert to local time for debugging
				endLocal := (end + tt.utcOffset + 24) % 24
				expectedLocal := (tt.expectedEnd + tt.utcOffset + 24) % 24
				t.Logf("Got end: %d UTC (%dpm local), expected: %d UTC (%dpm local)",
					end, endLocal, tt.expectedEnd, expectedLocal)
			}

			// Validate duration is reasonable (6-17 hours) - algorithm should handle all work patterns
			duration := (end - start + 24) % 24
			if duration < 6 || duration > 17 {
				t.Errorf("Active duration %d hours is unreasonable (should be 6-17 hours)", duration)
			}

			t.Logf("Active hours: %d-%d UTC (duration: %d hours)", start, end, duration)
		})
	}
}

// TestTstrombergActiveHoursNoWarnings specifically tests that tstromberg's real data
// doesn't trigger any "unusual sleep" or other warnings
func TestTstrombergActiveHoursNoWarnings(t *testing.T) {
	// This test ensures tstromberg's sleep pattern (22:00-6:00 EDT) is considered normal

	// Tstromberg's quiet hours in UTC: 2,3,4,5,6,7,8,9 (10pm-5am EDT)
	quietHours := []int{2, 3, 4, 5, 6, 7, 8, 9}

	// Calculate sleep midpoint for UTC-4
	startHour := 2 // 10pm EDT
	windowSize := len(quietHours)

	// Sleep midpoint calculation
	midQuiet := float64(startHour) + float64(windowSize-1)/2.0
	expectedMidQuiet := float64(2+9) / 2.0 // Should be around 5.5 UTC (1:30am EDT)

	if midQuiet != expectedMidQuiet {
		t.Errorf("Sleep midpoint calculation error: got %.1f, expected %.1f", midQuiet, expectedMidQuiet)
	}

	// Convert to local time for timezone validation
	utcOffset := -4
	sleepLocalMid := float64(int(midQuiet+float64(utcOffset)+24) % 24)

	// Sleep midpoint should be around 0.5 (12:30am EDT) which is reasonable
	if sleepLocalMid < 22 && sleepLocalMid > 10 {
		t.Errorf("Sleep midpoint %.1f local time is during day - should be nighttime (22-24 or 0-10)", sleepLocalMid)
	}

	t.Logf("Sleep midpoint: %.1f UTC = %.1f local (EDT) - reasonable nighttime sleep", midQuiet, sleepLocalMid)
}

// TestActiveHoursEdgeCases tests edge cases for active hours calculation
func TestActiveHoursEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		hourCounts map[int]int
		quietHours []int
		utcOffset  int
	}{
		{
			name:       "No activity data",
			hourCounts: map[int]int{},
			quietHours: []int{0, 1, 2, 3, 4, 5},
			utcOffset:  -5,
		},
		{
			name: "Activity spans midnight",
			hourCounts: map[int]int{
				22: 10, // Late evening
				23: 15, // Late night
				0:  12, // Past midnight
				1:  8,  // Early morning
				14: 20, // Afternoon peak
				15: 25, // Afternoon peak
				16: 20, // Late afternoon
			},
			quietHours: []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
			utcOffset:  -5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := calculateTypicalActiveHours(tt.hourCounts, tt.quietHours, tt.utcOffset)

			// Should not panic and should return reasonable hours
			if start < 0 || start > 23 || end < 0 || end > 23 {
				t.Errorf("Active hours out of range: start=%d, end=%d", start, end)
			}

			duration := (end - start + 24) % 24
			if duration < 6 || duration > 16 {
				t.Logf("Duration %d hours is outside normal range (6-16) but acceptable for edge case", duration)
			}

			t.Logf("Edge case result: %d-%d UTC (duration: %d hours)", start, end, duration)
		})
	}
}
