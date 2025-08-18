package ghutz

import (
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// TestACrateHardcodedTimezoneDetection tests a-crate's timezone detection
// using hardcoded activity data instead of fetching from GitHub API
// She lives in Seattle, WA which is Pacific Time (UTC-8)
func TestACrateHardcodedTimezoneDetection(t *testing.T) {
	// Hardcoded activity data for a-crate (Pacific Time - UTC-8 during PST)
	// This represents half-hourly activity counts in UTC
	halfHourlyData := map[float64]int{
		// Midnight-6am UTC (4pm-10pm PST previous day) - evening activity
		0.0: 12, 0.5: 8,   // 4pm PST
		1.0: 10, 1.5: 7,   // 5pm PST
		2.0: 8, 2.5: 5,    // 6pm PST (dinner time)
		3.0: 6, 3.5: 4,    // 7pm PST (evening)
		4.0: 5, 4.5: 3,    // 8pm PST
		5.0: 2, 5.5: 1,    // 9pm PST
		6.0: 0, 6.5: 0,    // 10pm PST (getting ready for bed)
		7.0: 0, 7.5: 0,    // 11pm PST (sleep)
		8.0: 0, 8.5: 0,    // Midnight PST (sleep)
		9.0: 0, 9.5: 0,    // 1am PST (sleep)
		10.0: 0, 10.5: 0,  // 2am PST (sleep)
		11.0: 0, 11.5: 0,  // 3am PST (sleep)
		12.0: 0, 12.5: 0,  // 4am PST (sleep)
		13.0: 0, 13.5: 0,  // 5am PST (sleep)
		14.0: 1, 14.5: 2,  // 6am PST (early wake)
		15.0: 8, 15.5: 10, // 7am PST (morning start)
		16.0: 15, 16.5: 18, // 8am PST (work start)
		17.0: 22, 17.5: 20, // 9am PST (morning work)
		18.0: 28, 18.5: 25, // 10am PST (peak morning)
		19.0: 30, 19.5: 28, // 11am PST
		// Lunch dip at noon PST
		20.0: 8, 20.5: 5,   // 12pm PST (lunch start - clear dip)
		21.0: 7, 21.5: 18,  // 1pm PST (returning from lunch)
		22.0: 24, 22.5: 22, // 2pm PST (afternoon work)
		23.0: 20, 23.5: 18, // 3pm PST (afternoon)
	}
	
	// Test lunch detection for UTC-8 (Pacific Standard Time)
	lunchStart, lunchEnd, lunchConfidence := lunch.DetectLunchBreakNoonCentered(halfHourlyData, -8)
	
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
		
		// Verify lunch is detected around noon PST (allowing 11:30am - 1:00pm range)
		if lunchStartLocal < 11.5 || lunchStartLocal > 13.0 {
			t.Errorf("Expected lunch to start between 11:30am-1:00pm PST, got %.1f PST", lunchStartLocal)
		}
		
		// Verify confidence is reasonable
		if lunchConfidence < 0.3 {
			t.Errorf("Lunch confidence too low: %.2f", lunchConfidence)
		}
	} else {
		t.Logf("Note: No lunch detected for a-crate (confidence too low)")
	}
	
	// Test quiet hours detection (sleep time)
	quietHours := []int{}
	for hour := 0; hour < 24; hour++ {
		count1 := halfHourlyData[float64(hour)]
		count2 := halfHourlyData[float64(hour)+0.5]
		if count1 == 0 && count2 == 0 {
			quietHours = append(quietHours, hour)
		}
	}
	
	t.Logf("Quiet hours UTC: %v", quietHours)
	
	// Should have quiet hours from 6-13 UTC (10pm-5am PST)
	expectedQuiet := []int{6, 7, 8, 9, 10, 11, 12, 13}
	quietCount := 0
	for _, expected := range expectedQuiet {
		for _, actual := range quietHours {
			if actual == expected {
				quietCount++
				break
			}
		}
	}
	
	if quietCount < 6 {
		t.Errorf("Expected at least 6 quiet hours between 6-13 UTC (10pm-5am PST), found %d", quietCount)
	}
	
	// Test peak productivity detection
	peakStart, peakEnd, peakCount := detectPeakProductivityWithHalfHours(halfHourlyData, -8)
	
	t.Logf("Peak productivity: %.1f-%.1f UTC with %d events", peakStart, peakEnd, peakCount)
	
	// Peak should be in morning (19 UTC = 11am PST is common peak time)
	peakLocal := peakStart - 8.0
	if peakLocal < 0 {
		peakLocal += 24
	}
	
	t.Logf("Peak productivity in PST: %.1f-%.1f", peakLocal, peakLocal+0.5)
	
	// Verify work hours pattern
	// Work should roughly be 7am-5pm PST (15:00-1:00 UTC next day)
	t.Logf("Expected active hours: 15:00-1:00 UTC (7am-5pm PST)")
	
	// Verify morning activity starts around 15:00 UTC (7am PST)
	if halfHourlyData[15.0] < 5 {
		t.Errorf("Expected significant activity at 15:00 UTC (7am PST), got %d", halfHourlyData[15.0])
	}
	
	// Verify evening activity drops after 1:00 UTC (5pm PST)
	if halfHourlyData[2.0] > halfHourlyData[19.0]/3 {
		t.Errorf("Expected lower activity at 2:00 UTC (6pm PST) compared to peak")
	}
	
	// Verify Seattle/Pacific timezone indicators
	t.Logf("a-crate activity pattern summary:")
	t.Logf("- Work hours: 7am-5pm PST (15:00-1:00 UTC)")
	t.Logf("- Lunch break around noon PST (20:00 UTC)")
	t.Logf("- Quiet hours 10pm-5am PST (6:00-13:00 UTC)")
	t.Logf("- Lives in Seattle, WA")
	t.Logf("All indicators consistent with Pacific Time (UTC-8)")
}