package timezone

import (
	"testing"
	"time"
)

func TestSteveBeattiePacificTimezone(t *testing.T) {
	// stevebeattie's actual activity data from the system
	// He lives in Portland, OR (UTC-7 in summer, UTC-8 in winter)
	hourCounts := map[int]int{
		0:  11, // 5pm PST / 4pm PDT
		1:  5,  // 6pm PST / 5pm PDT
		2:  12, // 7pm PST / 6pm PDT
		3:  5,  // 8pm PST / 7pm PDT
		4:  11, // 9pm PST / 8pm PDT
		5:  8,  // 10pm PST / 9pm PDT
		6:  11, // 11pm PST / 10pm PDT
		7:  7,  // midnight PST / 11pm PDT
		8:  6,  // 1am PST / midnight PDT
		9:  3,  // 2am PST / 1am PDT
		10: 2,  // 3am PST / 2am PDT
		11: 0,  // 4am PST / 3am PDT
		12: 0,  // 5am PST / 4am PDT
		13: 0,  // 6am PST / 5am PDT
		14: 0,  // 7am PST / 6am PDT
		15: 9,  // 8am PST / 7am PDT
		16: 8,  // 9am PST / 8am PDT
		17: 23, // 10am PST / 9am PDT
		18: 23, // 11am PST / 10am PDT
		19: 13, // noon PST / 11am PDT
		20: 10, // 1pm PST / noon PDT (lunch dip)
		21: 11, // 2pm PST / 1pm PDT
		22: 13, // 3pm PST / 2pm PDT
		23: 16, // 4pm PST / 3pm PDT
	}

	// Half-hour counts for lunch detection
	halfHourCounts := map[float64]int{
		0.0:  6,
		0.5:  5,
		1.0:  3,
		1.5:  2,
		2.0:  7,
		2.5:  5,
		3.0:  3,
		3.5:  2,
		4.0:  6,
		4.5:  5,
		5.0:  4,
		5.5:  4,
		6.0:  6,
		6.5:  5,
		7.0:  4,
		7.5:  3,
		8.0:  3,
		8.5:  3,
		9.0:  2,
		9.5:  1,
		10.0: 1,
		10.5: 1,
		11.0: 0,
		11.5: 0,
		12.0: 0,
		12.5: 0,
		13.0: 0,
		13.5: 0,
		14.0: 0,
		14.5: 0,
		15.0: 5,
		15.5: 4,
		16.0: 4,
		16.5: 4,
		17.0: 12,
		17.5: 11,
		18.0: 12,
		18.5: 11,
		19.0: 7,
		19.5: 6,
		20.0: 5, // Lunch dip at noon PDT
		20.5: 5,
		21.0: 6,
		21.5: 5,
		22.0: 7,
		22.5: 6,
		23.0: 8,
		23.5: 8,
	}

	totalActivity := 207
	quietHours := []int{10, 11, 12, 13, 14} // UTC quiet hours
	midQuiet := 12.5
	activeStart := 15.0 // First significant activity at 15 UTC

	bestGlobalLunch := GlobalLunchPattern{
		StartUTC:    20.5,
		EndUTC:      21.0,
		Confidence:  0.8,
		DropPercent: 50.0,
	}

	// Evaluate candidates
	candidates := EvaluateCandidates("stevebeattie", hourCounts, halfHourCounts,
		totalActivity, quietHours, midQuiet, activeStart, bestGlobalLunch, "", time.Now())

	// Check that we have multiple candidates
	if len(candidates) < 3 {
		t.Errorf("Expected at least 3 candidates, got %d", len(candidates))
	}

	// Check that UTC-7 or UTC-8 is in the top 3
	foundPacific := false
	for i := 0; i < len(candidates) && i < 3; i++ {
		offset := candidates[i].Offset
		if offset == -7 || offset == -8 {
			foundPacific = true
			t.Logf("Found Pacific timezone at position %d: UTC%+.0f with confidence %.1f%%",
				i+1, offset, candidates[i].Confidence)
			break
		}
	}

	if !foundPacific {
		t.Errorf("UTC-7 or UTC-8 should be in top 3 candidates for stevebeattie (Portland, OR)")
		t.Logf("Top 3 candidates:")
		for i := 0; i < len(candidates) && i < 3; i++ {
			t.Logf("  %d. UTC%+.0f (%.1f%% confidence)",
				i+1, candidates[i].Offset, candidates[i].Confidence)
		}
	}

	// Additionally check that UTC-7 has reasonable confidence (>5%)
	for _, c := range candidates {
		if c.Offset == -7 && c.Confidence < 5 {
			t.Errorf("UTC-7 confidence too low: %.1f%% (expected >5%%)", c.Confidence)
		}
	}
}
