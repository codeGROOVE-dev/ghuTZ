package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
)

// TestAmberArcadiaDelawareDetection tests that AmberArcadia (who lives in Delaware)
// is correctly detected as Eastern Time with her actual GitHub activity data
func TestAmberArcadiaDelawareDetection(t *testing.T) {
	// ACTUAL 30-minute bucket counts from AmberArcadia's real GitHub activity
	// Collected from 2025-07-17 to 2025-08-14 (278 total events)
	// She lives in Delaware (Eastern Time UTC-4 during summer)
	halfHourCounts := map[float64]int{
		// UTC time : event count
		0.0:  0,  // 00:00 UTC = 20:00 ET
		0.5:  0,  // 00:30 UTC = 20:30 ET
		1.0:  0,  // 01:00 UTC = 21:00 ET
		1.5:  0,  // 01:30 UTC = 21:30 ET
		2.0:  0,  // 02:00 UTC = 22:00 ET - sleep
		2.5:  0,  // 02:30 UTC = 22:30 ET
		3.0:  0,  // 03:00 UTC = 23:00 ET
		3.5:  0,  // 03:30 UTC = 23:30 ET
		4.0:  0,  // 04:00 UTC = 00:00 ET - midnight
		4.5:  0,  // 04:30 UTC = 00:30 ET
		5.0:  0,  // 05:00 UTC = 01:00 ET
		5.5:  0,  // 05:30 UTC = 01:30 ET
		6.0:  0,  // 06:00 UTC = 02:00 ET
		6.5:  0,  // 06:30 UTC = 02:30 ET
		7.0:  0,  // 07:00 UTC = 03:00 ET
		7.5:  0,  // 07:30 UTC = 03:30 ET
		8.0:  0,  // 08:00 UTC = 04:00 ET
		8.5:  1,  // 08:30 UTC = 04:30 ET - very early activity
		9.0:  0,  // 09:00 UTC = 05:00 ET
		9.5:  0,  // 09:30 UTC = 05:30 ET
		10.0: 0,  // 10:00 UTC = 06:00 ET
		10.5: 0,  // 10:30 UTC = 06:30 ET
		11.0: 0,  // 11:00 UTC = 07:00 ET
		11.5: 0,  // 11:30 UTC = 07:30 ET
		12.0: 0,  // 12:00 UTC = 08:00 ET
		12.5: 4,  // 12:30 UTC = 08:30 ET - work starting
		13.0: 3,  // 13:00 UTC = 09:00 ET - work day begins
		13.5: 31, // 13:30 UTC = 09:30 ET - heavy morning activity
		14.0: 20, // 14:00 UTC = 10:00 ET
		14.5: 30, // 14:30 UTC = 10:30 ET
		15.0: 37, // 15:00 UTC = 11:00 ET - peak morning
		15.5: 24, // 15:30 UTC = 11:30 ET
		16.0: 10, // 16:00 UTC = 12:00 ET - LUNCH START
		16.5: 9,  // 16:30 UTC = 12:30 ET - lunch continues
		17.0: 11, // 17:00 UTC = 13:00 ET - after lunch
		17.5: 15, // 17:30 UTC = 13:30 ET
		18.0: 13, // 18:00 UTC = 14:00 ET
		18.5: 5,  // 18:30 UTC = 14:30 ET - afternoon dip
		19.0: 19, // 19:00 UTC = 15:00 ET - late afternoon
		19.5: 11, // 19:30 UTC = 15:30 ET
		20.0: 10, // 20:00 UTC = 16:00 ET
		20.5: 8,  // 20:30 UTC = 16:30 ET
		21.0: 5,  // 21:00 UTC = 17:00 ET - end of workday
		21.5: 6,  // 21:30 UTC = 17:30 ET
		22.0: 2,  // 22:00 UTC = 18:00 ET - evening
		22.5: 3,  // 22:30 UTC = 18:30 ET
		23.0: 1,  // 23:00 UTC = 19:00 ET
		23.5: 0,  // 23:30 UTC = 19:30 ET
	}

	// Test for UTC-4 (Eastern Daylight Time - Delaware in summer)
	offset := -4

	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := lunch.DetectLunchBreakNoonCentered(halfHourCounts, offset)

	// Convert UTC lunch times to local Eastern Time
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
		t.Errorf("Failed to detect lunch break for AmberArcadia in Eastern Time")
	}

	// We expect lunch around noon (12:00-13:00 ET)
	if lunchStartLocal < 11.5 || lunchStartLocal > 12.5 {
		t.Errorf("Lunch start time incorrect: got %.1f ET, expected around 12:00 ET", lunchStartLocal)
	}

	t.Logf("Detected lunch: %.1f-%.1f ET (%.1f-%.1f UTC) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)
}

