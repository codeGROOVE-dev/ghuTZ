package ghutz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SimpleDetector is Rob Pike's simplified version - no unnecessary abstractions
type SimpleDetector struct {
	githubToken  string
	mapsAPIKey   string
	geminiAPIKey string
	logger       *slog.Logger
}

// NewSimple creates a simple, reliable detector
func NewSimple(opts ...Option) *SimpleDetector {
	return NewSimpleWithLogger(slog.Default(), opts...)
}

// NewSimpleWithLogger creates a simple detector with a specific logger
func NewSimpleWithLogger(logger *slog.Logger, opts ...Option) *SimpleDetector {
	// Apply options using temporary v1 detector
	v1 := &Detector{}
	for _, opt := range opts {
		opt(v1)
	}
	
	return &SimpleDetector{
		githubToken:  v1.githubToken,
		mapsAPIKey:   v1.mapsAPIKey,  
		geminiAPIKey: v1.geminiAPIKey,
		logger:       logger,
	}
}

// Detect finds timezone using the simplest reliable methods
func (d *SimpleDetector) Detect(ctx context.Context, username string) (*Result, error) {
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}
	
	d.logger.Info("detecting timezone", "username", username)
	
	// Method 1: Try GitHub profile scraping (most reliable)
	d.logger.Debug("trying profile HTML scraping", "username", username)
	if result := d.tryProfileScraping(ctx, username); result != nil {
		d.logger.Info("detected from profile HTML", "username", username, "timezone", result.Timezone)
		return result, nil
	}
	d.logger.Debug("profile HTML scraping failed", "username", username)
	
	// Method 2: Try GitHub profile location field
	d.logger.Debug("trying location field analysis", "username", username)
	if result := d.tryLocationField(ctx, username); result != nil {
		d.logger.Info("detected from location field", "username", username, "timezone", result.Timezone, "location", result.LocationName)
		return result, nil
	}
	d.logger.Debug("location field analysis failed", "username", username)
	
	// Method 3: Try activity patterns + Gemini analysis in one call
	d.logger.Debug("trying activity pattern analysis", "username", username)
	activityResult := d.tryActivityPatterns(ctx, username)
	
	// Method 4: Single Gemini call with or without activity data
	d.logger.Debug("trying Gemini analysis with contextual data", "username", username, "has_activity_data", activityResult != nil)
	if result := d.tryUnifiedGeminiAnalysis(ctx, username, activityResult); result != nil {
		if activityResult != nil {
			d.logger.Info("timezone detected with Gemini + activity", "username", username, 
				"activity_timezone", activityResult.Timezone, "final_timezone", result.Timezone)
		} else {
			d.logger.Info("timezone detected with Gemini only", "username", username, "timezone", result.Timezone)
		}
		return result, nil
	}
	d.logger.Debug("Gemini analysis failed", "username", username)
	
	// Fallback to activity-only if available but Gemini failed
	if activityResult != nil {
		d.logger.Info("using activity-only result as fallback", "username", username, "timezone", activityResult.Timezone)
		return activityResult, nil
	}
	
	return nil, fmt.Errorf("could not determine timezone for %s", username)
}

