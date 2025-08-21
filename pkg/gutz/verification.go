package gutz

import (
	"context"
	"math"
	"strconv"
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
func (d *Detector) verifyLocationAndTimezone(ctx context.Context, profile *github.User, detectedLocation *Location, detectedTimezone string, gitHubTimezone string) *VerificationResult {
	result := &VerificationResult{}

	// Check location discrepancy
	if profile.Location != "" {
		result.ClaimedLocation = profile.Location
		if detectedLocation != nil {
			distance := d.calculateLocationDistanceFromCoords(ctx, profile.Location, detectedLocation.Latitude, detectedLocation.Longitude)
			result.LocationDistanceMiles = distance
			// Only set mismatch if we could calculate a valid distance
			if distance > 0 {
				if distance > 1000 {
					result.LocationMismatch = "major"
				} else if distance > 250 {
					result.LocationMismatch = "minor"
				}
			}
			// If distance is -1, it means geocoding failed but we still want to show the claimed location
		} else {
			// No detected location, so we can't calculate distance
			// Set to -1 to indicate the claimed location should be shown
			result.LocationDistanceMiles = -1
		}
	}

	// Check timezone discrepancy
	// Use GitHub's official timezone from their profile if available
	if gitHubTimezone != "" && detectedTimezone != "" {
		result.ClaimedTimezone = gitHubTimezone
		d.logger.Debug("using GitHub profile timezone for verification", 
			"username", profile.Login, "github_timezone", gitHubTimezone)
		
		offsetDiff := d.calculateTimezoneOffsetDiff(gitHubTimezone, detectedTimezone)
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

// calculateLocationDistanceFromCoords calculates the distance in miles between a location string and known coordinates.
func (d *Detector) calculateLocationDistanceFromCoords(ctx context.Context, claimedLocation string, detectedLat, detectedLon float64) float64 {
	// Only need to geocode the claimed location
	coords, err := d.geocodeLocation(ctx, claimedLocation)
	if err != nil {
		d.logger.Debug("Failed to geocode claimed location for distance calculation",
			"claimed_location", claimedLocation, "error", err)
		// Return -1 to indicate geocoding failure
		return -1
	}

	// Calculate haversine distance using the known detected coordinates
	return haversineDistance(coords.Latitude, coords.Longitude, detectedLat, detectedLon)
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
	// Round the difference to nearest hour for comparison
	diff := offset1 - offset2
	return int(math.Round(diff))
}

// getTimezoneOffset returns the UTC offset in hours for a timezone.
func getTimezoneOffset(tz string) float64 {
	// Try to load the timezone
	loc, err := time.LoadLocation(tz)
	if err == nil {
		// Get the offset for a reference time (use January to avoid DST complications)
		refTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
		_, offset := refTime.In(loc).Zone()
		return float64(offset) / 3600 // Convert seconds to hours
	}

	// Handle UTC+/- format (including fractional hours like UTC+5.5)
	if strings.HasPrefix(tz, "UTC+") {
		offsetStr := strings.TrimPrefix(tz, "UTC+")
		if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
			return offset
		}
		// Try as int if float parsing fails
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			return float64(offset)
		}
	}
	if strings.HasPrefix(tz, "UTC-") {
		offsetStr := strings.TrimPrefix(tz, "UTC-")
		if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
			return -offset
		}
		// Try as int if float parsing fails
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			return float64(-offset)
		}
	}
	if strings.HasPrefix(tz, "UTC") && len(tz) > 3 {
		// Handle formats like UTC-5.5 or UTC5.5
		offsetStr := strings.TrimPrefix(tz, "UTC")
		if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
			return offset
		}
	}

	return 0
}

// abs returns the absolute value of an integer.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}