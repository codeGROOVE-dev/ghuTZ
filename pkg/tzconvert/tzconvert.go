// Package tzconvert provides foolproof timezone conversion utilities.
// ALL times in the codebase should be stored in UTC.
// These functions handle the conversion to/from local time for display only.
package tzconvert

import (
	"math"
	"time"
)

// UTCToLocal converts a UTC hour to local hour given a UTC offset.
// Example: UTCToLocal(15.5, -4) converts 15:30 UTC to 11:30 EDT (UTC-4)
// Example: UTCToLocal(2.0, 8) converts 02:00 UTC to 10:00 CST (UTC+8)
//
// Parameters:
//   - utcHour: Hour in UTC (0-24, can include fractional hours like 15.5 for 15:30)
//   - utcOffset: The timezone offset from UTC (negative for west, positive for east)
//     Examples: -4 for EDT, -7 for PDT, 0 for GMT, 8 for CST (China)
//
// Returns: Local hour (0-24), properly wrapped for day boundaries.
func UTCToLocal(utcHour float64, utcOffset int) float64 {
	// Add the offset to convert from UTC to local
	// For UTC-4: UTC 15:00 + (-4) = 11:00 local
	// For UTC+8: UTC 02:00 + 8 = 10:00 local
	localHour := utcHour + float64(utcOffset)

	// Wrap around 24-hour clock
	return math.Mod(localHour+24, 24)
}

// LocalToUTC converts a local hour to UTC given a UTC offset.
// Example: LocalToUTC(11.5, -4) converts 11:30 EDT to 15:30 UTC
// Example: LocalToUTC(10.0, 8) converts 10:00 CST to 02:00 UTC
//
// Parameters:
//   - localHour: Hour in local time (0-24, can include fractional hours)
//   - utcOffset: The timezone offset from UTC (negative for west, positive for east)
//
// Returns: UTC hour (0-24), properly wrapped for day boundaries.
func LocalToUTC(localHour float64, utcOffset int) float64 {
	// Subtract the offset to convert from local to UTC
	// For UTC-4: Local 11:00 - (-4) = 15:00 UTC
	// For UTC+8: Local 10:00 - 8 = 02:00 UTC
	utcHour := localHour - float64(utcOffset)

	// Wrap around 24-hour clock
	return math.Mod(utcHour+24, 24)
}

// ConvertRangeUTCToLocal converts a time range from UTC to local time.
// Handles ranges that may cross midnight.
//
// Parameters:
//   - startUTC, endUTC: Start and end hours in UTC
//   - utcOffset: The timezone offset from UTC
//
// Returns: Start and end hours in local time.
func ConvertRangeUTCToLocal(startUTC, endUTC float64, utcOffset int) (localStart, localEnd float64) {
	return UTCToLocal(startUTC, utcOffset), UTCToLocal(endUTC, utcOffset)
}

// ConvertRangeLocalToUTC converts a time range from local to UTC time.
// Handles ranges that may cross midnight.
//
// Parameters:
//   - startLocal, endLocal: Start and end hours in local time
//   - utcOffset: The timezone offset from UTC
//
// Returns: Start and end hours in UTC.
func ConvertRangeLocalToUTC(startLocal, endLocal float64, utcOffset int) (utcStart, utcEnd float64) {
	return LocalToUTC(startLocal, utcOffset), LocalToUTC(endLocal, utcOffset)
}

// ParseTimezoneOffset extracts the numeric offset from a timezone string.
// For IANA timezones, it uses Go's time package to get the current offset.
// Examples:
//   - "UTC-4" returns -4
//   - "UTC+8" returns 8
//   - "UTC" returns 0
//   - "America/New_York" returns -4 or -5 depending on DST
//   - "Pacific/Auckland" returns 12 or 13 depending on DST
//   - Invalid input returns 0
func ParseTimezoneOffset(timezone string) int {
	// First try UTC format
	if len(timezone) >= 3 && timezone[:3] == "UTC" {
		if len(timezone) == 3 {
			return 0 // Plain "UTC"
		}

		// Parse the offset
		offsetStr := timezone[3:]
		if offsetStr == "" {
			return 0
		}

		// Handle the sign
		sign := 1
		switch offsetStr[0] {
		case '-':
			sign = -1
			offsetStr = offsetStr[1:]
		case '+':
			offsetStr = offsetStr[1:]
		default:
			// No sign means positive offset
		}

		// Parse the number
		offset := 0
		for _, ch := range offsetStr {
			if ch < '0' || ch > '9' {
				break
			}
			offset = offset*10 + int(ch-'0')
		}

		return sign * offset
	}

	// Try loading as IANA timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return 0 // Invalid timezone
	}

	// Get current offset
	_, offset := time.Now().In(loc).Zone()
	return offset / 3600 // Convert seconds to hours
}
