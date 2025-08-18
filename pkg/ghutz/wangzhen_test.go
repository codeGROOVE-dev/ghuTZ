package ghutz

import (
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// TestWangzhenNoonLunch tests wangzhen127's real activity data
// Should detect lunch at 12:00pm (noon), not 1:00pm
func TestWangzhenNoonLunch(t *testing.T) {
	// ACTUAL 30-minute bucket counts from wangzhen127's GitHub activity
	// This is Pacific Time (UTC-7)
	halfHourCounts := map[float64]int{
		// Morning activity (UTC times -> PDT)
		13.0: 2,   // 6:00am PDT
		13.5: 1,   // 6:30am PDT
		14.5: 4,   // 7:30am PDT
		15.0: 4,   // 8:00am PDT
		15.5: 9,   // 8:30am PDT
		16.0: 3,   // 9:00am PDT
		16.5: 3,   // 9:30am PDT
		17.0: 4,   // 10:00am PDT
		17.5: 12,  // 10:30am PDT
		18.0: 17,  // 11:00am PDT
		18.5: 23,  // 11:30am PDT - PEAK activity
		19.0: 2,   // 12:00pm PDT - MASSIVE DROP (91% drop!) - OBVIOUS LUNCH
		19.5: 9,   // 12:30pm PDT - Recovery begins
		20.0: 3,   // 1:00pm PDT - Not lunch, just lower activity
		20.5: 6,   // 1:30pm PDT
		21.0: 14,  // 2:00pm PDT
		21.5: 16,  // 2:30pm PDT
		22.0: 1,   // 3:00pm PDT
		22.5: 9,   // 3:30pm PDT
		23.0: 3,   // 4:00pm PDT
		23.5: 4,   // 4:30pm PDT
		// Evening
		0.0: 5,    // 5:00pm PDT
		0.5: 12,   // 5:30pm PDT
		1.0: 3,    // 6:00pm PDT
		3.0: 1,    // 8:00pm PDT
		3.5: 3,    // 8:30pm PDT
		4.0: 8,    // 9:00pm PDT
		4.5: 10,   // 9:30pm PDT
		5.0: 13,   // 10:00pm PDT
		5.5: 11,   // 10:30pm PDT
		6.0: 2,    // 11:00pm PDT
		6.5: 2,    // 11:30pm PDT
		7.0: 1,    // 12:00am PDT
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
		t.Errorf("Failed to detect lunch break for wangzhen127 in Pacific Time")
	}
	
	// We expect lunch at 12:00pm PDT (noon), NOT 1:00pm
	// The data shows: 23 events at 11:30, drops to 2 at 12:00 (91% drop!)
	// This is the most obvious lunch break in the data
	if lunchStartLocal < 11.5 || lunchStartLocal > 12.5 {
		t.Errorf("Lunch start time incorrect: got %.1f PDT, expected 12:00 PDT (noon)", lunchStartLocal)
	}
	
	// Specifically check it's not detecting 1:00pm as lunch
	if lunchStartLocal >= 13.0 {
		t.Errorf("Algorithm incorrectly detected 1:00pm as lunch instead of noon")
	}
	
	t.Logf("Detected lunch: %.1f-%.1f PDT with confidence %.2f", 
		lunchStartLocal, lunchEndLocal, confidence)
}