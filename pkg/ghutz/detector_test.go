package ghutz

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestEasternTimeDetection verifies that users in Eastern Time zones 
// are correctly detected, especially during DST when they could be
// confused with Central Time
func TestEasternTimeDetection(t *testing.T) {
	tests := []struct {
		name           string
		quietHours     []int // UTC hours when user is typically quiet
		expectedOffset int    // Expected UTC offset
		expectedTZ     string // Expected timezone
		description    string
	}{
		{
			name:           "Miami user (Eastern Time, DST)",
			quietHours:     []int{4, 5, 6, 7, 8, 9}, // Quiet 12am-5am EDT = 4-9 UTC
			expectedOffset: -4,
			expectedTZ:     "America/New_York",
			description:    "User in Miami should be detected as Eastern Time during DST",
		},
		{
			name:           "Toronto user (Eastern Time, DST)",
			quietHours:     []int{4, 5, 6, 7, 8, 9}, // Quiet 12am-5am EDT = 4-9 UTC
			expectedOffset: -4,
			expectedTZ:     "America/New_York",
			description:    "User in Toronto should be detected as Eastern Time during DST",
		},
		{
			name:           "Chicago user (Central Time, DST)",
			quietHours:     []int{5, 6, 7, 8, 9, 10}, // Quiet 12am-5am CDT = 5-10 UTC
			expectedOffset: -5,
			expectedTZ:     "America/Chicago", // UTC-5 during DST maps to Chicago
			description:    "User in Chicago has different quiet hours pattern",
		},
		{
			name:           "Ambiguous Eastern/Central pattern",
			quietHours:     []int{4, 5, 6, 7, 8, 9, 10}, // Could be either
			expectedOffset: -4, // Should lean toward Eastern (more populous)
			expectedTZ:     "America/New_York",
			description:    "Ambiguous patterns should prefer Eastern Time",
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
			
			// Set active hours (high activity) - opposite of quiet hours
			for hour := 0; hour < 24; hour++ {
				isQuiet := false
				for _, qh := range tt.quietHours {
					if hour == qh {
						isQuiet = true
						break
					}
				}
				if !isQuiet {
					hourCounts[hour] = 10 // High activity during work hours
				}
			}
			
			// Find quiet hours using the function
			detectedQuietHours := findQuietHours(hourCounts)
			
			// Log detected quiet hours for debugging
			t.Logf("Detected quiet hours: %v (expected: %v)", detectedQuietHours, tt.quietHours)
			
			// Calculate midpoint
			var sum float64
			for _, hour := range detectedQuietHours {
				sum += float64(hour)
			}
			midQuiet := sum / float64(len(detectedQuietHours))
			
			// For US timezones, if quiet hours are early in UTC (like 4-9),
			// that means it's nighttime in the US (west of UTC)
			// If midQuiet is 6.5 UTC and that's 2:30am local, then:
			// 6.5 UTC = 2.5 local → offset = 2.5 - 6.5 = -4
			
			// Calculate offset: local_sleep_time = UTC_quiet_time + offset
			// So: offset = local_sleep_time - UTC_quiet_time
			// Assuming sleep midpoint is 2.5am local (middle of 0-5am)
			assumedSleepMidpoint := 2.5
			offsetFromUTC := assumedSleepMidpoint - midQuiet
			
			// Normalize to [-12, 12] range
			if offsetFromUTC > 12 {
				offsetFromUTC -= 24
			} else if offsetFromUTC <= -12 {
				offsetFromUTC += 24
			}
			
			offsetInt := int(offsetFromUTC)
			
			// Check offset calculation
			if offsetInt != tt.expectedOffset {
				t.Errorf("%s: expected offset %d, got %d (midQuiet=%.1f)", 
					tt.name, tt.expectedOffset, offsetInt, midQuiet)
			}
			
			// Check timezone mapping
			tz := timezoneFromOffset(offsetInt)
			t.Logf("Offset %d maps to timezone %s", offsetInt, tz)
			if tz != tt.expectedTZ {
				t.Errorf("%s: expected timezone %s, got %s (offset=%d)", 
					tt.name, tt.expectedTZ, tz, offsetInt)
			}
		})
	}
}

// TestActivityPatternAnalysis tests the full activity pattern analysis
// with real-world scenarios including the vladimirvivien and andrewsykim cases
func TestActivityPatternAnalysis(t *testing.T) {
	// Skip if no API keys are configured
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("Skipping integration test: GEMINI_API_KEY not set")
	}
	
	detector := NewWithLogger(
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})),
		WithGeminiAPIKey(os.Getenv("GEMINI_API_KEY")),
		WithMapsAPIKey(os.Getenv("GOOGLE_MAPS_API_KEY")),
	)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	tests := []struct {
		username     string
		expectedCity string
		expectedTZ   []string // Acceptable timezone options
	}{
		{
			username:     "vladimirvivien",
			expectedCity: "Florida",
			expectedTZ:   []string{"America/New_York"},
		},
		{
			username:     "andrewsykim", 
			expectedCity: "Toronto",
			expectedTZ:   []string{"America/Toronto", "America/New_York"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			result, err := detector.Detect(ctx, tt.username)
			if err != nil {
				t.Fatalf("Detection failed for %s: %v", tt.username, err)
			}
			
			// Check timezone
			found := false
			for _, tz := range tt.expectedTZ {
				if result.Timezone == tz {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: expected timezone to be one of %v, got %s", 
					tt.username, tt.expectedTZ, result.Timezone)
			}
			
			// Log the result for debugging
			t.Logf("%s detected as: TZ=%s, Location=%s, Method=%s, Confidence=%.2f",
				tt.username, result.Timezone, result.GeminiSuggestedLocation, 
				result.Method, result.Confidence)
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
			expectedTZ: []string{"America/New_York"},
		},
		{
			name:       "Central Time pattern",
			quietStart: 5,
			quietEnd:   10,
			expectedTZ: []string{"America/Chicago", "America/New_York"}, // Could be either during DST
		},
		{
			name:       "Mountain Time pattern",
			quietStart: 6,
			quietEnd:   11,
			expectedTZ: []string{"America/Denver", "America/Chicago"},
		},
		{
			name:       "Pacific Time pattern",
			quietStart: 7,
			quietEnd:   12,
			expectedTZ: []string{"America/Los_Angeles", "America/Denver", "America/Phoenix"}, // Phoenix doesn't observe DST
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