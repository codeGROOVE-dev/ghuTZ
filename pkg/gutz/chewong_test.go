package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
)

// TestChewongLunchDetection tests chewong's real activity data
// Should detect a 30-minute lunch at 11:30am, not 60 minutes
func TestChewongLunchDetection(t *testing.T) {
	// ACTUAL 30-minute bucket counts from chewong's GitHub activity
	// This is Pacific Time (UTC-7 in summer)
	halfHourCounts := map[float64]int{
		// Morning activity (UTC times)
		16.0: 20, // 9:00am PDT
		16.5: 34, // 9:30am PDT - PEAK
		17.0: 22, // 10:00am PDT
		17.5: 16, // 10:30am PDT
		18.0: 14, // 11:00am PDT
		18.5: 4,  // 11:30am PDT - LUNCH DIP (71% drop!)
		19.0: 11, // 12:00pm PDT - RECOVERY (back up)
		19.5: 12, // 12:30pm PDT
		20.0: 13, // 1:00pm PDT
		20.5: 23, // 1:30pm PDT
		21.0: 13, // 2:00pm PDT
		21.5: 9,  // 2:30pm PDT
		22.0: 17, // 3:00pm PDT
		22.5: 10, // 3:30pm PDT
		23.0: 8,  // 4:00pm PDT
		23.5: 11, // 4:30pm PDT
		// Evening
		0.0: 3,  // 5:00pm PDT
		0.5: 6,  // 5:30pm PDT
		1.0: 4,  // 6:00pm PDT
		1.5: 3,  // 6:30pm PDT
		2.5: 8,  // 7:30pm PDT
		3.0: 10, // 8:00pm PDT
		3.5: 8,  // 8:30pm PDT
	}

	// Test for UTC-7 (Pacific Daylight Time)
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
		t.Errorf("Failed to detect lunch break for chewong in Pacific Time")
	}

	// We expect lunch at 11:30am PDT for 30 minutes (not 60!)

	if lunchStartLocal < 11.0 || lunchStartLocal > 12.0 {
		t.Errorf("Lunch start time incorrect: got %.1f PDT, expected 11:30 PDT", lunchStartLocal)
	}

	// The key test: lunch should be 30 minutes, not 60!
	lunchDuration := (lunchEndLocal - lunchStartLocal) * 60 // in minutes
	if lunchDuration < 0 {
		lunchDuration += 24 * 60
	}

	if lunchDuration > 35 {
		t.Errorf("Lunch duration incorrect: got %.0f minutes, expected 30 minutes (strong rebound at 12pm)", lunchDuration)
	}

	t.Logf("Detected lunch: %.1f-%.1f PDT (duration: %.0f min) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchDuration, confidence)
}
