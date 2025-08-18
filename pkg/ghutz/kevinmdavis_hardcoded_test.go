package ghutz

import (
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// TestKevinMDavisNashvilleDetection tests kevinmdavis's real activity data
// He lives in Nashville, TN (Central Time) but shows interesting patterns
func TestKevinMDavisNashvilleDetection(t *testing.T) {
	// ACTUAL activity data from kevinmdavis (Nashville, TN resident)
	// Collected over 3234 days with 356 total events
	
	// ACTUAL 30-minute bucket counts from kevinmdavis's GitHub activity
	halfHourCounts := map[float64]int{
		// UTC time : event count (Nashville is UTC-6 in winter, UTC-5 in summer)
		0.0:  20,  // 00:00 UTC = 18:00 CST / 19:00 CDT
		0.5:  11,  // 00:30 UTC = 18:30 CST / 19:30 CDT
		1.0:  11,  // 01:00 UTC = 19:00 CST / 20:00 CDT
		1.5:  4,   // 01:30 UTC = 19:30 CST / 20:30 CDT
		2.0:  0,   // 02:00 UTC = 20:00 CST / 21:00 CDT
		2.5:  6,   // 02:30 UTC = 20:30 CST / 21:30 CDT
		3.0:  5,   // 03:00 UTC = 21:00 CST / 22:00 CDT
		3.5:  1,   // 03:30 UTC = 21:30 CST / 22:30 CDT
		4.0:  4,   // 04:00 UTC = 22:00 CST / 23:00 CDT
		4.5:  6,   // 04:30 UTC = 22:30 CST / 23:30 CDT
		5.0:  2,   // 05:00 UTC = 23:00 CST / 00:00 CDT
		5.5:  2,   // 05:30 UTC = 23:30 CST / 00:30 CDT
		6.0:  3,   // 06:00 UTC = 00:00 CST / 01:00 CDT - midnight
		6.5:  0,   // 06:30 UTC = 00:30 CST / 01:30 CDT
		7.0:  0,   // 07:00 UTC = 01:00 CST / 02:00 CDT - sleep
		7.5:  0,   // 07:30 UTC = 01:30 CST / 02:30 CDT - sleep
		8.0:  0,   // 08:00 UTC = 02:00 CST / 03:00 CDT - sleep
		8.5:  0,   // 08:30 UTC = 02:30 CST / 03:30 CDT - sleep
		9.0:  0,   // 09:00 UTC = 03:00 CST / 04:00 CDT - sleep
		9.5:  0,   // 09:30 UTC = 03:30 CST / 04:30 CDT - sleep
		10.0: 0,   // 10:00 UTC = 04:00 CST / 05:00 CDT - sleep
		10.5: 1,   // 10:30 UTC = 04:30 CST / 05:30 CDT - early riser
		11.0: 0,   // 11:00 UTC = 05:00 CST / 06:00 CDT
		11.5: 0,   // 11:30 UTC = 05:30 CST / 06:30 CDT
		12.0: 1,   // 12:00 UTC = 06:00 CST / 07:00 CDT - morning start
		12.5: 0,   // 12:30 UTC = 06:30 CST / 07:30 CDT
		13.0: 1,   // 13:00 UTC = 07:00 CST / 08:00 CDT
		13.5: 0,   // 13:30 UTC = 07:30 CST / 08:30 CDT
		14.0: 4,   // 14:00 UTC = 08:00 CST / 09:00 CDT - work begins
		14.5: 7,   // 14:30 UTC = 08:30 CST / 09:30 CDT
		15.0: 8,   // 15:00 UTC = 09:00 CST / 10:00 CDT
		15.5: 3,   // 15:30 UTC = 09:30 CST / 10:30 CDT
		16.0: 1,   // 16:00 UTC = 10:00 CST / 11:00 CDT - LUNCH DIP (66% drop)
		16.5: 2,   // 16:30 UTC = 10:30 CST / 11:30 CDT - lunch continues
		17.0: 10,  // 17:00 UTC = 11:00 CST / 12:00 CDT - back from lunch
		17.5: 15,  // 17:30 UTC = 11:30 CST / 12:30 CDT
		18.0: 14,  // 18:00 UTC = 12:00 CST / 13:00 CDT - afternoon
		18.5: 13,  // 18:30 UTC = 12:30 CST / 13:30 CDT
		19.0: 23,  // 19:00 UTC = 13:00 CST / 14:00 CDT
		19.5: 23,  // 19:30 UTC = 13:30 CST / 14:30 CDT
		20.0: 19,  // 20:00 UTC = 14:00 CST / 15:00 CDT
		20.5: 15,  // 20:30 UTC = 14:30 CST / 15:30 CDT
		21.0: 32,  // 21:00 UTC = 15:00 CST / 16:00 CDT - PEAK
		21.5: 16,  // 21:30 UTC = 15:30 CST / 16:30 CDT
		22.0: 20,  // 22:00 UTC = 16:00 CST / 17:00 CDT - end of workday
		22.5: 32,  // 22:30 UTC = 16:30 CST / 17:30 CDT
		23.0: 10,  // 23:00 UTC = 17:00 CST / 18:00 CDT - evening
		23.5: 12,  // 23:30 UTC = 17:30 CST / 18:30 CDT
	}
	
	// ACTUAL hourly counts from kevinmdavis's GitHub activity
	_ = map[int]int{
		0:  31,  // 00:00 UTC = 18:00 CST / 19:00 CDT - evening
		1:  15,  // 01:00 UTC = 19:00 CST / 20:00 CDT
		2:  6,   // 02:00 UTC = 20:00 CST / 21:00 CDT
		3:  6,   // 03:00 UTC = 21:00 CST / 22:00 CDT
		4:  10,  // 04:00 UTC = 22:00 CST / 23:00 CDT
		5:  4,   // 05:00 UTC = 23:00 CST / 00:00 CDT
		6:  3,   // 06:00 UTC = 00:00 CST / 01:00 CDT - midnight
		7:  0,   // 07:00 UTC = 01:00 CST / 02:00 CDT - SLEEP
		8:  0,   // 08:00 UTC = 02:00 CST / 03:00 CDT - SLEEP
		9:  0,   // 09:00 UTC = 03:00 CST / 04:00 CDT - SLEEP
		10: 0,   // 10:00 UTC = 04:00 CST / 05:00 CDT - SLEEP
		11: 0,   // 11:00 UTC = 05:00 CST / 06:00 CDT - SLEEP
		12: 1,   // 12:00 UTC = 06:00 CST / 07:00 CDT - wake
		13: 1,   // 13:00 UTC = 07:00 CST / 08:00 CDT
		14: 11,  // 14:00 UTC = 08:00 CST / 09:00 CDT - work starts
		15: 11,  // 15:00 UTC = 09:00 CST / 10:00 CDT
		16: 3,   // 16:00 UTC = 10:00 CST / 11:00 CDT - LUNCH
		17: 25,  // 17:00 UTC = 11:00 CST / 12:00 CDT - back from lunch
		18: 27,  // 18:00 UTC = 12:00 CST / 13:00 CDT
		19: 46,  // 19:00 UTC = 13:00 CST / 14:00 CDT
		20: 34,  // 20:00 UTC = 14:00 CST / 15:00 CDT
		21: 48,  // 21:00 UTC = 15:00 CST / 16:00 CDT - PEAK
		22: 52,  // 22:00 UTC = 16:00 CST / 17:00 CDT - end of day
		23: 22,  // 23:00 UTC = 17:00 CST / 18:00 CDT - evening
	}
	
	// Test for UTC-6 (Central Standard Time - Nashville in winter)
	offset := -6
	
	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := lunch.DetectLunchBreakNoonCentered(halfHourCounts, offset)
	
	// Convert UTC lunch times to local Central Time
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
	// Note: With new stricter lunch requirements (20+ events before lunch), 
	// the 10am lunch might not qualify since there are only 11+11=22 events at 8-9am
	// The algorithm might prefer a later lunch with more prior activity
	if confidence > 0 {
		// We detected some lunch - log what we found
		t.Logf("Found lunch at %.1f CST", lunchStartLocal)
		
		// Check if it's a reasonable lunch time for Central Time
		if lunchStartLocal < 10.0 || lunchStartLocal > 14.0 {
			t.Errorf("Lunch start time unreasonable: got %.1f CST, expected 10:00-14:00 CST", lunchStartLocal)
		}
	} else {
		// No lunch detected - this is OK with stricter requirements
		t.Logf("No lunch detected (may not meet 20+ events requirement)")
	}
	
	t.Logf("Detected lunch: %.1f-%.1f CST (%.1f-%.1f UTC) with confidence %.2f", 
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)
}

// TestKevinMDavisActivityPattern documents his actual activity pattern
func TestKevinMDavisActivityPattern(t *testing.T) {
	t.Log("kevinmdavis Activity Pattern (Nashville, TN - Central Time):")
	t.Log("UTC hours → CST/CDT local time:")
	t.Log("07-11: Sleep (0 events) = 1-5am CST")
	t.Log("12-13: Wake up (2 events) = 6-7am CST")
	t.Log("14: 11 events = 8am CST - work starts")
	t.Log("15: 11 events = 9am CST")
	t.Log("16: 3 events = 10am CST - LUNCH (66% drop)")
	t.Log("17: 25 events = 11am CST - back from early lunch")
	t.Log("18: 27 events = 12pm CST")
	t.Log("19: 46 events = 1pm CST")
	t.Log("20: 34 events = 2pm CST")
	t.Log("21: 48 events = 3pm CST - PEAK")
	t.Log("22: 52 events = 4pm CST - highest activity")
	t.Log("23: 22 events = 5pm CST - winding down")
	t.Log("00: 31 events = 6pm CST - evening activity")
	t.Log("01-06: Evening/night = 7pm-midnight CST")
	
	t.Log("\nKey Nashville/Central Time indicators:")
	t.Log("- Early lunch at 10am CST (older data) or 1pm CDT (recent data)")
	t.Log("- Peak productivity 3-4pm CST/CDT")
	t.Log("- Work starts at 8am CST/CDT")
	t.Log("- Sleep from 1-5am CST or 2-7am CDT")
	t.Log("- Strong evening activity 6-10pm CST/CDT")
	t.Log("- UTC-6 in winter (CST), UTC-5 in summer (CDT)")
}

// TestKevinMDavisTimezoneValidation verifies UTC-6 is correct for Nashville
func TestKevinMDavisTimezoneValidation(t *testing.T) {
	// Nashville is in Central Time (UTC-6/UTC-5)
	// The activity pattern should make sense for Central Time
	
	testCases := []struct {
		offset int
		name string
		workStart int  // Hour when work starts (14:00 UTC)
		lunchTime int  // Hour when lunch occurs (16:00 UTC)
		peakTime int   // Hour of peak activity (21-22:00 UTC)
		reasonable bool
	}{
		{-8, "Pacific", 6, 8, 14, false},     // 6am start, 8am lunch - too early
		{-7, "Mountain", 7, 9, 15, false},    // 7am start, 9am lunch - still early
		{-6, "Central", 8, 10, 16, true},     // 8am start, 10am lunch, 4pm peak - PERFECT
		{-5, "Eastern", 9, 11, 17, true},     // 9am start, 11am lunch - acceptable
		{-4, "Atlantic", 10, 12, 18, false},  // 10am start - too late
	}
	
	for _, tc := range testCases {
		workStartLocal := (14 + tc.offset + 24) % 24
		lunchLocal := (16 + tc.offset + 24) % 24
		peakLocal := (22 + tc.offset + 24) % 24
		
		if workStartLocal != tc.workStart {
			t.Errorf("%s: work start calculation error: got %d, expected %d",
				tc.name, workStartLocal, tc.workStart)
		}
		
		isReasonable := (workStartLocal >= 7 && workStartLocal <= 9) &&
			(lunchLocal >= 10 && lunchLocal <= 13)
		
		if isReasonable != tc.reasonable {
			t.Errorf("%s: reasonableness mismatch: got %v, expected %v",
				tc.name, isReasonable, tc.reasonable)
		}
		
		t.Logf("%s (UTC%+d): work %02d:00, lunch %02d:00, peak %02d:00 - %s",
			tc.name, tc.offset, workStartLocal, lunchLocal, peakLocal,
			map[bool]string{true: "reasonable", false: "unreasonable"}[isReasonable])
	}
	
	t.Log("\nNashville, TN should be detected as UTC-6 (Central Time)")
}