package lunch

import (
	"testing"
)

func TestJamonationLunchDetection(t *testing.T) {
	// Jamonation's actual hourly activity from the logs
	hourlyUTC := []int{6, 9, 4, 1, 0, 0, 0, 0, 1, 0, 1, 7, 8, 28, 35, 33, 23, 30, 28, 27, 29, 25, 6, 11}

	// Create half-hour counts map (simulating real data collection)
	halfHourCounts := make(map[float64]int)

	// For this test, let's assume events are distributed across the hour
	// In reality, the actual timestamps would determine this
	for hour, count := range hourlyUTC {
		if count > 0 {
			// Distribute events across two half-hour buckets
			// For simplicity, split 60/40 for first/second half
			halfHourCounts[float64(hour)] = (count * 6) / 10
			halfHourCounts[float64(hour)+0.5] = (count * 4) / 10
		}
	}

	// Test for UTC-4 (Eastern Time - Toronto)
	utcOffset := -4
	lunchStart, lunchEnd, confidence := DetectLunchBreakNoonCentered(halfHourCounts, utcOffset)

	// Convert to local time for checking
	lunchStartLocal := lunchStart + float64(utcOffset)
	if lunchStartLocal < 0 {
		lunchStartLocal += 24
	}

	t.Logf("UTC-4 Lunch Detection Results:")
	t.Logf("  Lunch Start UTC: %.1f", lunchStart)
	t.Logf("  Lunch Start Local: %.1f", lunchStartLocal)
	t.Logf("  Lunch End UTC: %.1f", lunchEnd)
	t.Logf("  Confidence: %.2f", confidence)

	// Check the activity around noon local time (UTC 16)
	t.Logf("\nActivity around noon UTC-4:")
	for utc := 14.0; utc <= 18.0; utc += 0.5 {
		if count, exists := halfHourCounts[utc]; exists {
			local := utc + float64(utcOffset)
			t.Logf("  UTC %.1f (%.1f local): %d events", utc, local, count)
		}
	}

	// We expect lunch to be detected around noon
	if lunchStart < 0 {
		t.Error("No lunch detected for UTC-4, but there's a clear drop at noon!")
	} else if lunchStartLocal < 11.0 || lunchStartLocal > 13.0 {
		t.Errorf("Lunch detected at unusual time: %.1f local (expected 11-13)", lunchStartLocal)
	}

	// Also test UTC-8 (Pacific - where it's incorrectly placing them)
	utcOffset = -8
	lunchStart8, _, confidence8 := DetectLunchBreakNoonCentered(halfHourCounts, utcOffset)
	lunchStartLocal8 := lunchStart8 + float64(utcOffset)
	if lunchStartLocal8 < 0 {
		lunchStartLocal8 += 24
	}

	t.Logf("\nUTC-8 Lunch Detection Results:")
	t.Logf("  Lunch Start Local: %.1f", lunchStartLocal8)
	t.Logf("  Confidence: %.2f", confidence8)

	// UTC-4 should have better lunch detection than UTC-8
	if confidence8 > confidence && lunchStart < 0 {
		t.Error("UTC-8 has lunch detection but UTC-4 doesn't, despite UTC-4 having a clearer noon drop")
	}
}
