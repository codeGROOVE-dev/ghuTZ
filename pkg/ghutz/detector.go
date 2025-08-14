package ghutz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codeGROOVE-dev/retry"
)

// SECURITY: GitHub token patterns for validation.
var (
	// GitHub Personal Access Token (classic) - ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.
	githubPATRegex = regexp.MustCompile(`^ghp_[a-zA-Z0-9]{36}$`)
	// GitHub App Installation Token - ghs_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.
	githubAppTokenRegex = regexp.MustCompile(`^ghs_[a-zA-Z0-9]{36}$`)
	// GitHub Fine-grained PAT - github_pat_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.
	githubFineGrainedRegex = regexp.MustCompile(`^github_pat_[a-zA-Z0-9_]{82}$`)
	// GitHub username validation regex
	validUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)
	// HTML tag removal regex
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
	// Whitespace normalization regex
	whitespaceRegex = regexp.MustCompile(`\s+`)
	// Timezone extraction patterns
	timezoneDataAttrRegex = regexp.MustCompile(`data-timezone="([^"]+)"`)
	timezoneJSONRegex = regexp.MustCompile(`"timezone":"([^"]+)"`)
	timezoneFieldRegex = regexp.MustCompile(`timezone:([^,}]+)`)
)

type Detector struct {
	githubToken   string
	mapsAPIKey    string
	geminiAPIKey  string
	geminiModel   string
	gcpProject    string
	logger        *slog.Logger
	httpClient    *http.Client
	forceActivity bool
	cache         *OtterCache
}

// retryableHTTPDo performs an HTTP request with exponential backoff and jitter.
// The returned response body must be closed by the caller.
func (d *Detector) retryableHTTPDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var lastErr error
	
	err := retry.Do(
		func() error {
			var err error
			resp, err = d.httpClient.Do(req.WithContext(ctx)) //nolint:bodyclose // Body closed on error, returned open on success for caller
			if err != nil {
				// Network errors are retryable
				lastErr = err
				return err
			}
			
			// Check for rate limiting or server errors
			if resp.StatusCode == 429 || resp.StatusCode == 403 || resp.StatusCode >= 500 {
				body, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				if readErr != nil {
					d.logger.Debug("failed to read error response body", "error", readErr)
				}
				if closeErr != nil {
					d.logger.Debug("failed to close error response body", "error", closeErr)
				}
				lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
				d.logger.Debug("retryable HTTP error", 
					"status", resp.StatusCode, 
					"url", req.URL.String(),
					"body", string(body))
				return lastErr
			}
			
			// Success - response body will be handled by caller
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.FullJitterBackoffDelay),
		retry.OnRetry(func(n uint, err error) {
			d.logger.Debug("retrying HTTP request", 
				"attempt", n+1,
				"url", req.URL.String(),
				"error", err)
		}),
		retry.RetryIf(func(err error) bool {
			// Retry on network errors and rate limits
			return err != nil
		}),
	)
	
	if err != nil {
		return nil, fmt.Errorf("request failed after retries: %w", lastErr)
	}
	
	return resp, nil
}

// isValidGitHubToken validates GitHub token format for security.
func (d *Detector) isValidGitHubToken(token string) bool {
	// SECURITY: Validate token format to prevent injection attacks
	token = strings.TrimSpace(token)
	
	// Check against known GitHub token patterns
	return githubPATRegex.MatchString(token) || 
		   githubAppTokenRegex.MatchString(token) || 
		   githubFineGrainedRegex.MatchString(token)
}

func New(opts ...Option) *Detector {
	return NewWithLogger(slog.Default(), opts...)
}

func NewWithLogger(logger *slog.Logger, opts ...Option) *Detector {
	optHolder := &OptionHolder{}
	for _, opt := range opts {
		opt(optHolder)
	}
	
	// Initialize cache
	var cache *OtterCache
	var cacheDir string
	
	if optHolder.cacheDir != "" {
		// Use custom cache directory
		cacheDir = optHolder.cacheDir
	} else if userCacheDir, err := os.UserCacheDir(); err == nil {
		// Use default user cache directory
		cacheDir = filepath.Join(userCacheDir, "ghutz")
	} else {
		logger.Debug("could not determine user cache directory", "error", err)
	}
	
	if cacheDir != "" {
		var err error
		cache, err = NewOtterCache(cacheDir, 20*24*time.Hour, logger)
		if err != nil {
			logger.Warn("cache initialization failed", "error", err, "cache_dir", cacheDir)
			// Cache is optional, continue without it
			cache = nil
		}
	}
	
	return &Detector{
		githubToken:   optHolder.githubToken,
		mapsAPIKey:    optHolder.mapsAPIKey,  
		geminiAPIKey:  optHolder.geminiAPIKey,
		geminiModel:   optHolder.geminiModel,
		gcpProject:    optHolder.gcpProject,
		logger:        logger,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		forceActivity: optHolder.forceActivity,
		cache:         cache,
	}
}

// Close properly shuts down the detector, including saving the cache to disk
func (d *Detector) Close() error {
	if d.cache != nil {
		return d.cache.Close()
	}
	return nil
}

func (d *Detector) Detect(ctx context.Context, username string) (*Result, error) {
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}
	
	// Validate username to prevent injection attacks
	// GitHub usernames can only contain alphanumeric characters or hyphens
	// Cannot have multiple consecutive hyphens
	// Cannot begin or end with a hyphen
	// Maximum 39 characters
	if len(username) > 39 {
		return nil, fmt.Errorf("username too long (max 39 characters)")
	}
	
	if !validUsernameRegex.MatchString(username) {
		return nil, fmt.Errorf("invalid username format")
	}
	
	d.logger.Info("detecting timezone", "username", username)
	
	// Fetch user profile to get the full name
	var fullName string
	if user := d.fetchUser(ctx, username); user != nil && user.Name != "" {
		fullName = user.Name
		d.logger.Debug("fetched user full name", "username", username, "name", fullName)
	}
	
	// Always perform activity analysis for fun and comparison
	d.logger.Debug("performing activity pattern analysis", "username", username)
	activityResult := d.tryActivityPatterns(ctx, username)
	
	// Try quick detection methods first
	d.logger.Debug("trying profile HTML scraping", "username", username)
	if result := d.tryProfileScraping(ctx, username); result != nil {
		d.logger.Info("detected from profile HTML", "username", username, "timezone", result.Timezone)
		result.Name = fullName
		// Add activity data if we have it
		if activityResult != nil {
			result.ActivityTimezone = activityResult.ActivityTimezone
			result.QuietHoursUTC = activityResult.QuietHoursUTC
			result.ActiveHoursLocal = activityResult.ActiveHoursLocal
			result.LunchHoursLocal = activityResult.LunchHoursLocal
		}
		return result, nil
	}
	d.logger.Debug("profile HTML scraping failed", "username", username)
	
	d.logger.Debug("trying location field analysis", "username", username)
	if result := d.tryLocationField(ctx, username); result != nil {
		d.logger.Info("detected from location field", "username", username, "timezone", result.Timezone, "location", result.LocationName)
		result.Name = fullName
		// Add activity data if we have it
		if activityResult != nil {
			result.ActivityTimezone = activityResult.ActivityTimezone
			result.QuietHoursUTC = activityResult.QuietHoursUTC
			result.ActiveHoursLocal = activityResult.ActiveHoursLocal
			result.LunchHoursLocal = activityResult.LunchHoursLocal
		}
		return result, nil
	}
	d.logger.Debug("location field analysis failed", "username", username)
	
	
	d.logger.Debug("trying Gemini analysis with contextual data", "username", username, "has_activity_data", activityResult != nil)
	if result := d.tryUnifiedGeminiAnalysis(ctx, username, activityResult); result != nil {
		result.Name = fullName
		if activityResult != nil {
			// Preserve activity data in the final result
			result.ActivityTimezone = activityResult.ActivityTimezone
			result.QuietHoursUTC = activityResult.QuietHoursUTC
			result.ActiveHoursLocal = activityResult.ActiveHoursLocal
			result.LunchHoursLocal = activityResult.LunchHoursLocal
			d.logger.Info("timezone detected with Gemini + activity", "username", username, 
				"activity_timezone", activityResult.Timezone, "final_timezone", result.Timezone)
		} else {
			d.logger.Info("timezone detected with Gemini only", "username", username, "timezone", result.Timezone)
		}
		return result, nil
	}
	d.logger.Debug("Gemini analysis failed", "username", username)
	
	if activityResult != nil {
		d.logger.Info("using activity-only result as fallback", "username", username, "timezone", activityResult.Timezone)
		activityResult.Name = fullName
		return activityResult, nil
	}
	
	return nil, fmt.Errorf("could not determine timezone for %s", username)
}

