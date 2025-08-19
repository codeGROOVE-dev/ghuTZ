package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/sleep"
)

func TestJamonationSleepDetection(t *testing.T) {
	// Actual half-hour activity data from jamonation
	// This shows activity at 22:00 UTC (2 events) but quiet from 22:30 onwards
	halfHourCounts := map[float64]int{
		0.0: 0, 0.5: 0,
		1.0: 0, 1.5: 0,
		2.0: 0, 2.5: 1, // One event at 2:30
		3.0: 0, 3.5: 0,
		4.0: 0, 4.5: 1, // One event at 4:30
		5.0: 0, 5.5: 0,
		6.0: 0, 6.5: 1, // One event at 6:30
		7.0: 0, 7.5: 0,
		8.0: 0, 8.5: 0,
		9.0: 0, 9.5: 0,
		10.0: 1, 10.5: 0, // One event at 10:00
		11.0: 3, 11.5: 3, // Activity starts picking up
		12.0: 5, 12.5: 2,
		13.0: 10, 13.5: 12,
		14.0: 9, 14.5: 11,
		15.0: 16, 15.5: 10, // Peak activity
		16.0: 5, 16.5: 2, // Lunch dip
		17.0: 13, 17.5: 7,
		18.0: 13, 18.5: 8,
		19.0: 8, 19.5: 10,
		20.0: 5, 20.5: 5,
		21.0: 13, 21.5: 7,
		22.0: 2, 22.5: 0, // Activity at 22:00, quiet from 22:30
		23.0: 0, 23.5: 1, // Mostly quiet with one blip at 23:30
	}

	// Aggregate to hourly for the old sleep detection
	hourCounts := aggregateHalfHoursToHours(halfHourCounts)

	// Old sleep detection (hourly)
	quietHours := sleep.FindSleepHours(hourCounts)

	// New sleep detection (half-hourly)
	sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(halfHourCounts)

	// Refine using our new function
	refinedSleepHours := refineHourlySleepFromBuckets(quietHours, sleepBuckets, halfHourCounts)

	// Check that hour 22 is NOT included in refined sleep hours
	// because it has activity in the first half
	hasHour22 := false
	for _, hour := range refinedSleepHours {
		if hour == 22 {
			hasHour22 = true
			break
		}
	}

	if hasHour22 {
		t.Errorf("Hour 22 should not be included in sleep hours since it has activity (2 events)")
	}

	// The sleep should start from hour 23 or later (since 22:30 is quiet)
	// But note that 23:30 has a blip, so it might skip 23 too
	t.Logf("Quiet hours (hourly): %v", quietHours)
	t.Logf("Sleep buckets (half-hourly): %v", sleepBuckets)
	t.Logf("Refined sleep hours: %v", refinedSleepHours)

	// For Toronto (UTC-4), sleep hours should map to reasonable local times
	// UTC hours 23,0,1,2,3,4,5,6,7,8,9,10 would be 19:00-06:00 local
	// That's 7pm-6am which is reasonable sleep time
	expectedSleepStart := 23 // Should start at 23 UTC (7pm local) or later

	if len(refinedSleepHours) > 0 {
		// Find the earliest sleep hour (accounting for wraparound)
		minSleepHour := refinedSleepHours[0]
		for _, hour := range refinedSleepHours {
			// Check if this creates a better start considering wraparound
			if hour >= 22 && hour <= 23 {
				// Evening hours
				if hour < minSleepHour || minSleepHour < 12 {
					minSleepHour = hour
				}
			}
		}

		if minSleepHour == 22 {
			t.Errorf("Sleep should not start at hour 22 UTC due to activity, expected %d or later", expectedSleepStart)
		}
	}
}

func TestSleepDetectionWithTrailingActivity(t *testing.T) {
	tests := []struct {
		name            string
		halfHourCounts  map[float64]int
		expectedExclude []int // Hours that should NOT be in sleep
		description     string
	}{
		{
			name: "Activity in first half of hour",
			halfHourCounts: map[float64]int{
				// Fill all buckets to avoid false positives
				8.0: 10, 8.5: 12, // Morning activity
				9.0: 8, 9.5: 10,
				10.0: 12, 10.5: 8,
				11.0: 10, 11.5: 14,
				12.0: 8, 12.5: 6,
				13.0: 11, 13.5: 9,
				14.0: 7, 14.5: 12,
				15.0: 10, 15.5: 8,
				16.0: 14, 16.5: 11,
				17.0: 9, 17.5: 7,
				18.0: 8, 18.5: 10,
				19.0: 6, 19.5: 5,
				20.0: 4, 20.5: 3, // Evening activity
				21.0: 5, 21.5: 0, // Activity in first half - should exclude
				22.0: 0, 22.5: 0, // Quiet
				23.0: 0, 23.5: 0, // Quiet
				0.0: 0, 0.5: 0, // Quiet
				1.0: 0, 1.5: 0, // Quiet
				2.0: 0, 2.5: 0, // Quiet
				3.0: 0, 3.5: 0, // Quiet
				4.0: 0, 4.5: 0, // Quiet
				5.0: 0, 5.5: 0, // Quiet
				6.0: 0, 6.5: 0, // Quiet
				7.0: 0, 7.5: 2, // Waking up
			},
			expectedExclude: []int{21},
			description:     "Hour 21 has activity in first half, should not be sleep",
		},
		{
			name: "Activity in second half of hour",
			halfHourCounts: map[float64]int{
				// Fill all buckets to avoid false positives
				8.0: 10, 8.5: 12, // Morning activity
				9.0: 8, 9.5: 10,
				10.0: 12, 10.5: 8,
				11.0: 10, 11.5: 14,
				12.0: 8, 12.5: 6,
				13.0: 11, 13.5: 9,
				14.0: 7, 14.5: 12,
				15.0: 10, 15.5: 8,
				16.0: 14, 16.5: 11,
				17.0: 9, 17.5: 7,
				18.0: 8, 18.5: 10,
				19.0: 6, 19.5: 5,
				20.0: 0, 20.5: 3, // Activity in second half - should exclude
				21.0: 0, 21.5: 0, // Quiet
				22.0: 0, 22.5: 0, // Quiet
				23.0: 0, 23.5: 0, // Quiet
				0.0: 0, 0.5: 0, // Quiet
				1.0: 0, 1.5: 0, // Quiet
				2.0: 0, 2.5: 0, // Quiet
				3.0: 0, 3.5: 0, // Quiet
				4.0: 0, 4.5: 0, // Quiet
				5.0: 0, 5.5: 0, // Quiet
				6.0: 0, 6.5: 0, // Quiet
				7.0: 0, 7.5: 2, // Waking up
			},
			expectedExclude: []int{20},
			description:     "Hour 20 has activity in second half, should not be sleep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get base hourly counts
			hourCounts := aggregateHalfHoursToHours(tt.halfHourCounts)

			// Get quiet hours and sleep buckets
			quietHours := sleep.FindSleepHours(hourCounts)
			sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(tt.halfHourCounts)

			// Refine
			refinedHours := refineHourlySleepFromBuckets(quietHours, sleepBuckets, tt.halfHourCounts)

			// Check excluded hours
			for _, excludeHour := range tt.expectedExclude {
				for _, hour := range refinedHours {
					if hour == excludeHour {
						t.Errorf("%s: Hour %d should not be in refined sleep hours. %s",
							tt.name, excludeHour, tt.description)
					}
				}
			}

			t.Logf("%s: Refined hours: %v", tt.name, refinedHours)
		})
	}
}