// tryProfileScraping attempts to extract timezone from GitHub profile HTML
func (d *SimpleDetector) tryProfileScraping(ctx context.Context, username string) *Result {
	url := fmt.Sprintf("https://github.com/%s", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	
	if d.githubToken != "" {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	
	// Look for timezone in profile HTML
	html := string(body)
	if tz := extractTimezoneFromHTML(html); tz != "" {
		return &Result{
			Username:   username,
			Timezone:   tz,
			Confidence: 0.95,
			Method:     "github_profile",
		}
	}
	
	return nil
}

// extractTimezoneFromHTML extracts timezone from GitHub profile HTML
func extractTimezoneFromHTML(html string) string {
	// Look for timezone in various HTML patterns
	patterns := []string{
		`data-timezone="([^"]+)"`,
		`timezone:([^,}]+)`,
		`"timezone":"([^"]+)"`,
	}
	
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(html); len(matches) > 1 {
			tz := strings.TrimSpace(matches[1])
			if tz != "" && tz != "UTC" {
				return tz
			}
		}
	}
	
	return ""
}

// tryLocationField attempts to detect timezone from GitHub profile location field using APIs
func (d *SimpleDetector) tryLocationField(ctx context.Context, username string) *Result {
	user := d.fetchUser(ctx, username)
	if user == nil || user.Location == "" {
		d.logger.Debug("no location field found", "username", username)
		return nil
	}
	
	d.logger.Debug("analyzing location field", "username", username, "location", user.Location)
	
	// Check if location is too vague for reliable geocoding
	if d.isLocationTooVague(user.Location) {
		d.logger.Debug("location too vague for geocoding", "username", username, "location", user.Location)
		return nil
	}
	
	// Use geocoding to convert location string to coordinates
	coords, err := d.geocodeLocation(ctx, user.Location)
	if err != nil {
		d.logger.Debug("geocoding failed", "username", username, "location", user.Location, "error", err)
		return nil
	}
	
	d.logger.Debug("geocoded location", "username", username, "location", user.Location, 
		"latitude", coords.Latitude, "longitude", coords.Longitude)
	
	// Convert coordinates to timezone
	timezone, err := d.timezoneForCoordinates(ctx, coords.Latitude, coords.Longitude)
	if err != nil {
		d.logger.Debug("timezone lookup failed", "username", username, "coordinates", 
			fmt.Sprintf("%.4f,%.4f", coords.Latitude, coords.Longitude), "error", err)
		return nil
	}
	
	d.logger.Debug("determined timezone from coordinates", "username", username, 
		"location", user.Location, "timezone", timezone)
	
	return &Result{
		Username:     username,
		Timezone:     timezone,
		Location:     coords,
		LocationName: user.Location,
		Confidence:   0.8, // Higher confidence from API-based detection
		Method:       "location_geocoding",
	}
}

// tryGeminiAnalysis uses Gemini to analyze contextual clues for timezone detection
func (d *SimpleDetector) tryGeminiAnalysis(ctx context.Context, username string) *Result {
	if d.geminiAPIKey == "" {
		d.logger.Debug("Gemini API key not configured", "username", username)
		return nil
	}
	
	// Gather contextual data about the user
	user := d.fetchUser(ctx, username)
	if user == nil {
		d.logger.Debug("could not fetch user data for Gemini analysis", "username", username)
		return nil
	}
	
	// Get recent PRs for additional context (up to 100)
	prs, _ := d.fetchPullRequests(ctx, username)
	
	// Extract only title and time for Gemini (reduce token usage)
	prSummary := make([]map[string]interface{}, len(prs))
	for i, pr := range prs {
		prSummary[i] = map[string]interface{}{
			"title":      pr.Title,
			"created_at": pr.CreatedAt,
		}
	}
	
	// Try to fetch blog/website content for additional context
	var websiteContent string
	if user.Blog != "" {
		d.logger.Debug("fetching website content for additional context", "username", username, "blog_url", user.Blog)
		websiteContent = d.fetchWebsiteContent(ctx, user.Blog)
	}
	
	// Build context for Gemini - pass complete data
	contextData := map[string]interface{}{
		"github_user_json": user,        // Complete GitHub user JSON
		"pull_requests": prSummary,      // PR titles and timestamps only
		"website_content": websiteContent, // Blog/website content for location clues
	}
	
	d.logger.Debug("analyzing with Gemini", "username", username, "profile_location", user.Location, "company", user.Company, "pr_count", len(prs))
	d.logger.Debug("Gemini context summary", "profile_fields", map[string]string{
		"location": user.Location,
		"company": user.Company, 
		"blog": user.Blog,
		"email": user.Email,
		"bio": user.Bio,
	}, "pr_titles_sample", func() []string {
		sample := make([]string, 0, 5)
		for i, pr := range prSummary {
			if i >= 5 { break }
			if title, ok := pr["title"].(string); ok {
				sample = append(sample, title)
			}
		}
		return sample
	}())
	
	timezone, location, confidence, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, true) // verbose logging enabled
	if err != nil {
		d.logger.Debug("Gemini analysis failed", "username", username, "error", err)
		return nil
	}
	
	if timezone == "" {
		d.logger.Debug("Gemini could not determine timezone", "username", username)
		return nil
	}
	
	return &Result{
		Username:                username,
		Timezone:                timezone,
		GeminiSuggestedLocation: location,
		Confidence:              confidence,
		Method:                  "gemini_analysis",
	}
}

