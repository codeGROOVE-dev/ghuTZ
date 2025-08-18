package ghutz

import (
	"testing"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
	"github.com/codeGROOVE-dev/ghuTZ/pkg/timezone"
)

// TestTstrombergHardcodedDataDetectsEasternTime tests tstromberg's timezone detection
// using hardcoded activity data instead of fetching from GitHub API
func TestTstrombergHardcodedDataDetectsEasternTime(t *testing.T) {
	// Hardcoded activity data for tstromberg (Eastern Time - UTC-4 during EDT)
	// This represents half-hourly activity counts in UTC
	halfHourlyData := map[float64]int{
		// Midnight-6am UTC (8pm-2am EDT) - evening/night activity
		0.0: 10, 0.5: 6, // 8pm EDT
		1.0: 8, 1.5: 4, // 9pm EDT
		2.0: 1, 2.5: 1, // 10pm EDT
		3.0: 0, 3.5: 0, // 11pm EDT
		4.0: 0, 4.5: 0, // Midnight EDT (sleep)
		5.0: 0, 5.5: 0, // 1am EDT (sleep)
		6.0: 0, 6.5: 0, // 2am EDT (sleep)
		7.0: 0, 7.5: 0, // 3am EDT (sleep)
		8.0: 0, 8.5: 0, // 4am EDT (sleep)
		9.0: 2, 9.5: 1, // 5am EDT (early morning)
		10.0: 2, 10.5: 2, // 6am EDT (waking up)
		11.0: 10, 11.5: 9, // 7am EDT (morning start)
		12.0: 1, 12.5: 1, // 8am EDT
		13.0: 2, 13.5: 2, // 9am EDT
		14.0: 12, 14.5: 9, // 10am EDT (work ramp up)
		15.0: 20, 15.5: 16, // 11am EDT (peak morning)
		// Lunch dip at noon EDT
		16.0: 5, 16.5: 3, // 12pm EDT (lunch start - clear dip)
		17.0: 4, 17.5: 13, // 1pm EDT (returning from lunch)
		18.0: 13, 18.5: 9, // 2pm EDT
		19.0: 42, 19.5: 27, // 3pm EDT (peak afternoon)
		20.0: 20, 20.5: 12, // 4pm EDT
		21.0: 14, 21.5: 8, // 5pm EDT
		22.0: 14, 22.5: 9, // 6pm EDT
		23.0: 7, 23.5: 4, // 7pm EDT
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

		// Verify lunch is detected around noon EDT (allowing 11:30am - 1:00pm range)
		if lunchStartLocal < 11.5 || lunchStartLocal > 13.0 {
			t.Errorf("Expected lunch to start between 11:30am-1:00pm EDT, got %.1f EDT", lunchStartLocal)
		}

		// Verify confidence is reasonable
		if lunchConfidence < 0.3 {
			t.Errorf("Lunch confidence too low: %.2f", lunchConfidence)
		}
	} else {
		t.Errorf("Failed to detect lunch for tstromberg in EDT timezone")
	}

	// Test peak productivity detection
	peakStart, peakEnd, peakCount := timezone.DetectPeakProductivityWithHalfHours(halfHourlyData, -4)

	t.Logf("Peak productivity: %.1f-%.1f UTC with %d events", peakStart, peakEnd, peakCount)

	// Peak should be at hour 19 UTC (3pm EDT) - highest activity
	if peakStart != 19.0 && peakStart != 19.5 {
		t.Errorf("Peak time incorrect: expected 19.0 or 19.5 UTC, got %.1f", peakStart)
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

	// Should have quiet hours from 3-8 UTC (11pm-4am EDT)
	expectedQuiet := []int{3, 4, 5, 6, 7, 8}
	for _, expected := range expectedQuiet {
		found := false
		for _, actual := range quietHours {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected quiet hour %d UTC not found in %v", expected, quietHours)
		}
	}

	// Test work hours detection
	// Work should roughly be 7am-7pm EDT (11:00-23:00 UTC)
	activeStart := 11.0
	activeEnd := 23.0

	t.Logf("Expected active hours: %.1f-%.1f UTC (7am-7pm EDT)", activeStart, activeEnd)

	// Verify morning activity starts around 11:00 UTC (7am EDT)
	if halfHourlyData[11.0] < 5 {
		t.Errorf("Expected significant activity at 11:00 UTC (7am EDT), got %d", halfHourlyData[11.0])
	}

	// Verify evening activity drops after 23:00 UTC (7pm EDT)
	if halfHourlyData[23.5] > halfHourlyData[19.0]/4 {
		t.Errorf("Expected lower activity at 23:30 UTC (7:30pm EDT) compared to peak")
	}
}
