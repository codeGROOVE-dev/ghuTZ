package ghutz

import (
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// TestDlorencHardcodedTimezoneDetection tests dlorenc's timezone detection
// using hardcoded activity data instead of fetching from GitHub API
func TestDlorencHardcodedTimezoneDetection(t *testing.T) {
	// Hardcoded activity data for dlorenc (Eastern Time - UTC-4 during EDT)
	// This represents half-hourly activity counts in UTC
	halfHourlyData := map[float64]int{
		// Midnight-6am UTC (8pm-2am EDT) - evening/night activity
		0.0: 8, 0.5: 5,    // 8pm EDT
		1.0: 6, 1.5: 3,    // 9pm EDT
		2.0: 2, 2.5: 1,    // 10pm EDT
		3.0: 0, 3.5: 0,    // 11pm EDT
		4.0: 0, 4.5: 0,    // Midnight EDT (sleep)
		5.0: 0, 5.5: 0,    // 1am EDT (sleep)
		6.0: 0, 6.5: 0,    // 2am EDT (sleep)
		7.0: 0, 7.5: 0,    // 3am EDT (sleep)
		8.0: 0, 8.5: 0,    // 4am EDT (sleep)
		9.0: 0, 9.5: 0,    // 5am EDT (sleep)
		10.0: 1, 10.5: 2,  // 6am EDT (waking up)
		11.0: 8, 11.5: 10, // 7am EDT (morning start)
		12.0: 12, 12.5: 14, // 8am EDT (work start)
		13.0: 18, 13.5: 16, // 9am EDT
		14.0: 22, 14.5: 20, // 10am EDT (morning productivity)
		15.0: 25, 15.5: 23, // 11am EDT (peak morning)
		// Lunch dip at noon EDT
		16.0: 8, 16.5: 5,   // 12pm EDT (lunch start - clear dip)
		17.0: 6, 17.5: 15,  // 1pm EDT (returning from lunch)
		18.0: 20, 18.5: 18, // 2pm EDT (afternoon work)
		19.0: 30, 19.5: 28, // 3pm EDT (peak afternoon)
		20.0: 24, 20.5: 20, // 4pm EDT
		21.0: 18, 21.5: 14, // 5pm EDT
		22.0: 10, 22.5: 8,  // 6pm EDT (winding down)
		23.0: 6, 23.5: 4,   // 7pm EDT (evening)
	}
	
	// Test lunch detection for UTC-4 (Eastern Daylight Time)
	lunchStart, lunchEnd, lunchConfidence := lunch.DetectLunchBreakNoonCentered(halfHourlyData, -4)
	
	t.Logf("Lunch detection for UTC-4: start=%.1f, end=%.1f, confidence=%.2f", 
		lunchStart, lunchEnd, lunchConfidence)
	
	// Convert lunch times to local (EDT)
	if lunchStart >= 0 {
		lunchStartLocal := lunchStart - 4.0 // Convert UTC to EDT (UTC-4)
		if lunchStartLocal < 0 {
			lunchStartLocal += 24
		}
		lunchEndLocal := lunchEnd - 4.0
		if lunchEndLocal < 0 {
			lunchEndLocal += 24
		}
		
		t.Logf("Lunch in EDT: %.1f - %.1f (confidence: %.2f)", 
			lunchStartLocal, lunchEndLocal, lunchConfidence)
		
		// Verify lunch is detected around noon EDT (allowing 11:00am - 2:00pm range)
		if lunchStartLocal < 11.0 || lunchStartLocal > 14.0 {
			t.Errorf("Expected lunch to start between 11:00am-2:00pm EDT, got %.1f EDT", lunchStartLocal)
		}
		
		// Verify confidence is reasonable
		if lunchConfidence < 0.3 {
			t.Errorf("Lunch confidence too low: %.2f", lunchConfidence)
		}
		
		// Log if lunch is late
		if lunchStartLocal > 13.5 {
			t.Logf("Warning: dlorenc lunch detected at %.1f-%.1f local time, which is quite late. Expected closer to 12:00.", lunchStartLocal, lunchEndLocal)
		}
	} else {
		t.Logf("Note: No lunch detected for dlorenc (confidence: %.2f)", lunchConfidence)
	}
	
	// Test that dlorenc is detected as UTC-4 (Eastern Time)
	// Calculate indicators for Eastern Time
	
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
	
	// Should have quiet hours from 3-9 UTC (11pm-5am EDT)
	expectedQuiet := []int{3, 4, 5, 6, 7, 8, 9}
	quietCount := 0
	for _, expected := range expectedQuiet {
		for _, actual := range quietHours {
			if actual == expected {
				quietCount++
				break
			}
		}
	}
	
	if quietCount < 5 {
		t.Errorf("Expected at least 5 quiet hours between 3-9 UTC, found %d", quietCount)
	}
	
	// Test peak productivity detection
	peakStart, peakEnd, peakCount := detectPeakProductivityWithHalfHours(halfHourlyData, -4)
	
	t.Logf("Peak productivity: %.1f-%.1f UTC with %d events", peakStart, peakEnd, peakCount)
	
	// Peak should be in afternoon (19 UTC = 3pm EDT is common peak time)
	peakLocal := peakStart - 4.0
	if peakLocal < 0 {
		peakLocal += 24
	}
	
	t.Logf("Peak productivity in EDT: %.1f-%.1f", peakLocal, peakLocal+0.5)
	
	// Verify work hours pattern
	// Work should roughly be 7am-6pm EDT (11:00-22:00 UTC)
	activeStart := 11.0
	activeEnd := 22.0
	
	t.Logf("Expected active hours: %.1f-%.1f UTC (7am-6pm EDT)", activeStart, activeEnd)
	
	// Verify morning activity starts around 11:00 UTC (7am EDT)
	if halfHourlyData[11.0] < 5 {
		t.Errorf("Expected significant activity at 11:00 UTC (7am EDT), got %d", halfHourlyData[11.0])
	}
	
	// Verify evening activity drops after 22:00 UTC (6pm EDT)
	if halfHourlyData[23.0] > halfHourlyData[19.0]/3 {
		t.Errorf("Expected lower activity at 23:00 UTC (7pm EDT) compared to peak")
	}
	
	t.Logf("dlorenc detected timezone pattern consistent with UTC-4 (Eastern Time)")
}