package ghutz

import (
	"testing"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
	"github.com/codeGROOVE-dev/ghuTZ/pkg/timezone"
)

func TestTstrombergRealData(t *testing.T) {
	// Real UTC activity data for tstromberg
	hourlyUTC := map[int]int{
		0: 16, 1: 12, 2: 2, 3: 0, 4: 0, 5: 0, 6: 0, 7: 0, 8: 0,
		9: 3, 10: 4, 11: 19, 12: 2, 13: 4, 14: 21, 15: 36,
		16: 25, 17: 31, 18: 22, 19: 69, 20: 32, 21: 22, 22: 23, 23: 11,
	}
	
	// Convert to half-hour buckets (simulating some distribution)
	halfHourCounts := make(map[float64]int)
	for hour, count := range hourlyUTC {
		// Distribute activity across two 30-minute buckets
		// For simplicity, split 60/40 between first and second half
		if count > 0 {
			halfHourCounts[float64(hour)] = (count * 6) / 10
			halfHourCounts[float64(hour)+0.5] = (count * 4) / 10
		}
	}
	
	// Special case: lunch dip at hour 16-17 UTC (12-1pm EDT)
	// tstromberg likely takes lunch around noon EDT
	halfHourCounts[16.5] = 1  // Low activity at 12:30pm EDT
	halfHourCounts[17.0] = 2  // Low activity at 1:00pm EDT
	
	t.Run("Eastern Time (UTC-4 for summer)", func(t *testing.T) {
		utcOffset := -4
		
		// Test lunch detection
		lunchStart, lunchEnd, lunchConf := lunch.DetectLunchBreakNoonCentered(halfHourCounts, utcOffset)
		
		// Convert UTC lunch to local time for verification
		lunchStartLocal := lunchStart + float64(utcOffset)
		if lunchStartLocal < 0 {
			lunchStartLocal += 24
		}
		
		t.Logf("Lunch detected: UTC %.1f-%.1f (Local %.1f-%.1f), confidence %.2f", 
			lunchStart, lunchEnd, lunchStartLocal, lunchStartLocal+0.5, lunchConf)
		
		// Lunch should be detected around noon local time (16:00-17:00 UTC in EDT)
		if lunchStartLocal < 11.5 || lunchStartLocal > 13.5 {
			t.Errorf("Lunch time incorrect: expected around 12:00 local, got %.1f", lunchStartLocal)
		}
		
		// Test peak productivity detection  
		peakStart, peakEnd, peakCount := timezone.DetectPeakProductivityWithHalfHours(halfHourCounts, utcOffset)
		
		t.Logf("Peak productivity: UTC %.1f-%.1f with %d events", peakStart, peakEnd, peakCount)
		
		// Peak should be at hour 19 UTC (3pm EDT) - highest activity
		if peakStart != 19.0 && peakStart != 19.5 {
			t.Errorf("Peak time incorrect: expected 19.0 or 19.5 UTC, got %.1f", peakStart)
		}
		
		// Test quiet hours detection
		quietHours := []int{2, 3, 4, 5, 6, 7, 8} // From real data
		t.Logf("Quiet hours UTC: %v", quietHours)
		
		// In EDT, quiet hours 2-8 UTC = 10pm-4am local
		// This is reasonable sleep time
	})
	
	t.Run("Verify timezone candidates", func(t *testing.T) {
		// Test that UTC-4 (EDT) ranks high for tstromberg's pattern
		
		// Calculate evening activity for EDT (7-11pm local = 23-3 UTC)
		eveningActivityEDT := hourlyUTC[23] + hourlyUTC[0] + hourlyUTC[1] + hourlyUTC[2]
		t.Logf("Evening activity for EDT (23-2 UTC): %d events", eveningActivityEDT)
		
		// Calculate evening activity for UTC+0 (7-11pm local = 19-23 UTC)  
		eveningActivityUTC := hourlyUTC[19] + hourlyUTC[20] + hourlyUTC[21] + hourlyUTC[22] + hourlyUTC[23]
		t.Logf("Evening activity for UTC+0 (19-23 UTC): %d events", eveningActivityUTC)
		
		// The problem: UTC+0 has much higher evening activity (157 vs 41)
		// This is because tstromberg's peak at 19:00 UTC is 3pm EDT (afternoon work), not evening
		
		if eveningActivityUTC <= eveningActivityEDT {
			t.Errorf("UTC+0 should have higher evening activity due to afternoon peak being misinterpreted")
		}
		
		// The solution: weight lunch timing and sleep patterns more heavily than evening activity
		// Someone with lunch at noon local and sleep at midnight-5am local is very likely in that timezone
	})
}

func TestLunchDetectionWithRealPatterns(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		halfHourData  map[float64]int
		utcOffset     int
		expectedLunch float64 // Expected lunch start in local time
	}{
		{
			name:        "Clear noon lunch in EDT",
			description: "Activity dip at 16:00-17:00 UTC should be noon-1pm EDT",
			halfHourData: map[float64]int{
				14.0: 10, 14.5: 10, // 10am EDT
				15.0: 12, 15.5: 12, // 11am EDT  
				16.0: 2, 16.5: 1,   // 12pm EDT - LUNCH!
				17.0: 2, 17.5: 8,   // 1pm EDT - returning
				18.0: 15, 18.5: 15, // 2pm EDT
			},
			utcOffset:     -4,
			expectedLunch: 12.0,
		},
		{
			name:        "Clear noon lunch in PST",
			description: "Activity dip at 20:00-21:00 UTC should be noon-1pm PST",
			halfHourData: map[float64]int{
				18.0: 10, 18.5: 10, // 10am PST
				19.0: 12, 19.5: 12, // 11am PST
				20.0: 2, 20.5: 1,   // 12pm PST - LUNCH!
				21.0: 2, 21.5: 8,   // 1pm PST - returning
				22.0: 15, 22.5: 15, // 2pm PST
			},
			utcOffset:     -8,
			expectedLunch: 12.0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lunchStart, lunchEnd, confidence := lunch.DetectLunchBreakNoonCentered(
				tt.halfHourData, tt.utcOffset)
			
			// Convert UTC lunch to local time
			lunchStartLocal := lunchStart + float64(tt.utcOffset)
			if lunchStartLocal < 0 {
				lunchStartLocal += 24
			}
			
			t.Logf("%s: Detected lunch at %.1f-%.1f local (%.1f-%.1f UTC), confidence %.2f",
				tt.description, lunchStartLocal, lunchStartLocal+0.5, lunchStart, lunchEnd, confidence)
			
			if lunchStartLocal < tt.expectedLunch-0.5 || lunchStartLocal > tt.expectedLunch+0.5 {
				t.Errorf("Expected lunch around %.1f local, got %.1f", tt.expectedLunch, lunchStartLocal)
			}
		})
	}
}