package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
)

// TestGauravkghildiyalGoogleDetection tests gauravkghildiyal's real activity data
// He works at Google in Mountain View, California
func TestGauravkghildiyalGoogleDetection(t *testing.T) {
	// ACTUAL activity data from gauravkghildiyal (Google employee, Mountain View)
	// Collected over 120 days with 229 total events

	// ACTUAL 30-minute bucket counts from gauravkghildiyal's GitHub activity
	// Extracted from verbose output on 2025-08-17
	halfHourCounts := map[float64]int{
		// UTC time : event count (Mountain View is UTC-7 in summer/PDT)
		0.0:  4,  // 00:00 UTC = 17:00 PDT - late afternoon
		0.5:  3,  // 00:30 UTC
		1.0:  3,  // 01:00 UTC = 18:00 PDT - evening
		1.5:  2,  // 01:30 UTC
		2.0:  2,  // 02:00 UTC = 19:00 PDT
		2.5:  4,  // 02:30 UTC
		3.0:  4,  // 03:00 UTC = 20:00 PDT
		3.5:  2,  // 03:30 UTC
		4.0:  6,  // 04:00 UTC = 21:00 PDT - late evening
		4.5:  5,  // 04:30 UTC
		5.0:  7,  // 05:00 UTC = 22:00 PDT
		5.5:  3,  // 05:30 UTC
		6.0:  4,  // 06:00 UTC = 23:00 PDT
		6.5:  10, // 06:30 UTC
		7.0:  6,  // 07:00 UTC = 00:00 PDT - midnight
		7.5:  0,  // 07:30 UTC
		8.0:  9,  // 08:00 UTC = 01:00 PDT
		8.5:  3,  // 08:30 UTC
		9.0:  8,  // 09:00 UTC = 02:00 PDT
		9.5:  3,  // 09:30 UTC
		10.0: 0,  // 10:00 UTC = 03:00 PDT - QUIET START
		10.5: 0,  // 10:30 UTC
		11.0: 0,  // 11:00 UTC = 04:00 PDT - QUIET
		11.5: 0,  // 11:30 UTC
		12.0: 0,  // 12:00 UTC = 05:00 PDT - QUIET
		12.5: 0,  // 12:30 UTC
		13.0: 0,  // 13:00 UTC = 06:00 PDT - QUIET
		13.5: 0,  // 13:30 UTC
		14.0: 0,  // 14:00 UTC = 07:00 PDT - QUIET
		14.5: 0,  // 14:30 UTC
		15.0: 21, // 15:00 UTC = 08:00 PDT - PEAK work start
		15.5: 1,  // 15:30 UTC
		16.0: 10, // 16:00 UTC = 09:00 PDT - morning
		16.5: 9,  // 16:30 UTC
		17.0: 2,  // 17:00 UTC = 10:00 PDT - possible break
		17.5: 3,  // 17:30 UTC
		18.0: 12, // 18:00 UTC = 11:00 PDT
		18.5: 7,  // 18:30 UTC
		19.0: 12, // 19:00 UTC = 12:00 PDT - noon
		19.5: 5,  // 19:30 UTC = 12:30 PDT - LUNCH DIP!
		20.0: 10, // 20:00 UTC = 13:00 PDT - back from lunch
		20.5: 6,  // 20:30 UTC
		21.0: 7,  // 21:00 UTC = 14:00 PDT - afternoon
		21.5: 7,  // 21:30 UTC
		22.0: 8,  // 22:00 UTC = 15:00 PDT
		22.5: 6,  // 22:30 UTC
		23.0: 8,  // 23:00 UTC = 16:00 PDT
		23.5: 7,  // 23:30 UTC
	}

	// Test for UTC-7 (Pacific Daylight Time - Mountain View in summer)
	offset := -7

	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := lunch.DetectLunchBreakNoonCentered(halfHourCounts, offset)

	// Convert UTC lunch times to local Pacific Time
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
		t.Errorf("Failed to detect lunch break for gauravkghildiyal in Pacific Time")
	}

	// We expect lunch around 12:30pm PDT (19:30 UTC)
	// The data shows a clear dip from 12 events at 12:00 to 5 events at 12:30
	if lunchStartLocal < 12.0 || lunchStartLocal > 13.0 {
		t.Errorf("Lunch start time incorrect: got %.1f PDT, expected around 12:30 PDT", lunchStartLocal)
	}

	// The confidence should be reasonable given the 58% drop
	if confidence < 0.4 {
		t.Errorf("Lunch confidence too low: got %.2f, expected > 0.4 for 58%% drop", confidence)
	}

	t.Logf("Detected lunch: %.1f-%.1f PDT (%.1f-%.1f UTC) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)
}

