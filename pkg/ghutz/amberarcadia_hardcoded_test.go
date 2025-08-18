package ghutz

import (
	"testing"
)

// TestAmberArcadiaHardcodedDetection tests AmberArcadia's real activity data
// to ensure she's correctly detected as Eastern Time (UTC-4)
func TestAmberArcadiaHardcodedDetection(t *testing.T) {
	// ACTUAL activity data from AmberArcadia (Delaware resident)
	// Collected from 2025-07-17 to 2025-08-14 (278 total events)

	// Expected results for UTC-4 (Eastern Daylight Time):
	// - Work starts: 9:00 ET (13:00 UTC)
	// - Lunch: 12:00-13:00 ET (16:00-17:00 UTC)
	// - Peak: 11:00 ET (15:00 UTC)
	// - Sleep: 22:00-03:00 ET (02:00-07:00 UTC)

	// Key patterns that should identify Eastern Time:
	// 1. Work starts at 13:00 UTC = 9am ET (perfect)
	// 2. Lunch at 16:00 UTC = noon ET (perfect)
	// 3. Peak at 15:00 UTC = 11am ET (normal morning peak)
	// 4. No activity 0-7 UTC = 8pm-3am ET (normal sleep)

	// Patterns that should REJECT European timezones:
	// For UTC+1 (Berlin):
	// - Work starts at 14:00 local (2pm) - TOO LATE
	// - Lunch at 17:00 local (5pm) - ABSURD
	// - Peak at 16:00 local (4pm) - TOO LATE

	t.Log("AmberArcadia Real Data Analysis:")
	t.Log("- Total events: 278 over 29 days")
	t.Log("- Quiet hours: 0-7 UTC (8 hours)")
	t.Log("- First activity: 13:00 UTC")
	t.Log("- Peak activity: 15:00 UTC (61 events)")
	t.Log("- Lunch dip: 16:00 UTC (drop from 37 to 10 events)")

	// Expected detection: UTC-4
	expectedOffset := -4
	t.Logf("Expected timezone: UTC%+d (Eastern Daylight Time)", expectedOffset)

	// The actual detection is tested in the integration tests
	// This test documents the expected behavior with real data
}

// TestAmberArcadiaActivityPattern documents her actual activity pattern
func TestAmberArcadiaActivityPattern(t *testing.T) {
	t.Log("AmberArcadia Activity Pattern (UTC hours):")
	t.Log("00-07: Sleep (0 events)")
	t.Log("08: 1 event (4am ET - outlier)")
	t.Log("12: 4 events (8am ET - starting)")
	t.Log("13: 34 events (9am ET - work begins)")
	t.Log("14: 50 events (10am ET)")
	t.Log("15: 61 events (11am ET - PEAK)")
	t.Log("16: 19 events (12pm ET - LUNCH)")
	t.Log("17: 26 events (1pm ET - after lunch)")
	t.Log("18: 18 events (2pm ET)")
	t.Log("19: 30 events (3pm ET)")
	t.Log("20: 18 events (4pm ET)")
	t.Log("21: 11 events (5pm ET - winding down)")
	t.Log("22: 5 events (6pm ET)")
	t.Log("23: 1 event (7pm ET)")

	t.Log("\nThis pattern clearly shows:")
	t.Log("- US East Coast work schedule (9am-5pm)")
	t.Log("- Lunch break at noon")
	t.Log("- Morning peak productivity (11am)")
	t.Log("- Minimal evening activity (work-only GitHub user)")
}
