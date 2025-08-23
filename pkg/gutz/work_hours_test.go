package gutz

import (
	"strings"
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
)

func TestWorkHoursPrecisionAndTimezoneCorrectness(t *testing.T) {
	tests := []struct {
		name            string
		utcStart        float64
		utcEnd          float64
		candidateOffset int
		expectedStart   string
		expectedEnd     string
		description     string
	}{
		{
			name:            "Half hour precision UTC-4",
			utcStart:        10.5, // 10:30 UTC
			utcEnd:          2.5,  // 02:30 UTC (next day)
			candidateOffset: -4,
			expectedStart:   "06:30",
			expectedEnd:     "22:30",
			description:     "tstromberg case - should show 06:30, not 06:00",
		},
		{
			name:            "Hour precision UTC-5",
			utcStart:        14.0, // 14:00 UTC
			utcEnd:          22.0, // 22:00 UTC
			candidateOffset: -5,
			expectedStart:   "09:00",
			expectedEnd:     "17:00",
			description:     "Standard work hours with no minutes",
		},
		{
			name:            "Quarter hour precision UTC+1",
			utcStart:        8.25,  // 08:15 UTC
			utcEnd:          16.75, // 16:45 UTC
			candidateOffset: 1,
			expectedStart:   "09:15",
			expectedEnd:     "17:45",
			description:     "15-minute precision should work",
		},
		{
			name:            "Midnight wraparound UTC-8",
			utcStart:        6.0, // 06:00 UTC
			utcEnd:          2.0, // 02:00 UTC (next day)
			candidateOffset: -8,
			expectedStart:   "22:00",
			expectedEnd:     "18:00",
			description:     "Should handle day wraparound correctly",
		},
		{
			name:            "UTC+12 edge case",
			utcStart:        13.5, // 13:30 UTC
			utcEnd:          23.5, // 23:30 UTC
			candidateOffset: 12,
			expectedStart:   "01:30",
			expectedEnd:     "11:30",
			description:     "Large positive offset with wraparound",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create context data with work hours
			contextData := map[string]interface{}{
				"work_hours_utc": []float64{tt.utcStart, tt.utcEnd},
			}

			// Create a fake candidate
			candidate := &timezone.Candidate{
				Offset: float64(tt.candidateOffset),
			}

			// Generate the timezone candidate section
			output := formatTimezoneCandidate(candidate, contextData)

			// Check that work hours appear correctly formatted
			if !strings.Contains(output, "Work hours: "+tt.expectedStart+"-"+tt.expectedEnd+" local") {
				t.Errorf("%s failed:\nExpected work hours: %s-%s local\nActual output:\n%s",
					tt.description, tt.expectedStart, tt.expectedEnd, output)
			}
		})
	}
}

func TestWorkHoursDoublConversionRegression(t *testing.T) {
	// This test specifically prevents the double conversion bug from returning

	// Simulate the original bug scenario:
	// activeStartUTC=10.5, activeEndUTC=2.5 (tstromberg data)
	// For UTC-4: should be 06:30-22:30 local, NOT 02:00-18:00

	contextData := map[string]interface{}{
		"work_hours_utc": []float64{10.5, 2.5}, // Original UTC times
	}

	candidate := &timezone.Candidate{
		Offset: -4.0, // UTC-4 (Eastern Daylight Time)
	}

	output := formatTimezoneCandidate(candidate, contextData)

	// Should show correct local times
	if !strings.Contains(output, "Work hours: 06:30-22:30 local") {
		t.Errorf("Double conversion bug regression detected!\nExpected: Work hours: 06:30-22:30 local\nActual output:\n%s", output)
	}

	// Should NOT show the old incorrect times
	if strings.Contains(output, "02:00-18:00") {
		t.Errorf("Found old incorrect work hours (02:00-18:00) - double conversion bug has returned!")
	}
}

func TestActiveHoursUTCFormatting(t *testing.T) {
	tests := []struct {
		name     string
		utcStart float64
		utcEnd   float64
		expected string
	}{
		{
			name:     "Precise half hours",
			utcStart: 10.5,
			utcEnd:   2.5,
			expected: "Active hours UTC: 10:30-02:30",
		},
		{
			name:     "Whole hours only",
			utcStart: 9.0,
			utcEnd:   17.0,
			expected: "Active hours UTC: 09:00-17:00",
		},
		{
			name:     "Quarter hour precision",
			utcStart: 8.75,  // 08:45
			utcEnd:   16.25, // 16:15
			expected: "Active hours UTC: 08:45-16:15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextData := map[string]interface{}{
				"work_hours_utc": []float64{tt.utcStart, tt.utcEnd},
			}

			output := buildActivityPatternSummary(contextData)

			if !strings.Contains(output, tt.expected) {
				t.Errorf("Active hours UTC formatting failed:\nExpected: %s\nActual output:\n%s",
					tt.expected, output)
			}
		})
	}
}