func (d *Detector) tryProfileScraping(ctx context.Context, username string) *Result {
	url := fmt.Sprintf("https://github.com/%s", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil
	}
	
	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	// Check if user exists - GitHub returns 404 for non-existent users
	if resp.StatusCode == http.StatusNotFound {
		d.logger.Info("GitHub user not found", "username", username)
		// Return a special result indicating user doesn't exist
		// This will be cached to avoid repeated lookups
		return &Result{
			Username:   username,
			Timezone:   "UTC",  // Default timezone for non-existent users
			Confidence: 0,      // Zero confidence indicates non-existent user
			Method:     "user_not_found",
		}
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	
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

func extractTimezoneFromHTML(html string) string {
	// Try each pre-compiled regex pattern
	patterns := []*regexp.Regexp{
		timezoneDataAttrRegex,
		timezoneFieldRegex,
		timezoneJSONRegex,
	}
	
	for _, re := range patterns {
		if matches := re.FindStringSubmatch(html); len(matches) > 1 {
			tz := strings.TrimSpace(matches[1])
			if tz != "" && tz != "UTC" {
				return tz
			}
		}
	}
	
	return ""
}

func (d *Detector) tryLocationField(ctx context.Context, username string) *Result {
	user := d.fetchUser(ctx, username)
	if user == nil || user.Location == "" {
		d.logger.Debug("no location field found", "username", username)
		return nil
	}
	
	d.logger.Debug("analyzing location field", "username", username, "location", user.Location)
	
	// Check if location is too vague for geocoding
	location := strings.ToLower(strings.TrimSpace(user.Location))
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
			d.logger.Debug("location too vague for geocoding", "username", username, "location", user.Location)
			return nil
		}
	}
	
	coords, err := d.geocodeLocation(ctx, user.Location)
	if err != nil {
		d.logger.Warn("geocoding failed - continuing without location data", "username", username, "location", user.Location, "error", err)
		return nil
	}
	
	d.logger.Debug("geocoded location", "username", username, "location", user.Location, 
		"latitude", coords.Latitude, "longitude", coords.Longitude)
	
	timezone, err := d.timezoneForCoordinates(ctx, coords.Latitude, coords.Longitude)
	if err != nil {
		d.logger.Warn("timezone lookup failed - continuing without timezone data", "username", username, "coordinates", 
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


// tryUnifiedGeminiAnalysis uses Gemini with all available data (activity + context) in a single call.
func (d *Detector) tryUnifiedGeminiAnalysis(ctx context.Context, username string, activityResult *Result) *Result {
	if d.geminiAPIKey == "" {
		d.logger.Warn("Gemini API key not configured - skipping AI analysis. Set GEMINI_API_KEY environment variable", "username", username)
		return nil
	}
	
	// Gather contextual data about the user
	user := d.fetchUser(ctx, username)
	if user == nil {
		d.logger.Debug("could not fetch user data for Gemini analysis", "username", username)
		return nil
	}
	
	prs, err := d.fetchPullRequests(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch pull requests", "username", username, "error", err)
		prs = []PullRequest{}
	}
	issues, err := d.fetchIssues(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch issues", "username", username, "error", err)
		issues = []Issue{}
	}
	
	// Find longest PR/issue body for language analysis
	var longestBody string
	var longestTitle string
	for _, pr := range prs {
		if len(pr.Body) > len(longestBody) {
			longestBody = pr.Body
			longestTitle = pr.Title
		}
	}
	for _, issue := range issues {
		if len(issue.Body) > len(longestBody) {
			longestBody = issue.Body
			longestTitle = issue.Title
		}
	}
	
	// Limit body to 5000 chars for token efficiency
	if len(longestBody) > 5000 {
		longestBody = longestBody[:5000] + "..."
	}
	
	prSummary := make([]map[string]interface{}, len(prs))
	for i, pr := range prs {
		prSummary[i] = map[string]interface{}{
			"title": pr.Title,
		}
	}
	
	var websiteContent string
	if user.Blog != "" {
		d.logger.Debug("fetching website content for Gemini analysis", "username", username, "blog_url", user.Blog)
		websiteContent = d.fetchWebsiteContent(ctx, user.Blog)
	}
	
	orgs, err := d.fetchOrganizations(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch organizations", "username", username, "error", err)
		orgs = []Organization{}
	}
	d.logger.Debug("fetched organization data", "username", username, "org_count", len(orgs))
	
	contextData := map[string]interface{}{
		"github_user_json": user,
		"pull_requests":    prSummary,
		"website_content":  websiteContent,
		"organizations":    orgs,
		"longest_pr_issue_body": longestBody,
		"longest_pr_issue_title": longestTitle,
		"issue_count": len(issues),
	}
	
	var method string
	if activityResult != nil {
		contextData["activity_detected_timezone"] = activityResult.Timezone
		contextData["activity_confidence"] = activityResult.Confidence
		
		// Add work schedule information from activity analysis
		if activityResult.ActiveHoursLocal.Start != 0 || activityResult.ActiveHoursLocal.End != 0 {
			contextData["work_start_local"] = activityResult.ActiveHoursLocal.Start
			contextData["work_end_local"] = activityResult.ActiveHoursLocal.End
		}
		
		// Add lunch timing information
		if activityResult.LunchHoursLocal.Start != 0 || activityResult.LunchHoursLocal.End != 0 {
			contextData["lunch_start_local"] = activityResult.LunchHoursLocal.Start
			contextData["lunch_end_local"] = activityResult.LunchHoursLocal.End
			contextData["lunch_confidence"] = activityResult.LunchHoursLocal.Confidence
		}
		
		// Add quiet hours (sleep pattern) in UTC
		if len(activityResult.QuietHoursUTC) > 0 {
			contextData["sleep_hours_utc"] = activityResult.QuietHoursUTC
		}
		
		// Add GMT offset info instead of specific timezone candidates
		if strings.HasPrefix(activityResult.Timezone, "UTC") {
			// Extract offset from UTC format (e.g., "UTC+5", "UTC-8")
			offsetStr := strings.TrimPrefix(activityResult.Timezone, "UTC")
			if offset, err := strconv.Atoi(offsetStr); err == nil {
				contextData["detected_gmt_offset"] = fmt.Sprintf("GMT%+d", offset)
				contextData["detected_gmt_offset_note"] = fmt.Sprintf("Activity patterns suggest GMT%+d timezone. Consider major cities and tech hubs in this offset.", offset)
			}
		}
		
		method = "gemini_refined_activity"
		d.logger.Debug("analyzing with Gemini + activity data", "username", username, 
			"activity_timezone", activityResult.Timezone, "profile_location", user.Location, 
			"company", user.Company, "website_available", websiteContent != "")
	} else {
		method = "gemini_analysis" 
		d.logger.Debug("analyzing with Gemini only", "username", username, 
			"profile_location", user.Location, "company", user.Company, "website_available", websiteContent != "")
	}
	
	timezone, location, confidence, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, true)
	if err != nil {
		d.logger.Debug("Gemini analysis failed", "username", username, "error", err)
		return nil
	}
	
	if timezone == "" {
		d.logger.Debug("Gemini could not determine timezone", "username", username)
		return nil
	}
	
	result := &Result{
		Username:                username,
		Timezone:                timezone,
		GeminiSuggestedLocation: location,
		Confidence:              confidence,
		Method:                  method,
	}
	
	// Try to geocode the AI-suggested location to get coordinates for the map
	if location != "" {
		d.logger.Debug("geocoding AI-suggested location", "username", username, "location", location)
		if coords, err := d.geocodeLocation(ctx, location); err == nil {
			result.Location = coords
			result.LocationName = location
			d.logger.Debug("successfully geocoded AI location", "username", username, "location", location, "lat", coords.Latitude, "lng", coords.Longitude)
		} else {
			d.logger.Debug("failed to geocode AI location", "username", username, "location", location, "error", err)
		}
	}
	
	return result
}

func (d *Detector) tryActivityPatterns(ctx context.Context, username string) *Result {
	// Fetch all activity data in parallel
	activity := d.fetchAllActivity(ctx, username)
	
	totalActivity := len(activity.PullRequests) + len(activity.Issues) + len(activity.Comments)
	if totalActivity < 20 {
		d.logger.Debug("insufficient activity data", "username", username, 
			"pr_count", len(activity.PullRequests), 
			"issue_count", len(activity.Issues),
			"comment_count", len(activity.Comments),
			"total", totalActivity, "minimum_required", 20)
		return nil
	}
	
	d.logger.Debug("analyzing activity patterns", "username", username, 
		"pr_count", len(activity.PullRequests),
		"issue_count", len(activity.Issues),
		"comment_count", len(activity.Comments))
	
	hourCounts := make(map[int]int)
	
	// Count PRs
	for _, pr := range activity.PullRequests {
		hour := pr.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}
	
	// Count issues
	for _, issue := range activity.Issues {
		hour := issue.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}
	
	// Count comments
	for _, comment := range activity.Comments {
		hour := comment.CreatedAt.UTC().Hour()
		hourCounts[hour]++
	}
	
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
	
	quietHours := findSleepHours(hourCounts)
	if len(quietHours) < 4 {
		d.logger.Debug("insufficient sleep hours", "username", username, "sleep_hours", len(quietHours))
		return nil
	}
	
	d.logger.Debug("activity pattern summary", "username", username, 
		"total_activity", totalActivity,
		"sleep_hours", quietHours, 
		"most_active_hours", mostActiveHours,
		"max_activity_count", maxActivity)
	
	hourlyActivity := make([]int, 24)
	for hour := 0; hour < 24; hour++ {
		hourlyActivity[hour] = hourCounts[hour]
	}
	d.logger.Debug("hourly activity distribution", "username", username, "hours_utc", hourlyActivity)
	
	// Find the middle of sleep hours, handling wrap-around
	start := quietHours[0]
	end := quietHours[len(quietHours)-1]
	var midQuiet float64
	
	// Check if quiet hours wrap around midnight
	if end < start || (start == 0 && end == 23) {
		// Wraps around (e.g., 22-3)
		totalHours := (24 - start) + end + 1
		midQuiet = float64(start) + float64(totalHours)/2.0
		if midQuiet >= 24 {
			midQuiet -= 24
		}
	} else {
		// Normal case (e.g., 3-8)
		// For a 6-hour window from start to end, the middle is (start + end) / 2
		midQuiet = (float64(start) + float64(end)) / 2.0
	}
	
	// Analyze the activity pattern to determine likely region
	// European pattern: more activity in hours 6-16 UTC (morning/afternoon in Europe)
	// American pattern: more activity in hours 12-22 UTC (morning/afternoon in Americas)
	europeanActivity := 0
	americanActivity := 0
	for hour := 6; hour <= 16; hour++ {
		europeanActivity += hourCounts[hour]
	}
	for hour := 12; hour <= 22; hour++ {
		americanActivity += hourCounts[hour]
	}
	
	// Sleep patterns are more reliable than work patterns for timezone detection
	// The middle of quiet time varies by region:
	// - Americans tend to sleep later (midpoint ~3:30am)
	// - Europeans tend to sleep earlier (midpoint ~2:30am)
	// - Asians vary widely
	var assumedSleepMidpoint float64
	if float64(europeanActivity) > float64(americanActivity)*1.2 {
		// Strong European pattern  
		// Europeans typically have earlier sleep patterns, midpoint around 2am
		assumedSleepMidpoint = 2.0
		d.logger.Debug("detected European activity pattern", "username", username, 
			"european_activity", europeanActivity, "american_activity", americanActivity)
	} else if float64(americanActivity) > float64(europeanActivity)*1.2 {
		// Strong American pattern
		// Americans typically sleep midnight-5am, midpoint around 2.5am
		// Using 2.5 instead of 3.5 to better match Eastern Time patterns
		assumedSleepMidpoint = 2.5
		d.logger.Debug("detected American activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	} else {
		// Unclear or Asian pattern, use default
		assumedSleepMidpoint = 3.0
		d.logger.Debug("unclear activity pattern", "username", username,
			"european_activity", europeanActivity, "american_activity", americanActivity)
	}
	
	offsetFromUTC := assumedSleepMidpoint - midQuiet
	
	d.logger.Debug("offset calculation details", "username", username,
		"assumedSleepMidpoint", assumedSleepMidpoint,
		"midQuiet", midQuiet,
		"rawOffset", offsetFromUTC)
	
	// For US timezones, check if the pattern matches known US work/activity patterns
	// US developers often have evening activity (7-11pm local) for open source
	// This would be roughly 13-17 UTC for Central Time (UTC-6)
	
	// Count activity in typical US evening hours (converted to UTC for different zones)
	// For Central Time (UTC-6): 7-11pm local = 1-5am UTC (next day) or 13-17 UTC (same day DST)
	// For Eastern Time (UTC-5): 7-11pm local = 0-4am UTC (next day) or 12-16 UTC (same day DST)
	// For Pacific Time (UTC-8): 7-11pm local = 3-7am UTC (next day) or 15-19 UTC (same day DST)
	
	// Check if this looks like a US pattern based on quiet hours
	// US timezones typically have quiet hours (midnight-5am local) that map to:
	// - Eastern (UTC-4 DST): quiet hours 4-9 UTC  
	// - Central (UTC-5 DST): quiet hours 5-10 UTC
	// - Mountain (UTC-6 DST): quiet hours 6-11 UTC
	// - Pacific (UTC-7 DST): quiet hours 7-12 UTC
	
	// If we detected American activity pattern and quiet hours suggest US timezone,
	// use the calculated offset as-is rather than forcing to Central
	if americanActivity > europeanActivity && midQuiet >= 4 && midQuiet <= 12 {
		// This looks like a US pattern, trust the calculated offset
		d.logger.Debug("detected US timezone pattern", "username", username,
			"mid_quiet_utc", midQuiet, "calculated_offset", offsetFromUTC)
		// Keep the calculated offset as-is
	}
	
	// Normalize to [-12, 12] range
	if offsetFromUTC > 12 {
		offsetFromUTC -= 24
	} else if offsetFromUTC <= -12 {
		offsetFromUTC += 24
	}
	
	offsetInt := int(math.Round(offsetFromUTC))
	
	d.logger.Debug("calculated timezone offset", "username", username, 
		"sleep_hours", quietHours,
		"mid_sleep_utc", midQuiet,
		"offset_calculated", offsetFromUTC,
		"offset_rounded", offsetInt)
	
	timezone := timezoneFromOffset(offsetInt)
	d.logger.Debug("Activity-based UTC offset", "username", username, "offset", offsetInt, "timezone", timezone)
	
	// Log the detected offset for verification
	if timezone != "" {
		now := time.Now().UTC()
		// Calculate what the local time would be with this offset
		localTime := now.Add(time.Duration(offsetInt) * time.Hour)
		d.logger.Debug("timezone verification", "username", username, "timezone", timezone,
			"utc_time", now.Format("15:04 MST"), 
			"estimated_local_time", localTime.Format("15:04"),
			"offset_hours", offsetInt)
	}
	
	// Calculate typical active hours in local time (excluding outliers)
	// We need to convert from UTC to local time
	activeStart, activeEnd := calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)
	
	// Detect lunch break
	lunchStart, lunchEnd, lunchConfidence := detectLunchBreak(hourCounts, offsetInt, activeStart, activeEnd)
	d.logger.Debug("lunch detection attempt", "username", username, 
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence,
		"work_start", activeStart, "work_end", activeEnd, "utc_offset", offsetInt)
		
	// Use work schedule validation for timezone detection
	// Most people start work between 8:00am-9:30am and have lunch 11:30am-1:00pm
	var offsetCorrection int
	var correctionReason string
	
	// Check work start time (should be 8:00am-9:30am) - be more strict
	if float64(activeStart) < 7.5 || float64(activeStart) > 9.5 {
		expectedWorkStart := 8.5  // 8:30am average
		workCorrection := int(expectedWorkStart - float64(activeStart))
		if workCorrection != 0 && workCorrection >= -8 && workCorrection <= 8 {
			offsetCorrection = workCorrection
			correctionReason = "work_start"
			d.logger.Debug("work start timing suggests timezone correction", "username", username,
				"work_start_local", activeStart, "expected_range", "7:30-9:30", 
				"suggested_correction", workCorrection)
		}
	}
	
	// Check lunch timing (should be 11:30am-12:30pm, much stricter)
	if lunchStart != -1 && lunchEnd != -1 {
		// Very strict validation: lunch should start between 11:30am-12:30pm
		if lunchStart < 11.5 || lunchStart > 12.5 || lunchEnd < 12.5 || lunchEnd > 13.5 {
			expectedLunchMid := 12.0  // 12:00pm
			actualLunchMid := (lunchStart + lunchEnd) / 2
			lunchCorrection := int(expectedLunchMid - actualLunchMid)
			
			// If we don't have a work start correction, or lunch correction is larger, use lunch correction
			if offsetCorrection == 0 || (lunchCorrection != 0 && int(math.Abs(float64(lunchCorrection))) > int(math.Abs(float64(offsetCorrection)))) {
				offsetCorrection = lunchCorrection
				correctionReason = "lunch_timing"
			}
			
			d.logger.Debug("lunch timing suggests timezone correction", "username", username,
				"lunch_start_local", lunchStart, "lunch_end_local", lunchEnd, 
				"expected_range", "11:30-12:30 start, 12:30-13:30 end", "suggested_correction", lunchCorrection)
		}
	}
	
	// Check evening wind-down time (should be 5:00pm-7:00pm)
	if float64(activeEnd) < 16.0 || float64(activeEnd) > 19.0 {
		expectedWorkEnd := 17.0  // 5:00pm average
		endCorrection := int(expectedWorkEnd - float64(activeEnd))
		if endCorrection != 0 && endCorrection >= -8 && endCorrection <= 8 {
			// If we don't have other corrections, or this correction is more significant, use it
			if offsetCorrection == 0 || (int(math.Abs(float64(endCorrection))) > int(math.Abs(float64(offsetCorrection)))) {
				offsetCorrection = endCorrection
				correctionReason = "work_end"
				d.logger.Debug("work end timing suggests timezone correction", "username", username,
					"work_end_local", activeEnd, "expected_range", "16:00-19:00", 
					"suggested_correction", endCorrection)
			}
		}
	}
	
	// Apply timezone correction if we found one
	if offsetCorrection != 0 && offsetCorrection >= -8 && offsetCorrection <= 8 {
		correctedOffset := offsetInt + offsetCorrection
		d.logger.Debug("correcting timezone based on work schedule", "username", username,
			"original_offset", offsetInt, "correction", offsetCorrection, 
			"corrected_offset", correctedOffset, "reason", correctionReason)
		offsetInt = correctedOffset
		timezone = timezoneFromOffset(offsetInt)
		
		// Recalculate active hours and lunch with corrected offset
		activeStart, activeEnd = calculateTypicalActiveHours(hourCounts, quietHours, offsetInt)
		lunchStart, lunchEnd, lunchConfidence = detectLunchBreak(hourCounts, offsetInt, activeStart, activeEnd)
	}
	
	result := &Result{
		Username:         username,
		Timezone:         timezone,
		ActivityTimezone: timezone, // Pure activity-based result
		QuietHoursUTC:    quietHours,
		ActiveHoursLocal: struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		}{
			Start: float64(activeStart),
			End:   float64(activeEnd),
		},
		Confidence: 0.8,
		Method:     "activity_patterns",
	}
	
	// Always add lunch hours (they're always detected now)
	result.LunchHoursLocal = struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Confidence float64 `json:"confidence"`
	}{
		Start:      lunchStart,
		End:        lunchEnd,
		Confidence: lunchConfidence,
	}
	d.logger.Debug("detected lunch break", "username", username, 
		"lunch_start", lunchStart, "lunch_end", lunchEnd, "confidence", lunchConfidence)
	
	return result
}

