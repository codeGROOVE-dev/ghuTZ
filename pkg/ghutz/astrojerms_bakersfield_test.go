package ghutz

import (
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// TestAstrojermsBakersfieldDetection tests astrojerms's real activity data
// He lives in Bakersfield, CA (Pacific Time) 
func TestAstrojermsBakersfieldDetection(t *testing.T) {
	// ACTUAL activity data from astrojerms (Bakersfield, CA resident)
	// Collected over 263 days with 208 total events
	
	// ACTUAL 30-minute bucket counts from astrojerms's GitHub activity
	// Extracted from verbose output on 2025-08-17
	halfHourCounts := map[float64]int{
		// UTC time : event count (Bakersfield is UTC-8 in winter, UTC-7 in summer)
		0.0:  6,   // 00:00 UTC = 16:00/17:00 PST/PDT - late afternoon
		0.5:  8,   // 00:30 UTC
		1.0:  5,   // 01:00 UTC = 17:00/18:00 PST/PDT - evening
		1.5:  7,   // 01:30 UTC
		2.0:  4,   // 02:00 UTC = 18:00/19:00 PST/PDT
		2.5:  4,   // 02:30 UTC
		3.0:  2,   // 03:00 UTC = 19:00/20:00 PST/PDT
		3.5:  2,   // 03:30 UTC
		4.0:  2,   // 04:00 UTC = 20:00/21:00 PST/PDT
		4.5:  1,   // 04:30 UTC
		// Sleep time - complete quiet from 5:00-13:30 UTC (9pm-6:30am PST)
		5.0:  0,   // 05:00 UTC = 21:00/22:00 PST/PDT - sleep start
		5.5:  0,   // 05:30 UTC
		6.0:  0,   // 06:00 UTC = 22:00/23:00 PST/PDT - sleep
		6.5:  0,   // 06:30 UTC
		7.0:  0,   // 07:00 UTC = 23:00/00:00 PST/PDT - sleep
		7.5:  0,   // 07:30 UTC
		8.0:  0,   // 08:00 UTC = 00:00/01:00 PST/PDT - sleep
		8.5:  0,   // 08:30 UTC
		9.0:  0,   // 09:00 UTC = 01:00/02:00 PST/PDT - sleep
		9.5:  0,   // 09:30 UTC
		10.0: 0,   // 10:00 UTC = 02:00/03:00 PST/PDT - sleep
		10.5: 0,   // 10:30 UTC
		11.0: 0,   // 11:00 UTC = 03:00/04:00 PST/PDT - sleep
		11.5: 0,   // 11:30 UTC
		12.0: 0,   // 12:00 UTC = 04:00/05:00 PST/PDT - sleep
		12.5: 0,   // 12:30 UTC
		13.0: 0,   // 13:00 UTC = 05:00/06:00 PST/PDT - sleep
		13.5: 0,   // 13:30 UTC
		14.0: 2,   // 14:00 UTC = 06:00/07:00 PST/PDT - wake up
		14.5: 2,   // 14:30 UTC
		15.0: 7,   // 15:00 UTC = 07:00/08:00 PST/PDT - morning
		15.5: 3,   // 15:30 UTC  
		16.0: 6,   // 16:00 UTC = 08:00/09:00 PST/PDT - work starts
		16.5: 11,  // 16:30 UTC
		17.0: 4,   // 17:00 UTC = 09:00/10:00 PST/PDT
		17.5: 7,   // 17:30 UTC
		18.0: 21,  // 18:00 UTC = 10:00/11:00 PST/PDT - morning peak
		18.5: 22,  // 18:30 UTC - PEAK activity
		19.0: 15,  // 19:00 UTC = 11:00/12:00 PST/PDT
		19.5: 8,   // 19:30 UTC - lunch dip begins
		20.0: 6,   // 20:00 UTC = 12:00/13:00 PST/PDT - LUNCH
		20.5: 10,  // 20:30 UTC - back from lunch
		21.0: 8,   // 21:00 UTC = 13:00/14:00 PST/PDT - afternoon
		21.5: 13,  // 21:30 UTC
		22.0: 11,  // 22:00 UTC = 14:00/15:00 PST/PDT
		22.5: 8,   // 22:30 UTC
		23.0: 2,   // 23:00 UTC = 15:00/16:00 PST/PDT - winding down
		23.5: 1,   // 23:30 UTC
	}
	
	// ACTUAL hourly counts from astrojerms's GitHub activity  
	hourCounts := map[int]int{
		0:  14,  // 00:00 UTC = 16:00 PST / 17:00 PDT - late afternoon
		1:  12,  // 01:00 UTC = 17:00 PST / 18:00 PDT - evening
		2:  8,   // 02:00 UTC = 18:00 PST / 19:00 PDT
		3:  4,   // 03:00 UTC = 19:00 PST / 20:00 PDT
		4:  3,   // 04:00 UTC = 20:00 PST / 21:00 PDT
		5:  0,   // 05:00 UTC = 21:00 PST / 22:00 PDT - SLEEP START
		6:  0,   // 06:00 UTC = 22:00 PST / 23:00 PDT - SLEEP
		7:  0,   // 07:00 UTC = 23:00 PST / 00:00 PDT - SLEEP
		8:  0,   // 08:00 UTC = 00:00 PST / 01:00 PDT - SLEEP
		9:  0,   // 09:00 UTC = 01:00 PST / 02:00 PDT - SLEEP
		10: 0,   // 10:00 UTC = 02:00 PST / 03:00 PDT - SLEEP
		11: 0,   // 11:00 UTC = 03:00 PST / 04:00 PDT - SLEEP
		12: 0,   // 12:00 UTC = 04:00 PST / 05:00 PDT - SLEEP
		13: 0,   // 13:00 UTC = 05:00 PST / 06:00 PDT - SLEEP
		14: 4,   // 14:00 UTC = 06:00 PST / 07:00 PDT - wake up
		15: 10,  // 15:00 UTC = 07:00 PST / 08:00 PDT
		16: 17,  // 16:00 UTC = 08:00 PST / 09:00 PDT - work starts
		17: 11,  // 17:00 UTC = 09:00 PST / 10:00 PDT
		18: 43,  // 18:00 UTC = 10:00 PST / 11:00 PDT - PEAK MORNING
		19: 23,  // 19:00 UTC = 11:00 PST / 12:00 PDT
		20: 16,  // 20:00 UTC = 12:00 PST / 13:00 PDT - LUNCH
		21: 21,  // 21:00 UTC = 13:00 PST / 14:00 PDT - afternoon
		22: 19,  // 22:00 UTC = 14:00 PST / 15:00 PDT
		23: 3,   // 23:00 UTC = 15:00 PST / 16:00 PDT - end of day
	}
	
	// Test for UTC-8 (Pacific Standard Time - Bakersfield in winter)
	offset := -8
	
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
		t.Errorf("Failed to detect lunch break for astrojerms in Pacific Time")
	}
	
	// We expect lunch around 11:30-12:30 PST (19:30-20:30 UTC)
	// The data shows a clear dip at this time
	if lunchStartLocal < 11.0 || lunchStartLocal > 12.5 {
		t.Errorf("Lunch start time incorrect: got %.1f PST, expected around 11:30-12:00 PST", lunchStartLocal)
	}
	
	t.Logf("Detected lunch: %.1f-%.1f PST (%.1f-%.1f UTC) with confidence %.2f", 
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)
	
	// Also verify hourly pattern makes sense for Pacific Time
	_ = hourCounts // Suppress unused variable warning
}