// tryUnifiedGeminiAnalysis uses Gemini with all available data (activity + context) in a single call
func (d *SimpleDetector) tryUnifiedGeminiAnalysis(ctx context.Context, username string, activityResult *Result) *Result {
	if d.geminiAPIKey == "" {
		d.logger.Debug("Gemini API key not configured", "username", username)
		return nil
	}
	
	// Gather contextual data about the user
	user := d.fetchUser(ctx, username)
	if user == nil {
		d.logger.Debug("could not fetch user data for Gemini analysis", "username", username)
		return nil
	}
	
	// Get PRs for additional context
	prs, _ := d.fetchPullRequests(ctx, username)
	
	// Extract only title and time for Gemini (reduce token usage)
	prSummary := make([]map[string]interface{}, len(prs))
	for i, pr := range prs {
		prSummary[i] = map[string]interface{}{
			"title":      pr.Title,
			"created_at": pr.CreatedAt,
		}
	}
	
	// Try to fetch blog/website content for additional context
	var websiteContent string
	if user.Blog != "" {
		d.logger.Debug("fetching website content for Gemini analysis", "username", username, "blog_url", user.Blog)
		websiteContent = d.fetchWebsiteContent(ctx, user.Blog)
	}
	
	// Fetch GitHub organization data for additional context
	orgs, _ := d.fetchOrganizations(ctx, username)
	d.logger.Debug("fetched organization data", "username", username, "org_count", len(orgs))
	
	// Build context for Gemini - include activity data if available
	contextData := map[string]interface{}{
		"github_user_json": user,
		"pull_requests":    prSummary,
		"website_content":  websiteContent,
		"organizations":    orgs,
	}
	
	// Add activity data if available
	var method string
	if activityResult != nil {
		contextData["activity_detected_timezone"] = activityResult.Timezone
		contextData["activity_confidence"] = activityResult.Confidence
		method = "gemini_refined_activity"
		d.logger.Debug("analyzing with Gemini + activity data", "username", username, 
			"activity_timezone", activityResult.Timezone, "profile_location", user.Location, 
			"company", user.Company, "website_available", websiteContent != "")
	} else {
		method = "gemini_analysis" 
		d.logger.Debug("analyzing with Gemini only", "username", username, 
			"profile_location", user.Location, "company", user.Company, "website_available", websiteContent != "")
	}
	
	timezone, location, confidence, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, true) // verbose logging enabled
	if err != nil {
		d.logger.Debug("Gemini analysis failed", "username", username, "error", err)
		return nil
	}
	
	if timezone == "" {
		d.logger.Debug("Gemini could not determine timezone", "username", username)
		return nil
	}
	
	return &Result{
		Username:                username,
		Timezone:                timezone,
		GeminiSuggestedLocation: location,
		Confidence:              confidence,
		Method:                  method,
	}
}

// tryActivityPatterns analyzes GitHub activity to infer timezone
func (d *SimpleDetector) tryActivityPatterns(ctx context.Context, username string) *Result {
	prs, err := d.fetchPullRequests(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch pull requests", "username", username, "error", err)
		return nil
	}
	
	if len(prs) < 10 {
		d.logger.Debug("insufficient activity data", "username", username, "pr_count", len(prs), "minimum_required", 10)
		return nil
	}
	
	d.logger.Debug("analyzing activity patterns", "username", username, "pr_count", len(prs))
	
	// Count activity by hour (UTC)
	hourCounts := make(map[int]int)
	for _, pr := range prs {
		hour := pr.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}
	
	// Find most active hours for better logging
	maxActivity := 0
	mostActiveHours := []int{}
	for hour, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
			mostActiveHours = []int{hour}
		} else if count == maxActivity {
			mostActiveHours = append(mostActiveHours, hour)
		}
	}
	
	// Find quiet hours (likely sleeping time)
	quietHours := findQuietHours(hourCounts)
	if len(quietHours) < 4 {
		d.logger.Debug("insufficient quiet hours", "username", username, "quiet_hours", len(quietHours))
		return nil
	}
	
	d.logger.Debug("activity pattern summary", "username", username, 
		"total_prs", len(prs),
		"quiet_hours", quietHours, 
		"most_active_hours", mostActiveHours,
		"max_activity_count", maxActivity)
	
	// Log hourly distribution for debugging
	hourlyActivity := make([]int, 24)
	for hour := 0; hour < 24; hour++ {
		hourlyActivity[hour] = hourCounts[hour]
	}
	d.logger.Debug("hourly activity distribution", "username", username, "hours_utc", hourlyActivity)
	
	// Estimate timezone based on quiet hours  
	// Assume sleep is 2am-7am local time (4:30am is middle)
	midSleep := float64(quietHours[0] + quietHours[len(quietHours)-1]) / 2.0
	
	// If midSleep is X UTC and we assume that's 4:30am local time,
	// then local_time = UTC + offset, so: 4.5 = X + offset, therefore offset = 4.5 - X
	offsetFromUTC := int(4.5 - midSleep)
	
	d.logger.Debug("calculated timezone offset", "username", username, 
		"mid_sleep_utc", midSleep, "estimated_offset", offsetFromUTC)
	
	if offsetFromUTC > 12 {
		offsetFromUTC -= 24
	} else if offsetFromUTC < -12 {
		offsetFromUTC += 24
	}
	
	timezone := timezoneFromOffset(offsetFromUTC)
	d.logger.Debug("DST-aware timezone selected", "username", username, "offset", offsetFromUTC, "timezone", timezone)
	
	// Log what this means in terms of local time
	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err == nil {
			now := time.Now()
			localTime := now.In(loc)
			utcTime := now.UTC()
			d.logger.Debug("timezone verification", "username", username, "timezone", timezone,
				"utc_time", utcTime.Format("15:04 MST"), 
				"local_time", localTime.Format("15:04 MST"),
				"current_offset_hours", float64(localTime.Hour() - utcTime.Hour()))
		}
	}
	
	// Let Gemini handle name-based improvements via context analysis
	
	return &Result{
		Username:   username,
		Timezone:   timezone,
		Confidence: 0.8,
		Method:     "activity_patterns",
	}
}

