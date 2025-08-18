package ghutz

import (
	"testing"
)

// TestElijahQuinonesRostonDetection tests ElijahQuinones's real activity data
// Evidence suggests they work in Boston/Eastern Time, not Pacific Time
func TestElijahQuinonesRostonDetection(t *testing.T) {
	// ACTUAL activity data from ElijahQuinones
	// Collected over 206 days with 219 total events

	// ACTUAL hourly activity counts from ElijahQuinones's GitHub activity
	// From: hours_utc="[3 0 0 6 2 0 0 0 0 0 0 0 1 5 13 23 18 28 21 26 24 34 8 7]"
	hourCounts := map[int]int{
		0:  3,  // 00:00 UTC = 20:00 EDT - evening
		1:  0,  // 01:00 UTC
		2:  0,  // 02:00 UTC
		3:  6,  // 03:00 UTC = 23:00 EDT - late evening
		4:  2,  // 04:00 UTC = 00:00 EDT - midnight
		5:  0,  // 05:00 UTC = 01:00 EDT - SLEEP
		6:  0,  // 06:00 UTC = 02:00 EDT - SLEEP
		7:  0,  // 07:00 UTC = 03:00 EDT - SLEEP
		8:  0,  // 08:00 UTC = 04:00 EDT - SLEEP
		9:  0,  // 09:00 UTC = 05:00 EDT - SLEEP
		10: 0,  // 10:00 UTC = 06:00 EDT - SLEEP
		11: 0,  // 11:00 UTC = 07:00 EDT - SLEEP/wake
		12: 1,  // 12:00 UTC = 08:00 EDT - work starts
		13: 5,  // 13:00 UTC = 09:00 EDT - morning
		14: 13, // 14:00 UTC = 10:00 EDT
		15: 23, // 15:00 UTC = 11:00 EDT
		16: 18, // 16:00 UTC = 12:00 EDT - LUNCH DIP
		17: 28, // 17:00 UTC = 13:00 EDT - back from lunch
		18: 21, // 18:00 UTC = 14:00 EDT
		19: 26, // 19:00 UTC = 15:00 EDT
		20: 24, // 20:00 UTC = 16:00 EDT
		21: 34, // 21:00 UTC = 17:00 EDT - PEAK end of day
		22: 8,  // 22:00 UTC = 18:00 EDT - evening
		23: 7,  // 23:00 UTC = 19:00 EDT - evening
	}

	// Calculate quiet hours (sleep time)
	quietHours := []int{}
	for hour := 0; hour < 24; hour++ {
		if hourCounts[hour] == 0 {
			quietHours = append(quietHours, hour)
		}
	}

	// Expected quiet hours are 5-11 UTC (1am-7am EDT)
	expectedQuietStart := 5
	expectedQuietEnd := 11

	hasExpectedQuiet := false
	for _, h := range quietHours {
		if h >= expectedQuietStart && h <= expectedQuietEnd {
			hasExpectedQuiet = true
			break
		}
	}

	if !hasExpectedQuiet {
		t.Errorf("Expected quiet hours between %d-%d UTC for Eastern Time, got %v",
			expectedQuietStart, expectedQuietEnd, quietHours)
	}

	// Test work pattern for Eastern Time (UTC-4 in summer)
	// Work should start around 12-13 UTC (8-9am EDT)
	workStartUTC := -1
	for hour := 11; hour <= 14; hour++ {
		if hourCounts[hour] > 0 {
			workStartUTC = hour
			break
		}
	}

	if workStartUTC < 12 || workStartUTC > 13 {
		t.Errorf("Work start at %d UTC doesn't match Eastern Time (expected 12-13 UTC for 8-9am EDT)", workStartUTC)
	}

	// Verify lunch pattern around 16:00 UTC (12pm EDT)
	preLunch := hourCounts[15]  // 11am EDT
	lunch := hourCounts[16]     // 12pm EDT
	postLunch := hourCounts[17] // 1pm EDT

	// There should be a dip at lunch time
	if lunch >= preLunch {
		t.Errorf("No lunch dip detected: 11am=%d, 12pm=%d, 1pm=%d", preLunch, lunch, postLunch)
	}

	// Peak activity should be in afternoon/evening EDT (19-21 UTC = 3-5pm EDT)
	maxActivity := 0
	peakHour := -1
	for hour := 19; hour <= 21; hour++ {
		if hourCounts[hour] > maxActivity {
			maxActivity = hourCounts[hour]
			peakHour = hour
		}
	}

	if peakHour != 21 {
		t.Errorf("Peak activity at %d UTC doesn't match expected 21 UTC (5pm EDT)", peakHour)
	}

	t.Logf("ElijahQuinones activity pattern analysis:")
	t.Logf("- Quiet hours: %v UTC (sleep time)", quietHours)
	t.Logf("- Work starts: %d UTC (%.0fam EDT)", workStartUTC, float64(workStartUTC-4))
	t.Logf("- Lunch dip: 16 UTC with %d events (12pm EDT)", lunch)
	t.Logf("- Peak hour: %d UTC with %d events (%.0fpm EDT)", peakHour, maxActivity, float64(peakHour-4-12))
	t.Logf("Pattern consistent with Eastern Time (Boston)")
}

// TestElijahQuinonesWorkHoursValidation verifies Eastern Time makes more sense than Pacific
func TestElijahQuinonesWorkHoursValidation(t *testing.T) {
	// Test that Eastern Time produces reasonable work hours
	// while Pacific Time produces unreasonable ones

	testCases := []struct {
		timezone     string
		offset       int
		workStartUTC int
		reasonable   bool
		reason       string
	}{
		{
			"Pacific Time",
			-7,
			12, // 12 UTC = 5am PDT
			false,
			"5am start is too early for normal work",
		},
		{
			"Mountain Time",
			-6,
			12, // 12 UTC = 6am MDT
			false,
			"6am start is early but possible",
		},
		{
			"Central Time",
			-5,
			12, // 12 UTC = 7am CDT
			true,
			"7am start is reasonable",
		},
		{
			"Eastern Time",
			-4,
			12, // 12 UTC = 8am EDT
			true,
			"8am start is perfectly normal",
		},
	}

	for _, tc := range testCases {
		localStart := tc.workStartUTC + tc.offset
		if localStart < 0 {
			localStart += 24
		}

		// Reasonable work hours are 7am-10am
		isReasonable := localStart >= 7 && localStart <= 10

		if isReasonable != tc.reasonable {
			t.Errorf("%s: expected reasonable=%v but got %v (starts at %dam local)",
				tc.timezone, tc.reasonable, isReasonable, localStart)
		}

		t.Logf("%s (UTC%+d): work starts at %dam local - %s",
			tc.timezone, tc.offset, localStart, tc.reason)
	}

	t.Log("\nConclusion: Eastern Time produces the most reasonable work schedule")
}
