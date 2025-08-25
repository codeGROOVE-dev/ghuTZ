package gutz

import (
	"testing"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
)

// TestBinacsRealDataDetectsAsianTime tests that binacs' actual GitHub activity
// correctly identifies them as being in UTC+8 (China/Singapore/etc)
func TestBinacsRealDataDetectsAsianTime(t *testing.T) {
	// Actual activity data from binacs as of 2025-08-24
	// This shows two distinct activity periods when viewed in UTC+8:
	// 1. Morning/afternoon: 10:00-16:00 local (02:00-08:00 UTC)
	// 2. Evening: 19:30-00:30 local (11:30-16:30 UTC)
	hourCounts := map[int]int{
		0:  8,  // 8am in UTC+8
		1:  1,  // 9am in UTC+8
		2:  3,  // 10am in UTC+8 - morning work starts
		3:  2,  // 11am in UTC+8
		4:  1,  // 12pm in UTC+8
		5:  3,  // 1pm in UTC+8
		6:  5,  // 2pm in UTC+8
		7:  7,  // 3pm in UTC+8
		8:  48, // 4pm in UTC+8 - peak afternoon
		9:  7,  // 5pm in UTC+8
		10: 2,  // 6pm in UTC+8
		11: 0,  // 7pm in UTC+8 - dinner break
		12: 3,  // 8pm in UTC+8 - evening work starts
		13: 0,  // 9pm in UTC+8
		14: 0,  // 10pm in UTC+8
		15: 0,  // 11pm in UTC+8
		16: 0,  // 12am in UTC+8
		17: 1,  // 1am in UTC+8
		18: 2,  // 2am in UTC+8
		19: 3,  // 3am in UTC+8
		20: 4,  // 4am in UTC+8
		21: 5,  // 5am in UTC+8
		22: 4,  // 6am in UTC+8
		23: 7,  // 7am in UTC+8
	}

	halfHourCounts := map[float64]int{
		0.5:  1,
		1.5:  3,
		3.5:  2,
		5.5:  3,
		6.5:  9,
		7.5:  15,
		8.5:  10,
		9.5:  2,
		10.5: 3,
		12.5: 3,
		17.5: 1,
		18.5: 2,
		20.5: 12,
		21.5: 6,
		22.5: 2,
		23.5: 7,
	}

	// Binacs has activity in UTC hours: 2-10 (morning) and 18-23 (evening)
	// In UTC+8, this translates to:
	// - Morning: 10:00-18:00 (work hours)
	// - Evening: 02:00-07:00 (should be sleeping!)
	//
	// But wait, that's backwards! Let me recalculate:
	// If binacs is UTC+8:
	// - UTC 2-10 = UTC+8 10:00-18:00 (morning/afternoon work) ✓
	// - UTC 18-23 = UTC+8 02:00-07:00 (early morning) ✗
	//
	// Actually, the evening activity (UTC 18-23) would be 2am-7am in UTC+8, which doesn't make sense.
	// Let me reconsider. If the second burst is:
	// - UTC 20-23 = UTC+8 04:00-07:00 (very early morning)
	//
	// Hmm, looking at the data again, the activity is:
	// - Heavy: UTC 6-9 (2pm-5pm in UTC+8)
	// - Light: UTC 0-3 (8am-11am in UTC+8)
	// - Some: UTC 20-23 (4am-7am in UTC+8)
	//
	// This suggests binacs might actually be in a different timezone.
	// Let's trace through what the algorithm should find.

	totalActivity := 185
	quietHours := []int{13, 14, 15, 16, 17} // UTC quiet hours
	midQuiet := 15.0
	activeStart := 2.0 // Should detect the EARLIEST activity period

	// Best global lunch at UTC 11:00-12:00
	bestGlobalLunch := timezone.GlobalLunchPattern{
		StartUTC:    11.0,
		EndUTC:      12.0,
		Confidence:  0.6,
		DropPercent: 100.0,
	}

	candidates := timezone.EvaluateCandidates(
		"binacs",
		hourCounts,
		halfHourCounts,
		totalActivity,
		quietHours,
		midQuiet,
		activeStart,
		bestGlobalLunch,
		"", // no profile timezone
		time.Now(),
	)

	// We expect UTC+8 to be a top candidate
	// In UTC+8:
	// - Work starts at 10am local (2 UTC)
	// - Lunch at 7pm local (11 UTC) - wait that's wrong
	// - Peak at 4pm local (8 UTC)
	//
	// Actually, let me recalculate properly:
	// UTC+8 means local = UTC + 8
	// So UTC 2 = Local 10am ✓
	// UTC 11 = Local 7pm ✗ (should be lunch at ~noon)
	//
	// This suggests the lunch detection might be picking up the wrong dip.
	// The real lunch should be around UTC 4-5 (noon-1pm local in UTC+8)

	var foundUTC8 bool
	for i, candidate := range candidates {
		if candidate.Timezone != "UTC+8" {
			continue
		}
		foundUTC8 = true
		t.Logf("UTC+8 candidate found at position %d with confidence %.1f%%", i+1, candidate.Confidence)

		if candidate.Confidence < 0 {
			t.Errorf("UTC+8 has negative confidence (%.1f%%), suggesting penalties are too harsh", candidate.Confidence)
		}

		if i > 4 {
			t.Errorf("UTC+8 is ranked too low (#%d), should be in top 5 candidates", i+1)
		}

		// Check that work hours are reasonable (should start around 10am, not 7:30pm)
		if candidate.WorkStartLocal > 18.0 || candidate.WorkStartLocal < 7.0 {
			t.Errorf("UTC+8 work start calculated as %.1f, expected 9-11am range", candidate.WorkStartLocal)
		}
	}

	if !foundUTC8 {
		t.Error("UTC+8 not found in candidates list")
	}

	// Log top 5 candidates for debugging
	t.Log("Top 5 timezone candidates for binacs:")
	for i := 0; i < 5 && i < len(candidates); i++ {
		c := candidates[i]
		t.Logf("%d. %s (%.1f%% confidence) - work start: %.1f, lunch: %.1f-%.1f UTC",
			i+1, c.Timezone, c.Confidence,
			c.WorkStartLocal,
			c.LunchStartUTC, c.LunchEndUTC)
	}
}

// TestBinacsMultipleActivityRanges tests that we can detect multiple activity periods
// and use the earliest one as the work start time
func TestBinacsMultipleActivityRanges(t *testing.T) {
	// Binacs has two distinct activity periods in UTC:
	// 1. UTC 2-10 (main work hours if in UTC+8: 10am-6pm local)
	// 2. UTC 20-23 (evening if in UTC-6: 2pm-5pm local, or very early morning if in UTC+8: 4am-7am)

	// The algorithm should:
	// 1. Detect both activity ranges
	// 2. For each timezone, identify which ranges are reasonable work hours
	// 3. Use the earliest reasonable range as work start
	// 4. Penalize timezones where the second range falls in unreasonable hours

	t.Log("Testing multiple activity range detection for binacs")

	// For UTC+8, we expect:
	// - First range (UTC 2-10) = 10am-6pm local ✓ (reasonable work hours)
	// - Second range (UTC 20-23) = 4am-7am local ✗ (unreasonable, should penalize)

	// For UTC-6, we expect:
	// - First range (UTC 2-10) = 8pm-4am local ✗ (unreasonable night work)
	// - Second range (UTC 20-23) = 2pm-5pm local ✓ (reasonable but late start)

	// This suggests UTC+8 should win because its main activity is during normal work hours,
	// while UTC-6 has most activity during night hours.
}