// fetchPullRequests gets recent pull requests for activity analysis
func (d *SimpleDetector) fetchPullRequests(ctx context.Context, username string) ([]PullRequest, error) {
	url := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr&sort=created&order=desc&per_page=100", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	if d.githubToken != "" {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	// Debug: log response status
	d.logger.Debug("GitHub PR search response", "username", username, "status", resp.StatusCode, "url", url)
	
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		d.logger.Debug("GitHub API error response", "username", username, "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	
	var result struct {
		TotalCount int `json:"total_count"`
		Items []struct {
			Title     string    `json:"title"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"items"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	d.logger.Debug("GitHub PR search results", "username", username, "total_count", result.TotalCount, "returned_items", len(result.Items))
	
	var prs []PullRequest
	for _, item := range result.Items {
		prs = append(prs, PullRequest{
			Title:     item.Title,
			CreatedAt: item.CreatedAt,
		})
	}
	
	return prs, nil
}

// fetchOrganizations gets GitHub organizations for a user
func (d *SimpleDetector) fetchOrganizations(ctx context.Context, username string) ([]Organization, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/orgs", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	if d.githubToken != "" {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var orgs []Organization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}
	
	return orgs, nil
}

// fetchUser gets basic GitHub user info
func (d *SimpleDetector) fetchUser(ctx context.Context, username string) *GitHubUser {
	url := fmt.Sprintf("https://api.github.com/users/%s", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	
	if d.githubToken != "" {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	
	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil
	}
	
	return &user
}

// geocodeLocation converts a location string to coordinates using Google Maps API
func (d *SimpleDetector) geocodeLocation(ctx context.Context, location string) (*Location, error) {
	if d.mapsAPIKey == "" {
		return nil, fmt.Errorf("Google Maps API key not configured")
	}
	
	// Properly URL encode location
	encodedLocation := url.QueryEscape(location)
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s", 
		encodedLocation, d.mapsAPIKey)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result struct {
		Results []struct {
			Geometry struct {
				Location struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"location"`
				LocationType string `json:"location_type"`
			} `json:"geometry"`
			Types           []string `json:"types"`
			FormattedAddress string  `json:"formatted_address"`
		} `json:"results"`
		Status string `json:"status"`
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	// Debug log the raw response for troubleshooting
	d.logger.Debug("geocoding API raw response", "location", location, "status", resp.StatusCode, 
		"content_type", resp.Header.Get("Content-Type"), "body_preview", string(body[:min(200, len(body))]))
	
	if err := json.Unmarshal(body, &result); err != nil {
		d.logger.Debug("geocoding JSON parse error", "location", location, "error", err, "full_body", string(body))
		return nil, fmt.Errorf("failed to parse geocoding response: %v", err)
	}
	
	if result.Status != "OK" || len(result.Results) == 0 {
		return nil, fmt.Errorf("geocoding failed: %s", result.Status)
	}
	
	firstResult := result.Results[0]
	
	// Check location precision - reject imprecise results that might lead to wrong timezones
	locationType := firstResult.Geometry.LocationType
	d.logger.Debug("geocoding result precision", "location", location, 
		"location_type", locationType, "types", firstResult.Types, 
		"formatted_address", firstResult.FormattedAddress)
	
	// APPROXIMATE results are often geographic centers of large areas and unreliable for timezone detection
	if locationType == "APPROXIMATE" {
		// Check if it's a country-level or very broad result
		hasCountryType := false
		hasPreciseType := false
		for _, t := range firstResult.Types {
			if t == "country" || t == "administrative_area_level_1" {
				hasCountryType = true
			}
			if t == "locality" || t == "sublocality" || t == "neighborhood" || t == "street_address" {
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

// timezoneForCoordinates gets timezone for coordinates using Google Maps Timezone API  
func (d *SimpleDetector) timezoneForCoordinates(ctx context.Context, lat, lng float64) (string, error) {
	if d.mapsAPIKey == "" {
		return "", fmt.Errorf("Google Maps API key not configured")
	}
	
	// Use current timestamp for timezone lookup
	timestamp := time.Now().Unix()
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/timezone/json?location=%.6f,%.6f&timestamp=%d&key=%s",
		lat, lng, timestamp, d.mapsAPIKey)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	var result struct {
		TimeZoneID string `json:"timeZoneId"`
		Status     string `json:"status"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	
	if result.Status != "OK" {
		return "", fmt.Errorf("timezone lookup failed: %s", result.Status)
	}
	
	return result.TimeZoneID, nil
}

// isLocationTooVague checks if a location string is too vague for reliable geocoding
func (d *SimpleDetector) isLocationTooVague(location string) bool {
	location = strings.ToLower(strings.TrimSpace(location))
	
	// Countries without specific cities are too vague
	vagueLocations := []string{
		"united states", "usa", "us", "america",
		"canada", "uk", "united kingdom", "britain",
		"germany", "france", "italy", "spain",
		"australia", "japan", "china", "india",
		"brazil", "russia", "mexico",
		"earth", "world", "planet earth",
	}
	
	for _, vague := range vagueLocations {
		if location == vague {
			return true
		}
	}
	
	return false
}

// findQuietHours finds consecutive hours with least activity
func findQuietHours(hourCounts map[int]int) []int {
	minSum := 999999
	minStart := 0
	windowSize := 6
	
	for start := 0; start < 24; start++ {
		sum := 0
		for i := 0; i < windowSize; i++ {
			hour := (start + i) % 24
			sum += hourCounts[hour]
		}
		if sum < minSum {
			minSum = sum
			minStart = start
		}
	}
	
	quietHours := make([]int, windowSize)
	for i := 0; i < windowSize; i++ {
		quietHours[i] = (minStart + i) % 24
	}
	
	return quietHours
}

// timezoneFromOffset converts UTC offset to timezone name with DST awareness
func timezoneFromOffset(offsetHours int) string {
	now := time.Now()
	
	// Get candidate timezones for this offset
	candidates := getTimezoneCandidatesForOffset(float64(offsetHours))
	
	// Find the first candidate that currently matches the offset
	for _, tzName := range candidates {
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			continue
		}
		
		_, currentOffset := now.In(loc).Zone()
		currentOffsetHours := float64(currentOffset) / 3600.0
		
		if currentOffsetHours == float64(offsetHours) {
			return tzName
		}
	}
	
	// Fallback to simple mapping
	switch offsetHours {
	case -8:
		return "America/Los_Angeles"
	case -7:
		return "America/Denver"
	case -6:
		return "America/Chicago"
	case -5:
		return "America/New_York"
	case 0:
		return "Europe/London"
	case 1:
		return "Europe/Paris"
	case 2:
		return "Europe/Berlin"
	default:
		if offsetHours < 0 {
			return fmt.Sprintf("Etc/GMT+%d", -offsetHours)
		}
		return fmt.Sprintf("Etc/GMT-%d", offsetHours)
	}
}

// getTimezoneCandidatesForOffset returns candidate timezones for a UTC offset
func getTimezoneCandidatesForOffset(offsetHours float64) []string {
	switch offsetHours {
	case -9:
		return []string{"America/Anchorage", "America/Los_Angeles"} // Alaska or Pacific with unusual pattern
	case -8:
		return []string{"America/Los_Angeles", "America/Vancouver", "America/Tijuana"}
	case -7:
		// During DST (Mar-Nov), Los Angeles is UTC-7; Phoenix is always UTC-7
		// During standard time, Denver is UTC-7
		return []string{"America/Los_Angeles", "America/Phoenix", "America/Denver"}
	case -6:
		// Prioritize Mountain Time (Denver) for UTC-6 during DST season
		return []string{"America/Denver", "America/Chicago", "America/Mexico_City"}
	case -5:
		// Prioritize Central Time (Chicago) for UTC-5 during DST season
		return []string{"America/Chicago", "America/New_York", "America/Bogota"}
	case -4:
		return []string{"America/New_York", "America/Halifax", "America/Caracas"}
	case 0:
		return []string{"Europe/London", "Europe/Lisbon", "Africa/Casablanca"}
	case 1:
		return []string{"Europe/Paris", "Europe/Berlin", "Europe/Amsterdam", "Europe/Warsaw"}
	case 2:
		return []string{"Europe/Berlin", "Europe/Warsaw", "Europe/Paris", "Europe/Rome"}
	default:
		return []string{}
	}
}

// queryUnifiedGeminiForTimezone asks Gemini to analyze all available data for timezone detection
func (d *SimpleDetector) queryUnifiedGeminiForTimezone(ctx context.Context, contextData map[string]interface{}, verbose bool) (string, string, float64, error) {
	// Check if we have activity data to determine prompt type
	activityTimezone := ""
	hasActivityData := false
	if tz, ok := contextData["activity_detected_timezone"].(string); ok && tz != "" {
		activityTimezone = tz
		hasActivityData = true
	}
	
	var prompt string
	if hasActivityData {
		// Activity refinement prompt
		if verbose {
			prompt = `You are a timezone detection expert. I have detected a timezone based on GitHub activity patterns, but I want you to validate or refine this detection using additional contextual clues.

ACTIVITY-BASED DETECTION: The user's GitHub pull request timing patterns suggest they are in timezone: %s

Your task is to either:
1. CONFIRM the activity-based timezone if the contextual evidence supports it
2. REFINE it to a more accurate timezone in the same general region if you have strong evidence
3. Return "UNKNOWN" if the contextual evidence strongly contradicts the activity patterns

Consider these additional clues to validate/refine:
- Complete GitHub profile JSON (location, company, blog, bio, email, etc.)
- GitHub organization memberships and their locations/descriptions
- Pull request titles (may contain location/conference references)
- Website/blog content (may contain explicit location info, language, regional context)
- Username patterns that might indicate nationality/region (e.g., "wojciechka" suggests Polish origin)
- Company location vs user location (remote work is common)

Important guidelines:
- TRUST the activity patterns - they are based on actual behavior
- Only change the timezone if you have STRONG evidence (explicit location mentions)
- CRITICAL: Remote work is extremely common in tech - DO NOT assume someone lives where their company is based
- If working for US company but no explicit current location, DO NOT assume US location
- Birth country/nationality often indicates current location unless explicitly stated otherwise
- Prefer timezones in the same UTC offset or neighboring regions  
- Consider that the user might work remotely for companies in different countries
- When username suggests a specific nationality/region that uses the SAME timezone as detected, feel confident suggesting that location and potentially refining to the country-specific timezone
- For example: if activity patterns suggest Europe/Berlin and username suggests Polish origin, Europe/Warsaw (Poland) is more accurate since both are UTC+1/+2

Respond with your reasoning followed by your conclusion on separate lines:
"TIMEZONE:" followed by ONLY a valid IANA timezone identifier or "UNKNOWN"
"LOCATION:" followed by a specific location name (city, region) or "UNKNOWN"

Context data: %s`
		} else {
			prompt = `You are a timezone detection expert. I have detected a timezone based on GitHub activity patterns, but I want you to validate or refine this detection using additional contextual clues.

ACTIVITY-BASED DETECTION: The user's GitHub pull request timing patterns suggest they are in timezone: %s

Your task is to either:
1. CONFIRM the activity-based timezone if the contextual evidence supports it
2. REFINE it to a more accurate timezone in the same general region if you have strong evidence
3. Return "UNKNOWN" if the contextual evidence strongly contradicts the activity patterns

Consider these additional clues to validate/refine:
- Complete GitHub profile JSON (location, company, blog, bio, email, etc.)
- GitHub organization memberships and their locations/descriptions
- Pull request titles (may contain location/conference references)
- Website/blog content (may contain explicit location info, language, regional context)
- Username patterns that might indicate nationality/region (e.g., "wojciechka" suggests Polish origin)
- Company location vs user location (remote work is common)

Important guidelines:
- TRUST the activity patterns - they are based on actual behavior
- Only change the timezone if you have STRONG evidence (explicit location mentions)
- CRITICAL: Remote work is extremely common in tech - DO NOT assume someone lives where their company is based
- If working for US company but no explicit current location, DO NOT assume US location
- Birth country/nationality often indicates current location unless explicitly stated otherwise
- Prefer timezones in the same UTC offset or neighboring regions
- Consider that the user might work remotely for companies in different countries
- When username suggests a specific nationality/region that uses the SAME timezone as detected, feel confident suggesting that location and potentially refining to the country-specific timezone
- For example: if activity patterns suggest Europe/Berlin and username suggests Polish origin, Europe/Warsaw (Poland) is more accurate since both are UTC+1/+2

Respond with two lines:
Line 1: TIMEZONE: followed by ONLY a valid IANA timezone identifier or "UNKNOWN"
Line 2: LOCATION: followed by a specific location name (city, region) or "UNKNOWN"

Context data: %s`
		}
	} else {
		// Pure contextual analysis prompt
		if verbose {
			prompt = `You are a timezone detection expert. Based on the complete GitHub user data and pull request history provided, determine the most likely timezone and specific location for this user.

Analyze all available clues including:
- Complete GitHub profile JSON (location, company, blog, bio, email, created_at, etc.)
- GitHub organization memberships and their locations/descriptions  
- Pull request titles and creation timestamps (up to 100 PRs)  
- Website/blog content (may contain location references, language, or regional context)
- Username patterns that might indicate nationality/region
- Company names that might indicate location
- Any contextual information from profile data, PR titles, organizations, or website content
- Timing patterns in PR creation that might indicate working hours

The location field may contain:
- Real city/country names
- Joke locations (like "Anarchist Jurisdiction") 
- Company locations
- Vague references

Look beyond the obvious location field and use all contextual clues to make an informed decision. Be specific about location when you have strong evidence (e.g., "San Francisco Bay Area" instead of just "California").

Username patterns can be valuable clues:
- Names like "wojciechka" suggest Polish origin → consider Poland/Europe/Warsaw
- Names like "giuseppe" suggest Italian origin → consider Italy/Europe/Rome  
- Trust your instincts about nationality/region based on usernames when they align with detected timezone

Important guidelines:
- CRITICAL: Remote work is extremely common in tech - DO NOT assume someone lives where their company is based
- Birth country/nationality often indicates current location unless explicitly stated otherwise  
- If someone was born in Spain and works for a US company, they likely still live in Spain
- Only assume relocation if there's explicit evidence of recent moves or current location mentions
- Company employment alone is NOT evidence of physical location

Respond with your reasoning followed by your conclusion on separate lines:
"TIMEZONE:" followed by ONLY a valid IANA timezone identifier or "UNKNOWN"
"LOCATION:" followed by a specific location name (city, region) or "UNKNOWN"

User data: %s`
		} else {
			prompt = `You are a timezone detection expert. Based on the complete GitHub user data and pull request history provided, determine the most likely timezone and specific location for this user.

Analyze all available clues including:
- Complete GitHub profile JSON (location, company, blog, bio, email, created_at, etc.)
- GitHub organization memberships and their locations/descriptions  
- Pull request titles and creation timestamps (up to 100 PRs)  
- Website/blog content (may contain location references, language, or regional context)
- Username patterns that might indicate nationality/region
- Company names that might indicate location
- Any contextual information from profile data, PR titles, organizations, or website content
- Timing patterns in PR creation that might indicate working hours

The location field may contain:
- Real city/country names
- Joke locations (like "Anarchist Jurisdiction") 
- Company locations
- Vague references

Look beyond the obvious location field and use all contextual clues to make an informed decision. Be specific about location when you have strong evidence (e.g., "San Francisco Bay Area" instead of just "California").

Username patterns can be valuable clues:
- Names like "wojciechka" suggest Polish origin → consider Poland/Europe/Warsaw
- Names like "giuseppe" suggest Italian origin → consider Italy/Europe/Rome  
- Trust your instincts about nationality/region based on usernames when they align with detected timezone

Important guidelines:
- CRITICAL: Remote work is extremely common in tech - DO NOT assume someone lives where their company is based
- Birth country/nationality often indicates current location unless explicitly stated otherwise
- If someone was born in Spain and works for a US company, they likely still live in Spain
- Only assume relocation if there's explicit evidence of recent moves or current location mentions
- Company employment alone is NOT evidence of physical location

Respond with two lines:
Line 1: TIMEZONE: followed by ONLY a valid IANA timezone identifier or "UNKNOWN"  
Line 2: LOCATION: followed by a specific location name (city, region) or "UNKNOWN"

User data: %s`
		}
	}

	contextJSON, err := json.Marshal(contextData)
	if err != nil {
		return "", "", 0, err
	}
	
	var fullPrompt string
	if hasActivityData {
		fullPrompt = fmt.Sprintf(prompt, activityTimezone, string(contextJSON))
		d.logger.Debug("Gemini unified prompt (with activity)", "prompt_length", len(fullPrompt), 
			"context_size_bytes", len(contextJSON), "activity_timezone", activityTimezone)
	} else {
		fullPrompt = fmt.Sprintf(prompt, string(contextJSON))
		d.logger.Debug("Gemini unified prompt (context only)", "prompt_length", len(fullPrompt), 
			"context_size_bytes", len(contextJSON))
	}
	
	// Log full prompt in verbose mode
	if verbose {
		d.logger.Debug("Gemini full prompt", "full_prompt", fullPrompt)
	}
	
	// Call Gemini API
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": fullPrompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.1, // Low temperature for consistent results
			"maxOutputTokens": func() int {
				if verbose {
					return 300 // Allow more tokens for reasoning in verbose mode
				}
				return 100
			}(),
		},
	}
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", 0, err
	}
	
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash-exp:generateContent?key=%s", d.geminiAPIKey)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", "", 0, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", "", 0, fmt.Errorf("Gemini API error: %d %s", resp.StatusCode, string(body))
	}
	
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", "", 0, err
	}
	
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", "", 0, fmt.Errorf("no response from Gemini")
	}
	
	fullResponse := strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text)
	
	// Log the full response for debugging (contains reasoning in verbose mode)
	d.logger.Debug("Gemini full response", "full_response", fullResponse)
	
	// Parse response - extract timezone and location
	var timezone, location string
	if strings.Contains(fullResponse, "TIMEZONE:") {
		// Extract both timezone and location from structured response
		lines := strings.Split(fullResponse, "\n")
		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if strings.HasPrefix(trimmedLine, "TIMEZONE:") {
				timezone = strings.TrimSpace(strings.TrimPrefix(trimmedLine, "TIMEZONE:"))
			} else if strings.HasPrefix(trimmedLine, "LOCATION:") {
				location = strings.TrimSpace(strings.TrimPrefix(trimmedLine, "LOCATION:"))
			}
		}
	} else {
		// Legacy format - just timezone
		timezone = fullResponse
	}
	
	d.logger.Debug("Gemini unified response", "extracted_timezone", timezone, "extracted_location", location, "had_activity_data", hasActivityData, "verbose_mode", verbose)
	if hasActivityData {
		d.logger.Debug("Gemini activity comparison", "original_activity_timezone", activityTimezone, "gemini_response", timezone, "gemini_location", location)
	}
	
	// Log the full context sent to Gemini for debugging
	d.logger.Debug("Gemini unified context", "context_json", string(contextJSON))
	
	if timezone == "UNKNOWN" || timezone == "" {
		return "", "", 0, nil
	}
	
	// Clean up location if it's "UNKNOWN" 
	if location == "UNKNOWN" {
		location = ""
	}
	
	// Validate timezone format
	if !strings.Contains(timezone, "/") {
		d.logger.Debug("invalid timezone format from unified Gemini", "timezone", timezone)
		return "", "", 0, nil
	}
	
	// Test if timezone is valid
	_, err = time.LoadLocation(timezone)
	if err != nil {
		d.logger.Debug("invalid timezone from unified Gemini", "timezone", timezone, "error", err)
		return "", "", 0, nil
	}
	
	// Determine confidence based on whether we had activity data
	var confidence float64
	if hasActivityData {
		if timezone == activityTimezone {
			d.logger.Debug("Gemini confirmed activity-based timezone", "timezone", timezone)
			confidence = 0.9 // High confidence when activity + context agree
		} else {
			d.logger.Debug("Gemini refined activity-based timezone", "original", activityTimezone, "refined", timezone)
			confidence = 0.85 // Good confidence for refinement
		}
	} else {
		confidence = 0.75 // Medium confidence for pure contextual analysis
	}
	
	return timezone, location, confidence, nil
}


// fetchWebsiteContent fetches and extracts relevant content from a user's blog/website
func (d *SimpleDetector) fetchWebsiteContent(ctx context.Context, blogURL string) string {
	if blogURL == "" {
		return ""
	}
	
	// Clean up URL
	if !strings.HasPrefix(blogURL, "http://") && !strings.HasPrefix(blogURL, "https://") {
		blogURL = "https://" + blogURL
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", blogURL, nil)
	if err != nil {
		d.logger.Debug("failed to create website request", "url", blogURL, "error", err)
		return ""
	}
	
	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		d.logger.Debug("failed to fetch website", "url", blogURL, "error", err)
		return ""
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		d.logger.Debug("website returned non-200 status", "url", blogURL, "status", resp.StatusCode)
		return ""
	}
	
	// Read content (limit to 50KB to avoid excessive token usage)
	body := make([]byte, 50*1024)
	n, _ := io.ReadFull(resp.Body, body)
	content := string(body[:n])
	
	// Basic HTML tag removal for cleaner text analysis
	content = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(content, " ")
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	content = strings.TrimSpace(content)
	
	// Limit content length for token efficiency (keep first 2000 chars)
	if len(content) > 2000 {
		content = content[:2000] + "..."
	}
	
	d.logger.Debug("fetched website content", "url", blogURL, "content_length", len(content))
	return content
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Let Gemini handle name-based detection - no hardcoded rules needed