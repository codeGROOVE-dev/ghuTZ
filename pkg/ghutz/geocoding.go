package ghutz

import (
	"context"
	"net/http"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/googlemaps"
)

// geocodeLocation converts a location string to coordinates using Google Geocoding API.
func (d *Detector) geocodeLocation(ctx context.Context, location string) (*Location, error) {
	// Create a custom HTTP client that uses our caching mechanism
	cachedClient := &httpClientWrapper{
		detector: d,
		ctx:      ctx,
	}
	client := googlemaps.NewClient(d.mapsAPIKey, cachedClient, d.logger)
	geoLoc, err := client.GeocodeLocation(ctx, location)
	if err != nil {
		return nil, err
	}
	return &Location{
		Latitude:  geoLoc.Latitude,
		Longitude: geoLoc.Longitude,
	}, nil
}

// timezoneForCoordinates gets the timezone for given coordinates using Google Timezone API.
func (d *Detector) timezoneForCoordinates(ctx context.Context, lat, lng float64) (string, error) {
	// Create a custom HTTP client that uses our caching mechanism
	cachedClient := &httpClientWrapper{
		detector: d,
		ctx:      ctx,
	}
	client := googlemaps.NewClient(d.mapsAPIKey, cachedClient, d.logger)
	return client.TimezoneForCoordinates(ctx, lat, lng)
}

// httpClientWrapper wraps the Detector's cachedHTTPDo method to implement http.Client interface
type httpClientWrapper struct {
	detector *Detector
	ctx      context.Context
}

func (h *httpClientWrapper) Do(req *http.Request) (*http.Response, error) {
	return h.detector.cachedHTTPDo(h.ctx, req)
}