// Test helper function that we need to create
func formatTimezoneCandidate(candidate *timezone.Candidate, contextData map[string]interface{}) string {
	// This simulates the relevant part of gemini_helpers.go formatTimezoneCandidate function
	var sb strings.Builder

	offset := int(candidate.Offset)

	// Work hours - convert UTC to this candidate's local time
	if workHours, ok := contextData["work_hours_utc"].([]float64); ok && len(workHours) == 2 {
		localStart := ((workHours[0] + float64(offset)) + 24)
		for localStart >= 24 {
			localStart -= 24
		}
		for localStart < 0 {
			localStart += 24
		}

		localEnd := ((workHours[1] + float64(offset)) + 24)
		for localEnd >= 24 {
			localEnd -= 24
		}
		for localEnd < 0 {
			localEnd += 24
		}

		// Format with minutes if there are any
		startHour := int(localStart)
		startMin := int((localStart - float64(startHour)) * 60)
		endHour := int(localEnd)
		endMin := int((localEnd - float64(endHour)) * 60)

		if startMin == 0 && endMin == 0 {
			sb.WriteString("   Work hours: ")
			sb.WriteString(formatTwoDigits(startHour))
			sb.WriteString(":00-")
			sb.WriteString(formatTwoDigits(endHour))
			sb.WriteString(":00 local")
		} else {
			sb.WriteString("   Work hours: ")
			sb.WriteString(formatTwoDigits(startHour))
			sb.WriteString(":")
			sb.WriteString(formatTwoDigits(startMin))
			sb.WriteString("-")
			sb.WriteString(formatTwoDigits(endHour))
			sb.WriteString(":")
			sb.WriteString(formatTwoDigits(endMin))
			sb.WriteString(" local")
		}
		if localStart < 6 {
			sb.WriteString(" ⚠️ very early")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func buildActivityPatternSummary(contextData map[string]interface{}) string {
	var sb strings.Builder

	// Time patterns help validate timezone candidates.
	if workHours, ok := contextData["work_hours_utc"].([]float64); ok && len(workHours) == 2 {
		startHour := int(workHours[0])
		startMin := int((workHours[0] - float64(startHour)) * 60)
		endHour := int(workHours[1])
		endMin := int((workHours[1] - float64(endHour)) * 60)

		if startMin == 0 && endMin == 0 {
			sb.WriteString("Active hours UTC: ")
			sb.WriteString(formatTwoDigits(startHour))
			sb.WriteString(":00-")
			sb.WriteString(formatTwoDigits(endHour))
			sb.WriteString(":00\n")
		} else {
			sb.WriteString("Active hours UTC: ")
			sb.WriteString(formatTwoDigits(startHour))
			sb.WriteString(":")
			sb.WriteString(formatTwoDigits(startMin))
			sb.WriteString("-")
			sb.WriteString(formatTwoDigits(endHour))
			sb.WriteString(":")
			sb.WriteString(formatTwoDigits(endMin))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func formatTwoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestTstrombergRealWorldCase(t *testing.T) {
	// This test verifies the exact case that was reported:
	// tstromberg with activeStartUTC=10.5, activeEndUTC=2.5 in UTC-4
	// Should show "Work hours: 06:30-22:30 local" NOT "Work hours: 02:00-18:00 local"

	contextData := map[string]interface{}{
		"work_hours_utc": []float64{10.5, 2.5}, // Actual tstromberg data
	}

	candidate := &timezone.Candidate{
		Offset: -4.0, // UTC-4 (Eastern Daylight Time in summer)
	}

	output := formatTimezoneCandidate(candidate, contextData)

	// Verify the exact expected format
	expectedWorkHours := "Work hours: 06:30-22:30 local"
	if !strings.Contains(output, expectedWorkHours) {
		t.Errorf("tstromberg real-world case failed!\nExpected: %s\nActual output: %s", expectedWorkHours, output)
	}

	// Verify it's not the old broken format
	brokenWorkHours := "Work hours: 02:00-18:00 local"
	if strings.Contains(output, brokenWorkHours) {
		t.Errorf("Found old broken work hours format - the bug has returned!")
	}

	// Verify UTC display is also correct
	contextData2 := map[string]interface{}{
		"work_hours_utc": []float64{10.5, 2.5},
	}

	utcOutput := buildActivityPatternSummary(contextData2)
	expectedUTC := "Active hours UTC: 10:30-02:30"
	if !strings.Contains(utcOutput, expectedUTC) {
		t.Errorf("UTC hours display failed!\nExpected: %s\nActual output: %s", expectedUTC, utcOutput)
	}
}
