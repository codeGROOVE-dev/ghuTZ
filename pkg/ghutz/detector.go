// Package ghutz provides GitHub user timezone detection functionality.
package ghutz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/retry"
	md "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// SECURITY: GitHub token patterns for validation.
var (
	// GitHub Personal Access Token (classic) - ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.
	githubPATRegex = regexp.MustCompile(`^ghp_[a-zA-Z0-9]{36}$`)
	// GitHub App Installation Token - ghs_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.
	githubAppTokenRegex = regexp.MustCompile(`^ghs_[a-zA-Z0-9]{36}$`)
	// GitHub Fine-grained PAT - github_pat_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.
	githubFineGrainedRegex = regexp.MustCompile(`^github_pat_[a-zA-Z0-9_]{82}$`)
	// GitHub username validation regex.
	validUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)
	// Timezone extraction patterns.
	timezoneDataAttrRegex = regexp.MustCompile(`data-timezone="([^"]+)"`)
	timezoneJSONRegex     = regexp.MustCompile(`"timezone":"([^"]+)"`)
	timezoneFieldRegex    = regexp.MustCompile(`timezone:([^,}]+)`)
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
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode >= http.StatusInternalServerError {
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

func New(ctx context.Context, opts ...Option) *Detector {
	return NewWithLogger(ctx, slog.Default(), opts...)
}

func NewWithLogger(ctx context.Context, logger *slog.Logger, opts ...Option) *Detector {
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
		cache, err = NewOtterCache(ctx, cacheDir, 20*24*time.Hour, logger)
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

// Close properly shuts down the detector, including saving the cache to disk.
func (d *Detector) Close() error {
	if d.cache != nil {
		return d.cache.Close()
	}
	return nil
}

// mergeActivityData copies activity analysis data into the result.
func mergeActivityData(result, activityResult *Result) {
	mergeActivityDataWithLogger(result, activityResult, slog.Default())
}

// mergeActivityDataWithLogger copies activity analysis data into the result.
func mergeActivityDataWithLogger(result, activityResult *Result, logger *slog.Logger) {
	if activityResult == nil || result == nil {
		return
	}
	
	// Log to see what's being merged
	logger.Debug("mergeActivityData called",
		"result.Timezone", result.Timezone,
		"activityResult.Timezone", activityResult.Timezone,
		"has_candidates", activityResult.TimezoneCandidates != nil)
	result.ActivityTimezone = activityResult.ActivityTimezone
	result.QuietHoursUTC = activityResult.QuietHoursUTC
	result.SleepBucketsUTC = activityResult.SleepBucketsUTC
	result.ActiveHoursLocal = activityResult.ActiveHoursLocal
	result.LunchHoursUTC = activityResult.LunchHoursUTC
	result.PeakProductivity = activityResult.PeakProductivity
	result.TopOrganizations = activityResult.TopOrganizations
	result.HourlyActivityUTC = activityResult.HourlyActivityUTC
	result.HalfHourlyActivityUTC = activityResult.HalfHourlyActivityUTC
	result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
	result.TimezoneCandidates = activityResult.TimezoneCandidates
	
	// Always use the lunch times for the final chosen timezone
	// First check if we already calculated lunch for this timezone in our candidates
	// This is needed because Gemini might pick a named timezone like America/Los_Angeles
	// but our activity analysis used UTC-8, and they might have different lunch calculations
	if result.HalfHourlyActivityUTC != nil && result.TimezoneCandidates != nil {
		// Calculate timezone offset for the new timezone
		newOffset := offsetFromNamedTimezone(result.Timezone)
		oldOffset := offsetFromNamedTimezone(activityResult.Timezone)
		
		logger.Debug("mergeActivityData checking candidates",
			"result.Timezone", result.Timezone,
			"activityResult.Timezone", activityResult.Timezone,
			"newOffset", newOffset,
			"oldOffset", oldOffset,
			"num_candidates", len(result.TimezoneCandidates))
		
		// Check if we have this timezone in our candidates (to reuse calculation)
		lunchFound := false
		
		// For timezones with DST, check both possible offsets
		// e.g., America/Los_Angeles could be -7 (PDT) or -8 (PST)
		// We prefer the current offset (newOffset) first
		possibleOffsets := []int{newOffset}
		if result.Timezone == "America/Los_Angeles" {
			// Currently August, so PDT (-7) is active
			possibleOffsets = []int{-7, -8}
		} else if result.Timezone == "America/New_York" {
			// Currently August, so EDT (-4) is active
			possibleOffsets = []int{-4, -5}
		} else if result.Timezone == "America/Chicago" {
			// Currently August, so CDT (-5) is active
			possibleOffsets = []int{-5, -6}
		} else if result.Timezone == "America/Denver" {
			// Currently August, so MDT (-6) is active
			possibleOffsets = []int{-6, -7}
		}
		
		// Look through all candidates for a matching offset
		// We check offsets in order of preference (current DST offset first)
		for _, offset := range possibleOffsets {
			for _, candidate := range result.TimezoneCandidates {
				if int(candidate.Offset) == offset && candidate.LunchStartUTC >= 0 {
					// Reuse the lunch calculation from this candidate
					logger.Debug("reusing lunch from candidate",
						"timezone", result.Timezone,
						"candidate_offset", candidate.Offset,
						"lunch_start_utc", candidate.LunchStartUTC,
						"lunch_end_utc", candidate.LunchEndUTC,
						"lunch_confidence", candidate.LunchConfidence)
					result.LunchHoursUTC = struct {
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
					}{
						Start:      candidate.LunchStartUTC,
						End:        candidate.LunchEndUTC,
						Confidence: candidate.LunchConfidence,
					}
					lunchFound = true
					break
				}
			}
			if lunchFound {
				break
			}
		}
		
		// If we didn't find a pre-calculated lunch, calculate it now
		if !lunchFound {
			logger.Debug("no matching candidate lunch found, calculating new lunch",
				"timezone", result.Timezone,
				"offset", newOffset)
			lunchStart, lunchEnd, lunchConfidence := detectLunchBreakNoonCentered(result.HalfHourlyActivityUTC, newOffset)
			result.LunchHoursUTC = struct {
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
			}{
				Start:      lunchStart,
				End:        lunchEnd,
				Confidence: lunchConfidence,
			}
		}
	}
}

func (d *Detector) Detect(ctx context.Context, username string) (*Result, error) {
	if username == "" {
		return nil, errors.New("username cannot be empty")
	}

	// Validate username to prevent injection attacks
	// GitHub usernames can only contain alphanumeric characters or hyphens
	// Cannot have multiple consecutive hyphens
	// Cannot begin or end with a hyphen
	// Maximum 39 characters
	if len(username) > 39 {
		return nil, errors.New("username too long (max 39 characters)")
	}

	if !validUsernameRegex.MatchString(username) {
		return nil, errors.New("invalid username format")
	}

	d.logger.Info("detecting timezone", "username", username)

	// Fetch user profile to get the full name
	var fullName string
	if user := d.fetchUser(ctx, username); user != nil && user.Name != "" {
		fullName = user.Name
		d.logger.Debug("fetched user full name", "username", username, "name", fullName)
	}

	// Fetch public events once and share between analyses
	events, err := d.fetchPublicEvents(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch public events", "username", username, "error", err)
		events = []PublicEvent{}
	}

	// Always perform activity analysis for fun and comparison
	d.logger.Debug("performing activity pattern analysis", "username", username)
	activityResult := d.tryActivityPatternsWithEvents(ctx, username, events)

	// Try quick detection methods first
	d.logger.Debug("trying profile HTML scraping", "username", username)
	if result := d.tryProfileScraping(ctx, username); result != nil {
		d.logger.Info("detected from profile HTML", "username", username, "timezone", result.Timezone)
		result.Name = fullName
		mergeActivityData(result, activityResult)
		return result, nil
	}
	d.logger.Debug("profile HTML scraping failed", "username", username)

	d.logger.Debug("trying location field analysis", "username", username)
	if result := d.tryLocationField(ctx, username); result != nil {
		d.logger.Info("detected from location field", "username", username, "timezone", result.Timezone, "location", result.LocationName)
		result.Name = fullName
		mergeActivityData(result, activityResult)
		return result, nil
	}
	d.logger.Debug("location field analysis failed", "username", username)

	d.logger.Debug("trying Gemini analysis with contextual data", "username", username, "has_activity_data", activityResult != nil)
	if result := d.tryUnifiedGeminiAnalysisWithEvents(ctx, username, activityResult, events); result != nil {
		result.Name = fullName
		// Use mergeActivityData to properly handle lunch time reuse from candidates
		mergeActivityDataWithLogger(result, activityResult, d.logger)
		if activityResult != nil {
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

// fetchProfileHTML fetches the GitHub profile HTML for a user.
func (d *Detector) fetchProfileHTML(ctx context.Context, username string) string {
	url := fmt.Sprintf("https://github.com/%s", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return ""
	}

	// SECURITY: Validate and sanitize GitHub token before use
	if d.githubToken != "" && d.isValidGitHubToken(d.githubToken) {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}

	resp, err := d.cachedHTTPDo(ctx, req)
	if err != nil {
		return ""
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			d.logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	return string(body)
}

func (d *Detector) tryProfileScraping(ctx context.Context, username string) *Result {
	html := d.fetchProfileHTML(ctx, username)
	if html == "" {
		url := fmt.Sprintf("https://github.com/%s", username)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
				Timezone:   "UTC", // Default timezone for non-existent users
				Confidence: 0,     // Zero confidence indicates non-existent user
				Method:     "user_not_found",
			}
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}

		html = string(body)
	}

	// Try extracting timezone from HTML using pre-compiled regex patterns
	patterns := []*regexp.Regexp{
		timezoneDataAttrRegex,
		timezoneFieldRegex,
		timezoneJSONRegex,
	}

	for _, re := range patterns {
		if matches := re.FindStringSubmatch(html); len(matches) > 1 {
			tz := strings.TrimSpace(matches[1])
			if tz != "" && tz != "UTC" {
				return &Result{
					Username:   username,
					Timezone:   tz,
					Confidence: 0.95,
					Method:     "github_profile",
				}
			}
		}
	}

	return nil
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

// tryUnifiedGeminiAnalysisWithEvents uses Gemini with provided events data.



// calculateTypicalActiveHours determines typical work hours based on activity patterns
// It uses percentiles to exclude outliers (e.g., occasional early starts or late nights).

// findSleepHours looks for extended periods of zero or near-zero activity
// This is more reliable than finding "quiet" hours which might just be evening time.





func (d *Detector) fetchWebsiteContent(ctx context.Context, blogURL string) string {
	if blogURL == "" {
		return ""
	}

	if !strings.HasPrefix(blogURL, "http://") && !strings.HasPrefix(blogURL, "https://") {
		blogURL = "https://" + blogURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blogURL, http.NoBody)
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

	if resp.StatusCode != http.StatusOK {
		d.logger.Debug("website returned non-200 status", "url", blogURL, "status", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		d.logger.Debug("failed to read website body", "url", blogURL, "error", err)
		return ""
	}

	// Convert HTML to markdown for better text extraction
	markdown, err := md.ConvertString(string(body))
	if err != nil {
		// If conversion fails, return the raw HTML
		d.logger.Debug("failed to convert HTML to markdown", "url", blogURL, "error", err)
		return string(body)
	}

	return markdown
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis.
