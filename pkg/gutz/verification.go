package gutz

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
)

// VerificationResult contains the results of verifying profile vs detected location/timezone.
type VerificationResult struct {
	ProfileLocation         string  `json:"profile_location,omitempty"`
	ProfileTimezone         string  `json:"profile_timezone,omitempty"`
	ProfileLocationTimezone string  `json:"profile_location_timezone,omitempty"`
	LocationMismatch        string  `json:"location_mismatch,omitempty"`
	TimezoneMismatch        string  `json:"timezone_mismatch,omitempty"`
	ProfileLocationDiff     int     `json:"profile_location_diff,omitempty"`
	ActivityOffsetDiff      int     `json:"activity_offset_diff,omitempty"`
	LocationDistanceKm      float64 `json:"location_distance_km,omitempty"`
	TimezoneOffsetDiff      int     `json:"timezone_offset_diff,omitempty"`
	ActivityMismatch        bool    `json:"activity_mismatch,omitempty"`
}

// verifyLocationAndTimezone checks for discrepancies between profile and detected location/timezone.
func (d *Detector) verifyLocationAndTimezone(ctx context.Context, profile *github.User, detectedLocation *Location, detectedTimezone string, profileTimezone string, profileLocationTimezone string, activityTimezone string) *VerificationResult {
	result := &VerificationResult{
		ProfileTimezone:         profileTimezone,
		ProfileLocationTimezone: profileLocationTimezone,
	}

	// Calculate difference between profile timezone and profile location timezone if both exist
	if profileTimezone != "" && profileLocationTimezone != "" {
		profileLocationDiff := d.calculateTimezoneOffsetDiff(profileTimezone, profileLocationTimezone)
		if profileLocationDiff != 0 {
			if profileLocationDiff < 0 {
				result.ProfileLocationDiff = -profileLocationDiff
			} else {
				result.ProfileLocationDiff = profileLocationDiff
			}
			d.logger.Debug("profile timezone vs profile location timezone difference",
				"username", profile.Login,
				"profile_timezone", profileTimezone,
				"profile_location_timezone", profileLocationTimezone,
				"diff_hours", profileLocationDiff)
		}
	}

	// Check location discrepancy
	if profile.Location != "" {
		result.ProfileLocation = profile.Location
		if detectedLocation != nil {
			distance := d.calculateLocationDistanceFromCoords(ctx, profile.Location, detectedLocation.Latitude, detectedLocation.Longitude)
			result.LocationDistanceKm = distance
			// Only set mismatch if we could calculate a valid distance
			if distance > 0 {
				if distance > 1000 {
					result.LocationMismatch = "major"
				} else if distance > 400 {
					result.LocationMismatch = "minor"
				}
			}
			// If distance is -1, it means geocoding failed but we still want to show the claimed location
		} else {
			// No detected location, so we can't calculate distance
			// Set to -1 to indicate the claimed location should be shown
			result.LocationDistanceKm = -1
		}
	}

	// Check timezone discrepancy
	// Prefer profile timezone from GitHub profile, fallback to profile location timezone
	timezoneToCheck := profileTimezone
	if timezoneToCheck == "" {
		timezoneToCheck = profileLocationTimezone
	}

	if timezoneToCheck != "" && detectedTimezone != "" {
		offsetDiff := d.calculateTimezoneOffsetDiff(timezoneToCheck, detectedTimezone)
		if offsetDiff != 0 {
			absOffsetDiff := offsetDiff
			if absOffsetDiff < 0 {
				absOffsetDiff = -absOffsetDiff
			}
			result.TimezoneOffsetDiff = absOffsetDiff
			if absOffsetDiff > 3 {
				result.TimezoneMismatch = "major"
			} else if absOffsetDiff > 1 {
				result.TimezoneMismatch = "minor"
			}

			d.logger.Debug("timezone mismatch detected",
				"username", profile.Login,
				"profile_tz", profileTimezone,
				"profile_location_tz", profileLocationTimezone,
				"detected_tz", detectedTimezone,
				"diff_hours", offsetDiff)
		}
	}

	// Check activity timezone discrepancy (pure activity-based offset)
	if activityTimezone != "" {
		// Compare activity timezone to profile timezone or profile location timezone (whichever exists)
		compareToTimezone := profileTimezone
		if compareToTimezone == "" {
			compareToTimezone = profileLocationTimezone
		}

		if compareToTimezone != "" {
			activityDiff := d.calculateTimezoneOffsetDiff(activityTimezone, compareToTimezone)
			absActivityDiff := activityDiff
			if absActivityDiff < 0 {
				absActivityDiff = -absActivityDiff
			}
			result.ActivityOffsetDiff = absActivityDiff

			// Flag as activity mismatch if difference is >4 hours
			if absActivityDiff > 4 {
				result.ActivityMismatch = true
				d.logger.Warn("large activity timezone discrepancy detected",
					"username", profile.Login,
					"activity_tz", activityTimezone,
					"profile_tz", profileTimezone,
					"profile_location_tz", profileLocationTimezone,
					"diff_hours", activityDiff)
			}
		}
	}

	return result
}

// calculateLocationDistanceFromCoords calculates the distance in miles between a location string and known coordinates.
func (d *Detector) calculateLocationDistanceFromCoords(ctx context.Context, profileLocation string, detectedLat, detectedLon float64) float64 {
	// Only need to geocode the profile location
	coords, err := d.geocodeLocation(ctx, profileLocation)
	if err != nil {
		d.logger.Debug("Failed to geocode profile location for distance calculation",
			"profile_location", profileLocation, "error", err)
		// Return -1 to indicate geocoding failure
		return -1
	}

	// Calculate haversine distance using the known detected coordinates
	return haversineDistance(coords.Latitude, coords.Longitude, detectedLat, detectedLon)
}

// haversineDistance calculates the distance in kilometers between two coordinates.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0 // Earth's radius in kilometers

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

	return earthRadiusKm * c
}

// calculateTimezoneOffsetDiff calculates the difference in UTC offsets between two timezones.
func (*Detector) calculateTimezoneOffsetDiff(tz1, tz2 string) int {
	offset1 := timezoneOffset(tz1)
	offset2 := timezoneOffset(tz2)
	// Round the difference to nearest hour for comparison
	diff := offset1 - offset2
	return int(math.Round(diff))
}

// timezoneOffset returns the UTC offset in hours for a timezone.
func timezoneOffset(tz string) float64 {
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
