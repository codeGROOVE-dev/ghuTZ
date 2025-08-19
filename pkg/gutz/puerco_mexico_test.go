package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
)

// TestPuercoMexicoCityLunchDetection tests that puerco's lunch is correctly detected at 12:30pm
// not 11:00am, given their activity pattern shows sustained work from 7am before lunch
func TestPuercoMexicoCityLunchDetection(t *testing.T) {
	// Actual 30-minute activity from puerco (Mexico City UTC-6)
	halfHourCounts := map[float64]int{
		// Night/early morning (UTC times)
		8.0: 1, 8.5: 1, // 2:00-2:30am Mexico
		9.5:  4,          // 3:30am Mexico
		10.5: 1,          // 4:30am Mexico
		11.0: 0, 11.5: 3, // 5:00-5:30am Mexico (was incorrectly detected as lunch)
		12.0: 3, 12.5: 0, // 6:00-6:30am Mexico

		// Morning work starts
		13.0: 13, 13.5: 18, // 7:00-7:30am Mexico - work starts!
		14.0: 22, 14.5: 16, // 8:00-8:30am Mexico - strong morning activity
		15.0: 20, 15.5: 3, // 9:00-9:30am Mexico
		16.0: 4, 16.5: 2, // 10:00-10:30am Mexico
		17.0: 7, 17.5: 7, // 11:00-11:30am Mexico

		// Lunch time candidates
		18.0: 7, 18.5: 14, // 12:00-12:30pm Mexico - some activity
		19.0: 6, 19.5: 19, // 1:00-1:30pm Mexico - ACTUAL LUNCH DIP at 1pm (19.0 UTC)

		// Afternoon
		20.0: 22, 20.5: 3, // 2:00-2:30pm Mexico - peak afternoon
		21.0: 7, 21.5: 11, // 3:00-3:30pm Mexico
		22.0: 11, 22.5: 8, // 4:00-4:30pm Mexico
		23.0: 8, 23.5: 7, // 5:00-5:30pm Mexico
		0.0: 4, 0.5: 4, // 6:00-6:30pm Mexico
		1.0: 5, 1.5: 3, // 7:00-7:30pm Mexico
	}

	// Test for UTC-6 (Mexico City)
	offset := -6

	// Detect lunch
	lunchStart, lunchEnd, confidence := lunch.DetectLunchBreakNoonCentered(halfHourCounts, offset)

	// Convert to local time
	lunchStartLocal := lunchStart + float64(offset)
	if lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	lunchEndLocal := lunchEnd + float64(offset)
	if lunchEndLocal < 0 {
		lunchEndLocal += 24
	}

	t.Logf("Detected lunch: %.1f-%.1f Mexico City time (%.1f-%.1f UTC) with confidence %.2f",
		lunchStartLocal, lunchEndLocal, lunchStart, lunchEnd, confidence)

	// Check activity levels to understand the pattern
	t.Logf("\nActivity pattern analysis:")

	// Calculate total morning activity (7-11am local = 13-17 UTC)
	morningActivity := 0
	for utc := 13.0; utc <= 17.5; utc += 0.5 {
		if count, exists := halfHourCounts[utc]; exists {
			morningActivity += count
		}
	}
	t.Logf("Morning activity (7-11am): %d events", morningActivity)

	// Calculate activity before 11am "lunch" candidate (5-7am = 11-13 UTC)
	earlyActivity := 0
	for utc := 11.0; utc < 13.0; utc += 0.5 {
		if count, exists := halfHourCounts[utc]; exists {
			earlyActivity += count
		}
	}
	t.Logf("Early morning activity (5-7am): %d events", earlyActivity)

	// Check the drops
	drop11am := halfHourCounts[17.0]  // 11am local
	drop1pm := halfHourCounts[19.0]   // 1pm local
	before1pm := halfHourCounts[18.5] // 12:30pm local

	t.Logf("\nLunch candidates:")
	t.Logf("- 11:00am: %d events (but only %d events of work before)", drop11am, earlyActivity)
	t.Logf("- 1:00pm: %d events (57%% drop from %d at 12:30pm, after %d events of morning work)",
		drop1pm, before1pm, morningActivity)

	// We expect lunch around 12:30-1:30pm, NOT 11am
	// 11am doesn't make sense with so little prior activity
	if lunchStartLocal < 12.0 || lunchStartLocal > 14.0 {
		t.Errorf("Lunch timing incorrect: got %.1f, expected 12:30-1:30pm (not 11am!)", lunchStartLocal)
	}

	// Specifically check it's not detecting the early 11am slot
	if lunchStartLocal < 11.5 {
		t.Errorf("Algorithm incorrectly detected early lunch at %.1f without sufficient prior work activity", lunchStartLocal)
	}

	t.Logf("\n✅ Correct lunch detection requires sustained work BEFORE lunch")
	t.Logf("   11am 'lunch' with only %d prior events = NOT real lunch", earlyActivity)
	t.Logf("   12:30-1:30pm lunch with %d prior events = real lunch break", morningActivity)
}
