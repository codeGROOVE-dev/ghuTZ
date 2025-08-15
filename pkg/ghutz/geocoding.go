package ghutz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// geocodeLocation converts a location string to coordinates using Google Geocoding API.
func (d *Detector) geocodeLocation(ctx context.Context, location string) (*Location, error) {
	if d.mapsAPIKey == "" {
		d.logger.Warn("Google Maps API key not configured - skipping geocoding", "location", location)
		return nil, errors.New("google Maps API key not configured")
	}

	encodedLocation := url.QueryEscape(location)
	apiURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s",
		encodedLocation, d.mapsAPIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	var result struct {
		Results []struct {
			Geometry struct {
				Location struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"location"`
				LocationType string `json:"location_type"`
			} `json:"geometry"`
			Types            []string `json:"types"`
			FormattedAddress string   `json:"formatted_address"`
		} `json:"results"`
		Status string `json:"status"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	d.logger.Debug("geocoding API raw response", "location", location, "status", resp.StatusCode,
		"content_type", resp.Header.Get("Content-Type"), "body_preview", string(body[:min(200, len(body))]))

	if err := json.Unmarshal(body, &result); err != nil {
		d.logger.Debug("geocoding JSON parse error", "location", location, "error", err, "full_body", string(body))
		return nil, fmt.Errorf("failed to parse geocoding response: %w", err)
	}

	if result.Status != "OK" || len(result.Results) == 0 {
		d.logger.Debug("geocoding failed", "location", location, "status", result.Status, "results_count", len(result.Results))
		return nil, fmt.Errorf("geocoding failed for %s: %s", location, result.Status)
	}

	// Use the first result
	firstResult := result.Results[0]
	locationType := strings.ToLower(firstResult.Geometry.LocationType)

	// Check if result is too imprecise
	if locationType == "approximate" {
		// Check if this is a country-level result (too imprecise)
		hasCountryType := false
		hasPreciseType := false
		for _, t := range firstResult.Types {
			if t == "country" {
				hasCountryType = true
			}
			if t == "locality" || t == "administrative_area_level_1" || t == "administrative_area_level_2" {
				hasPreciseType = true
			}
		}

		if hasCountryType && !hasPreciseType {
			d.logger.Debug("rejecting imprecise geocoding result", "location", location,
				"location_type", locationType, "reason", "country-level approximate result")
			return nil, fmt.Errorf("location too imprecise for reliable timezone detection: %s", location)
		}
	}

	coords := &Location{
		Latitude:  firstResult.Geometry.Location.Lat,
		Longitude: firstResult.Geometry.Location.Lng,
	}

	return coords, nil
}

// timezoneForCoordinates gets the timezone for given coordinates using Google Timezone API.
func (d *Detector) timezoneForCoordinates(ctx context.Context, lat, lng float64) (string, error) {
	if d.mapsAPIKey == "" {
		return "", fmt.Errorf("Google Maps API key not configured")
	}

	timestamp := "1609459200" // 2021-01-01 00:00:00 UTC (arbitrary date for timezone lookup)
	apiURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/timezone/json?location=%f,%f&timestamp=%s&key=%s",
		lat, lng, timestamp, d.mapsAPIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return "", err
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	var result struct {
		TimeZoneID   string `json:"timeZoneId"`
		TimeZoneName string `json:"timeZoneName"`
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Status != "OK" {
		if result.ErrorMessage != "" {
			return "", fmt.Errorf("timezone API failed: %s", result.ErrorMessage)
		}
		return "", fmt.Errorf("timezone API failed with status: %s", result.Status)
	}

	return result.TimeZoneID, nil
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}