package lunch

import (
	"testing"
)

func TestLunchDetectionTstromberg(t *testing.T) {
	// Actual 30-minute activity data from tstromberg
	halfHourData := map[float64]int{
		0.0: 0, 0.5: 0,     // 00:00, 00:30
		1.0: 0, 1.5: 0,     // 01:00, 01:30
		2.0: 0, 2.5: 0,     // 02:00, 02:30
		3.0: 0, 3.5: 0,     // 03:00, 03:30
		4.0: 0, 4.5: 0,     // 04:00, 04:30
		5.0: 0, 5.5: 0,     // 05:00, 05:30
		6.0: 0, 6.5: 0,     // 06:00, 06:30
		7.0: 0, 7.5: 0,     // 07:00, 07:30
		8.0: 0, 8.5: 0,     // 08:00, 08:30
		9.0: 0, 9.5: 0,     // 09:00, 09:30
		10.0: 0, 10.5: 0,   // 10:00, 10:30
		11.0: 0, 11.5: 0,   // 11:00, 11:30
		12.0: 5, 12.5: 13,  // 12:00, 12:30
		13.0: 29, 13.5: 2,  // 13:00, 13:30
		14.0: 3, 14.5: 17,  // 14:00, 14:30
		15.0: 31, 15.5: 5,  // 15:00, 15:30 (11:00-11:30 EDT)
		16.0: 6, 16.5: 19,  // 16:00, 16:30 (12:00-12:30 EDT)
		17.0: 29, 17.5: 29, // 17:00, 17:30 (13:00-13:30 EDT)
		18.0: 17, 18.5: 25, // 18:00, 18:30
		19.0: 52, 19.5: 51, // 19:00, 19:30
		20.0: 48, 20.5: 58, // 20:00, 20:30
		21.0: 77, 21.5: 46, // 21:00, 21:30
		22.0: 37, 22.5: 23, // 22:00, 22:30
		23.0: 12, 23.5: 5,  // 23:00, 23:30
	}

	// Test with UTC-4 (EDT) offset
	utcOffset := -4

	// The algorithm should detect lunch from 11:30-12:30 EDT

	lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourData, utcOffset)

	// Convert to local time for verification
	lunchStartLocal := (lunchStart + float64(utcOffset) + 24)
	for lunchStartLocal >= 24 {
		lunchStartLocal -= 24
	}
	for lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	
	lunchEndLocal := (lunchEnd + float64(utcOffset) + 24)
	for lunchEndLocal >= 24 {
		lunchEndLocal -= 24
	}
	for lunchEndLocal < 0 {
		lunchEndLocal += 24
	}

	t.Logf("Detected lunch: %.1f-%.1f UTC (%.1f-%.1f local), confidence: %.2f", 
		lunchStart, lunchEnd, lunchStartLocal, lunchEndLocal, confidence)

	// Should detect lunch at 11:30-12:30 local time
	if lunchStartLocal != 11.5 {
		t.Errorf("Expected lunch start at 11:30 local, got %.1f", lunchStartLocal)
	}
	if lunchEndLocal != 12.5 {
		t.Errorf("Expected lunch end at 12:30 local, got %.1f", lunchEndLocal)
	}
	if confidence < 0.5 {
		t.Errorf("Expected reasonable confidence, got %.2f", confidence)
	}
}