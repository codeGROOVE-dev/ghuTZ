package ghutz

import (
	"fmt"
	"strings"
	"time"
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