func (d *Detector) fetchAllActivity(ctx context.Context, username string) *ActivityData {
	type result struct {
		prs      []PullRequest
		issues   []Issue
		comments []Comment
	}
	
	ch := make(chan result, 1)
	
	go func() {
		var res result
		
		// Fetch in parallel using goroutines
		var wg sync.WaitGroup
		wg.Add(3)
		
		// Fetch PRs
		go func() {
			defer wg.Done()
			if prs, err := d.fetchPullRequests(ctx, username); err == nil {
				res.prs = prs
			} else {
				d.logger.Debug("failed to fetch PRs", "username", username, "error", err)
			}
		}()
		
		// Fetch Issues
		go func() {
			defer wg.Done()
			if issues, err := d.fetchIssues(ctx, username); err == nil {
				res.issues = issues
			} else {
				d.logger.Debug("failed to fetch issues", "username", username, "error", err)
			}
		}()
		
		// Fetch Comments via GraphQL
		go func() {
			defer wg.Done()
			if comments, err := d.fetchUserComments(ctx, username); err == nil {
				res.comments = comments
			} else {
				d.logger.Debug("failed to fetch comments", "username", username, "error", err)
			}
		}()
		
		wg.Wait()
		ch <- res
	}()
	
	select {
	case res := <-ch:
		return &ActivityData{
			PullRequests: res.prs,
			Issues:       res.issues,
			Comments:     res.comments,
		}
	case <-ctx.Done():
		return &ActivityData{}
	}
}

