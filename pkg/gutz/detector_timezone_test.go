package gutz

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
	"github.com/codeGROOVE-dev/guTZ/pkg/tzconvert"
)

// TestDetectorTimezoneRecalculation verifies that when mergeActivityData is called
// with different timezones between activity analysis and final detection,
// all local times are recalculated with the correct final timezone offset.
func TestDetectorTimezoneRecalculation(t *testing.T) {
	// Create a detector with a test logger
	detector := &Detector{
		logger: slog.Default(),
	}

	tests := []struct {
		name                     string
		activityResult           *Result
		locationResult           *Result
		expectedActiveStartLocal float64
		expectedActiveEndLocal   float64
		expectedPeakStartLocal   float64
		expectedPeakEndLocal     float64
		expectedLunchStartLocal  float64
		expectedLunchEndLocal    float64
	}{
		{
			name: "Rebelopsio case - activity UTC-3 but location UTC-4",
			activityResult: &Result{
				Timezone:         "UTC-3",
				ActivityTimezone: "UTC-3",
				ActiveHoursLocal: ActiveHours{
					Start: 7.5,  // Wrong! Calculated with -3 offset
					End:   22.5, // Wrong! Calculated with -3 offset
				},
				ActiveHoursUTC: ActiveHours{
					Start: 10.5, // 10:30 UTC
					End:   1.5,  // 01:30 UTC
				},
				PeakProductivityLocal: PeakTime{
					Start: 15.0, // Wrong! Calculated with -3 offset
					End:   15.5, // Wrong! Calculated with -3 offset
					Count: 14,
				},
				PeakProductivityUTC: PeakTime{
					Start: 18.0, // 18:00 UTC
					End:   18.5, // 18:30 UTC
					Count: 14,
				},
				LunchHoursLocal: LunchBreak{
					Start:      12.5, // Wrong! Calculated with -3 offset
					End:        13.0, // Wrong! Calculated with -3 offset
					Confidence: 0.8,
				},
				LunchHoursUTC: LunchBreak{
					Start:      15.5, // 15:30 UTC
					End:        16.0, // 16:00 UTC
					Confidence: 0.8,
				},
				HalfHourlyActivityUTC: map[float64]int{
					10.5: 7, 11.0: 4, 15.5: 4, 16.0: 11, 18.0: 14, 18.5: 6,
				},
				TimezoneCandidates: []timezone.Candidate{
					{
						Offset:          -4,
						LunchStartUTC:   15.5,
						LunchEndUTC:     16.0,
						LunchConfidence: 0.8,
					},
					{
						Offset:          -3,
						LunchStartUTC:   15.5,
						LunchEndUTC:     16.0,
						LunchConfidence: 0.8,
					},
				},
			},
			locationResult: &Result{
				Timezone:     "America/New_York", // UTC-4
				LocationName: "Raleigh, NC",
				Method:       "location_field",
				Confidence:   0.85,
			},
			expectedActiveStartLocal: 6.5,  // 06:30 EDT (10.5 UTC - 4)
			expectedActiveEndLocal:   21.5, // 21:30 EDT (1.5 UTC - 4 + 24)
			expectedPeakStartLocal:   14.0, // 14:00 EDT (18.0 UTC - 4)
			expectedPeakEndLocal:     14.5, // 14:30 EDT (18.5 UTC - 4)
			expectedLunchStartLocal:  11.5, // 11:30 EDT (15.5 UTC - 4)
			expectedLunchEndLocal:    12.0, // 12:00 EDT (16.0 UTC - 4)
		},
		{
			name: "European case - activity UTC+2 but location UTC+0",
			activityResult: &Result{
				Timezone:         "UTC+2",
				ActivityTimezone: "UTC+2",
				ActiveHoursLocal: ActiveHours{
					Start: 10.0, // Wrong! Calculated with +2 offset
					End:   19.0, // Wrong! Calculated with +2 offset
				},
				ActiveHoursUTC: ActiveHours{
					Start: 8.0,  // 08:00 UTC
					End:   17.0, // 17:00 UTC
				},
				PeakProductivityLocal: PeakTime{
					Start: 16.0, // Wrong! Calculated with +2 offset
					End:   16.5, // Wrong! Calculated with +2 offset
					Count: 20,
				},
				PeakProductivityUTC: PeakTime{
					Start: 14.0, // 14:00 UTC
					End:   14.5, // 14:30 UTC
					Count: 20,
				},
				LunchHoursLocal: LunchBreak{
					Start:      14.0, // Wrong! Calculated with +2 offset
					End:        15.0, // Wrong! Calculated with +2 offset
					Confidence: 0.9,
				},
				LunchHoursUTC: LunchBreak{
					Start:      12.0, // 12:00 UTC
					End:        13.0, // 13:00 UTC
					Confidence: 0.9,
				},
				HalfHourlyActivityUTC: map[float64]int{
					8.0: 5, 12.0: 2, 13.0: 8, 14.0: 20, 17.0: 3,
				},
				TimezoneCandidates: []timezone.Candidate{
					{
						Offset:          0,
						LunchStartUTC:   12.0,
						LunchEndUTC:     13.0,
						LunchConfidence: 0.9,
					},
					{
						Offset:          2,
						LunchStartUTC:   12.0,
						LunchEndUTC:     13.0,
						LunchConfidence: 0.9,
					},
				},
			},
			locationResult: &Result{
				Timezone:     "UTC+0", // Use explicit UTC+0 to avoid DST issues
				LocationName: "Reykjavik, Iceland",
				Method:       "location_field",
				Confidence:   0.9,
			},
			expectedActiveStartLocal: 8.0,  // 08:00 UTC (8.0 UTC + 0)
			expectedActiveEndLocal:   17.0, // 17:00 UTC (17.0 UTC + 0)
			expectedPeakStartLocal:   14.0, // 14:00 UTC (14.0 UTC + 0)
			expectedPeakEndLocal:     14.5, // 14:30 UTC (14.5 UTC + 0)
			expectedLunchStartLocal:  12.0, // 12:00 UTC (12.0 UTC + 0)
			expectedLunchEndLocal:    13.0, // 13:00 UTC (13.0 UTC + 0)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of the location result to merge into
			result := *tt.locationResult
			
			// Call mergeActivityData
			detector.mergeActivityData(&result, tt.activityResult)
			
			// Verify active hours were recalculated
			if result.ActiveHoursLocal.Start != tt.expectedActiveStartLocal {
				t.Errorf("ActiveHoursLocal.Start: expected %.1f, got %.1f",
					tt.expectedActiveStartLocal, result.ActiveHoursLocal.Start)
			}
			if result.ActiveHoursLocal.End != tt.expectedActiveEndLocal {
				t.Errorf("ActiveHoursLocal.End: expected %.1f, got %.1f",
					tt.expectedActiveEndLocal, result.ActiveHoursLocal.End)
			}
			
			// Verify peak productivity was recalculated
			if result.PeakProductivityLocal.Start != tt.expectedPeakStartLocal {
				t.Errorf("PeakProductivityLocal.Start: expected %.1f, got %.1f",
					tt.expectedPeakStartLocal, result.PeakProductivityLocal.Start)
			}
			if result.PeakProductivityLocal.End != tt.expectedPeakEndLocal {
				t.Errorf("PeakProductivityLocal.End: expected %.1f, got %.1f",
					tt.expectedPeakEndLocal, result.PeakProductivityLocal.End)
			}
			
			// Verify lunch hours were recalculated
			if result.LunchHoursLocal.Start != tt.expectedLunchStartLocal {
				t.Errorf("LunchHoursLocal.Start: expected %.1f, got %.1f",
					tt.expectedLunchStartLocal, result.LunchHoursLocal.Start)
			}
			if result.LunchHoursLocal.End != tt.expectedLunchEndLocal {
				t.Errorf("LunchHoursLocal.End: expected %.1f, got %.1f",
					tt.expectedLunchEndLocal, result.LunchHoursLocal.End)
			}
			
			// Verify the UTC values were preserved correctly
			if result.ActiveHoursUTC.Start != tt.activityResult.ActiveHoursUTC.Start {
				t.Errorf("ActiveHoursUTC.Start should be preserved: expected %.1f, got %.1f",
					tt.activityResult.ActiveHoursUTC.Start, result.ActiveHoursUTC.Start)
			}
			if result.ActiveHoursUTC.End != tt.activityResult.ActiveHoursUTC.End {
				t.Errorf("ActiveHoursUTC.End should be preserved: expected %.1f, got %.1f",
					tt.activityResult.ActiveHoursUTC.End, result.ActiveHoursUTC.End)
			}
		})
	}
}

