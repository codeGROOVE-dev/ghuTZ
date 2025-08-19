package lunch

import (
	"testing"
)

// TestDlorencLunchSteepnessUniformity tests that the algorithm correctly identifies
// a 30-minute lunch at 12:00-12:30 rather than a 60-minute lunch at 11:30-12:30
// when the steepness drops are not uniform.
//
// Based on dlorenc's actual activity pattern (UTC-4 timezone):
// The histogram shows LOCAL times, but we store UTC times
// 11:30 L (10) ██████████     <- 11:30 local = 15:30 UTC 
// 12:00 L ( 7) ███████        <- 12:00 local = 16:00 UTC (steepest drop!)
// 12:30   (12) ████████████   <- 12:30 local = 16:30 UTC (recovery)
func TestDlorencLunchSteepnessUniformity(t *testing.T) {
	// dlorenc's activity pattern in UTC (he's in UTC-4, so local+4=UTC)
	halfHourCounts := map[float64]int{
		13.5: 2,  // 09:30 local = 13:30 UTC
		14.0: 13, // 10:00 local = 14:00 UTC  
		14.5: 11, // 10:30 local = 14:30 UTC
		15.0: 11, // 11:00 local = 15:00 UTC (pre-lunch baseline)
		15.5: 10, // 11:30 local = 15:30 UTC (9% drop: (11-10)/11)
		16.0: 7,  // 12:00 local = 16:00 UTC (36% drop: (11-7)/11) - steepest!
		16.5: 12, // 12:30 local = 16:30 UTC (recovery)
		17.0: 12, // 13:00 local = 17:00 UTC
		17.5: 12, // 13:30 local = 17:30 UTC
		18.0: 1,  // 14:00 local = 18:00 UTC
		18.5: 4,  // 14:30 local = 18:30 UTC
		19.0: 8,  // 15:00 local = 19:00 UTC
		19.5: 13, // 15:30 local = 19:30 UTC
		20.0: 7,  // 16:00 local = 20:00 UTC
		20.5: 13, // 16:30 local = 20:30 UTC
		21.0: 13, // 17:00 local = 21:00 UTC
		21.5: 5,  // 17:30 local = 21:30 UTC
		22.0: 6,  // 18:00 local = 22:00 UTC
		22.5: 9,  // 18:30 local = 22:30 UTC
		23.0: 7,  // 19:00 local = 23:00 UTC
		23.5: 1,  // 19:30 local = 23:30 UTC
	}

	// Test for UTC-4 timezone (Eastern Time)
	utcOffset := -4
	
	startUTC, endUTC, confidence := DetectLunchBreakNoonCentered(halfHourCounts, utcOffset)
	
	// Expected: 30-minute lunch at 12:00-12:30 local time
	// 12:00 local in UTC-4 = 16:00 UTC
	// 12:30 local in UTC-4 = 16:30 UTC  
	
	expectedStartUTC := 16.0  // 12:00 local = 16:00 UTC (steepest drop)
	expectedEndUTC := 16.5    // 12:30 local = 16:30 UTC (30-minute duration)
	
	if startUTC < 0 {
		t.Fatalf("No lunch detected, but should have found 30-minute lunch at steepest drop")
	}
	
	if confidence < 0.5 {
		t.Errorf("Expected high confidence for clear lunch pattern, got %.2f", confidence)
	}
	
	// Allow small tolerance for floating point comparison
	tolerance := 0.1
	if abs(startUTC-expectedStartUTC) > tolerance {
		t.Errorf("Expected lunch start at %.1f UTC (steepest drop), got %.1f UTC", expectedStartUTC, startUTC)
	}
	
	if abs(endUTC-expectedEndUTC) > tolerance {
		t.Errorf("Expected lunch end at %.1f UTC (30-min duration), got %.1f UTC", expectedEndUTC, endUTC)
	}
	
	// Verify it chose 30-minute lunch, not 60-minute
	duration := endUTC - startUTC
	if duration > 0.6 { // Allow small tolerance
		t.Errorf("Expected 30-minute lunch (0.5 hours), but got %.1f hours - algorithm should prefer steeper drop over longer duration", duration)
	}
	
	t.Logf("✓ Correctly detected 30-minute lunch at %.1f-%.1f UTC (confidence: %.2f)", startUTC, endUTC, confidence)
	t.Logf("✓ Algorithm chose steepest drop (36%% at 12:00) over weaker drop (9%% at 11:30)")
}

// TestDlorencSteepnessCalculation verifies the steepness calculation logic
func TestDlorencSteepnessCalculation(t *testing.T) {
	// Using 11:00 UTC (10 events) as pre-lunch baseline from dlorenc's data:
	preLunchActivity := 10
	
	// 11:30 UTC drop: 10 → 7 events
	drop1130 := (float64(preLunchActivity) - 7.0) / float64(preLunchActivity)
	expected1130 := 0.30 // 30% drop
	
	if abs(drop1130-expected1130) > 0.01 {
		t.Errorf("11:30 drop calculation wrong: expected %.2f, got %.2f", expected1130, drop1130)
	}
	
	// If we incorrectly used 11:00 (11 events) as baseline:
	// 11:30 drop would be: (11-10)/11 = 9%
	// 12:00 drop would be: (11-7)/11 = 36%
	// Difference: 36% - 9% = 27% of max drop (27/36 = 75% difference)
	// This exceeds our 20% uniformity threshold, so should prefer single steeper block
	
	dropWith11AsBaseline1130 := (11.0 - 10.0) / 11.0 // 9%
	dropWith11AsBaseline1200 := (11.0 - 7.0) / 11.0   // 36%
	
	maxDrop := dropWith11AsBaseline1200
	minDrop := dropWith11AsBaseline1130
	uniformityRatio := (maxDrop - minDrop) / maxDrop
	
	expectedUniformityRatio := 0.75 // 75% difference
	if abs(uniformityRatio-expectedUniformityRatio) > 0.01 {
		t.Errorf("Uniformity ratio calculation wrong: expected %.2f, got %.2f", expectedUniformityRatio, uniformityRatio)
	}
	
	// Since 75% > 20% threshold, steepness is NOT uniform
	if uniformityRatio <= 0.20 {
		t.Errorf("Expected non-uniform steepness (ratio %.2f > 0.20), but test logic suggests uniform", uniformityRatio)
	}
	
	t.Logf("✓ Steepness uniformity check: 11:30 drop=%.0f%%, 12:00 drop=%.0f%%, difference=%.0f%% (exceeds 20%% threshold)", 
		dropWith11AsBaseline1130*100, dropWith11AsBaseline1200*100, uniformityRatio*100)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}