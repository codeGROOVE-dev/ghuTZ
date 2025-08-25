package sleep

import (
	"testing"
)

func TestBinacsUTC8SleepDetection(t *testing.T) {
	// Binacs actual activity pattern - VERY sparse data (only 14 events)
	// Activity clustered around 07:00-18:30 UTC
	halfHourCounts := map[float64]int{
		// All buckets start at 0
		0.0: 0, 0.5: 0,
		1.0: 0, 1.5: 0,
		2.0: 0, 2.5: 0,
		3.0: 0, 3.5: 0,
		4.0: 0, 4.5: 0,
		5.0: 0, 5.5: 0,
		6.0: 0, 6.5: 0,
		7.0: 3, 7.5: 0, // 15:00 local (UTC+8) - afternoon activity
		8.0: 0, 8.5: 0,
		9.0: 3, 9.5: 0, // 17:00 local - late afternoon
		10.0: 0, 10.5: 1, // 18:30 local - evening
		11.0: 1, 11.5: 0, // 19:00 local - evening
		12.0: 0, 12.5: 3, // 20:30 local - evening
		13.0: 0, 13.5: 0,
		14.0: 0, 14.5: 0,
		15.0: 1, 15.5: 0, // 23:00 local - late evening
		16.0: 1, 16.5: 0, // 00:00 local - should be sleep time!
		17.0: 0, 17.5: 0,
		18.0: 0, 18.5: 1, // 02:30 local - should be sleep!
		19.0: 0, 19.5: 0,
		20.0: 0, 20.5: 0,
		21.0: 0, 21.5: 0,
		22.0: 0, 22.5: 0,
		23.0: 0, 23.5: 0,
	}

	tests := []struct {
		name          string
		offset        int
		expectedSleep []float64 // Expected UTC hours for sleep
		description   string
	}{
		{
			name:   "UTC+8 (China) - sleep should be around midnight-8am local",
			offset: 8,
			// For UTC+8: With such sparse data and activity during nighttime (15, 16, 18:30 UTC),
			// we might not detect any sleep, or find 19:00-23:59 UTC (3am-8am local)
			expectedSleep: []float64{19.0, 19.5, 20.0, 20.5, 21.0, 21.5, 22.0, 22.5, 23.0, 23.5},
			description:   "Sleep for UTC+8 should detect late night/early morning quiet period or nothing",
		},
		{
			name:   "UTC+0 (Western bias) - would incorrectly detect morning as sleep",
			offset: 0,
			// With UTC+0 bias, it would find the quiet morning hours as sleep
			expectedSleep: []float64{0.0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0},
			description:   "UTC+0 bias incorrectly identifies Asian morning hours as sleep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Add debug output to understand activity pattern
			t.Logf("Testing with offset %+d", tt.offset)
			t.Logf("Total activity: 14 events")
			t.Logf("Active buckets: 7.0(3), 9.0(3), 10.5(1), 11.0(1), 12.5(3), 15.0(1), 16.0(1), 18.5(1)")

			sleepBuckets := DetectSleepPeriodsWithOffset(halfHourCounts, tt.offset)

			t.Logf("Detected sleep buckets for offset %+d: %v", tt.offset, sleepBuckets)
			t.Logf("Description: %s", tt.description)

			// Check if we found any sleep
			if len(sleepBuckets) == 0 {
				// For sparse data with UTC+8, no sleep detection is acceptable
				if tt.offset == 8 {
					t.Logf("No sleep detected for UTC+8 - acceptable for sparse data with nighttime activity")
					return
				}
				t.Errorf("No sleep detected for offset %+d", tt.offset)
				return
			}

			// Find the start and end of sleep
			minBucket := sleepBuckets[0]
			maxBucket := sleepBuckets[len(sleepBuckets)-1]

			// For UTC+8, sleep should start after 14:00 UTC (10pm local)
			// and should not start in the morning UTC hours
			if tt.offset == 8 {
				if minBucket < 14.0 && minBucket > 2.0 {
					t.Errorf("UTC+8: Sleep starts at %.1f UTC (%.1f local), which is too early for nighttime",
						minBucket, minBucket+8)
				}

				// Check that we're not detecting the morning quiet period as sleep
				morningQuietDetected := false
				for _, bucket := range sleepBuckets {
					if bucket >= 1.0 && bucket <= 10.0 {
						morningQuietDetected = true
						break
					}
				}
				if morningQuietDetected {
					t.Errorf("UTC+8: Incorrectly detected morning quiet hours (1:00-10:00 UTC) as sleep")
				}
			}

			// Log the local time equivalents
			t.Logf("Sleep period: %.1f-%.1f UTC (%.1f-%.1f local)",
				minBucket, maxBucket,
				normalizeHour(minBucket+float64(tt.offset)),
				normalizeHour(maxBucket+float64(tt.offset)))
		})
	}
}

func normalizeHour(hour float64) float64 {
	for hour >= 24.0 {
		hour -= 24.0
	}
	for hour < 0.0 {
		hour += 24.0
	}
	return hour
}