func (d *Detector) fetchPullRequests(ctx context.Context, username string) ([]PullRequest, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr&sort=created&order=desc&per_page=100", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching pull requests: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var result struct {
		TotalCount int `json:"total_count"`
		Items []struct {
			Title     string    `json:"title"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"items"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	
	d.logger.Debug("GitHub PR search results", "username", username, "total_count", result.TotalCount, "returned_items", len(result.Items))
	
	var prs []PullRequest
	for _, item := range result.Items {
		prs = append(prs, PullRequest{
			Title:     item.Title,
			Body:      item.Body,
			CreatedAt: item.CreatedAt,
		})
	}
	
	return prs, nil
}

func (d *Detector) fetchIssues(ctx context.Context, username string) ([]Issue, error) {
	apiURL := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:issue&sort=created&order=desc&per_page=100", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching issues: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var result struct {
		TotalCount int `json:"total_count"`
		Items []struct {
			Title     string    `json:"title"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
			HTMLURL   string    `json:"html_url"`
		} `json:"items"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	d.logger.Debug("GitHub issue search results", "username", username, "total_count", result.TotalCount, "returned_items", len(result.Items))
	
	var issues []Issue
	for _, item := range result.Items {
		issues = append(issues, Issue{
			Title:     item.Title,
			Body:      item.Body,
			CreatedAt: item.CreatedAt,
			HTMLURL:   item.HTMLURL,
		})
	}
	
	return issues, nil
}

func (d *Detector) fetchUserComments(ctx context.Context, username string) ([]Comment, error) {
	if d.githubToken == "" {
		d.logger.Debug("GitHub token required for GraphQL API", "username", username)
		return nil, fmt.Errorf("GitHub token required for GraphQL API")
	}
	
	query := fmt.Sprintf(`{
		user(login: "%s") {
			issueComments(first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					createdAt
				}
			}
			commitComments(first: 100) {
				nodes {
					createdAt
				}
			}
		}
	}`, username)
	
	reqBody := map[string]string{
		"query": query,
	}
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling GraphQL query: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	req.Header.Set("Authorization", "bearer "+d.githubToken)
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching comments: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub GraphQL API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub GraphQL API returned status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse the response
	var result struct {
		Data struct {
			User struct {
				IssueComments struct {
					Nodes []struct {
						CreatedAt time.Time `json:"createdAt"`
					} `json:"nodes"`
				} `json:"issueComments"`
				CommitComments struct {
					Nodes []struct {
						CreatedAt time.Time `json:"createdAt"`
					} `json:"nodes"`
				} `json:"commitComments"`
			} `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	
	if len(result.Errors) > 0 {
		d.logger.Debug("GitHub GraphQL errors", "username", username, "errors", result.Errors)
		return nil, fmt.Errorf("GraphQL errors: %v", result.Errors[0].Message)
	}
	
	var comments []Comment
	
	// Add issue comments
	for _, node := range result.Data.User.IssueComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt: node.CreatedAt,
			Type:      "issue",
		})
	}
	
	// Add commit comments
	for _, node := range result.Data.User.CommitComments.Nodes {
		comments = append(comments, Comment{
			CreatedAt: node.CreatedAt,
			Type:      "commit",
		})
	}
	
	d.logger.Debug("fetched user comments", "username", username, 
		"issue_comments", len(result.Data.User.IssueComments.Nodes),
		"commit_comments", len(result.Data.User.CommitComments.Nodes),
		"total", len(comments))
	
	return comments, nil
}

