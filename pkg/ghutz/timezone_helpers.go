package ghutz

import (
	"fmt"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/timezone"
)

// offsetFromNamedTimezone converts IANA timezone name to current UTC offset
func offsetFromNamedTimezone(tzName string) int {
	// Handle UTC offset strings like "UTC-4" or "UTC+8"
	if strings.HasPrefix(tzName, "UTC") {
		var offset int
		if n, err := fmt.Sscanf(tzName, "UTC%d", &offset); n == 1 && err == nil {
			return offset
		}
	}
	
	// Load the IANA timezone location
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Default to 0 if we can't parse the timezone
		return 0
	}
	
	// Get current offset for this timezone
	now := time.Now()
	_, offset := now.In(loc).Zone()
	return offset / 3600 // Convert seconds to hours
}

// timezoneFromOffset converts a UTC offset to a timezone string.
func timezoneFromOffset(offsetHours int) string {
	// Return generic UTC offset format since we don't know the country at this stage
	// This is used for activity-only detection where location is unknown
	if offsetHours >= 0 {
		return fmt.Sprintf("UTC+%d", offsetHours)
	}
	return fmt.Sprintf("UTC%d", offsetHours) // Negative sign is already included
}

// generateAlternativeTimezones generates alternative timezone candidates based on work patterns.
func generateAlternativeTimezones(primaryTz string, workStart float64) []timezone.TimezoneCandidate {
	candidates := []timezone.TimezoneCandidate{}

	// Map common timezone patterns to their alternatives
	switch primaryTz {
	case "America/New_York", "US/Eastern":
		// Eastern US alternatives
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "America/Toronto",
			Offset:     -5, // Same as Eastern
			Confidence: 0.7,
		})
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "America/Montreal",
			Offset:     -5, // Same as Eastern
			Confidence: 0.7,
		})
		if workStart > 10 {
			candidates = append(candidates, timezone.TimezoneCandidate{
				Timezone:   "America/Chicago",
				Offset:     -6, // Central
				Confidence: 0.6,
			})
		}

	case "America/Los_Angeles", "US/Pacific":
		// Pacific US alternatives
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "America/Vancouver",
			Offset:     -8, // Same as Pacific
			Confidence: 0.7,
		})
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "America/Tijuana",
			Offset:     -8, // Same as Pacific
			Confidence: 0.6,
		})

	case "Europe/London":
		// UK alternatives
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Europe/Dublin",
			Offset:     0, // Same as UK
			Confidence: 0.7,
		})
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Europe/Lisbon",
			Offset:     0, // Same as UK
			Confidence: 0.6,
		})

	case "Europe/Berlin", "Europe/Amsterdam", "Europe/Paris":
		// Central European alternatives
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Europe/Brussels",
			Offset:     1, // CET
			Confidence: 0.7,
		})
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Europe/Copenhagen",
			Offset:     1, // CET
			Confidence: 0.7,
		})
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Europe/Stockholm",
			Offset:     1, // CET
			Confidence: 0.7,
		})

	case "Asia/Tokyo":
		// Japan alternatives
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Asia/Seoul",
			Offset:     9, // Same as Japan
			Confidence: 0.6,
		})

	case "Australia/Sydney", "Australia/Melbourne":
		// Australian alternatives
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Australia/Brisbane",
			Offset:     10, // AEST
			Confidence: 0.6,
		})
		candidates = append(candidates, timezone.TimezoneCandidate{
			Timezone:   "Pacific/Auckland",
			Offset:     12, // NZST
			Confidence: 0.5,
		})
	}

	// Add generic alternatives based on work start time if no specific match
	if len(candidates) == 0 {
		if workStart >= 6 && workStart <= 8 {
			candidates = append(candidates, timezone.TimezoneCandidate{
				Timezone:   "Early riser timezone",
				Offset:     0, // Unknown
				Confidence: 0.3,
			})
		} else if workStart >= 10 && workStart <= 12 {
			candidates = append(candidates, timezone.TimezoneCandidate{
				Timezone:   "Late starter timezone",
				Offset:     0, // Unknown
				Confidence: 0.3,
			})
		}
	}

	return candidates
}