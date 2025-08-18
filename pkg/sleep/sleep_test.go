package sleep

import (
	"testing"
)

func TestFindQuietHours(t *testing.T) {
	// Create a pattern with clear quiet hours (midnight to 6am)
	hourCounts := make(map[int]int)
	// Quiet hours: 0-5 (minimal activity)
	for i := range 6 {
		hourCounts[i] = 1
	}
	// Active hours: 9-17 (high activity)
	for i := 9; i < 18; i++ {
		hourCounts[i] = 10
	}
	// Evening: 18-23 (moderate activity)
	for i := 18; i < 24; i++ {
		hourCounts[i] = 5
	}

	quietHours := FindQuietHours(hourCounts)
	if len(quietHours) != 6 {
		t.Errorf("Expected 6 quiet hours, got %d", len(quietHours))
	}

	// Check that we found some reasonable quiet hours
	// The algorithm finds the 6 consecutive hours with least activity
	hasQuietHour := false
	for _, hour := range quietHours {
		if hour >= 0 && hour < 6 {
			hasQuietHour = true
			break
		}
	}
	if !hasQuietHour {
		t.Errorf("Expected at least one hour in 0-5 range, got %v", quietHours)
	}
}
