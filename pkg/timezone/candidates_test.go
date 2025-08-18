package timezone

import (
	"testing"
)

// TestDetectPeakProductivityWithHalfHours tests the peak detection function
func TestDetectPeakProductivityWithHalfHours(t *testing.T) {
	t.Run("Empty map returns no peak", func(t *testing.T) {
		start, end, count := DetectPeakProductivityWithHalfHours(map[float64]int{}, 0)
		if start != -1 || end != -1 || count != 0 {
			t.Errorf("Expected no peak for empty map, got start=%v, end=%v, count=%v", start, end, count)
		}
	})

	t.Run("Single bucket returns that bucket", func(t *testing.T) {
		halfHourCounts := map[float64]int{
			12.0: 10,
		}
		start, end, count := DetectPeakProductivityWithHalfHours(halfHourCounts, 0)
		if start != 12.0 || end != 12.5 || count != 10 {
			t.Errorf("Expected start=12.0, end=12.5, count=10, got start=%v, end=%v, count=%v", start, end, count)
		}
	})

	t.Run("Multiple buckets returns highest", func(t *testing.T) {
		halfHourCounts := map[float64]int{
			12.0: 10,
			13.0: 25,
			14.0: 15,
		}
		start, end, count := DetectPeakProductivityWithHalfHours(halfHourCounts, 0)
		if start != 13.0 || end != 13.5 || count != 25 {
			t.Errorf("Expected start=13.0, end=13.5, count=25, got start=%v, end=%v, count=%v", start, end, count)
		}
	})
}

// TestEvaluateTimezoneCandidates tests the timezone candidate evaluation function
func TestEvaluateTimezoneCandidates(t *testing.T) {
	// Test with realistic activity pattern - someone working 9am-5pm EST
	hourCounts := map[int]int{
		13: 20, // 9am EST
		14: 30, // 10am EST
		15: 35, // 11am EST
		16: 25, // 12pm EST - lunch dip
		17: 20, // 1pm EST
		18: 40, // 2pm EST - peak
		19: 35, // 3pm EST
		20: 30, // 4pm EST
		21: 25, // 5pm EST
	}

	halfHourCounts := map[float64]int{
		13.0: 10, 13.5: 10,
		14.0: 15, 14.5: 15,
		15.0: 18, 15.5: 17,
		16.0: 10, 16.5: 15, // lunch dip at 12pm EST
		17.0: 10, 17.5: 10,
		18.0: 20, 18.5: 20, // peak at 2pm EST
		19.0: 18, 19.5: 17,
		20.0: 15, 20.5: 15,
		21.0: 13, 21.5: 12,
	}

	totalActivity := 300
	quietHours := []int{4, 5, 6, 7, 8, 9} // midnight-6am EST
	midQuiet := 6.5
	activeStart := 13 // 9am EST

	// Lunch pattern at noon EST (16 UTC)
	bestGlobalLunch := GlobalLunchPattern{
		StartUTC:    16.0,
		EndUTC:      17.0,
		Confidence:  0.8,
		DropPercent: 30.0,
	}

	candidates := EvaluateTimezoneCandidates("testuser", hourCounts, halfHourCounts, totalActivity, quietHours, midQuiet, activeStart, bestGlobalLunch)

	if len(candidates) == 0 {
		t.Fatal("Expected at least one timezone candidate")
	}

	// Check that UTC-5 (EST) is among the top candidates
	foundEST := false
	for _, candidate := range candidates {
		if candidate.Offset == -5 {
			foundEST = true
			if candidate.Confidence < 0.5 {
				t.Errorf("Expected EST candidate to have confidence >= 0.5, got %v", candidate.Confidence)
			}
			if !candidate.LunchReasonable {
				t.Error("Expected EST candidate to have reasonable lunch timing")
			}
			break
		}
	}

	if !foundEST {
		t.Error("Expected UTC-5 (EST) to be among timezone candidates")
	}

	// Verify candidates are sorted by confidence (highest first)
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1].Confidence < candidates[i].Confidence {
			t.Errorf("Candidates not sorted by confidence: candidate %d has confidence %v < candidate %d confidence %v",
				i-1, candidates[i-1].Confidence, i, candidates[i].Confidence)
		}
	}
}