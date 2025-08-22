package sleep

import (
	"testing"
)

// TestXnoxSleepDetection tests the specific case where xnox should have 05:00-09:30 rest hours
func TestXnoxSleepDetection(t *testing.T) {
	// xnox's actual half-hourly activity pattern (UTC times converted to local UTC+1)
	halfHourCounts := map[float64]int{
		// Late night activity (UTC times, which is local UTC+1 times - 1)
		0.0: 23, // 01:00 local - peak activity
		0.5: 25, // 01:30 local
		1.0: 33, // 02:00 local - morning peak
		1.5: 4,  // 02:30 local
		2.0: 11, // 03:00 local
		2.5: 12, // 03:30 local
		3.0: 6,  // 04:00 local
		3.5: 5,  // 04:30 local
		4.0: 6,  // 05:00 local
		4.5: 4,  // 05:30 local

		// Rest period should start here
		5.0: 0, // 06:00 local (05:00 rest start)
		5.5: 2, // 06:30 local (05:30)
		6.0: 1, // 07:00 local (06:00)
		6.5: 1, // 07:30 local (06:30)
		7.0: 0, // 08:00 local (07:00)
		7.5: 0, // 08:30 local (07:30)
		8.0: 0, // 09:00 local (08:00)
		8.5: 1, // 09:30 local (08:30)
		9.0: 2, // 10:00 local (09:00)
		// Rest period should end here at 09:30 local

		9.5:  6,  // 10:30 local (09:30) - activity resumes
		10.0: 9,  // 11:00 local
		10.5: 3,  // 11:30 local
		11.0: 5,  // 12:00 local
		11.5: 28, // 12:30 local - pre-lunch activity
		12.0: 11, // 13:00 local - lunch dip
		12.5: 21, // 13:30 local
		// ... rest of day continues
	}

	sleepBuckets := DetectSleepPeriodsWithHalfHours(halfHourCounts)

	// We expect sleep buckets from 5.0 to 9.0 (05:00 to 09:30 local time)
	expectedBuckets := []float64{5.0, 5.5, 6.0, 6.5, 7.0, 7.5, 8.0, 8.5, 9.0}

	if len(sleepBuckets) == 0 {
		t.Fatal("No sleep buckets detected, but should have detected 05:00-09:30 rest period")
	}

	// Check that we detected a rest period
	t.Logf("Detected sleep buckets: %v", sleepBuckets)
	t.Logf("Expected sleep buckets: %v", expectedBuckets)

	// Verify we have the right number of buckets (9 half-hour periods = 4.5 hours)
	if len(sleepBuckets) < 8 {
		t.Errorf("Expected at least 8 sleep buckets for 4+ hour rest period, got %d", len(sleepBuckets))
	}

	// Check that the rest period includes the key quiet times
	sleepSet := make(map[float64]bool)
	for _, bucket := range sleepBuckets {
		sleepSet[bucket] = true
	}

	// Should include the core quiet hours
	criticalBuckets := []float64{6.0, 7.0, 7.5, 8.0} // 06:00, 07:00, 07:30, 08:00 local
	for _, bucket := range criticalBuckets {
		if !sleepSet[bucket] {
			t.Errorf("Sleep period should include bucket %.1f (critical quiet time)", bucket)
		}
	}

	// Should include start of rest period
	if !sleepSet[5.0] {
		t.Errorf("Sleep period should include bucket 5.0 (start of rest at 05:00 local)")
	}

	// Should include most of rest period ending
	if !sleepSet[9.0] {
		t.Errorf("Sleep period should include bucket 9.0 (end of rest at 09:00 local)")
	}
}
