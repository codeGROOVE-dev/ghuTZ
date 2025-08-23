package gutz

import (
	"sort"
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

	// The algorithm detects the longest continuous quiet period
	// In this case, it's the morning hours 0-9 (20 consecutive quiet half-hour buckets)
	// This is a valid sleep period for someone in Toronto (UTC-4):
	// UTC 0-9 = 20:00-05:00 local time (8pm-5am)

	t.Logf("Quiet hours (hourly): %v", quietHours)
	t.Logf("Sleep buckets (half-hourly): %v", sleepBuckets)
	t.Logf("Refined sleep hours: %v", refinedSleepHours)

	// Verify we found a reasonable sleep period
	if len(refinedSleepHours) < 6 {
		t.Errorf("Expected at least 6 hours of sleep, got %d", len(refinedSleepHours))
	}

	// The algorithm correctly identifies hours 0-9 as the main sleep period
	// For Toronto (UTC-4), this maps to 20:00-05:00 local (8pm-5am)
	// which is a reasonable sleep schedule
}

func TestSleepStartsWithQuietBuckets(t *testing.T) {
	// Test case for rebelopsio bug: sleep was starting at 21:00 with 3 activities
	// instead of 21:30 with 0 activities
	// Key pattern: 0.5=0, 1.0=3, 1.5=0 - should skip 1.0 and start at 1.5
	halfHourCounts := map[float64]int{
		// Active period before sleep
		23.5: 2,
		0.0:  4, 0.5: 0, // 0.5 is quiet but isolated
		1.0: 3, 1.5: 0, // 1.0 has 3 activities, 1.5 starts continuous quiet
		2.0: 0, 2.5: 0,
		3.0: 0, 3.5: 0,
		4.0: 0, 4.5: 0,
		5.0: 0, 5.5: 0,
		6.0: 0, 6.5: 0,
		7.0: 0, 7.5: 0,
		8.0: 0, 8.5: 0,
		9.0: 0, 9.5: 0,
		10.0: 1, 10.5: 5, // Wake up
		11.0: 2, 11.5: 6, // Morning activity
		// Fill in day with activity to prevent morning sleep detection
		12.0: 8, 12.5: 10,
		13.0: 9, 13.5: 12,
		14.0: 6, 14.5: 4,
		15.0: 10, 15.5: 8,
		16.0: 7, 16.5: 10,
		17.0: 9, 17.5: 9,
		18.0: 14, 18.5: 15,
		19.0: 12, 19.5: 7,
		20.0: 10, 20.5: 23,
		21.0: 12, 21.5: 4,
		22.0: 6, 22.5: 4,
		23.0: 5,
	}

	// Detect sleep periods
	sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(halfHourCounts)

	// Sleep should NOT start at 0.5 (isolated quiet bucket after activity at 0.0)
	// Sleep should NOT start at 1.0 (has 3 activities)
	// Sleep should start at 1.5 (first of two consecutive quiet buckets)
	if len(sleepBuckets) == 0 {
		t.Fatal("No sleep buckets detected")
	}

	// Sort buckets to find the first one
	sort.Float64s(sleepBuckets)
	firstBucket := sleepBuckets[0]

	t.Logf("Sleep buckets detected: %v", sleepBuckets)
	t.Logf("First sleep bucket: %.1f (has %d activities)", firstBucket, halfHourCounts[firstBucket])

	// The first bucket should be 1.5 UTC (21:30 local, where continuous quiet period starts)
	// NOT 0.5 (isolated quiet) or 1.0 (has 3 activities)
	if firstBucket == 1.0 {
		t.Errorf("Sleep started at bucket 1.0 which has 3 activities - should skip to 1.5")
	}

	// Sleep should start with a quiet bucket (0-2 activities)
	if halfHourCounts[firstBucket] > 2 {
		t.Errorf("Sleep started with an active bucket (%.1f has %d activities)", firstBucket, halfHourCounts[firstBucket])
	}

	// Verify the sleep period doesn't include the 1.0 bucket with 3 activities as the start
	if firstBucket == 0.5 && len(sleepBuckets) > 1 && sleepBuckets[1] == 1.0 {
		t.Errorf("Sleep starts at 0.5 followed by 1.0 (3 activities) - not a valid quiet start")
	}

	// Specifically verify that we skip 1.0 and start at 1.5
	if firstBucket != 1.5 {
		t.Errorf("Expected sleep to start at 1.5 (first of consecutive quiet buckets), got %.1f", firstBucket)
	}
}

func TestSleepDetectionWithTrailingActivity(t *testing.T) {
	tests := []struct {
		name            string
		halfHourCounts  map[float64]int
		expectedInclude []int // Hours that SHOULD be in sleep (with new algorithm)
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
				20.0: 4, 20.5: 3, // Evening activity - TWO consecutive buckets with >2
				21.0: 5, 21.5: 0, // Activity in first half - now included (only one bucket >2)
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
			expectedInclude: []int{0, 1, 2, 3, 4, 5, 6, 7},
			description:     "Algorithm picks longest continuous quiet period (morning hours)",
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
				20.0: 0, 20.5: 3, // Activity in second half - now included (only one bucket >2)
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
			expectedInclude: []int{0, 1, 2, 3, 4, 5, 6, 7},
			description:     "Algorithm picks longest continuous quiet period (morning hours)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get base hourly counts
			hourCounts := aggregateHalfHoursToHours(tt.halfHourCounts)

			// Get quiet hours and sleep buckets
			quietHours := sleep.FindSleepHours(hourCounts)
			sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(tt.halfHourCounts)

			// Debug output
			t.Logf("%s: quietHours from FindSleepHours: %v", tt.name, quietHours)
			t.Logf("%s: sleepBuckets from DetectSleepPeriodsWithHalfHours: %v", tt.name, sleepBuckets)
			t.Logf("%s: len(sleepBuckets): %d", tt.name, len(sleepBuckets))

			// Refine
			refinedHours := refineHourlySleepFromBuckets(quietHours, sleepBuckets, tt.halfHourCounts)

			// Check that expected hours are included
			refinedMap := make(map[int]bool)
			for _, hour := range refinedHours {
				refinedMap[hour] = true
			}

			for _, includeHour := range tt.expectedInclude {
				if !refinedMap[includeHour] {
					t.Errorf("%s: Hour %d should be in refined sleep hours. %s",
						tt.name, includeHour, tt.description)
				}
			}

			t.Logf("%s: Refined hours: %v", tt.name, refinedHours)
			t.Logf("%s: Expected to include: %v", tt.name, tt.expectedInclude)
		})
	}
}
