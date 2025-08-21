package gutz

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// VerificationResult contains the results of verifying claimed vs detected location/timezone.
type VerificationResult struct {
	ClaimedLocation      string  `json:"claimed_location,omitempty"`
	ClaimedTimezone      string  `json:"claimed_timezone,omitempty"`
	LocationDistanceMiles float64 `json:"location_distance_miles,omitempty"`
	TimezoneOffsetDiff   int     `json:"timezone_offset_diff,omitempty"`
	LocationMismatch     string  `json:"location_mismatch,omitempty"` // "major" (>1000mi), "minor" (>250mi), or ""
	TimezoneMismatch     string  `json:"timezone_mismatch,omitempty"` // "major" (>3tz), "minor" (>1tz), or ""
}

// verifyLocationAndTimezone checks for discrepancies between claimed and detected location/timezone.
func (d *Detector) verifyLocationAndTimezone(ctx context.Context, profile *github.User, detectedLocation, detectedTimezone string) *VerificationResult {
	result := &VerificationResult{}

	// Check location discrepancy
	if profile.Location != "" && detectedLocation != "" {
		result.ClaimedLocation = profile.Location
		distance := d.calculateLocationDistance(ctx, profile.Location, detectedLocation)
		if distance > 0 {
			result.LocationDistanceMiles = distance
			if distance > 1000 {
				result.LocationMismatch = "major"
			} else if distance > 250 {
				result.LocationMismatch = "minor"
			}
		}
	}

	// Check timezone discrepancy
	// Note: GitHub doesn't have a timezone field, but users sometimes put it in bio or location
	// We'll check if the location field contains timezone info
	claimedTZ := extractTimezoneFromText(profile.Location)
	if claimedTZ == "" && profile.Bio != "" {
		claimedTZ = extractTimezoneFromText(profile.Bio)
	}

	if claimedTZ != "" && detectedTimezone != "" {
		result.ClaimedTimezone = claimedTZ
		offsetDiff := d.calculateTimezoneOffsetDiff(claimedTZ, detectedTimezone)
		if offsetDiff != 0 {
			result.TimezoneOffsetDiff = abs(offsetDiff)
			if abs(offsetDiff) > 3 {
				result.TimezoneMismatch = "major"
			} else if abs(offsetDiff) > 1 {
				result.TimezoneMismatch = "minor"
			}
		}
	}

	return result
}

// calculateLocationDistance calculates the distance in miles between two location strings.
func (d *Detector) calculateLocationDistance(ctx context.Context, location1, location2 string) float64 {
	// Geocode both locations
	coords1, err1 := d.geocodeLocation(ctx, location1)
	coords2, err2 := d.geocodeLocation(ctx, location2)

	if err1 != nil || err2 != nil {
		d.logger.Debug("Failed to geocode locations for distance calculation",
			"location1", location1, "error1", err1,
			"location2", location2, "error2", err2)
		return 0
	}

	// Calculate haversine distance
	return haversineDistance(coords1.Latitude, coords1.Longitude, coords2.Latitude, coords2.Longitude)
}

// haversineDistance calculates the distance in miles between two coordinates.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMiles = 3959.0 // Earth's radius in miles

	// Convert to radians
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180

	// Haversine formula
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMiles * c
}

// calculateTimezoneOffsetDiff calculates the difference in UTC offsets between two timezones.
func (d *Detector) calculateTimezoneOffsetDiff(tz1, tz2 string) int {
	offset1 := getTimezoneOffset(tz1)
	offset2 := getTimezoneOffset(tz2)
	return offset1 - offset2
}

// getTimezoneOffset returns the UTC offset in hours for a timezone.
func getTimezoneOffset(tz string) int {
	// Try to load the timezone
	loc, err := time.LoadLocation(tz)
	if err == nil {
		// Get the offset for a reference time (use January to avoid DST complications)
		refTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
		_, offset := refTime.In(loc).Zone()
		return offset / 3600 // Convert seconds to hours
	}

	// Handle UTC+/- format
	if strings.HasPrefix(tz, "UTC+") {
		offset, _ := parseOffset(strings.TrimPrefix(tz, "UTC+"))
		return offset
	}
	if strings.HasPrefix(tz, "UTC-") {
		offset, _ := parseOffset(strings.TrimPrefix(tz, "UTC-"))
		return -offset
	}

	return 0
}

// parseOffset parses an offset string like "5" or "5:30" to hours.
func parseOffset(s string) (int, error) {
	// Simple parsing - just get the hour part
	if idx := strings.Index(s, ":"); idx > 0 {
		s = s[:idx]
	}
	var hours int
	_, err := fmt.Sscanf(s, "%d", &hours)
	return hours, err
}

// extractTimezoneFromText attempts to extract timezone information from text.
func extractTimezoneFromText(text string) string {
	if text == "" {
		return ""
	}

	// Common timezone patterns
	patterns := []string{
		"PST", "PDT", "Pacific",
		"MST", "MDT", "Mountain",
		"CST", "CDT", "Central",
		"EST", "EDT", "Eastern",
		"GMT", "UTC", "BST",
		"CET", "CEST",
		"JST", "KST", "IST",
	}

	upperText := strings.ToUpper(text)
	for _, pattern := range patterns {
		if strings.Contains(upperText, pattern) {
			// Map common abbreviations to IANA timezones
			switch pattern {
			case "PST", "PDT", "PACIFIC":
				return "America/Los_Angeles"
			case "MST", "MDT", "MOUNTAIN":
				return "America/Denver"
			case "CST", "CDT", "CENTRAL":
				return "America/Chicago"
			case "EST", "EDT", "EASTERN":
				return "America/New_York"
			case "GMT", "UTC":
				return "UTC"
			case "BST":
				return "Europe/London"
			case "CET", "CEST":
				return "Europe/Berlin"
			case "JST":
				return "Asia/Tokyo"
			case "KST":
				return "Asia/Seoul"
			case "IST":
				return "Asia/Kolkata"
			}
		}
	}

	// Check for UTC+/- format
	if idx := strings.Index(upperText, "UTC+"); idx >= 0 {
		// Extract UTC+X format
		remaining := upperText[idx+4:]
		if len(remaining) > 0 {
			// Get first digit/number
			for i, ch := range remaining {
				if ch < '0' || ch > '9' {
					if i > 0 {
						return "UTC+" + remaining[:i]
					}
					break
				}
			}
		}
	}
	if idx := strings.Index(upperText, "UTC-"); idx >= 0 {
		remaining := upperText[idx+4:]
		if len(remaining) > 0 {
			for i, ch := range remaining {
				if ch < '0' || ch > '9' {
					if i > 0 {
						return "UTC-" + remaining[:i]
					}
					break
				}
			}
		}
	}

	return ""
}

// abs returns the absolute value of an integer.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}