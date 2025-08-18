package lunch

import (
	"testing"
)

// TestAsh2kSydneyLunchDetection tests that ash2k's lunch at 12:30pm Sydney is detected
func TestAsh2kSydneyLunchDetection(t *testing.T) {
	// Actual 30-minute bucket counts from ash2k's GitHub activity
	// Focusing on the lunch period
	halfHourCounts := map[float64]int{
		// UTC times showing clear lunch at 2:30 UTC (12:30pm Sydney)
		1.5:  15,  // 1:30 UTC = 11:30am Sydney
		2.0:  9,   // 2:00 UTC = 12:00pm Sydney
		2.5:  0,   // 2:30 UTC = 12:30pm Sydney - LUNCH! (100% drop)
		3.0:  9,   // 3:00 UTC = 1:00pm Sydney - activity resumes
		3.5:  16,  // 3:30 UTC = 1:30pm Sydney - peak afternoon
		
		// Also include the false positive at 0:30 UTC
		0.0:  1,   // 0:00 UTC = 10:00am Sydney
		0.5:  4,   // 0:30 UTC = 10:30am Sydney - small dip (not lunch)
		1.0:  10,  // 1:00 UTC = 11:00am Sydney - activity increases
		
		// Include some morning data
		23.0: 1,   // 23:00 UTC = 9:00am Sydney (previous day UTC)
		23.5: 7,   // 23:30 UTC = 9:30am Sydney
	}
	
	// Test for UTC+10 (Sydney)
	// NOTE: In this codebase, positive UTC offsets use positive offset values
	// So UTC+10 uses offset = 10, not -10
	offset := 10
	
	// Detect lunch for Sydney timezone
	lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourCounts, offset)
	
	// Convert UTC lunch times to local Sydney time
	lunchStartLocal := lunchStart + float64(offset)
	lunchEndLocal := lunchEnd + float64(offset)
	
	// Normalize to 24-hour format
	for lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	for lunchEndLocal < 0 {
		lunchEndLocal += 24
	}
	
	// We expect lunch at 12:30pm Sydney (2:30 UTC), NOT 10:30am (0:30 UTC)
	if lunchStartLocal < 12.0 || lunchStartLocal > 13.0 {
		t.Errorf("Lunch start time incorrect: got %.1f Sydney time, expected ~12:30pm", lunchStartLocal)
	}
	
	// Specifically check it's not detecting the 10:30am dip
	if lunchStartLocal < 11.5 {
		t.Errorf("Algorithm incorrectly detected early morning dip (10:30am) as lunch instead of actual 12:30pm break")
	}
	
	// The confidence should be high given the 100% drop at 12:30pm
	if confidence < 0.7 {
		t.Errorf("Lunch confidence too low: got %.2f, expected > 0.7 for 100%% drop", confidence)
	}
	
	t.Logf("Detected lunch: %.1f-%.1f Sydney time with confidence %.2f", 
		lunchStartLocal, lunchEndLocal, confidence)
	
	// Verify the activity pattern
	beforeLunch := halfHourCounts[2.0]   // 12:00pm Sydney
	duringLunch := halfHourCounts[2.5]   // 12:30pm Sydney
	afterLunch := halfHourCounts[3.0]    // 1:00pm Sydney
	
	t.Logf("Activity: 12:00pm=%d, 12:30pm=%d, 1:00pm=%d (100%% drop at lunch)",
		beforeLunch, duringLunch, afterLunch)
	
	if duringLunch != 0 {
		t.Errorf("Expected 0 activity during lunch at 12:30pm, got %d", duringLunch)
	}
}