func (d *Detector) fetchOrganizations(ctx context.Context, username string) ([]Organization, error) {
	apiURL := fmt.Sprintf("https://api.github.com/users/%s/orgs", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetching organizations: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("GitHub API returned status %d (failed to read response)", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var orgs []Organization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	
	return orgs, nil
}

func (d *Detector) fetchUser(ctx context.Context, username string) *GitHubUser {
	url := fmt.Sprintf("https://api.github.com/users/%s", username)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil
	}
	
	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil
	}
	
	return &user
}

func (d *Detector) geocodeLocation(ctx context.Context, location string) (*Location, error) {
	if d.mapsAPIKey == "" {
		d.logger.Warn("Google Maps API key not configured - skipping geocoding", "location", location)
		return nil, fmt.Errorf("Google Maps API key not configured")
	}
	
	encodedLocation := url.QueryEscape(location)
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s", 
		encodedLocation, d.mapsAPIKey)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
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
			Types           []string `json:"types"`
			FormattedAddress string  `json:"formatted_address"`
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
		return nil, fmt.Errorf("geocoding failed: %s", result.Status)
	}
	
	firstResult := result.Results[0]
	
	locationType := firstResult.Geometry.LocationType
	d.logger.Debug("geocoding result precision", "location", location, 
		"location_type", locationType, "types", firstResult.Types, 
		"formatted_address", firstResult.FormattedAddress)
	
	// APPROXIMATE results are often geographic centers of large areas and unreliable for timezone detection
	if locationType == "APPROXIMATE" {
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

func (d *Detector) timezoneForCoordinates(ctx context.Context, lat, lng float64) (string, error) {
	if d.mapsAPIKey == "" {
		d.logger.Warn("Google Maps API key not configured - skipping timezone lookup", "lat", lat, "lng", lng)
		return "", fmt.Errorf("Google Maps API key not configured")
	}
	
	timestamp := time.Now().Unix()
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/timezone/json?location=%.6f,%.6f&timestamp=%d&key=%s",
		lat, lng, timestamp, d.mapsAPIKey)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
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


// calculateTypicalActiveHours determines typical work hours based on activity patterns
// It uses percentiles to exclude outliers (e.g., occasional early starts or late nights)
func calculateTypicalActiveHours(hourCounts map[int]int, quietHours []int, utcOffset int) (start, end int) {
	// Create a map for easy lookup of quiet hours
	quietMap := make(map[int]bool)
	for _, h := range quietHours {
		quietMap[h] = true
	}
	
	// Find hours with meaningful activity (>10% of max activity)
	maxActivity := 0
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	threshold := maxActivity / 10
	
	// Collect active hours (not in quiet period and above threshold)
	var activeHours []int
	for hour := 0; hour < 24; hour++ {
		if !quietMap[hour] && hourCounts[hour] > threshold {
			activeHours = append(activeHours, hour)
		}
	}
	
	if len(activeHours) == 0 {
		// Default to 9am-5pm if no clear pattern
		return 9, 17
	}
	
	// Find the continuous block of active hours
	// Handle wrap-around (e.g., activity from 22-02)
	sort.Ints(activeHours)
	
	// Find the largest gap to determine where the active period starts/ends
	maxGap := 0
	gapStart := activeHours[len(activeHours)-1]
	for i := 0; i < len(activeHours); i++ {
		gap := activeHours[i] - gapStart
		if gap < 0 {
			gap += 24
		}
		if gap > maxGap {
			maxGap = gap
			start = activeHours[i]
		}
		gapStart = activeHours[i]
	}
	
	// Find the end of the active period
	end = start
	for i := 0; i < len(activeHours); i++ {
		hour := activeHours[i]
		// Check if this hour is part of the continuous block
		diff := hour - start
		if diff < 0 {
			diff += 24
		}
		if diff < 16 { // Maximum 16-hour workday
			end = hour
		}
	}
	
	// Apply smart filtering: use 10th and 90th percentiles to exclude outliers
	// This prevents occasional early/late activity from skewing the results
	activityInRange := make([]int, 0)
	for h := start; ; h = (h + 1) % 24 {
		if hourCounts[h] > 0 {
			// Add this hour's count multiple times to weight the calculation
			for i := 0; i < hourCounts[h]; i++ {
				activityInRange = append(activityInRange, h)
			}
		}
		if h == end {
			break
		}
	}
	
	if len(activityInRange) > 10 {
		sort.Ints(activityInRange)
		// Use 10th percentile for start (ignore occasional early starts)
		percentile10 := len(activityInRange) / 10
		// Use 90th percentile for end (ignore occasional late nights)
		percentile90 := len(activityInRange) * 9 / 10
		
		start = activityInRange[percentile10]
		end = activityInRange[percentile90]
	}
	
	// Convert from UTC to local time
	start = (start + utcOffset + 24) % 24
	end = (end + utcOffset + 24) % 24
	
	return start, end
}

// findSleepHours looks for extended periods of zero or near-zero activity
// This is more reliable than finding "quiet" hours which might just be evening time
func findSleepHours(hourCounts map[int]int) []int {
	// First, find all hours with zero or minimal activity
	zeroHours := []int{}
	for hour := 0; hour < 24; hour++ {
		if hourCounts[hour] <= 1 { // Allow for 1 random event
			zeroHours = append(zeroHours, hour)
		}
	}
	
	// If we have a good stretch of zero activity, use that
	if len(zeroHours) >= 5 {
		// Find the longest consecutive sequence
		maxLen := 0
		maxStart := 0
		currentStart := zeroHours[0]
		currentLen := 1
		
		for i := 1; i < len(zeroHours); i++ {
			if zeroHours[i] == zeroHours[i-1]+1 || (zeroHours[i-1] == 23 && zeroHours[i] == 0) {
				currentLen++
			} else {
				if currentLen > maxLen {
					maxLen = currentLen
					maxStart = currentStart
				}
				currentStart = zeroHours[i]
				currentLen = 1
			}
		}
		if currentLen > maxLen {
			maxLen = currentLen
			maxStart = currentStart
		}
		
		// Extract the core sleep hours from the zero-activity period
		// Skip early evening hours and wake-up hours to focus on deep sleep
		result := []int{}
		sleepStart := maxStart
		sleepLength := maxLen
		
		// If we have a long zero period (8+ hours), it likely includes evening time
		// Skip the first 2-3 hours to avoid evening time, and the last hour for wake-up
		if maxLen >= 8 {
			sleepStart = (maxStart + 3) % 24  // Skip first 3 hours (evening)
			sleepLength = maxLen - 4          // Also skip last hour (wake-up)
		} else if maxLen >= 6 {
			sleepStart = (maxStart + 1) % 24  // Skip first hour
			sleepLength = maxLen - 2          // Also skip last hour
		}
		
		// Limit to reasonable sleep duration (4-7 hours)
		if sleepLength > 7 {
			sleepLength = 7
		}
		if sleepLength < 4 {
			sleepLength = maxLen  // Use original if adjustment made it too short
			sleepStart = maxStart
		}
		
		for i := 0; i < sleepLength; i++ {
			hour := (sleepStart + i) % 24
			result = append(result, hour)
		}
		
		if len(result) >= 4 {
			return result
		}
	}
	
	// Fall back to the old method if we don't have clear zero periods
	return findQuietHours(hourCounts)
}

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

func timezoneFromOffset(offsetHours int) string {
	// Return generic UTC offset format since we don't know the country at this stage
	// This is used for activity-only detection where location is unknown
	if offsetHours >= 0 {
		return fmt.Sprintf("UTC+%d", offsetHours)
	}
	return fmt.Sprintf("UTC%d", offsetHours) // Negative sign is already included
}

func timezoneCandidatesForOffset(offsetHours float64) []string {
	switch offsetHours {
	case -9:
		return []string{"America/Anchorage"}
	case -8:
		return []string{"America/Los_Angeles", "America/Vancouver", "America/Tijuana"}
	case -7:
		return []string{"America/Denver", "America/Phoenix", "America/Los_Angeles"} // Denver for MST, Phoenix for no DST, LA during DST
	case -6:
		return []string{"America/Chicago", "America/Denver", "America/Mexico_City"} // Chicago for CST, Denver during DST
	case -5:
		return []string{"America/New_York", "America/Chicago", "America/Bogota", "America/Toronto"} // NY for EST, Chicago during DST
	case -4:
		return []string{"America/Halifax", "America/New_York", "America/Caracas", "America/Santiago"} // Halifax for AST, NY during DST
	case -3:
		return []string{"America/Sao_Paulo", "America/Buenos_Aires", "America/Halifax"} // Brazil, Argentina, Halifax during DST
	case 0:
		return []string{"Europe/London", "Europe/Lisbon", "Africa/Casablanca"} // Remove UTC to prefer actual locations
	case 1:
		return []string{"Europe/Paris", "Europe/Berlin", "Europe/Amsterdam", "Europe/Warsaw", "Europe/Madrid", "Europe/Rome"}
	case 2:
		return []string{"Europe/Berlin", "Europe/Warsaw", "Europe/Paris", "Europe/Rome", "Europe/Athens", "Africa/Cairo"}
	case 3:
		return []string{"Europe/Moscow", "Africa/Nairobi", "Asia/Baghdad", "Europe/Istanbul"}
	case 4:
		return []string{"Asia/Dubai", "Europe/Moscow", "Asia/Baku", "Asia/Tbilisi"} // UAE, Russia (some parts), Azerbaijan, Georgia
	case 5:
		return []string{"Asia/Karachi", "Asia/Tashkent", "Asia/Yekaterinburg"}
	case 5.5:
		return []string{"Asia/Kolkata", "Asia/Colombo"} // India, Sri Lanka
	case 6:
		return []string{"Asia/Dhaka", "Asia/Almaty", "Asia/Omsk"}
	case 7:
		return []string{"Asia/Bangkok", "Asia/Jakarta", "Asia/Ho_Chi_Minh"}
	case 8:
		return []string{"Asia/Shanghai", "Asia/Singapore", "Asia/Hong_Kong", "Asia/Taipei", "Australia/Perth"}
	case 9:
		return []string{"Asia/Tokyo", "Asia/Seoul", "Asia/Yakutsk"}
	case 10:
		return []string{"Australia/Sydney", "Australia/Melbourne", "Asia/Vladivostok"}
	default:
		return []string{}
	}
}

func (d *Detector) queryUnifiedGeminiForTimezone(ctx context.Context, contextData map[string]interface{}, verbose bool) (string, string, float64, error) {
	// Check if we have activity data for confidence scoring later
	activityTimezone := ""
	hasActivityData := false
	if tz, ok := contextData["activity_detected_timezone"].(string); ok && tz != "" {
		activityTimezone = tz
		hasActivityData = true
	}
	
	// Use the single consolidated prompt for all cases
	prompt := unifiedGeminiPrompt()
	
	// Format evidence in a clear, readable way instead of a massive JSON blob
	formattedEvidence := d.formatEvidenceForGemini(contextData)
	
	// The consolidated prompt only takes one parameter - the formatted evidence
	fullPrompt := fmt.Sprintf(prompt, formattedEvidence)
	
	if hasActivityData {
		d.logger.Debug("Gemini unified prompt (with activity)", "prompt_length", len(fullPrompt), 
			"evidence_length", len(formattedEvidence), "activity_timezone", activityTimezone)
	} else {
		d.logger.Debug("Gemini unified prompt (context only)", "prompt_length", len(fullPrompt), 
			"evidence_length", len(formattedEvidence))
	}
	
	if verbose {
		d.logger.Debug("Gemini full prompt", "full_prompt", fullPrompt)
	}
	
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": fullPrompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.1,
			"responseMimeType": "application/json",
			"responseSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]string{
						"type": "string",
						"description": "IANA timezone identifier",
					},
					"location": map[string]string{
						"type": "string", 
						"description": "Specific location name (city, region) - NEVER return UNKNOWN",
					},
					"confidence": map[string]string{
						"type": "string",
						"description": "Confidence level: high, medium, or low",
					},
					"reasoning": map[string]string{
						"type": "string",
						"description": "Brief explanation of the decision and evidence used",
					},
				},
				"required": []string{"timezone", "location", "confidence", "reasoning"},
			},
			"maxOutputTokens": func() int {
				if verbose {
					return 300
				}
				return 100
			}(),
		},
	}
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", 0, err
	}
	
	model := d.geminiModel
	if model == "" {
		model = "gemini-2.5-flash-lite"
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, d.geminiAPIKey)
	
	// Check cache first for Gemini API calls
	var responseBody []byte
	if d.cache != nil {
		if cachedData, found := d.cache.GetAPICall(url, jsonBody); found {
			responseBody = cachedData
		} else {
			d.logger.Warn("GEMINI CACHE MISS - making API call", "url", url)
		}
	}
	
	if responseBody == nil {
		// Make actual API call if not cached
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
		defer func() {
			if err := resp.Body.Close(); err != nil {
				d.logger.Debug("failed to close response body", "error", err)
			}
		}()
		
		if resp.StatusCode != 200 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", "", 0, fmt.Errorf("Gemini API error: %d (failed to read response)", resp.StatusCode)
			}
			return "", "", 0, fmt.Errorf("Gemini API error: %d %s", resp.StatusCode, string(body))
		}
		
		responseBody, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", "", 0, err
		}
		
		// Cache the response for 20 days
		if d.cache != nil {
			if err := d.cache.SetAPICall(url, jsonBody, responseBody); err != nil {
				d.logger.Error("Failed to cache Gemini response", "error", err)
			} else {
				d.logger.Info("Gemini response cached for 20 days", "url", url)
			}
		}
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
	
	if err := json.Unmarshal(responseBody, &geminiResp); err != nil {
		return "", "", 0, err
	}
	
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", "", 0, fmt.Errorf("no response from Gemini")
	}
	
	fullResponse := strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text)
	
	d.logger.Debug("Gemini full response", "full_response", fullResponse)
	
	var timezone, location, confidence string
	
	var jsonResponse map[string]interface{}
	if err := json.Unmarshal([]byte(fullResponse), &jsonResponse); err != nil {
		d.logger.Debug("failed to parse Gemini JSON response", "error", err, "response", fullResponse)
		return "", "", 0, fmt.Errorf("invalid JSON response from Gemini: %w", err)
	}
	
	if tz, ok := jsonResponse["timezone"].(string); ok {
		timezone = tz
	}
	if loc, ok := jsonResponse["location"].(string); ok {
		location = loc
	}
	if conf, ok := jsonResponse["confidence"].(string); ok {
		confidence = conf
	}
	if reasoning, ok := jsonResponse["reasoning"].(string); ok {
		d.logger.Debug("Gemini reasoning", "reasoning", reasoning)
	}
	
	d.logger.Debug("Gemini unified response", "extracted_timezone", timezone, "extracted_location", location, "confidence", confidence, "had_activity_data", hasActivityData, "verbose_mode", verbose)
	if hasActivityData {
		d.logger.Debug("Gemini activity comparison", "original_activity_timezone", activityTimezone, "gemini_response", timezone, "gemini_location", location)
	}
	
	if timezone == "" {
		return "", "", 0, fmt.Errorf("Gemini did not provide a timezone")
	}
	if location == "" {
		return "", "", 0, fmt.Errorf("Gemini did not provide a location")
	}
	
	if !strings.Contains(timezone, "/") {
		d.logger.Debug("invalid timezone format from unified Gemini", "timezone", timezone)
		return "", "", 0, fmt.Errorf("invalid timezone format: %s", timezone)
	}
	
	// Calculate confidence based on Gemini's assessment and activity data
	var confidenceFloat float64
	switch confidence {
	case "high":
		confidenceFloat = 0.9
	case "medium":
		confidenceFloat = 0.7
	case "low":
		confidenceFloat = 0.5
	default:
		// Default confidence if not provided or invalid
		confidenceFloat = 0.6
	}
	
	// Boost confidence if we have strong activity data that matches
	if hasActivityData {
		if timezone == activityTimezone {
			d.logger.Debug("Gemini confirmed activity-based timezone", "timezone", timezone)
			confidenceFloat = 0.9 // High confidence when activity + context agree
		} else {
			d.logger.Debug("Gemini refined activity-based timezone", "original", activityTimezone, "refined", timezone)
			// Keep Gemini's confidence level since it might have good reasons for the refinement
		}
	}
	
	return timezone, location, confidenceFloat, nil
}