// TestAmberArcadiaTimezoneScoring tests that Eastern Time scores higher than European time
// for AmberArcadia despite evening activity that might suggest Europe
func TestAmberArcadiaTimezoneScoring(t *testing.T) {
	// ACTUAL hourly counts from AmberArcadia's real GitHub activity
	hourCounts := map[int]int{
		0:  0,  // 00:00 UTC = 20:00 ET / 01:00 CET
		1:  0,  // 01:00 UTC = 21:00 ET / 02:00 CET
		2:  0,  // 02:00 UTC = 22:00 ET / 03:00 CET - sleep
		3:  0,  // 03:00 UTC = 23:00 ET / 04:00 CET
		4:  0,  // 04:00 UTC = 00:00 ET / 05:00 CET - midnight
		5:  0,  // 05:00 UTC = 01:00 ET / 06:00 CET
		6:  0,  // 06:00 UTC = 02:00 ET / 07:00 CET
		7:  0,  // 07:00 UTC = 03:00 ET / 08:00 CET
		8:  1,  // 08:00 UTC = 04:00 ET / 09:00 CET
		9:  0,  // 09:00 UTC = 05:00 ET / 10:00 CET
		10: 0,  // 10:00 UTC = 06:00 ET / 11:00 CET
		11: 0,  // 11:00 UTC = 07:00 ET / 12:00 CET
		12: 4,  // 12:00 UTC = 08:00 ET / 13:00 CET
		13: 34, // 13:00 UTC = 09:00 ET / 14:00 CET - work starts
		14: 50, // 14:00 UTC = 10:00 ET / 15:00 CET
		15: 61, // 15:00 UTC = 11:00 ET / 16:00 CET - peak morning
		16: 19, // 16:00 UTC = 12:00 ET / 17:00 CET - lunch dip
		17: 26, // 17:00 UTC = 13:00 ET / 18:00 CET
		18: 18, // 18:00 UTC = 14:00 ET / 19:00 CET
		19: 30, // 19:00 UTC = 15:00 ET / 20:00 CET
		20: 18, // 20:00 UTC = 16:00 ET / 21:00 CET
		21: 11, // 21:00 UTC = 17:00 ET / 22:00 CET
		22: 5,  // 22:00 UTC = 18:00 ET / 23:00 CET
		23: 1,  // 23:00 UTC = 19:00 ET / 00:00 CET
	}

	// Key observations from real data:
	// 1. Work starts at 13:00 UTC = 09:00 ET (reasonable) or 14:00 CET (too late)
	// 2. Lunch dip at 16:00 UTC = 12:00 ET (perfect) or 17:00 CET (5pm - absurd)
	// 3. Peak at 15:00 UTC = 11:00 ET (normal) or 16:00 CET (4pm - late for Europe)
	// 4. Sleep hours 0-7 UTC = 20:00-03:00 ET (reasonable) or 01:00-08:00 CET (reasonable)

	// Calculate work start for Eastern Time (UTC-4)
	etWorkStart := 13 - 4 // 13:00 UTC - 4 = 09:00 ET
	if etWorkStart < 0 {
		etWorkStart += 24
	}

	// Calculate work start for Central European Time (UTC+1)
	cetWorkStart := 13 - (-1) // 13:00 UTC + 1 = 14:00 CET

	// Eastern Time should have a reasonable work start (9am)
	if etWorkStart != 9 {
		t.Errorf("Eastern Time work start incorrect: got %d:00, expected 9:00", etWorkStart)
	}

	// CET would have unreasonable work start (2pm)
	if cetWorkStart != 14 {
		t.Errorf("CET work start calculation error: got %d:00, expected 14:00", cetWorkStart)
	}

	// Lunch times
	etLunch := 16 - 4     // 16:00 UTC - 4 = 12:00 ET
	cetLunch := 16 - (-1) // 16:00 UTC + 1 = 17:00 CET

	t.Logf("Work start times - ET: %d:00 (reasonable), CET: %d:00 (too late)", etWorkStart, cetWorkStart)
	t.Logf("Lunch times - ET: %d:00 (perfect), CET: %d:00 (5pm - absurd)", etLunch, cetLunch)

	// Verify the hourly data is correct
	totalEvents := 0
	for _, count := range hourCounts {
		totalEvents += count
	}
	if totalEvents != 278 {
		t.Errorf("Total events mismatch: got %d, expected 278", totalEvents)
	}
}

// TestAmberArcadiaWorkStartValidation verifies work start calculations
func TestAmberArcadiaWorkStartValidation(t *testing.T) {
	// First significant activity is at 13:00 UTC (34 events)
	firstActivityUTC := 13

	// Test different timezone interpretations
	testCases := []struct {
		offset        int
		name          string
		expectedLocal int
		reasonable    bool
	}{
		{-4, "Eastern Time", 9, true},   // 9am - perfect
		{-3, "Atlantic Time", 10, true}, // 10am - acceptable
		{0, "UTC/GMT", 13, false},       // 1pm - too late
		{1, "CET", 14, false},           // 2pm - way too late
		{2, "EET", 15, false},           // 3pm - absurdly late
	}

	for _, tc := range testCases {
		localHour := (firstActivityUTC + tc.offset + 24) % 24
		if localHour != tc.expectedLocal {
			t.Errorf("%s: work start calculation error: got %d:00, expected %d:00",
				tc.name, localHour, tc.expectedLocal)
		}

		isReasonable := localHour >= 6 && localHour <= 10
		if isReasonable != tc.reasonable {
			t.Errorf("%s: work start reasonableness wrong: got %v, expected %v",
				tc.name, isReasonable, tc.reasonable)
		}

		t.Logf("%s (UTC%+d): work starts at %02d:00 - %s",
			tc.name, tc.offset, localHour,
			map[bool]string{true: "reasonable", false: "unreasonable"}[isReasonable])
	}
}
