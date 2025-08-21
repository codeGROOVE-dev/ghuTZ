package lunch

import (
	"testing"
)

// TestTstromberg30MinuteLunch tests that we correctly detect the 30-minute lunch
// from 11:30 to 12:00 for tstromberg in Eastern Time based on new data
func TestTstromberg30MinuteLunch(t *testing.T) {
	// Updated 30-minute bucket counts from tstromberg's recent activity data
	// These show a clear 30-minute lunch break from 11:30-12:00 EDT (15:30-16:00 UTC)
	halfHourCounts := map[float64]int{
		13.0: 2,  // 09:00 EDT
		13.5: 3,  // 09:30 EDT
		14.0: 12, // 10:00 EDT
		14.5: 11, // 10:30 EDT
		15.0: 29, // 11:00 EDT - high activity before lunch
		15.5: 3,  // 11:30 EDT - LUNCH START (90% drop from 29 to 3)
		16.0: 14, // 12:00 EDT - activity resumes immediately
		16.5: 13, // 12:30 EDT
		17.0: 17, // 13:00 EDT
		17.5: 12, // 13:30 EDT
		18.0: 5,  // 14:00 EDT
		18.5: 15, // 14:30 EDT
		19.0: 41, // 15:00 EDT - peak activity
		19.5: 22, // 15:30 EDT
		20.0: 18, // 16:00 EDT
		20.5: 11, // 16:30 EDT
		21.0: 13, // 17:00 EDT
		21.5: 9,  // 17:30 EDT
		22.0: 21, // 18:00 EDT
		22.5: 2,  // 18:30 EDT - NOT lunch, just low evening activity
		23.0: 3,  // 19:00 EDT
		23.5: 11, // 19:30 EDT
	}

	// Test for UTC-4 (Eastern Daylight Time)
	offset := -4

	// Detect lunch for this timezone
	lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourCounts, offset)

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

	// We expect lunch to be detected at 11:30-12:00 (30 minutes)
	expectedStart := 11.5
	expectedEnd := 12.0

	if lunchStartLocal < expectedStart-0.1 || lunchStartLocal > expectedStart+0.1 {
		t.Errorf("Lunch start time incorrect: got %.1f EDT, expected %.1f EDT", lunchStartLocal, expectedStart)
	}

	if lunchEndLocal < expectedEnd-0.1 || lunchEndLocal > expectedEnd+0.1 {
		t.Errorf("Lunch end time incorrect: got %.1f EDT, expected %.1f EDT", lunchEndLocal, expectedEnd)
	}

	// Check that it's a 30-minute lunch, not 60
	lunchDuration := lunchEnd - lunchStart
	if lunchDuration < 0 {
		lunchDuration += 24
	}

	if lunchDuration < 0.4 || lunchDuration > 0.6 {
		t.Errorf("Lunch duration incorrect: got %.1f hours, expected 0.5 hours", lunchDuration)
	}

	// The confidence should be high given the clear 90% drop
	if confidence < 0.7 {
		t.Errorf("Lunch confidence too low: got %.2f, expected > 0.7 for clear lunch pattern", confidence)
	}

	// Verify that 18:30 EDT (22:30 UTC) is NOT detected as lunch
	// This is evening activity and should not be confused with lunch
	if lunchStartLocal >= 18.0 && lunchStartLocal <= 19.0 {
		t.Errorf("INCORRECT: Detected evening activity (18:30 EDT) as lunch. Got lunch at %.1f EDT", lunchStartLocal)
	}

	t.Logf("Detected lunch: %.1f-%.1f EDT (%.1f-%.1f UTC) duration %.1f hours with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, lunchDuration, confidence)
}