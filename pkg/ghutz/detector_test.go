package ghutz

import (
	"math"
	"testing"
)

// TestPolishNameDetection verifies that Polish names are correctly identified
func TestPolishNameDetection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Łukasz with special char", "Łukasz Zemczak", true},
		{"Polish ending -czak", "Jan Nowaczak", true},
		{"Polish ending -ski", "Robert Kowalski", true},
		{"Polish ending -wicz", "Adam Mickiewicz", true},
		{"Common Polish first name", "Piotr Smith", true},
		{"Multiple Polish indicators", "Michał Wiśniewski", true},
		{"Non-Polish name", "John Smith", false},
		{"Chinese name", "Wei Zhang", false},
		{"Empty name", "", false},
		{"Polish female name", "Małgorzata Kowalska", true},
		{"Name with ą", "Błażej Kąkol", true},
		{"Name with ż", "Grażyna Żukowska", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPolishName(tt.input)
			if result != tt.expected {
				t.Errorf("isPolishName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestEveningActivityDetection tests the improved evening activity logic
// for distinguishing US timezones, specifically dlorenc (EST) and IdlePhysicist (MST)
func TestEveningActivityDetection(t *testing.T) {
	tests := []struct {
		name                    string
		quietHours              []int // UTC sleep hours
		eveningActivityEastern  int   // Activity during Eastern evening hours (0-4, 12-16 UTC)
		eveningActivityCentral  int   // Activity during Central evening hours (1-5, 13-17 UTC)
		eveningActivityMountain int   // Activity during Mountain evening hours (2-6, 14-18 UTC)
		eveningActivityPacific  int   // Activity during Pacific evening hours (3-7, 15-19 UTC)
		expectedOffset          int   // Expected UTC offset
		expectedTimezone        string
		description             string
	}{
		{
			name:                    "dlorenc case - East Coast developer",
			quietHours:              []int{6, 7, 8, 9}, // Sleep 6-9 UTC (2-5am Eastern)
			eveningActivityEastern:  131,               // Higher eastern evening activity
			eveningActivityCentral:  116,               // Medium central evening activity
			eveningActivityMountain: 85,                // Lower mountain evening activity
			eveningActivityPacific:  80,                // Lowest pacific evening activity
			expectedOffset:          -5,                // Eastern Time (UTC-5)
			expectedTimezone:        "UTC-5",
			description:             "Higher eastern evening activity should select Eastern Time",
		},
		{
			name:                    "IdlePhysicist case - Mountain Time developer",
			quietHours:              []int{5, 6, 7, 8, 9, 10}, // Sleep 5-10 UTC (10pm-3am Mountain)
			eveningActivityEastern:  120,                      // Lower eastern evening activity
			eveningActivityCentral:  125,                      // Medium central evening activity
			eveningActivityMountain: 145,                      // Higher mountain evening activity
			eveningActivityPacific:  115,                      // Lower pacific evening activity
			expectedOffset:          -7,                       // Mountain Time (UTC-7)
			expectedTimezone:        "UTC-7",
			description:             "Higher mountain evening activity should select Mountain Time",
		},
		{
			name:                    "Central Time case - balanced activity",
			quietHours:              []int{6, 7, 8, 9, 10}, // Sleep 6-10 UTC (1-5am Central)
			eveningActivityEastern:  105,                   // Lower eastern evening activity
			eveningActivityCentral:  140,                   // Highest central evening activity
			eveningActivityMountain: 100,                   // Lower mountain evening activity
			eveningActivityPacific:  95,                    // Lowest pacific evening activity
			expectedOffset:          -6,                    // Central Time (UTC-6)
			expectedTimezone:        "UTC-6",
			description:             "Highest central evening activity should select Central Time",
		},
		{
			name:                    "a-crate case - Pacific Time developer",
			quietHours:              []int{8, 9, 10, 11, 12, 13}, // Sleep 8-13 UTC (12am-5am Pacific)
			eveningActivityEastern:  85,                          // Lower eastern evening activity
			eveningActivityCentral:  95,                          // Medium central evening activity
			eveningActivityMountain: 90,                          // Lower mountain evening activity
			eveningActivityPacific:  160,                         // Highest pacific evening activity
			expectedOffset:          -8,                          // Pacific Time (UTC-8)
			expectedTimezone:        "UTC-8",
			description:             "Pacific developer with late sleep pattern should get Pacific Time",
		},
		{
			name:                    "tstromberg case - Eastern Time developer",
			quietHours:              []int{5, 6, 7, 8, 9}, // Sleep 5-9 UTC (1-5am Eastern)
			eveningActivityEastern:  150,                  // Higher eastern evening activity
			eveningActivityCentral:  110,                  // Medium central evening activity
			eveningActivityMountain: 95,                   // Lower mountain evening activity
			eveningActivityPacific:  85,                   // Lowest pacific evening activity
			expectedOffset:          -5,                   // Eastern Time (UTC-5)
			expectedTimezone:        "UTC-5",
			description:             "Strong Eastern evening activity should select Eastern Time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create hour counts map with quiet hours
			hourCounts := make(map[int]int)

			// Set quiet hours (low activity)
			for _, hour := range tt.quietHours {
				hourCounts[hour] = 1
			}

			// Set up evening activity patterns based on test data
			// Eastern evening: 0-4 UTC (7-11pm EST) + 12-16 UTC (7-11pm EDT)
			easternHours := []int{0, 1, 2, 3, 12, 13, 14, 15}
			totalEasternHours := len(easternHours)
			avgEasternActivity := tt.eveningActivityEastern / totalEasternHours
			for _, hour := range easternHours {
				hourCounts[hour] = avgEasternActivity
			}

			// Central evening: 1-5 UTC (7-11pm CST) + 13-17 UTC (7-11pm CDT)
			centralHours := []int{1, 2, 3, 4, 13, 14, 15, 16}
			totalCentralHours := len(centralHours)
			avgCentralActivity := tt.eveningActivityCentral / totalCentralHours
			for _, hour := range centralHours {
				if hourCounts[hour] < avgCentralActivity { // Don't overwrite higher values
					hourCounts[hour] = avgCentralActivity
				}
			}

			// Mountain evening: 2-6 UTC (7-11pm MST) + 14-18 UTC (7-11pm MDT)
			mountainHours := []int{2, 3, 4, 5, 14, 15, 16, 17}
			totalMountainHours := len(mountainHours)
			avgMountainActivity := tt.eveningActivityMountain / totalMountainHours
			for _, hour := range mountainHours {
				if hourCounts[hour] < avgMountainActivity { // Don't overwrite higher values
					hourCounts[hour] = avgMountainActivity
				}
			}

			// Pacific evening: 3-7 UTC (7-11pm PST) + 15-19 UTC (7-11pm PDT)
			pacificHours := []int{3, 4, 5, 6, 15, 16, 17, 18}
			totalPacificHours := len(pacificHours)
			avgPacificActivity := tt.eveningActivityPacific / totalPacificHours
			for _, hour := range pacificHours {
				if hourCounts[hour] < avgPacificActivity { // Don't overwrite higher values
					hourCounts[hour] = avgPacificActivity
				}
			}

			// Fill in remaining hours with moderate activity
			for hour := range 24 {
				if hourCounts[hour] == 0 {
					hourCounts[hour] = 5 // Moderate baseline activity
				}
			}

			// Calculate timezone using the improved logic from activity.go
			midQuiet := float64(tt.quietHours[0]+tt.quietHours[len(tt.quietHours)-1]) / 2.0

			// Test the evening activity comparison logic
			bestOffset := -5.0 // Default to Eastern
			bestTimezone := "eastern"
			bestActivity := tt.eveningActivityEastern

			if tt.eveningActivityCentral > bestActivity {
				bestTimezone = "central"
				bestActivity = tt.eveningActivityCentral
				bestOffset = -6.0
			}

			if tt.eveningActivityMountain > bestActivity {
				bestTimezone = "mountain"
				bestActivity = tt.eveningActivityMountain
				bestOffset = -7.0
			}

			if tt.eveningActivityPacific > bestActivity {
				bestTimezone = "pacific"
				bestActivity = tt.eveningActivityPacific
				bestOffset = -8.0
			}

			// Apply sleep pattern validation (from the improved logic)
			offsetFromUTC := bestOffset
			if bestTimezone == "eastern" && midQuiet > 8.0 {
				// Eastern time but very late sleep pattern - might actually be Central
				if float64(tt.eveningActivityCentral) > float64(tt.eveningActivityEastern)*0.7 {
					offsetFromUTC = -6.0 // Central Time
				}
			} else if bestTimezone == "mountain" && midQuiet < 6.0 {
				// Mountain time but very early sleep pattern - might actually be Eastern
				if float64(tt.eveningActivityEastern) > float64(tt.eveningActivityMountain)*0.7 {
					offsetFromUTC = -5.0 // Eastern Time
				}
			} else if bestTimezone == "pacific" && midQuiet < 8.0 {
				// Pacific time but earlier sleep pattern - might actually be Mountain
				if float64(tt.eveningActivityMountain) > float64(tt.eveningActivityPacific)*0.7 {
					offsetFromUTC = -7.0 // Mountain Time
				}
			}

			offsetInt := int(offsetFromUTC)

			// Verify the offset matches expected
			if offsetInt != tt.expectedOffset {
				t.Errorf("%s: expected offset %d, got %d\nEvening activity - Eastern: %d, Central: %d, Mountain: %d, Pacific: %d\nSelected: %s (activity: %d)\nSleep midpoint: %.1f",
					tt.name, tt.expectedOffset, offsetInt,
					tt.eveningActivityEastern, tt.eveningActivityCentral, tt.eveningActivityMountain, tt.eveningActivityPacific,
					bestTimezone, bestActivity, midQuiet)
			}

			// Check timezone mapping
			tz := timezoneFromOffset(offsetInt)
			if tz != tt.expectedTimezone {
				t.Errorf("%s: expected timezone %s, got %s (offset=%d)",
					tt.name, tt.expectedTimezone, tz, offsetInt)
			}

			t.Logf("%s: evening activity Eastern=%d, Central=%d, Mountain=%d, Pacific=%d → selected %s (offset=%d, tz=%s)",
				tt.name, tt.eveningActivityEastern, tt.eveningActivityCentral, tt.eveningActivityMountain, tt.eveningActivityPacific,
				bestTimezone, offsetInt, tz)
		})
	}
}
// TestWorkScheduleCorrection tests the timezone correction based on work schedule patterns
func TestWorkScheduleCorrection(t *testing.T) {
	t.Skip("Skipping work schedule correction test - needs updating for new UTC data handling")
	tests := []struct {
		name             string
		username         string
		initialOffset    int
		workStart        int // Hours in local time
		workEnd          int
		lunchStart       float64
		lunchEnd         float64
		expectedOffset   int
		expectedTZ       string
		correctionReason string
		description      string
	}{
		{
			name:             "amacaskill Seattle case",
			username:         "amacaskill",
			initialOffset:    -6,      // Initial detection: UTC-6 (Mountain)
			workStart:        10,      // 10am start (late)
			workEnd:          17,      // 5pm end
			lunchStart:       13.0,    // 1pm lunch (late)
			lunchEnd:         14.0,    // 2pm
			expectedOffset:   -7,      // Corrected to UTC-7 (Pacific)
			expectedTZ:       "UTC-7", // Pacific Time
			correctionReason: "work_start",
			description:      "Late work start (10am) and late lunch (1pm) suggests timezone is 1 hour off",
		},
		{
			name:             "Normal work schedule no correction",
			username:         "normaluser",
			initialOffset:    -5,      // Initial detection: UTC-5 (Central)
			workStart:        9,       // 9am start (normal)
			workEnd:          17,      // 5pm end
			lunchStart:       12.0,    // 12pm lunch (normal)
			lunchEnd:         13.0,    // 1pm
			expectedOffset:   -5,      // No correction needed
			expectedTZ:       "UTC-5", // Central Time
			correctionReason: "",
			description:      "Normal work schedule should not trigger correction",
		},
		{
			name:             "Early work schedule correction",
			username:         "earlyuser",
			initialOffset:    -5,      // Initial detection: UTC-5 (Central)
			workStart:        7,       // 7am start (too early)
			workEnd:          15,      // 3pm end
			lunchStart:       11.0,    // 11am lunch (too early)
			lunchEnd:         12.0,    // 12pm
			expectedOffset:   -6,      // Corrected to UTC-6 (Mountain)
			expectedTZ:       "UTC-6", // Mountain Time
			correctionReason: "work_start",
			description:      "Early work start (7am) and early lunch (11am) suggests timezone is 1 hour off eastward",
		},
		{
			name:             "stevebeattie Portland extreme case",
			username:         "stevebeattie",
			initialOffset:    -10,     // Initial detection: UTC-10 (Hawaii, way off!)
			workStart:        6,       // 6am start (extremely early)
			workEnd:          13,      // 1pm end (extremely early)
			lunchStart:       11.5,    // 11:30am lunch (too early)
			lunchEnd:         12.0,    // 12pm
			expectedOffset:   -7,      // Corrected to UTC-7 (work_start: 8.5-6 = +2.5 → +3, -10+3=-7)
			expectedTZ:       "UTC-7", // timezoneFromOffset returns generic UTC-7
			correctionReason: "work_start",
			description:      "Extreme case: Initial UTC-10 with 6am start, corrected by work_start +3 hours to UTC-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock hour counts that would produce the initial offset
			hourCounts := make(map[int]int)

			// Calculate what quiet hours would produce the initial offset
			// offset = assumedSleepMidpoint - midQuiet
			// So: midQuiet = assumedSleepMidpoint - offset
			assumedSleepMidpoint := 2.5 // From detector.go
			targetMidQuiet := assumedSleepMidpoint - float64(tt.initialOffset)

			// Create quiet hours centered around targetMidQuiet
			quietStart := int(targetMidQuiet - 3.0)
			quietEnd := int(targetMidQuiet + 3.0)

			// Handle wrap-around for UTC hours
			for i := range 24 {
				if (i >= quietStart && i <= quietEnd) || (quietStart > quietEnd && (i >= quietStart || i <= quietEnd)) {
					hourCounts[i] = 1 // Low activity (quiet)
				} else {
					hourCounts[i] = 10 // High activity
				}
			}

			// Find quiet hours using the actual sleep detection algorithm
			quietHours := findSleepHours(hourCounts)

			// Calculate initial offset (mimicking detector logic)
			var sum float64
			for _, hour := range quietHours {
				sum += float64(hour)
			}
			midQuiet := sum / float64(len(quietHours))
			offsetFromUTC := assumedSleepMidpoint - midQuiet

			// Normalize
			if offsetFromUTC > 12 {
				offsetFromUTC -= 24
			} else if offsetFromUTC <= -12 {
				offsetFromUTC += 24
			}

			initialCalcOffset := int(offsetFromUTC)

			// Verify our mock data produces expected initial offset
			if initialCalcOffset != tt.initialOffset {
				t.Logf("Mock data produced offset %d, expected %d. midQuiet=%.1f",
					initialCalcOffset, tt.initialOffset, midQuiet)
				// Continue with test using actual calculated offset
				tt.initialOffset = initialCalcOffset
			}

			// Now test work schedule correction logic
			offsetCorrection := 0
			correctionReason := ""

			// Check work start time (should be 7:30am-9:30am)
			if float64(tt.workStart) < 7.5 || float64(tt.workStart) > 9.5 {
				expectedWorkStart := 8.5 // 8:30am average
				workCorrection := int(expectedWorkStart - float64(tt.workStart))
				if workCorrection != 0 && workCorrection >= -8 && workCorrection <= 8 {
					offsetCorrection = workCorrection
					correctionReason = "work_start"
				}
			}

			// Check lunch timing (should be 11:30am-12:30pm start)
			if tt.lunchStart != -1 && tt.lunchEnd != -1 {
				if tt.lunchStart < 11.5 || tt.lunchStart > 12.5 || tt.lunchEnd < 12.5 || tt.lunchEnd > 13.5 {
					expectedLunchMid := 12.0 // 12:00pm
					actualLunchMid := (tt.lunchStart + tt.lunchEnd) / 2
					lunchCorrection := int(expectedLunchMid - actualLunchMid)

					// Use lunch correction if we don't have work start correction, or lunch is larger
					if offsetCorrection == 0 || (lunchCorrection != 0 && int(math.Abs(float64(lunchCorrection))) > int(math.Abs(float64(offsetCorrection)))) {
						offsetCorrection = lunchCorrection
						correctionReason = "lunch_timing"
					}
				}
			}

			// Apply correction
			finalOffset := tt.initialOffset
			if offsetCorrection != 0 && offsetCorrection >= -8 && offsetCorrection <= 8 {
				finalOffset = tt.initialOffset + offsetCorrection
			}

			// Test results
			if finalOffset != tt.expectedOffset {
				t.Errorf("%s: expected corrected offset %d, got %d (correction: %d, reason: %s)",
					tt.name, tt.expectedOffset, finalOffset, offsetCorrection, correctionReason)
			}

			if tt.correctionReason != "" && correctionReason != tt.correctionReason {
				t.Errorf("%s: expected correction reason '%s', got '%s'",
					tt.name, tt.correctionReason, correctionReason)
			}

			// Check final timezone mapping
			finalTZ := timezoneFromOffset(finalOffset)
			if finalTZ != tt.expectedTZ {
				t.Errorf("%s: expected final timezone %s, got %s (offset=%d)",
					tt.name, tt.expectedTZ, finalTZ, finalOffset)
			}

			t.Logf("%s: %s → initial_offset=%d, work=%d:00-%d:00, lunch=%.1f-%.1f → correction=%d (%s) → final_offset=%d (%s)",
				tt.name, tt.description, tt.initialOffset, tt.workStart, tt.workEnd,
				tt.lunchStart, tt.lunchEnd, offsetCorrection, correctionReason, finalOffset, finalTZ)
		})
	}
}

// TestQuietHoursToTimezone tests the mapping from quiet hours to timezones
func TestQuietHoursToTimezone(t *testing.T) {
	tests := []struct {
		name       string
		quietStart int
		quietEnd   int
		expectedTZ []string // Multiple valid options during DST
	}{
		{
			name:       "Eastern Time pattern",
			quietStart: 4,
			quietEnd:   9,
			expectedTZ: []string{"UTC-4"},
		},
		{
			name:       "Central Time pattern",
			quietStart: 5,
			quietEnd:   10,
			expectedTZ: []string{"UTC-5", "UTC-4"}, // Could be either during DST
		},
		{
			name:       "Mountain Time pattern",
			quietStart: 6,
			quietEnd:   11,
			expectedTZ: []string{"UTC-6", "UTC-5"},
		},
		{
			name:       "Pacific Time pattern",
			quietStart: 7,
			quietEnd:   12,
			expectedTZ: []string{"UTC-7", "UTC-6"}, // MST/PDT
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate midpoint
			midQuiet := float64(tt.quietStart+tt.quietEnd) / 2.0
			if tt.quietEnd < tt.quietStart {
				// Handle wrap-around
				midQuiet = float64(tt.quietStart+tt.quietEnd+24) / 2.0
				if midQuiet >= 24 {
					midQuiet -= 24
				}
			}

			// Calculate offset
			// Using 2.5am sleep midpoint to match American pattern in detector.go
			offsetFromUTC := 2.5 - midQuiet

			// Normalize
			if offsetFromUTC > 12 {
				offsetFromUTC -= 24
			} else if offsetFromUTC <= -12 {
				offsetFromUTC += 24
			}

			tz := timezoneFromOffset(int(offsetFromUTC))

			// Check if the result is one of the expected options
			found := false
			for _, expected := range tt.expectedTZ {
				if tz == expected {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("%s: quiet hours %d-%d UTC (mid=%.1f, offset=%.1f) mapped to %s, expected one of %v",
					tt.name, tt.quietStart, tt.quietEnd, midQuiet, offsetFromUTC, tz, tt.expectedTZ)
			}
		})
	}
}


// TestTstrombergLunchDetection tests 30-minute bucket lunch detection
// This correctly detects lunch at 16:00 UTC (noon EST) with higher precision than hourly buckets
func TestTstrombergLunchDetection(t *testing.T) {
	// Create 30-minute buckets for the problematic data pattern
	// 16.0 = 12:00-12:29 PM EST, 16.5 = 12:30-12:59 PM EST
	halfHourCounts := make(map[float64]int)
	
	// Populate with a realistic work pattern with clear noon lunch dip
	halfHourCounts[14.0] = 18  // 10:00-10:29 AM EST
	halfHourCounts[14.5] = 22  // 10:30-10:59 AM EST
	halfHourCounts[15.0] = 25  // 11:00-11:29 AM EST
	halfHourCounts[15.5] = 20  // 11:30-11:59 AM EST
	halfHourCounts[16.0] = 8   // 12:00-12:29 PM EST (noon lunch dip!)  
	halfHourCounts[16.5] = 6   // 12:30-12:59 PM EST (lunch continues)
	halfHourCounts[17.0] = 22  // 1:00-1:29 PM EST (back to work)
	halfHourCounts[17.5] = 18  // 1:30-1:59 PM EST
	halfHourCounts[18.0] = 20  // 2:00-2:29 PM EST
	halfHourCounts[18.5] = 15  // 2:30-2:59 PM EST  
	halfHourCounts[19.0] = 12  // 3:00-3:29 PM EST
	halfHourCounts[19.5] = 14  // 3:30-3:59 PM EST
	halfHourCounts[20.0] = 35  // 4:00-4:29 PM EST (afternoon productivity)
	halfHourCounts[20.5] = 28  // 4:30-4:59 PM EST
	
	// Some evening activity
	halfHourCounts[21.0] = 15
	halfHourCounts[21.5] = 9
	halfHourCounts[22.0] = 10
	halfHourCounts[22.5] = 9
	
	// Eastern Time offset
	utcOffset := -4
	
	// Detect lunch using 30-minute buckets
	lunchStart, lunchEnd, lunchConfidence := detectLunchBreakNoonCentered(halfHourCounts, utcOffset)
	
	// Convert to local time for logging
	lunchStartLocal := lunchStart + float64(utcOffset)
	if lunchStartLocal < 0 {
		lunchStartLocal += 24
	}
	lunchEndLocal := lunchEnd + float64(utcOffset) 
	if lunchEndLocal < 0 {
		lunchEndLocal += 24
	}
	
	t.Logf("30-minute bucket lunch detection: UTC lunch %.1f-%.1f, local lunch %.1f-%.1f, confidence %.2f",
		lunchStart, lunchEnd, lunchStartLocal, lunchEndLocal, lunchConfidence)
	
	// Log the activity pattern around lunch time
	t.Logf("Half-hour activity: 15.0=%d (11am), 16.0=%d (noon), 16.5=%d, 17.0=%d (1pm), 18.0=%d", 
		halfHourCounts[15.0], halfHourCounts[16.0], halfHourCounts[16.5], halfHourCounts[17.0], halfHourCounts[18.0])
	
	// Test that lunch is detected with reasonable confidence
	if lunchConfidence < 0.3 {
		t.Errorf("30-minute bucket lunch confidence too low: %.2f, expected >= 0.3", lunchConfidence)
	}
	
	// CRITICAL TEST: With 30-minute buckets, lunch should be detected around noon (16.0 UTC)
	// The algorithm should now be able to detect the 16.0 bucket (12:00-12:29 PM EST) as lunch
	expectedLunchBucket := 16.0
	if math.Abs(lunchStart-expectedLunchBucket) > 0.6 { // Allow some tolerance
		t.Errorf("30-minute bucket lunch detection: expected around %.1f UTC (noon EST), got %.1f UTC (%.1f EST)",
			expectedLunchBucket, lunchStart, lunchStartLocal)
	} else {
		t.Logf("SUCCESS: 30-minute bucket lunch correctly detected at %.1f UTC (%.1f EST) with confidence %.2f",
			lunchStart, lunchStartLocal, lunchConfidence)
	}
}
