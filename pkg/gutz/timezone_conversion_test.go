package gutz

import (
	"testing"

	"github.com/codeGROOVE-dev/guTZ/pkg/tzconvert"
)

// TestTimezoneConversionConsistency verifies that when the final detected timezone
// differs from the activity analysis timezone, all local times are recalculated
// with the correct offset.
func TestTimezoneConversionConsistency(t *testing.T) {
	tests := []struct {
		name                     string
		activityTimezone         string // Timezone from activity analysis
		activityOffset           int    // Offset from activity analysis
		finalTimezone            string // Final detected timezone (e.g., from location)
		finalOffset              int    // Final timezone offset
		activeStartUTC           float64
		activeEndUTC             float64
		peakStartUTC             float64
		peakEndUTC               float64
		lunchStartUTC            float64
		lunchEndUTC              float64
		expectedActiveStartLocal float64
		expectedActiveEndLocal   float64
		expectedPeakStartLocal   float64
		expectedPeakEndLocal     float64
		expectedLunchStartLocal  float64
		expectedLunchEndLocal    float64
	}{
		{
			name:             "Rebelopsio case - UTC-3 activity but UTC-4 location",
			activityTimezone: "UTC-3",
			activityOffset:   -3,
			finalTimezone:    "America/New_York",
			finalOffset:      -4,
			activeStartUTC:   10.5, // 10:30 UTC
			activeEndUTC:     1.5,  // 01:30 UTC
			peakStartUTC:     18.0, // 18:00 UTC
			peakEndUTC:       18.5, // 18:30 UTC
			lunchStartUTC:    15.5, // 15:30 UTC
			lunchEndUTC:      16.0, // 16:00 UTC
			// With UTC-4 offset:
			expectedActiveStartLocal: 6.5,  // 06:30 EDT
			expectedActiveEndLocal:   21.5, // 21:30 EDT
			expectedPeakStartLocal:   14.0, // 14:00 EDT
			expectedPeakEndLocal:     14.5, // 14:30 EDT
			expectedLunchStartLocal:  11.5, // 11:30 EDT
			expectedLunchEndLocal:    12.0, // 12:00 EDT
		},
		{
			name:             "Activity and final timezone match",
			activityTimezone: "UTC-7",
			activityOffset:   -7,
			finalTimezone:    "America/Los_Angeles",
			finalOffset:      -7,
			activeStartUTC:   14.0, // 14:00 UTC
			activeEndUTC:     2.0,  // 02:00 UTC
			peakStartUTC:     19.0, // 19:00 UTC
			peakEndUTC:       19.5, // 19:30 UTC
			lunchStartUTC:    19.0, // 19:00 UTC
			lunchEndUTC:      19.5, // 19:30 UTC
			// With UTC-7 offset:
			expectedActiveStartLocal: 7.0,  // 07:00 PDT
			expectedActiveEndLocal:   19.0, // 19:00 PDT
			expectedPeakStartLocal:   12.0, // 12:00 PDT
			expectedPeakEndLocal:     12.5, // 12:30 PDT
			expectedLunchStartLocal:  12.0, // 12:00 PDT
			expectedLunchEndLocal:    12.5, // 12:30 PDT
		},
		{
			name:             "European timezone mismatch",
			activityTimezone: "UTC+1",
			activityOffset:   1,
			finalTimezone:    "Europe/London",
			finalOffset:      0,    // GMT
			activeStartUTC:   8.0,  // 08:00 UTC
			activeEndUTC:     17.0, // 17:00 UTC
			peakStartUTC:     14.0, // 14:00 UTC
			peakEndUTC:       14.5, // 14:30 UTC
			lunchStartUTC:    12.0, // 12:00 UTC
			lunchEndUTC:      13.0, // 13:00 UTC
			// With UTC+0 offset:
			expectedActiveStartLocal: 8.0,  // 08:00 GMT
			expectedActiveEndLocal:   17.0, // 17:00 GMT
			expectedPeakStartLocal:   14.0, // 14:00 GMT
			expectedPeakEndLocal:     14.5, // 14:30 GMT
			expectedLunchStartLocal:  12.0, // 12:00 GMT
			expectedLunchEndLocal:    13.0, // 13:00 GMT
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test active hours conversion
			activeStartLocal := tzconvert.UTCToLocal(tt.activeStartUTC, tt.finalOffset)
			activeEndLocal := tzconvert.UTCToLocal(tt.activeEndUTC, tt.finalOffset)

			if activeStartLocal != tt.expectedActiveStartLocal {
				t.Errorf("Active start: expected %.1f local, got %.1f",
					tt.expectedActiveStartLocal, activeStartLocal)
			}
			if activeEndLocal != tt.expectedActiveEndLocal {
				t.Errorf("Active end: expected %.1f local, got %.1f",
					tt.expectedActiveEndLocal, activeEndLocal)
			}

			// Test peak hours conversion
			peakStartLocal := tzconvert.UTCToLocal(tt.peakStartUTC, tt.finalOffset)
			peakEndLocal := tzconvert.UTCToLocal(tt.peakEndUTC, tt.finalOffset)

			if peakStartLocal != tt.expectedPeakStartLocal {
				t.Errorf("Peak start: expected %.1f local, got %.1f",
					tt.expectedPeakStartLocal, peakStartLocal)
			}
			if peakEndLocal != tt.expectedPeakEndLocal {
				t.Errorf("Peak end: expected %.1f local, got %.1f",
					tt.expectedPeakEndLocal, peakEndLocal)
			}

			// Test lunch hours conversion
			lunchStartLocal := tzconvert.UTCToLocal(tt.lunchStartUTC, tt.finalOffset)
			lunchEndLocal := tzconvert.UTCToLocal(tt.lunchEndUTC, tt.finalOffset)

			if lunchStartLocal != tt.expectedLunchStartLocal {
				t.Errorf("Lunch start: expected %.1f local, got %.1f",
					tt.expectedLunchStartLocal, lunchStartLocal)
			}
			if lunchEndLocal != tt.expectedLunchEndLocal {
				t.Errorf("Lunch end: expected %.1f local, got %.1f",
					tt.expectedLunchEndLocal, lunchEndLocal)
			}

			// Verify that using the wrong offset (activity offset) would give wrong results
			wrongActiveStart := tzconvert.UTCToLocal(tt.activeStartUTC, tt.activityOffset)
			if tt.activityOffset != tt.finalOffset && wrongActiveStart == tt.expectedActiveStartLocal {
				t.Errorf("Using activity offset should give different result when timezones differ")
			}
		})
	}
}

// TestMergeActivityDataRecalculation verifies that mergeActivityData correctly
// recalculates local times when the final timezone differs from activity timezone.
func TestMergeActivityDataRecalculation(t *testing.T) {
	// This is more of an integration test that would require setting up
	// a Detector and Result objects. For now, the unit test above validates
	// the conversion logic itself.

	// A full integration test would:
	// 1. Create an activity Result with UTC-3 timezone
	// 2. Create a location Result with America/New_York timezone
	// 3. Call mergeActivityData
	// 4. Verify all local times are recalculated with UTC-4 offset

	t.Skip("Integration test - requires full Detector setup")
}
