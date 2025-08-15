package ghutz

import (
	"fmt"
)

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
func generateAlternativeTimezones(primaryTz string, workStart float64) []TimezoneCandidate {
	candidates := []TimezoneCandidate{}

	// Map common timezone patterns to their alternatives
	switch primaryTz {
	case "America/New_York", "US/Eastern":
		// Eastern US alternatives
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "America/Toronto",
			Evidence:   []string{"Similar activity pattern to Eastern US", "Adjacent timezone"},
			Confidence: 0.7,
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "America/Montreal",
			Evidence:   []string{"Similar activity pattern to Eastern US", "Same timezone"},
			Confidence: 0.7,
		})
		if workStart > 10 {
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "America/Chicago",
				Evidence:   []string{"Later work start time suggests Central timezone"},
				Confidence: 0.6,
			})
		}

	case "America/Los_Angeles", "US/Pacific":
		// Pacific US alternatives
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "America/Vancouver",
			Evidence:   []string{"Similar activity pattern to Pacific US", "Same timezone"},
			Confidence: 0.7,
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "America/Tijuana",
			Evidence:   []string{"Similar activity pattern to Pacific US", "Adjacent timezone"},
			Confidence: 0.6,
		})

	case "Europe/London":
		// UK alternatives
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Europe/Dublin",
			Evidence:   []string{"Similar activity pattern to UK", "Same timezone (most of year)"},
			Confidence: 0.7,
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Europe/Lisbon",
			Evidence:   []string{"Similar activity pattern to UK", "Same timezone (most of year)"},
			Confidence: 0.6,
		})

	case "Europe/Berlin", "Europe/Amsterdam", "Europe/Paris":
		// Central European alternatives
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Europe/Brussels",
			Evidence:   []string{"Similar activity pattern to Central Europe", "Same timezone"},
			Confidence: 0.7,
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Europe/Copenhagen",
			Evidence:   []string{"Similar activity pattern to Central Europe", "Same timezone"},
			Confidence: 0.7,
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Europe/Stockholm",
			Evidence:   []string{"Similar activity pattern to Central Europe", "Same timezone"},
			Confidence: 0.7,
		})

	case "Asia/Tokyo":
		// Japan alternatives
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Asia/Seoul",
			Evidence:   []string{"Similar activity pattern to Japan", "Same timezone"},
			Confidence: 0.6,
		})

	case "Australia/Sydney", "Australia/Melbourne":
		// Australian alternatives
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Australia/Brisbane",
			Evidence:   []string{"Similar activity pattern to Eastern Australia", "Adjacent timezone"},
			Confidence: 0.6,
		})
		candidates = append(candidates, TimezoneCandidate{
			Timezone:   "Pacific/Auckland",
			Evidence:   []string{"Similar activity pattern", "2-hour offset from Eastern Australia"},
			Confidence: 0.5,
		})
	}

	// Add generic alternatives based on work start time if no specific match
	if len(candidates) == 0 {
		if workStart >= 6 && workStart <= 8 {
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "Early riser timezone",
				Evidence:   []string{fmt.Sprintf("Work starts at %.0f:00 local time", workStart)},
				Confidence: 0.3,
			})
		} else if workStart >= 10 && workStart <= 12 {
			candidates = append(candidates, TimezoneCandidate{
				Timezone:   "Late starter timezone",
				Evidence:   []string{fmt.Sprintf("Work starts at %.0f:00 local time", workStart)},
				Confidence: 0.3,
			})
		}
	}

	return candidates
}