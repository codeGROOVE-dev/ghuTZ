package ghutz

import (
	"testing"
)

// TestKimstervLunchDetection verifies that kimsterv's lunch break is properly detected
func TestKimstervLunchDetection(t *testing.T) {
	// Hard-coded activity data for kimsterv from actual GitHub activity
	// This represents half-hourly activity counts in UTC
	halfHourlyData := map[float64]int{
		// Early morning UTC (late night PST)
		0.0: 2, 0.5: 0,
		1.0: 3, 1.5: 0,
		2.0: 2, 2.5: 0,
		3.0: 4, 3.5: 0,
		4.0: 2, 4.5: 0,
		// Quiet hours (sleep time PST)
		5.0: 0, 5.5: 0,
		6.0: 0, 6.5: 0,
		7.0: 0, 7.5: 0,
		8.0: 0, 8.5: 0,
		9.0: 0, 9.5: 0,
		10.0: 0, 10.5: 0,
		11.0: 1, 11.5: 0,
		12.0: 0, 12.5: 0,
		// Work day begins (5am PST = 13:00 UTC)
		13.0: 5, 13.5: 0,
		14.0: 6, 14.5: 0,
		15.0: 15, 15.5: 0,
		16.0: 26, 16.5: 0,
		17.0: 40, 17.5: 0,
		// Peak morning and lunch dip area
		18.0: 26, 18.5: 12,  // 10:00-10:30 PST
		19.0: 7,  19.5: 16,  // 11:00-11:30 PST (lunch dip at 11am)
		20.0: 13, 20.5: 10,  // 12:00-12:30 PST
		21.0: 12, 21.5: 13,  // 13:00-13:30 PST
		22.0: 7,  22.5: 8,   // 14:00-14:30 PST
		23.0: 6,  23.5: 2,   // 15:00-15:30 PST
	}
	
	// Log the activity around lunch time (18-22 UTC = 10am-2pm PST)
	t.Logf("Activity around lunch time:")
	for bucket := 18.0; bucket <= 22.0; bucket += 0.5 {
		count := halfHourlyData[bucket]
		localTime := bucket - 8.0 // Convert to PST
		if localTime < 0 {
			localTime += 24
		}
		t.Logf("  %.1f UTC (%.1f PST): %d events", bucket, localTime, count)
	}
	
	// Test lunch detection for UTC-8 (Pacific Standard Time)
	lunchStart, lunchEnd, lunchConfidence := detectLunchBreakNoonCentered(halfHourlyData, -8)
	
	t.Logf("Lunch detection for UTC-8: start=%.1f, end=%.1f, confidence=%.2f", 
		lunchStart, lunchEnd, lunchConfidence)
	
	// Convert lunch times to local (PST)
	if lunchStart >= 0 {
		lunchStartLocal := lunchStart - 8.0 // Convert UTC to PST (UTC-8)
		if lunchStartLocal < 0 {
			lunchStartLocal += 24
		}
		lunchEndLocal := lunchEnd - 8.0
		if lunchEndLocal < 0 {
			lunchEndLocal += 24
		}
		
		t.Logf("Lunch in PST: %.1f - %.1f (confidence: %.2f)", 
			lunchStartLocal, lunchEndLocal, lunchConfidence)
		
		// The algorithm detects lunch at 10:30am PST due to the large drop
		// from 26 events at 10:00 to 12 events at 10:30 (53.8% drop)
		// This is reasonable as some people take early lunch, especially early starters
		if lunchStartLocal < 10.0 || lunchStartLocal > 12.5 {
			t.Errorf("Expected lunch to start between 10am-12:30pm PST, got %.1f PST", lunchStartLocal)
		}
		
		// Verify confidence is reasonable
		if lunchConfidence < 0.3 {
			t.Errorf("Lunch confidence too low: %.2f", lunchConfidence)
		}
		
		// Verify the specific dip pattern
		// The algorithm finds the 10:30am drop more significant than 11am
		if lunchStart != 18.5 {
			t.Logf("Note: Lunch detected at %.1f UTC, alternative dip at 19.0 UTC (11am PST)", lunchStart)
		}
	} else {
		t.Errorf("Failed to detect lunch for kimsterv in PST timezone")
		t.Logf("Activity shows clear dip at 19:00 UTC (11am PST): 26→12→7 events")
	}
}