func (d *Detector) fetchWebsiteContent(ctx context.Context, blogURL string) string {
	if blogURL == "" {
		return ""
	}
	
	if !strings.HasPrefix(blogURL, "http://") && !strings.HasPrefix(blogURL, "https://") {
		blogURL = "https://" + blogURL
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", blogURL, http.NoBody)
	if err != nil {
		d.logger.Debug("failed to create website request", "url", blogURL, "error", err)
		return ""
	}
	
	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")
	
	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		d.logger.Debug("failed to fetch website", "url", blogURL, "error", err)
		return ""
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()
	
	if resp.StatusCode != 200 {
		d.logger.Debug("website returned non-200 status", "url", blogURL, "status", resp.StatusCode)
		return ""
	}
	
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		d.logger.Debug("failed to read website body", "url", blogURL, "error", err)
		return ""
	}
	
	return string(body)
}

func detectLunchBreak(hourCounts map[int]int, utcOffset int, workStart, workEnd int) (lunchStart, lunchEnd, confidence float64) {
	// Convert hour counts to 30-minute buckets for better precision
	bucketCounts := make(map[float64]int)
	for hour, count := range hourCounts {
		// Distribute the count evenly between two 30-minute buckets
		bucketCounts[float64(hour)] += count / 2
		bucketCounts[float64(hour)+0.5] += count / 2
		// Handle odd counts
		if count%2 == 1 {
			bucketCounts[float64(hour)] += 1
		}
	}
	
	// Look for activity dips during typical lunch hours (10am-3pm local for broader search)
	typicalLunchStart := 10.0
	typicalLunchEnd := 15.0
	
	// Convert local lunch hours to UTC
	lunchStartUTC := typicalLunchStart - float64(utcOffset)
	lunchEndUTC := typicalLunchEnd - float64(utcOffset)
	
	// Normalize to 0-24 range
	for lunchStartUTC < 0 {
		lunchStartUTC += 24
	}
	for lunchEndUTC < 0 {
		lunchEndUTC += 24
	}
	for lunchStartUTC >= 24 {
		lunchStartUTC -= 24
	}
	for lunchEndUTC >= 24 {
		lunchEndUTC -= 24
	}
	
	// Calculate average activity during work hours for comparison
	totalActivity := 0
	bucketCount := 0
	workHourBuckets := make([]float64, 0)
	for bucket := float64(workStart); bucket < float64(workEnd); bucket += 0.5 {
		utcBucket := bucket - float64(utcOffset)
		for utcBucket < 0 {
			utcBucket += 24
		}
		for utcBucket >= 24 {
			utcBucket -= 24
		}
		totalActivity += bucketCounts[utcBucket]
		bucketCount++
		workHourBuckets = append(workHourBuckets, utcBucket)
	}
	
	avgActivity := 0.0
	if bucketCount > 0 {
		avgActivity = float64(totalActivity) / float64(bucketCount)
	}
	
	// Find all candidate lunch periods (30-minute and 1-hour windows)
	type lunchCandidate struct {
		start      float64
		end        float64
		avgDip     float64
		distFrom12 float64
		confidence float64
	}
	
	candidates := make([]lunchCandidate, 0)
	
	// Check all possible 30-minute and 1-hour windows in the lunch timeframe
	for windowStart := lunchStartUTC; ; windowStart += 0.5 {
		if windowStart >= 24 {
			windowStart -= 24
		}
		
		// Try both 30-minute (1-bucket) and 60-minute (2-bucket) windows
		for windowSize := 1; windowSize <= 2; windowSize++ {
			windowEnd := windowStart + float64(windowSize)*0.5
			if windowEnd >= 24 {
				windowEnd -= 24
			}
			
			// Calculate average activity in this window
			windowActivity := 0.0
			windowBuckets := 0
			for bucket := windowStart; windowBuckets < windowSize; bucket += 0.5 {
				if bucket >= 24 {
					bucket -= 24
				}
				windowActivity += float64(bucketCounts[bucket])
				windowBuckets++
				if windowBuckets >= windowSize {
					break
				}
			}
			
			if windowBuckets > 0 {
				avgWindowActivity := windowActivity / float64(windowBuckets)
				
				// Calculate the dip relative to average work activity
				var dipPercentage float64
				if avgActivity > 0 {
					dipPercentage = (avgActivity - avgWindowActivity) / avgActivity
				}
				
				// Convert window center to local time to check distance from 12pm
				windowCenter := windowStart + float64(windowSize)*0.25
				localCenter := windowCenter + float64(utcOffset)
				for localCenter < 0 {
					localCenter += 24
				}
				for localCenter >= 24 {
					localCenter -= 24
				}
				distanceFrom12 := math.Abs(localCenter - 12.0)
				if distanceFrom12 > 12 {
					distanceFrom12 = 24 - distanceFrom12
				}
				
				// Calculate confidence based on dip size and proximity to 12pm
				confidence := 0.1 // Base confidence - always show something
				
				// Dip size component (max 0.6)
				dipComponent := 0.0
				if dipPercentage > 0.1 {
					dipComponent += 0.2 // Small dip
				}
				if dipPercentage > 0.25 {
					dipComponent += 0.2 // Significant dip
				}
				if dipPercentage > 0.5 {
					dipComponent += 0.2 // Very large dip
				}
				
				// Proximity to 12pm component (max 0.2)
				proximityComponent := 0.0
				if distanceFrom12 <= 2.0 {
					proximityComponent = (2.0 - distanceFrom12) / 2.0 * 0.2
				}
				
				// Duration appropriateness component (max 0.1)
				durationComponent := 0.0
				if windowSize == 1 && dipPercentage > 0.3 {
					// 30-minute breaks with strong dip pattern
					durationComponent = 0.1
				} else if windowSize == 2 && dipPercentage > 0.2 {
					// 1-hour breaks with reasonable dip pattern  
					durationComponent = 0.1
				}
				
				// Combine components and cap at 1.0
				confidence = confidence + dipComponent + proximityComponent + durationComponent
				if confidence > 1.0 {
					confidence = 1.0
				}
				
				candidates = append(candidates, lunchCandidate{
					start:      windowStart + float64(utcOffset),
					end:        windowEnd + float64(utcOffset),
					avgDip:     dipPercentage,
					distFrom12: distanceFrom12,
					confidence: confidence,
				})
			}
		}
		
		// Stop when we've covered the lunch window
		if (lunchEndUTC > lunchStartUTC && windowStart >= lunchEndUTC) ||
		   (lunchEndUTC < lunchStartUTC && windowStart >= lunchEndUTC && windowStart < lunchStartUTC) {
			break
		}
	}
	
	// Find the best candidate (highest confidence, prefer closer to 12pm for ties)
	bestCandidate := lunchCandidate{start: 12.0, end: 13.0, confidence: 0.1} // Default fallback
	
	for _, candidate := range candidates {
		// Normalize candidate times to 0-24 range
		for candidate.start < 0 {
			candidate.start += 24
		}
		for candidate.start >= 24 {
			candidate.start -= 24
		}
		for candidate.end < 0 {
			candidate.end += 24
		}
		for candidate.end >= 24 {
			candidate.end -= 24
		}
		
		// Prefer higher confidence, with proximity to 12pm as tiebreaker
		if candidate.confidence > bestCandidate.confidence ||
		   (candidate.confidence == bestCandidate.confidence && candidate.distFrom12 < bestCandidate.distFrom12) {
			bestCandidate = candidate
		}
	}
	
	return bestCandidate.start, bestCandidate.end, bestCandidate.confidence
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis
func (d *Detector) formatEvidenceForGemini(contextData map[string]interface{}) string {
	var evidence strings.Builder
	
	// ACTIVITY ANALYSIS SECTION
	if activityTz, ok := contextData["activity_detected_timezone"].(string); ok {
		evidence.WriteString("## ACTIVITY ANALYSIS (HIGHLY RELIABLE)\n")
		evidence.WriteString(fmt.Sprintf("Detected Timezone: %s\n", activityTz))
		
		if confidence, ok := contextData["activity_confidence"].(float64); ok {
			evidence.WriteString(fmt.Sprintf("Activity Confidence: %.1f%%\n", confidence*100))
		}
		
		if workStart, ok := contextData["work_start_local"].(float64); ok {
			if workEnd, ok := contextData["work_end_local"].(float64); ok {
				evidence.WriteString(fmt.Sprintf("Work Hours: %.1f-%.1f local time\n", workStart, workEnd))
			}
		}
		
		if lunchStart, ok := contextData["lunch_start_local"].(float64); ok {
			if lunchEnd, ok := contextData["lunch_end_local"].(float64); ok {
				if lunchConf, ok := contextData["lunch_confidence"].(float64); ok {
					evidence.WriteString(fmt.Sprintf("Lunch Hours: %.1f-%.1f local time (%.1f%% confidence)\n", 
						lunchStart, lunchEnd, lunchConf*100))
				}
			}
		}
		
		if sleepHours, ok := contextData["sleep_hours_utc"].([]int); ok && len(sleepHours) > 0 {
			evidence.WriteString(fmt.Sprintf("Sleep Hours UTC: %v\n", sleepHours))
		}
		
		if offset, ok := contextData["detected_gmt_offset"].(string); ok {
			evidence.WriteString(fmt.Sprintf("GMT Offset: %s\n", offset))
		}
		
		evidence.WriteString("\n")
	}
	
	// GITHUB USER PROFILE SECTION
	if userJSON, ok := contextData["github_user_json"]; ok {
		evidence.WriteString("## GITHUB USER PROFILE\n")
		if userBytes, err := json.MarshalIndent(userJSON, "", "  "); err == nil {
			evidence.WriteString(string(userBytes))
		}
		evidence.WriteString("\n\n")
	}
	
	// ORGANIZATIONS SECTION
	if orgs, ok := contextData["organizations"]; ok {
		evidence.WriteString("## ORGANIZATION MEMBERSHIPS\n")
		if orgBytes, err := json.MarshalIndent(orgs, "", "  "); err == nil {
			evidence.WriteString(string(orgBytes))
		}
		evidence.WriteString("\n\n")
	}
	
	// PULL REQUESTS SECTION
	if prs, ok := contextData["pull_requests"]; ok {
		evidence.WriteString("## RECENT PULL REQUEST TITLES\n")
		if prBytes, err := json.MarshalIndent(prs, "", "  "); err == nil {
			evidence.WriteString(string(prBytes))
		}
		evidence.WriteString("\n\n")
	}
	
	// LONGEST PR/ISSUE CONTENT SECTION (inline, not JSON)
	if title, ok := contextData["longest_pr_issue_title"].(string); ok && title != "" {
		evidence.WriteString("## LONGEST PR/ISSUE CONTENT\n")
		evidence.WriteString(fmt.Sprintf("Title: %s\n\n", title))
		
		if body, ok := contextData["longest_pr_issue_body"].(string); ok && body != "" {
			evidence.WriteString("Body:\n")
			evidence.WriteString(body)
			evidence.WriteString("\n\n")
		}
	}
	
	// WEBSITE CONTENT SECTION
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		evidence.WriteString("## WEBSITE/BLOG CONTENT\n")
		evidence.WriteString(websiteContent)
		evidence.WriteString("\n\n")
	}
	
	// ISSUE COUNT
	if issueCount, ok := contextData["issue_count"].(int); ok {
		evidence.WriteString(fmt.Sprintf("## ADDITIONAL METRICS\n"))
		evidence.WriteString(fmt.Sprintf("Issue Count: %d\n", issueCount))
	}
	
	return evidence.String()
}

