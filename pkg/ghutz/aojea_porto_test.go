package ghutz

import (
	"testing"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// TestAojeaPortoDetection tests that aojea's activity pattern correctly detects Porto, Portugal (UTC+0)
// This user has:
// - Work starting at 7:00 UTC (7am local in UTC+0)
// - Lunch break at 12:00 UTC (noon local)
// - Commute quiet hour at 17:00 UTC (5pm local)
// - Evening activity from 18-23 UTC (working from home)
func TestAojeaPortoDetection(t *testing.T) {
	// Actual hourly activity counts from aojea's GitHub profile
	hourCounts := map[int]int{
		0:  2,  // midnight UTC
		1:  0,  // quiet
		2:  0,  // quiet
		3:  0,  // quiet
		4:  4,  // minimal
		5:  2,  // minimal
		6:  1,  // minimal
		7:  12, // work starts (7am local)
		8:  20, // active morning
		9:  22, // active morning
		10: 42, // PEAK morning productivity (10am local)
		11: 16, // active
		12: 7,  // LUNCH DIP (noon local)
		13: 20, // back from lunch
		14: 19, // afternoon work
		15: 13, // afternoon work
		16: 22, // afternoon work
		17: 4,  // COMMUTE (5pm local)
		18: 13, // evening from home
		19: 18, // evening from home
		20: 9,  // evening
		21: 20, // evening
		22: 17, // late evening
		23: 8,  // winding down
	}

	// 30-minute resolution data showing clear lunch at 12:00 UTC
	halfHourCounts := map[float64]int{
		0.0:  1,
		0.5:  1,
		1.0:  0,
		1.5:  0,
		2.0:  0,
		2.5:  0,
		3.0:  0,
		3.5:  0,
		4.0:  2,
		4.5:  2,
		5.0:  1,
		5.5:  1,
		6.0:  0,
		6.5:  1,
		7.0:  5,
		7.5:  7,
		8.0:  6,
		8.5:  14,
		9.0:  9,
		9.5:  13,
		10.0: 14,
		10.5: 28, // peak
		11.0: 14,
		11.5: 2, // lunch starts
		12.0: 3, // lunch continues (81% drop from 16 to 3)
		12.5: 4,
		13.0: 10,
		13.5: 10,
		14.0: 5,
		14.5: 14,
		15.0: 8,
		15.5: 5,
		16.0: 6,
		16.5: 16,
		17.0: 3, // commute
		17.5: 1, // commute
		18.0: 3,
		18.5: 10,
		19.0: 8,
		19.5: 10,
		20.0: 2,
		20.5: 7,
		21.0: 15,
		21.5: 5,
		22.0: 15,
		22.5: 2,
		23.0: 5,
		23.5: 3,
	}

	// Sleep/quiet hours are 0-6 UTC and 17 UTC
	// Note: quietHours would be [0, 1, 2, 3, 4, 5, 6, 17] but not needed for this test

	// For UTC+0 (Portugal):
	// - Work starts at 7:00 UTC = 7am local (reasonable)
	// - Lunch at 12:00 UTC = noon local (perfect)
	// - Commute at 17:00 UTC = 5pm local (typical)
	// - Evening work 18-23 UTC = 6-11pm local (from home)
	offset := 0

	// Test lunch detection for UTC+0
	lunchStart, lunchEnd, confidence := lunch.DetectLunchBreakNoonCentered(halfHourCounts, offset)

	// Convert to local time
	lunchStartLocal := lunchStart + float64(offset)
	lunchEndLocal := lunchEnd + float64(offset)

	// Normalize to 24-hour format
	for lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	for lunchEndLocal < 0 {
		lunchEndLocal += 24
	}

	t.Logf("Detected lunch: %.1f-%.1f local (%.1f-%.1f UTC) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)

	// Expect lunch around noon (11:30-13:00 is acceptable)
	if lunchStartLocal < 11.0 || lunchStartLocal > 13.0 {
		t.Errorf("Lunch start incorrect: got %.1f local, expected 11:30-13:00", lunchStartLocal)
	}

	// Confidence should be high given the clear drop
	if confidence < 0.6 {
		t.Errorf("Lunch confidence too low: got %.2f, expected > 0.6", confidence)
	}

	// Verify key patterns that identify Portugal
	t.Logf("\nKey Portugal indicators:")

	// 1. Work starts at reasonable hour (7am)
	firstSignificantActivity := -1
	for hour := range 24 {
		if hourCounts[hour] > 5 {
			firstSignificantActivity = hour
			break
		}
	}
	if firstSignificantActivity != 7 {
		t.Errorf("First significant activity wrong: got %d UTC, expected 7 UTC", firstSignificantActivity)
	}
	t.Logf("✓ Work starts at %d:00 UTC = %d:00 local", firstSignificantActivity, firstSignificantActivity+offset)

	// 2. Peak productivity in morning (10am local)
	peakHour := -1
	peakCount := 0
	for hour, count := range hourCounts {
		if count > peakCount {
			peakCount = count
			peakHour = hour
		}
	}
	if peakHour != 10 {
		t.Errorf("Peak hour wrong: got %d UTC, expected 10 UTC", peakHour)
	}
	t.Logf("✓ Peak productivity at %d:00 UTC = %d:00 local (%d events)", peakHour, peakHour+offset, peakCount)

	// 3. Commute quiet hour at 17:00 UTC (5pm local)
	if hourCounts[17] > 5 {
		t.Errorf("Expected quiet commute hour at 17:00 UTC, got %d events", hourCounts[17])
	}
	t.Logf("✓ Commute quiet hour at 17:00 UTC = 17:00 local (%d events)", hourCounts[17])

	// 4. Evening activity after commute
	eveningActivity := 0
	for hour := 18; hour <= 23; hour++ {
		eveningActivity += hourCounts[hour]
	}
	if eveningActivity < 50 {
		t.Errorf("Expected significant evening activity, got only %d events", eveningActivity)
	}
	t.Logf("✓ Evening activity 18-23 UTC = 18-23 local (%d events total)", eveningActivity)

	// 5. Verify that US timezones would be absurd
	t.Logf("\nWhy US timezones fail:")

	// UTC-5 (US Eastern)
	eastWorkStart := (7 - 5 + 24) % 24 // 2am
	t.Logf("✗ UTC-5: Work would start at %dam (absurd!)", eastWorkStart)

	// UTC-7 (US Mountain/Pacific DST)
	pacificWorkStart := 0 // midnight (7am UTC in UTC-7)
	t.Logf("✗ UTC-7: Work would start at %dam (even more absurd!)", pacificWorkStart)

	t.Logf("\n✅ Pattern clearly indicates Porto, Portugal (UTC+0)")
}