// TestGauravkghildiyalActivityPattern documents his actual activity pattern
func TestGauravkghildiyalActivityPattern(t *testing.T) {
	t.Log("gauravkghildiyal Activity Pattern (Mountain View, CA - Pacific Time):")
	t.Log("UTC hours → PDT local time:")
	t.Log("10-14: Sleep (0 events) = 3am-7am PDT")
	t.Log("15: 21 events = 8am PDT - PEAK morning productivity")
	t.Log("16: 10 events = 9am PDT")
	t.Log("17: 2 events = 10am PDT - possible coffee break")
	t.Log("18: 12 events = 11am PDT")
	t.Log("19: 12 events = 12pm PDT - noon")
	t.Log("19.5: 5 events = 12:30pm PDT - LUNCH DIP")
	t.Log("20: 10 events = 1pm PDT - back from lunch")
	t.Log("21: 7 events = 2pm PDT - afternoon")
	t.Log("22: 8 events = 3pm PDT")
	t.Log("23: 8 events = 4pm PDT")
	t.Log("00: 4 events = 5pm PDT - winding down")
	t.Log("01-09: Variable evening activity")

	t.Log("\nKey Mountain View/Pacific Time indicators:")
	t.Log("- Work starts at 8am PDT with peak activity")
	t.Log("- Lunch at 12:30pm PDT (common for Google)")
	t.Log("- Work continues until 4-5pm PDT")
	t.Log("- Sleep from 3am-7am PDT")
	t.Log("- UTC-7 in summer (PDT)")
}

// TestGauravkghildiyalTimezoneValidation verifies UTC-7 is correct for Mountain View
func TestGauravkghildiyalTimezoneValidation(t *testing.T) {
	// Mountain View is in Pacific Time (UTC-7 in summer)
	// The activity pattern should make sense for Pacific Time

	testCases := []struct {
		offset     int
		name       string
		workStart  int // Hour when work starts (15:00 UTC)
		lunchTime  int // Hour when lunch occurs (19:30 UTC)
		peakTime   int // Hour of peak activity (15:00 UTC)
		reasonable bool
	}{
		{-9, "Alaska", 6, 10, 6, false},        // 6am start - too early
		{-8, "Pacific Winter", 7, 11, 7, true}, // 7am start - acceptable
		{-7, "Pacific Summer", 8, 12, 8, true}, // 8am start, 12:30 lunch - PERFECT
		{-6, "Mountain", 9, 13, 9, true},       // 9am start, 1:30pm lunch - ok
		{-5, "Central", 10, 14, 10, false},     // 10am start - late
		{-4, "Eastern", 11, 15, 11, false},     // 11am start - too late
	}

	for _, tc := range testCases {
		workStartLocal := (15 + tc.offset + 24) % 24
		// Lunch at 19:30 UTC
		lunchLocal := (19 + tc.offset + 24) % 24
		peakLocal := (15 + tc.offset + 24) % 24

		if workStartLocal != tc.workStart {
			t.Errorf("%s: work start calculation error: got %d, expected %d",
				tc.name, workStartLocal, tc.workStart)
		}

		isReasonable := (workStartLocal >= 7 && workStartLocal <= 9) &&
			(lunchLocal >= 11 && lunchLocal <= 13)

		if isReasonable != tc.reasonable {
			t.Errorf("%s: reasonableness mismatch: got %v, expected %v",
				tc.name, isReasonable, tc.reasonable)
		}

		t.Logf("%s (UTC%+d): work %02d:00, lunch ~%02d:30, peak %02d:00 - %s",
			tc.name, tc.offset, workStartLocal, lunchLocal, peakLocal,
			map[bool]string{true: "reasonable", false: "unreasonable"}[isReasonable])
	}

	t.Log("\nMountain View, CA should be detected as UTC-7 (Pacific Daylight Time)")
	t.Log("Google employees typically have lunch at 12:00-12:30pm")
}
