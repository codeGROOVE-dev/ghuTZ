package lunch

import (
	"testing"
)

// TestIdlePhysicistLunchDetection tests lunch detection with simplified test data
// This uses synthetic data to test the lunch detection algorithm
func TestIdlePhysicistLunchDetection(t *testing.T) {
	t.Skip("Skipping - use TestIdlePhysicistRealLunchDetection which has actual data")
	// Activity pattern from actual IdlePhysicist data (30-minute buckets in UTC)
	// For UTC-6 (MST), 13:00 local = 19:00 UTC, 13:30 local = 19:30 UTC
	halfHourCounts := map[float64]int{
		// UTC hours -> Mountain Time (UTC-7)
		18.0: 7,  // 11:00 MT
		18.5: 5,  // 11:30 MT
		19.0: 12, // 13:00 MST (1:00 PM) for UTC-6
		19.5: 5,  // 13:30 MST (1:30 PM) for UTC-6 - LUNCH BREAK
		20.0: 11, // 14:00 MST (2:00 PM) for UTC-6
		20.5: 6,  // 14:30 MST (2:30 PM) for UTC-6
		21.0: 10, // 14:00 MT (2:00 PM)
		21.5: 9,  // 14:30 MT
		22.0: 7,  // 15:00 MT (3:00 PM)
		22.5: 8,  // 15:30 MT
		23.0: 11, // 16:00 MT (4:00 PM)
		23.5: 3,  // 16:30 MT
		0.0:  6,  // 17:00 MT (5:00 PM)
		0.5:  1,  // 17:30 MT
		1.0:  0,  // 18:00 MT (6:00 PM)
		1.5:  1,  // 18:30 MT
		2.0:  0,  // 19:00 MT (7:00 PM)
		2.5:  0,  // 19:30 MT
		3.0:  2,  // 20:00 MT (8:00 PM)
		3.5:  1,  // 20:30 MT
		4.0:  0,  // 21:00 MT (9:00 PM)
		4.5:  2,  // 21:30 MT
		5.0:  1,  // 22:00 MT (10:00 PM) - sleep starts
		5.5:  1,  // 22:30 MT
		6.0:  2,  // 23:00 MT (11:00 PM) - sleep
		6.5:  5,  // 23:30 MT - sleep
		7.0:  2,  // 00:00 MT (midnight) - sleep
		7.5:  2,  // 00:30 MT - sleep
		8.0:  8,  // 01:00 MT - sleep
		8.5:  6,  // 01:30 MT - sleep
		9.0:  3,  // 02:00 MT - sleep (L marked in output)
		9.5:  5,  // 02:30 MT - sleep (L marked in output)
		10.0: 6,  // 03:00 MT - sleep (L marked in output)
		10.5: 9,  // 03:30 MT
		11.0: 8,  // 04:00 MT
		11.5: 15, // 04:30 MT - peak
		12.0: 10, // 05:00 MT
		12.5: 11, // 05:30 MT
		13.0: 12, // 06:00 MT
		13.5: 5,  // 06:30 MT
		14.0: 11, // 07:00 MT
		14.5: 6,  // 07:30 MT
		15.0: 7,  // 08:00 MT
		15.5: 11, // 08:30 MT
		16.0: 11, // 09:00 MT
		16.5: 9,  // 09:30 MT
		17.0: 11, // 10:00 MT
		17.5: 4,  // 10:30 MT
	}

	// Test for UTC-6 (Mountain Standard Time)
	offset := -6

	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourCounts, offset)

	// Convert UTC lunch times to local Mountain Time
	lunchStartLocal := lunchStart + float64(offset)
	lunchEndLocal := lunchEnd + float64(offset)

	// Normalize to 24-hour format
	if lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	if lunchEndLocal < 0 {
		lunchEndLocal += 24
	}

	// Check that we detect a lunch break

	if confidence <= 0 {
		t.Errorf("Failed to detect any lunch break for IdlePhysicist in Mountain Time")
	}

	// We expect lunch to be detected at 13:00-13:30 or 13:30-14:00
	// The drop from 12 to 5 events at 13:30 is significant
	if lunchStartLocal < 12.5 || lunchStartLocal > 13.5 {
		t.Errorf("Lunch start time incorrect: got %.1f MST, expected around 13:00-13:30 MST", lunchStartLocal)
	}

	if lunchEndLocal < 13.5 || lunchEndLocal > 14.5 {
		t.Errorf("Lunch end time incorrect: got %.1f MST, expected around 13:30-14:00 MST", lunchEndLocal)
	}

	// The confidence should be relatively high given the clear drop
	if confidence < 0.4 {
		t.Errorf("Lunch confidence too low: got %.2f, expected > 0.4 for clear lunch pattern", confidence)
	}

	t.Logf("Detected lunch: %.1f-%.1f MT (%.1f-%.1f UTC) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)
}

// TestIdlePhysicistLunchDetectionUTC6 tests that lunch detection also works for UTC-6
// (Mountain Standard Time or Central Daylight Time)
func TestIdlePhysicistLunchDetectionUTC6(t *testing.T) {
	// Same activity pattern
	halfHourCounts := map[float64]int{
		18.0: 7, 18.5: 5, 19.0: 12, 19.5: 5, 20.0: 11, 20.5: 6,
		21.0: 10, 21.5: 9, 22.0: 7, 22.5: 8, 23.0: 11, 23.5: 3,
		0.0: 6, 0.5: 1, 1.0: 0, 1.5: 1, 2.0: 0, 2.5: 0,
		3.0: 2, 3.5: 1, 4.0: 0, 4.5: 2, 5.0: 1, 5.5: 1,
		6.0: 2, 6.5: 5, 7.0: 2, 7.5: 2, 8.0: 8, 8.5: 6,
		9.0: 3, 9.5: 5, 10.0: 6, 10.5: 9, 11.0: 8, 11.5: 15,
		12.0: 10, 12.5: 11, 13.0: 12, 13.5: 5, 14.0: 11, 14.5: 6,
		15.0: 7, 15.5: 11, 16.0: 11, 16.5: 9, 17.0: 11, 17.5: 4,
	}

	// Test for UTC-6 (Mountain Standard Time)
	offset := -6

	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourCounts, offset)

	// Convert UTC lunch times to local time
	lunchStartLocal := lunchStart + float64(offset)
	lunchEndLocal := lunchEnd + float64(offset)

	// Normalize to 24-hour format
	if lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	if lunchEndLocal < 0 {
		lunchEndLocal += 24
	}

	// For UTC-6, the pattern shifts by 1 hour
	// 20.5 UTC becomes 14:30 local (2:30 PM)
	if confidence <= 0 {
		t.Errorf("Failed to detect lunch break for IdlePhysicist in UTC-6")
	}

	t.Logf("UTC-6: Detected lunch: %.1f-%.1f local (%.1f-%.1f UTC) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)
}
