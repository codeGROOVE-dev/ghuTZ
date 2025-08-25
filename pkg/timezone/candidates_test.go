package timezone

import (
	"testing"
	"time"
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

// TestEvaluateCandidates tests the timezone candidate evaluation function
func TestEvaluateCandidates(t *testing.T) {
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
	activeStart := 13.0 // 9am EST

	// Lunch pattern at noon EST (16 UTC)
	bestGlobalLunch := GlobalLunchPattern{
		StartUTC:    16.0,
		EndUTC:      17.0,
		Confidence:  0.8,
		DropPercent: 30.0,
	}

	candidates := EvaluateCandidates("testuser", hourCounts, halfHourCounts, totalActivity, quietHours, midQuiet, activeStart, bestGlobalLunch, "", time.Now())

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

// TestUKTimezoneDetection tests that UK users are correctly detected as UTC+0/UTC+1
func TestUKTimezoneDetection(t *testing.T) {
	// max-allan-cgr's actual activity pattern from the logs
	// Active hours UTC: 08:00-17:00
	// Sleep hours: [2 3 4 5 6 7 8 18 19 20 22 23]
	hourCounts := map[int]int{
		0:  0,
		1:  2,
		2:  1, // sleep
		3:  0, // sleep
		4:  0, // sleep
		5:  0, // sleep
		6:  0, // sleep
		7:  0, // sleep
		8:  2, // work starts
		9:  10,
		10: 21, // peak activity
		11: 20,
		12: 7, // lunch dip
		13: 10,
		14: 14,
		15: 14,
		16: 11,
		17: 6, // work ends
		18: 0, // quiet
		19: 2, // quiet
		20: 0, // quiet
		21: 6, // some evening activity
		22: 1, // quiet
		23: 0, // sleep
	}

	// Half-hour resolution for lunch detection
	halfHourCounts := map[float64]int{
		0.0: 0, 0.5: 0,
		1.0: 1, 1.5: 1,
		2.0: 0, 2.5: 1,
		3.0: 0, 3.5: 0,
		4.0: 0, 4.5: 0,
		5.0: 0, 5.5: 0,
		6.0: 0, 6.5: 0,
		7.0: 0, 7.5: 0,
		8.0: 1, 8.5: 1,
		9.0: 5, 9.5: 5,
		10.0: 10, 10.5: 11, // Morning peak
		11.0: 10, 11.5: 10,
		12.0: 2, 12.5: 5, // Lunch dip at noon
		13.0: 5, 13.5: 5,
		14.0: 7, 14.5: 7,
		15.0: 7, 15.5: 7,
		16.0: 6, 16.5: 5,
		17.0: 3, 17.5: 3,
		18.0: 0, 18.5: 0, // Evening quiet
		19.0: 1, 19.5: 1,
		20.0: 0, 20.5: 0,
		21.0: 3, 21.5: 3,
		22.0: 0, 22.5: 1,
		23.0: 0, 23.5: 0,
	}

	totalActivity := 127
	quietHours := []int{2, 3, 4, 5, 6, 7, 8, 18, 19, 20, 22, 23}
	midQuiet := 8.0 // As calculated in the actual implementation
	activeStart := 8.0

	// Best global lunch pattern at 12:00 UTC
	bestGlobalLunch := GlobalLunchPattern{
		StartUTC:    12.0,
		EndUTC:      12.5,
		Confidence:  0.8,
		DropPercent: 0.75,
	}

	candidates := EvaluateCandidates("max-allan-cgr", hourCounts, halfHourCounts,
		totalActivity, quietHours, midQuiet, activeStart, bestGlobalLunch, "", time.Now())

	// Test 1: UTC+0 should be the top candidate
	if len(candidates) == 0 {
		t.Fatal("No candidates returned")
	}

	topCandidate := candidates[0]
	if topCandidate.Offset != 0 {
		t.Errorf("Expected UTC+0 as top candidate, got UTC%+.0f", topCandidate.Offset)
	}

	// Test 2: UTC+0 should have confidence > 65% (after 1.5x scaling, reduced from 70% after balancing geographic scoring)
	if topCandidate.Confidence < 65 {
		t.Errorf("UTC+0 confidence too low: %.1f%%, expected > 65%%", topCandidate.Confidence)
	}

	// Test 3: UTC+0 should have reasonable work hours (10am start based on activity data)
	// The data shows minimal activity at 8-9am (1-2 events) but significant activity from 10am (10+ events)
	if topCandidate.WorkStartLocal != 10 {
		t.Errorf("UTC+0 work start incorrect: %.1f, expected 10 (based on actual activity pattern)", topCandidate.WorkStartLocal)
	}

	// Test 4: UTC+0 should have lunch detected at noon
	if topCandidate.LunchLocalTime < 11.5 || topCandidate.LunchLocalTime > 12.5 {
		t.Errorf("UTC+0 lunch time incorrect: %.1f, expected ~12.0", topCandidate.LunchLocalTime)
	}

	// Test 5: UTC+0 should be marked as having reasonable patterns
	if !topCandidate.LunchReasonable {
		t.Error("UTC+0 lunch should be marked as reasonable")
	}
	if !topCandidate.WorkHoursReasonable {
		t.Error("UTC+0 work hours should be marked as reasonable")
	}
	if !topCandidate.SleepReasonable {
		t.Error("UTC+0 sleep should be marked as reasonable (midnight-8am)")
	}

	// Test 6: UTC+1 should also be a strong candidate (UK BST)
	var utcPlus1 *Candidate
	for i := range candidates {
		if candidates[i].Offset == 1 {
			utcPlus1 = &candidates[i]
			break
		}
	}

	if utcPlus1 == nil {
		t.Error("UTC+1 should be among candidates for UK detection")
	} else {
		// UTC+1 should also have good confidence (> 50%)
		if utcPlus1.Confidence < 50 {
			t.Errorf("UTC+1 confidence too low: %.1f%%, expected > 50%%", utcPlus1.Confidence)
		}

		// UTC+1 sleep (1am-9am) should be marked as reasonable
		if !utcPlus1.SleepReasonable {
			t.Errorf("UTC+1 sleep should be reasonable (1am-9am, mid=%.1f)", utcPlus1.SleepMidLocal)
		}
	}

	// Test 7: Verify both UTC+0 and UTC+1 get UK/Europe population boost
	foundUKBoost := false
	for _, detail := range topCandidate.ScoringDetails {
		if contains(detail, "UK") || contains(detail, "Western Europe") {
			foundUKBoost = true
			break
		}
	}
	if !foundUKBoost {
		t.Error("UTC+0 should get UK/Western Europe population boost")
	}

	// Test 8: No work start penalties for 8am/9am starts
	for _, detail := range topCandidate.ScoringDetails {
		if contains(detail, "impossible") || contains(detail, "extremely early") {
			t.Errorf("UTC+0 should not have work start penalties: %s", detail)
		}
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
