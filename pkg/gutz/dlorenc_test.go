package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/sleep"
)

func TestDlorencSleepDetection(t *testing.T) {
	// Actual half-hour activity data from dlorenc
	// Shows minimal activity (2 events) at 05:30 UTC that shouldn't break an 8-hour sleep period
	halfHourCounts := map[float64]int{
		0.0: 0, 0.5: 0, // Quiet
		1.0: 0, 1.5: 0, // Quiet
		2.0: 0, 2.5: 0, // Quiet
		3.0: 0, 3.5: 0, // Quiet
		4.0: 0, 4.5: 0, // Quiet
		5.0: 0, 5.5: 2, // Minor blip - 2 events at 05:30
		6.0: 0, 6.5: 0, // Quiet again
		7.0: 2, 7.5: 1, // Activity starting
		8.0: 14, 8.5: 12, // Full activity
		9.0: 8, 9.5: 17, // Peak activity
		10.0: 2, 10.5: 13,
		11.0: 11, 11.5: 10, // Lunch approaching
		12.0: 7, 12.5: 12, // Lunch period
		13.0: 12, 13.5: 12,
		14.0: 1, 14.5: 4,
		15.0: 8, 15.5: 13,
		16.0: 7, 16.5: 13,
		17.0: 13, 17.5: 5,
		18.0: 6, 18.5: 9,
		19.0: 7, 19.5: 1,
		20.0: 6, 20.5: 11,
		21.0: 4, 21.5: 9,
		22.0: 5, 22.5: 3,
		23.0: 1, 23.5: 0, // Winding down
	}

	// Add debugging for quiet bucket detection
	var quietBuckets []float64
	for bucket := 0.0; bucket < 24.0; bucket += 0.5 {
		count := halfHourCounts[bucket]
		if count <= 1 {
			quietBuckets = append(quietBuckets, bucket)
		}
	}
	t.Logf("Quiet buckets (0-1 events): %v", quietBuckets)

	// Test with current detection
	sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(halfHourCounts)

	t.Logf("Sleep buckets detected: %v", sleepBuckets)
	t.Logf("Data at 23:00-23:30: %d events", halfHourCounts[23.0])
	t.Logf("Data at 23:30-24:00: %d events", halfHourCounts[23.5])
	t.Logf("Data at 22:30-23:00: %d events", halfHourCounts[22.5])

	// We should get a continuous sleep period from approximately 23:00-23:30 to 06:30-07:00 UTC
	// The minor blip at 05:30 (2 events) should not break the sleep period
	// After 07:00, activity ramps up continuously (2, 1, then increases) with no gap
	// This gives exactly 8 hours of sleep (23:00-07:00)

	// Check that we have approximately 8 hours (16 half-hour buckets) of sleep
	expectedBuckets := 16 // 8 hours * 2 buckets per hour
	tolerance := 2        // Allow +/- 1 hour tolerance
	if len(sleepBuckets) < expectedBuckets-tolerance || len(sleepBuckets) > expectedBuckets+tolerance {
		t.Errorf("Expected approximately 8 hours of sleep (16±2 buckets), got %d buckets (%.1f hours)",
			len(sleepBuckets), float64(len(sleepBuckets))/2)
	}

	// For Pacific time (UTC-8), sleep should be reasonable local hours
	// UTC 23:30-06:30 would be 15:30-22:30 Pacific which is wrong
	// So dlorenc is likely Eastern (UTC-5) where this would be 18:30-01:30
	// Or Central (UTC-6) where this would be 17:30-00:30
	// Let's verify the sleep period makes sense

	minBucket := 24.0
	maxBucket := 0.0
	for _, bucket := range sleepBuckets {
		if bucket < minBucket {
			minBucket = bucket
		}
		if bucket > maxBucket {
			maxBucket = bucket
		}
	}

	t.Logf("Sleep period: %.1f to %.1f UTC", minBucket, maxBucket)

	// The minor blip at 5.5 should not split the sleep period
	// We should have one continuous period, not two separate ones
	hasGap := false
	if len(sleepBuckets) > 1 {
		for i := 1; i < len(sleepBuckets); i++ {
			prevBucket := sleepBuckets[i-1]
			currBucket := sleepBuckets[i]

			// Check for gaps (accounting for wraparound)
			if currBucket != prevBucket+0.5 && !(prevBucket == 23.5 && currBucket == 0.0) {
				hasGap = true
				t.Logf("Gap detected between %.1f and %.1f", prevBucket, currBucket)
			}
		}
	}

	if hasGap {
		t.Error("Sleep period should be continuous despite minor blip at 05:30")
	}
}

