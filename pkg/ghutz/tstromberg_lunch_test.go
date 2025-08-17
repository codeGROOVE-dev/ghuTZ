package ghutz

import (
	"testing"
)

// TestTstrombergFullHourLunch tests that we correctly detect the full hour lunch
// from 11:30 to 12:30 for tstromberg in Eastern Time
func TestTstrombergFullHourLunch(t *testing.T) {
	// Actual 30-minute bucket counts from tstromberg's activity data
	// These show a clear lunch break from 11:30-12:30 EST (15:30-16:30 UTC)
	halfHourCounts := map[float64]int{
		13.0: 1,   // 09:00 EST
		13.5: 2,   // 09:30 EST
		14.0: 10,  // 10:00 EST
		14.5: 9,   // 10:30 EST
		15.0: 27,  // 11:00 EST - high activity before lunch
		15.5: 4,   // 11:30 EST - LUNCH START (85% drop from 27 to 4)
		16.0: 5,   // 12:00 EST - LUNCH CONTINUES (still very low)
		16.5: 11,  // 12:30 EST - activity resumes
		17.0: 15,  // 13:00 EST
		17.5: 11,  // 13:30 EST
		18.0: 3,   // 14:00 EST
		18.5: 13,  // 14:30 EST
		19.0: 38,  // 15:00 EST - peak activity
		19.5: 19,  // 15:30 EST
		20.0: 15,  // 16:00 EST
		20.5: 9,   // 16:30 EST
		21.0: 11,  // 17:00 EST
		21.5: 8,   // 17:30 EST
		22.0: 19,  // 18:00 EST
		22.5: 0,   // 18:30 EST
		23.0: 1,   // 19:00 EST
		23.5: 9,   // 19:30 EST
	}

	// Test for UTC-4 (Eastern Daylight Time)
	offset := -4

	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := detectLunchBreakNoonCentered(halfHourCounts, offset)

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
		t.Errorf("Failed to detect lunch break for tstromberg in Eastern Time")
	}

	// We expect lunch to be detected at 11:30-12:30 (full hour)
	if lunchStartLocal < 11.0 || lunchStartLocal > 12.0 {
		t.Errorf("Lunch start time incorrect: got %.1f EST, expected 11:30 EST", lunchStartLocal)
	}

	// Check that it's a 60-minute lunch, not 30
	lunchDuration := lunchEnd - lunchStart
	if lunchDuration < 0 {
		lunchDuration += 24
	}
	
	if lunchDuration < 0.9 || lunchDuration > 1.1 {
		t.Errorf("Lunch duration incorrect: got %.1f hours, expected 1.0 hour", lunchDuration)
	}

	// The confidence should be high given the clear 83% average drop over the hour
	if confidence < 0.7 {
		t.Errorf("Lunch confidence too low: got %.2f, expected > 0.7 for clear lunch pattern", confidence)
	}

	t.Logf("Detected lunch: %.1f-%.1f EST (%.1f-%.1f UTC) duration %.1f hours with confidence %.2f", 
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, lunchDuration, confidence)
}