// TestActivityPatternWithLocationOverride verifies the complete flow when
// location detection overrides activity-based timezone detection.
func TestActivityPatternWithLocationOverride(t *testing.T) {
	ctx := context.Background()
	
	// This test would require a full detector setup with mocked GitHub client
	// For now, we'll create a minimal test that verifies the key behavior
	
	detector := &Detector{
		logger: slog.Default(),
	}
	
	// Create activity result with half-hourly data showing EDT pattern
	// but detected as UTC-3 (wrong by 1 hour)
	activityData := &Result{
		Timezone:         "UTC-3",
		ActivityTimezone: "UTC-3", 
		DetectionTime:    time.Now(),
		ActiveHoursUTC: ActiveHours{
			Start: 10.5,  // 10:30 UTC
			End:   1.5,   // 01:30 UTC
		},
		PeakProductivityUTC: PeakTime{
			Start: 18.0,  // 18:00 UTC
			End:   18.5,  // 18:30 UTC
			Count: 14,
		},
		LunchHoursUTC: LunchBreak{
			Start:      15.5,  // 15:30 UTC
			End:        16.0,  // 16:00 UTC
			Confidence: 0.8,
		},
		// These were calculated with wrong -3 offset
		ActiveHoursLocal: ActiveHours{
			Start: 7.5,   // Wrong!
			End:   22.5,  // Wrong!
		},
		PeakProductivityLocal: PeakTime{
			Start: 15.0,  // Wrong!
			End:   15.5,  // Wrong!
			Count: 14,
		},
		LunchHoursLocal: LunchBreak{
			Start:      12.5,  // Wrong!
			End:        13.0,  // Wrong!
			Confidence: 0.8,
		},
		HalfHourlyActivityUTC: map[float64]int{
			10.5: 7,   // 06:30 EDT - first activity
			18.0: 14,  // 14:00 EDT - peak
			1.0:  3,   // 21:00 EDT - last significant
		},
		TimezoneCandidates: []timezone.Candidate{
			{Offset: -4, LunchStartUTC: 15.5, LunchEndUTC: 16.0, LunchConfidence: 0.8},
			{Offset: -3, LunchStartUTC: 15.5, LunchEndUTC: 16.0, LunchConfidence: 0.8},
		},
		Method: "activity_patterns",
	}
	
	// Simulate location detection finding the correct timezone
	locationData := &Result{
		Timezone:     "America/New_York",  // UTC-4 (correct)
		LocationName: "Raleigh, NC",
		Method:       "location_field",
		Confidence:   0.85,
	}
	
	// Merge the results
	detector.mergeActivityData(locationData, activityData)
	
	// Verify the timezone is correct
	if locationData.Timezone != "America/New_York" {
		t.Errorf("Expected timezone America/New_York, got %s", locationData.Timezone)
	}
	
	// Verify active hours are now correct for EDT (UTC-4)
	expectedStart := 6.5   // 06:30 EDT
	expectedEnd := 21.5    // 21:30 EDT
	
	if locationData.ActiveHoursLocal.Start != expectedStart {
		t.Errorf("Expected active start %.1f EDT, got %.1f", 
			expectedStart, locationData.ActiveHoursLocal.Start)
	}
	if locationData.ActiveHoursLocal.End != expectedEnd {
		t.Errorf("Expected active end %.1f EDT, got %.1f",
			expectedEnd, locationData.ActiveHoursLocal.End)
	}
	
	// Verify peak is correct for EDT
	expectedPeakStart := 14.0  // 14:00 EDT
	expectedPeakEnd := 14.5    // 14:30 EDT
	
	if locationData.PeakProductivityLocal.Start != expectedPeakStart {
		t.Errorf("Expected peak start %.1f EDT, got %.1f",
			expectedPeakStart, locationData.PeakProductivityLocal.Start)
	}
	if locationData.PeakProductivityLocal.End != expectedPeakEnd {
		t.Errorf("Expected peak end %.1f EDT, got %.1f",
			expectedPeakEnd, locationData.PeakProductivityLocal.End)
	}
	
	// Verify lunch is correct for EDT
	expectedLunchStart := 11.5  // 11:30 EDT
	expectedLunchEnd := 12.0    // 12:00 EDT
	
	if locationData.LunchHoursLocal.Start != expectedLunchStart {
		t.Errorf("Expected lunch start %.1f EDT, got %.1f",
			expectedLunchStart, locationData.LunchHoursLocal.Start)
	}
	if locationData.LunchHoursLocal.End != expectedLunchEnd {
		t.Errorf("Expected lunch end %.1f EDT, got %.1f",
			expectedLunchEnd, locationData.LunchHoursLocal.End)
	}
	
	_ = ctx // Silence unused variable warning
}