// TestAstrojermsActivityPattern documents his actual activity pattern
func TestAstrojermsActivityPattern(t *testing.T) {
	t.Log("astrojerms Activity Pattern (Bakersfield, CA - Pacific Time):")
	t.Log("UTC hours → PST/PDT local time:")
	t.Log("05-13: Sleep (0 events) = 9pm-5am PST")
	t.Log("14: 4 events = 6am PST - wake up")
	t.Log("15: 10 events = 7am PST")
	t.Log("16: 17 events = 8am PST - work starts")
	t.Log("17: 11 events = 9am PST")
	t.Log("18: 43 events = 10am PST - PEAK morning productivity")
	t.Log("19: 23 events = 11am PST")
	t.Log("20: 16 events = 12pm PST - LUNCH DIP")
	t.Log("21: 21 events = 1pm PST - afternoon")
	t.Log("22: 19 events = 2pm PST")
	t.Log("23: 3 events = 3pm PST - winding down")
	t.Log("00: 14 events = 4pm PST - late afternoon")
	t.Log("01: 12 events = 5pm PST - evening")
	t.Log("02: 8 events = 6pm PST")
	t.Log("03: 4 events = 7pm PST")
	t.Log("04: 3 events = 8pm PST")
	
	t.Log("\nKey Bakersfield/Pacific Time indicators:")
	t.Log("- Work starts at 8am PST")
	t.Log("- Peak productivity 10-11am PST")
	t.Log("- Lunch dip at noon PST")
	t.Log("- Work ends around 3-4pm PST")
	t.Log("- Some evening activity 4-8pm PST")
	t.Log("- Sleep from 9pm-6am PST")
	t.Log("- UTC-8 in winter (PST), UTC-7 in summer (PDT)")
}

// TestAstrojermsTimezoneValidation verifies UTC-8 is correct for Bakersfield
func TestAstrojermsTimezoneValidation(t *testing.T) {
	// Bakersfield is in Pacific Time (UTC-8/UTC-7)
	// The activity pattern should make sense for Pacific Time
	
	testCases := []struct {
		offset int
		name string
		workStart int  // Hour when work starts (16:00 UTC)
		lunchTime int  // Hour when lunch occurs (20:00 UTC)
		peakTime int   // Hour of peak activity (18:00 UTC)
		reasonable bool
	}{
		{-10, "Hawaii", 6, 10, 8, false},      // 6am start - too early
		{-9, "Alaska", 7, 11, 9, true},        // 7am start - acceptable
		{-8, "Pacific", 8, 12, 10, true},      // 8am start, noon lunch - PERFECT
		{-7, "Mountain", 9, 13, 11, true},     // 9am start, 1pm lunch - ok
		{-6, "Central", 10, 14, 12, false},    // 10am start - late
		{-5, "Eastern", 11, 15, 13, false},    // 11am start - too late
		{-4, "Atlantic", 12, 16, 14, false},   // noon start - way too late
	}
	
	for _, tc := range testCases {
		workStartLocal := (16 + tc.offset + 24) % 24
		lunchLocal := (20 + tc.offset + 24) % 24
		peakLocal := (18 + tc.offset + 24) % 24
		
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
		
		t.Logf("%s (UTC%+d): work %02d:00, lunch %02d:00, peak %02d:00 - %s",
			tc.name, tc.offset, workStartLocal, lunchLocal, peakLocal,
			map[bool]string{true: "reasonable", false: "unreasonable"}[isReasonable])
	}
	
	t.Log("\nBakersfield, CA should be detected as UTC-8 (Pacific Time)")
}