func TestSleepDetectionIgnoresMinorBlips(t *testing.T) {
	tests := []struct {
		name             string
		halfHourCounts   map[float64]int
		minExpectedHours float64
		description      string
	}{
		{
			name: "Single minor blip in 8-hour sleep",
			halfHourCounts: map[float64]int{
				// Fill in all buckets to avoid false positives
				9.0: 15, 9.5: 10, // Day activity
				10.0: 12, 10.5: 8,
				11.0: 10, 11.5: 14,
				12.0: 8, 12.5: 6,
				13.0: 11, 13.5: 9,
				14.0: 7, 14.5: 12,
				15.0: 10, 15.5: 8,
				16.0: 14, 16.5: 11,
				17.0: 9, 17.5: 7,
				18.0: 8, 18.5: 10,
				19.0: 6, 19.5: 9,
				20.0: 7, 20.5: 5,
				21.0: 8, 21.5: 4,
				22.0: 5, 22.5: 2, // Evening activity
				23.0: 1, 23.5: 0, // Winding down
				0.0: 0, 0.5: 0, // Sleep
				1.0: 0, 1.5: 0, // Sleep
				2.0: 0, 2.5: 0, // Sleep
				3.0: 0, 3.5: 0, // Sleep
				4.0: 0, 4.5: 0, // Sleep
				5.0: 0, 5.5: 2, // Minor blip (2 events)
				6.0: 0, 6.5: 0, // Back to sleep
				7.0: 0, 7.5: 3, // Starting to wake
				8.0: 10, 8.5: 12, // Full activity
			},
			minExpectedHours: 7.0, // Should get at least 7 hours despite blip
			description:      "Minor blip of 2 events shouldn't break sleep period",
		},
		{
			name: "Bathroom break pattern",
			halfHourCounts: map[float64]int{
				// Fill in all day activity
				9.0: 12, 9.5: 8,
				10.0: 10, 10.5: 14,
				11.0: 8, 11.5: 11,
				12.0: 9, 12.5: 7,
				13.0: 10, 13.5: 12,
				14.0: 8, 14.5: 9,
				15.0: 11, 15.5: 10,
				16.0: 7, 16.5: 13,
				17.0: 9, 17.5: 8,
				18.0: 10, 18.5: 6,
				19.0: 8, 19.5: 7,
				20.0: 9, 20.5: 5,
				21.0: 6, 21.5: 4,
				22.0: 5, 22.5: 2,
				23.0: 1, 23.5: 0, // Going to bed
				0.0: 0, 0.5: 0, // Sleep
				1.0: 0, 1.5: 0, // Sleep
				2.0: 0, 2.5: 0, // Sleep
				3.0: 0, 3.5: 1, // Bathroom break (1 event)
				4.0: 0, 4.5: 0, // Back to sleep
				5.0: 0, 5.5: 0, // Sleep
				6.0: 0, 6.5: 0, // Sleep
				7.0: 0, 7.5: 2, // Waking up
				8.0: 8, 8.5: 10, // Full activity
			},
			minExpectedHours: 7.5, // Should get full sleep period
			description:      "Single event (bathroom break) shouldn't break sleep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sleepBuckets := sleep.DetectSleepPeriodsWithHalfHours(tt.halfHourCounts)

			hoursOfSleep := float64(len(sleepBuckets)) / 2.0

			t.Logf("%s: Detected %.1f hours of sleep", tt.name, hoursOfSleep)
			t.Logf("Sleep buckets: %v", sleepBuckets)

			if hoursOfSleep < tt.minExpectedHours {
				t.Errorf("%s: Expected at least %.1f hours of sleep, got %.1f. %s",
					tt.name, tt.minExpectedHours, hoursOfSleep, tt.description)
			}
		})
	}
}