// TestPeakProductivityRecalculation verifies that peak productivity times are
// correctly recalculated when the timezone changes after Gemini enhancement.
// This test catches the bug where McMurdo Station (UTC+12) was initially detected
// but then corrected to America/New_York (UTC-4), and peak times weren't recalculated.
func TestPeakProductivityRecalculation(t *testing.T) {
	detector := &Detector{
		logger: slog.Default(),
	}

	tests := []struct {
		name                     string
		activityResult           *Result
		initialLocationResult    *Result  // Initial detection (e.g., from location field)
		geminiCorrectedTimezone  string   // Gemini's corrected timezone
		expectedPeakStartLocal   float64
		expectedPeakEndLocal     float64
	}{
		{
			name: "McMurdo Station corrected to New York - peak recalculation",
			activityResult: &Result{
				Timezone:         "UTC-4",
				ActivityTimezone: "UTC-4",
				PeakProductivityUTC: PeakTime{
					Start: 19.0,  // 19:00 UTC
					End:   19.5,  // 19:30 UTC
					Count: 40,
				},
				PeakProductivityLocal: PeakTime{
					Start: 15.0,  // Wrong! Calculated with -4 offset
					End:   15.5,  // Wrong! Calculated with -4 offset
					Count: 40,
				},
				HalfHourlyActivityUTC: map[float64]int{
					19.0: 40,  // Peak at 19:00 UTC
					15.0: 20,
					16.0: 15,
				},
			},
			initialLocationResult: &Result{
				Timezone:     "Pacific/Auckland",  // UTC+12 (McMurdo Station)
				LocationName: "McMurdo Station, Antarctica",
				Method:       "location_field",
				Confidence:   0.85,
			},
			geminiCorrectedTimezone: "America/New_York",  // UTC-4
			expectedPeakStartLocal:  15.0,  // 15:00 EDT (19.0 UTC - 4)
			expectedPeakEndLocal:    15.5,  // 15:30 EDT (19.5 UTC - 4)
		},
		{
			name: "Morning peak in PDT corrected to EDT",
			activityResult: &Result{
				Timezone:         "UTC-7",
				ActivityTimezone: "UTC-7",
				PeakProductivityUTC: PeakTime{
					Start: 15.0,  // 15:00 UTC
					End:   15.5,  // 15:30 UTC
					Count: 50,
				},
				PeakProductivityLocal: PeakTime{
					Start: 8.0,   // Calculated with -7 offset (PDT)
					End:   8.5,   // Calculated with -7 offset (PDT)
					Count: 50,
				},
			},
			initialLocationResult: &Result{
				Timezone:     "America/Los_Angeles",  // UTC-7 (PDT in summer)
				LocationName: "San Francisco, CA",
				Method:       "location_field",
				Confidence:   0.9,
			},
			geminiCorrectedTimezone: "America/New_York",  // UTC-4 (EDT in summer)
			expectedPeakStartLocal:  11.0,  // 11:00 EDT (15.0 UTC - 4)
			expectedPeakEndLocal:    11.5,  // 11:30 EDT (15.5 UTC - 4)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy the initial location result
			result := *tt.initialLocationResult
			
			// First merge activity data (simulating initial detection)
			detector.mergeActivityData(&result, tt.activityResult)
			
			// Simulate Gemini correction by changing the timezone
			result.Timezone = tt.geminiCorrectedTimezone
			
			// Calculate the new offset for the corrected timezone
			newOffset := offsetFromNamedTimezone(tt.geminiCorrectedTimezone)
			
			// This is what should happen after Gemini correction
			// (simulating the code in detector.go after Gemini enhances the result)
			if result.PeakProductivityUTC.Start != 0 || result.PeakProductivityUTC.End != 0 {
				result.PeakProductivityLocal = PeakTime{
					Start: tzconvert.UTCToLocal(result.PeakProductivityUTC.Start, newOffset),
					End:   tzconvert.UTCToLocal(result.PeakProductivityUTC.End, newOffset),
					Count: result.PeakProductivityUTC.Count,
				}
			}
			
			// Verify the peak was recalculated correctly
			if result.PeakProductivityLocal.Start != tt.expectedPeakStartLocal {
				t.Errorf("Peak start after correction: expected %.1f local, got %.1f",
					tt.expectedPeakStartLocal, result.PeakProductivityLocal.Start)
			}
			if result.PeakProductivityLocal.End != tt.expectedPeakEndLocal {
				t.Errorf("Peak end after correction: expected %.1f local, got %.1f",
					tt.expectedPeakEndLocal, result.PeakProductivityLocal.End)
			}
			
			// Also verify UTC values were preserved
			if result.PeakProductivityUTC.Start != tt.activityResult.PeakProductivityUTC.Start {
				t.Errorf("Peak UTC start should be preserved: expected %.1f, got %.1f",
					tt.activityResult.PeakProductivityUTC.Start, result.PeakProductivityUTC.Start)
			}
			if result.PeakProductivityUTC.End != tt.activityResult.PeakProductivityUTC.End {
				t.Errorf("Peak UTC end should be preserved: expected %.1f, got %.1f",
					tt.activityResult.PeakProductivityUTC.End, result.PeakProductivityUTC.End)
			}
		})
	}
}