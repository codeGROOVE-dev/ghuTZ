// Package ghutz provides GitHub user timezone detection functionality.
package ghutz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/retry"
	"google.golang.org/genai"
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
	if activityResult == nil || result == nil {
		return
	}
	result.ActivityTimezone = activityResult.ActivityTimezone
	result.QuietHoursUTC = activityResult.QuietHoursUTC
	result.ActiveHoursLocal = activityResult.ActiveHoursLocal
	result.LunchHoursLocal = activityResult.LunchHoursLocal
	result.PeakProductivity = activityResult.PeakProductivity
	result.TopOrganizations = activityResult.TopOrganizations
	result.HourlyActivityUTC = activityResult.HourlyActivityUTC
	result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
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
func (d *Detector) tryUnifiedGeminiAnalysisWithEvents(ctx context.Context, username string, activityResult *Result, events []PublicEvent) *Result {
	// The SDK will automatically use API key if set, otherwise try Application Default Credentials
	// No need to explicitly check - let the SDK handle authentication

	// Gather contextual data about the user
	user := d.fetchUser(ctx, username)
	if user == nil {
		d.logger.Debug("could not fetch user data for Gemini analysis", "username", username)
		return nil
	}

	// Use provided events (already fetched)

	// Collect unique repositories from all activity
	uniqueRepos := make(map[string]bool)

	// Add repos from events
	for _, event := range events {
		if event.Repo.Name != "" {
			uniqueRepos[event.Repo.Name] = true
		}
	}

	// Extract PR and issue information from events for context
	// We'll keep unique titles (up to 100) for context to send to Gemini
	var prSummary []map[string]interface{}
	seenTitles := make(map[string]bool)
	var longestBody string
	var longestTitle string
	issueCount := 0

	for _, event := range events {
		if len(prSummary) >= 100 {
			break // Limit to 100 items for Gemini context
		}

		// Extract information based on event type
		switch event.Type {
		case "PullRequestEvent", "PullRequestReviewEvent", "PullRequestReviewCommentEvent":
			// Extract PR title if available in payload
			var payload map[string]interface{}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				// Skip if we can't parse payload
				continue
			}
			if pr, ok := payload["pull_request"].(map[string]interface{}); ok {
				if title, ok := pr["title"].(string); ok {
					// Only add if we haven't seen this title before
					if !seenTitles[title] {
						seenTitles[title] = true
						prSummary = append(prSummary, map[string]interface{}{
							"title": title,
						})
					}
					if body, ok := pr["body"].(string); ok && len(body) > len(longestBody) {
						longestBody = body
						longestTitle = title
					}
				}
			}
		case "IssuesEvent", "IssueCommentEvent":
			issueCount++
			// Extract issue information if needed
			var payload map[string]interface{}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				// Skip if we can't parse payload
				continue
			}
			if issue, ok := payload["issue"].(map[string]interface{}); ok {
				if title, ok := issue["title"].(string); ok {
					if body, ok := issue["body"].(string); ok && len(body) > len(longestBody) {
						longestBody = body
						longestTitle = title
					}
				}
			}
		}
	}

	// Limit body to 5000 chars for token efficiency
	if len(longestBody) > 5000 {
		longestBody = longestBody[:5000] + "..."
	}

	// Fetch PRs and issues to get more repository info
	prs, err := d.fetchPullRequests(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch PRs for repos", "username", username, "error", err)
	} else {
		for _, pr := range prs {
			if pr.Repository != "" {
				uniqueRepos[pr.Repository] = true
			}
		}
	}

	issues, err := d.fetchIssues(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch issues for repos", "username", username, "error", err)
	} else {
		for _, issue := range issues {
			if issue.Repository != "" {
				uniqueRepos[issue.Repository] = true
			}
		}
	}

	// Convert unique repos to sorted list
	var repositories []string
	for repo := range uniqueRepos {
		repositories = append(repositories, repo)
	}
	sort.Strings(repositories)

	// Limit to 50 repos for Gemini context
	if len(repositories) > 50 {
		repositories = repositories[:50]
	}

	d.logger.Debug("collected unique repositories", "username", username, "count", len(repositories))

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

	// Fetch user's top repositories (pinned or popular)
	userRepos, err := d.fetchUserRepositories(ctx, username)
	if err != nil {
		d.logger.Debug("failed to fetch user repositories", "username", username, "error", err)
		userRepos = []Repository{}
	}
	d.logger.Debug("fetched user repositories", "username", username, "count", len(userRepos))

	// Extract country-code TLDs from user's social media URLs
	socialURLs := extractSocialMediaURLs(user)

	// Also try to extract social media URLs from the GitHub profile HTML
	profileHTML := d.fetchProfileHTML(ctx, username)
	if profileHTML != "" {
		htmlSocialURLs := extractSocialMediaFromHTML(profileHTML)
		d.logger.Debug("extracted social URLs from HTML", "username", username, "urls", htmlSocialURLs)

		// For Mastodon URLs, try to fetch the profile and extract website
		// For Twitter/X URLs, try to fetch the profile and extract location/bio data
		for _, url := range htmlSocialURLs {
			if strings.Contains(url, "/@") || strings.Contains(url, "mastodon") || strings.Contains(url, ".exchange") || strings.Contains(url, ".social") {
				website := d.fetchMastodonWebsite(ctx, url)
				if website != "" {
					d.logger.Info("extracted website from Mastodon profile", "mastodon", url, "website", website)
					socialURLs = append(socialURLs, website)
				}
			}
			// Add the social URL itself
			socialURLs = append(socialURLs, url)
		}
	}

	ccTLDs := extractCountryTLDs(socialURLs...)
	d.logger.Debug("extracted ccTLDs", "username", username, "count", len(ccTLDs), "tlds", ccTLDs)

	// Initialize context data for Gemini analysis
	contextData := map[string]interface{}{
		"github_user_json":       user,
		"pull_requests":          prSummary,
		"website_content":        websiteContent,
		"organizations":          orgs,
		"repositories":           repositories, // existing activity-based repositories
		"user_repositories":      userRepos,    // new: user's top/pinned repositories
		"country_tlds":           ccTLDs,       // new: country-code TLDs from URLs
		"longest_pr_issue_body":  longestBody,
		"longest_pr_issue_title": longestTitle,
		"issue_count":            issueCount,
	}

	// Track Twitter/X URLs to inform Gemini they exist
	twitterURLs := []string{}
	if profileHTML != "" {
		htmlSocialURLs := extractSocialMediaFromHTML(profileHTML)
		for _, url := range htmlSocialURLs {
			if strings.Contains(url, "twitter.com") || strings.Contains(url, "x.com") {
				twitterURLs = append(twitterURLs, url)
				d.logger.Debug("found Twitter/X profile URL", "url", url)
			}
		}
	}

	// Add Twitter URLs to context for Gemini
	if len(twitterURLs) > 0 {
		contextData["twitter_urls"] = twitterURLs
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

		// Add activity date range information for daylight saving context
		if !activityResult.ActivityDateRange.OldestActivity.IsZero() && !activityResult.ActivityDateRange.NewestActivity.IsZero() {
			contextData["activity_oldest_date"] = activityResult.ActivityDateRange.OldestActivity.Format("2006-01-02")
			contextData["activity_newest_date"] = activityResult.ActivityDateRange.NewestActivity.Format("2006-01-02")
			contextData["activity_total_days"] = activityResult.ActivityDateRange.TotalDays

			// Determine if data spans daylight saving transitions
			contextData["activity_spans_dst_transitions"] = activityResult.ActivityDateRange.SpansDSTTransitions
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

	timezone, location, confidence, prompt, reasoning, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, true)
	if err != nil {
		d.logger.Warn("Gemini analysis failed", "username", username, "error", err)
		return nil
	}

	if timezone == "" {
		d.logger.Warn("Gemini could not determine timezone",
			"username", username,
			"location", location,
			"confidence", confidence,
			"reason", "empty timezone in response")
		return nil
	}

	result := &Result{
		Username:                username,
		Timezone:                timezone,
		GeminiSuggestedLocation: location,
		Confidence:              confidence,
		Method:                  method,
		GeminiPrompt:            prompt,
		GeminiReasoning:         reasoning,
	}

	// Preserve activity data from activityResult if available
	mergeActivityData(result, activityResult)

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

func (d *Detector) geocodeLocation(ctx context.Context, location string) (*Location, error) {
	if d.mapsAPIKey == "" {
		d.logger.Warn("Google Maps API key not configured - skipping geocoding", "location", location)
		return nil, errors.New("google Maps API key not configured")
	}

	encodedLocation := url.QueryEscape(location)
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/geocode/json?address=%s&key=%s",
		encodedLocation, d.mapsAPIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
		return "", errors.New("google Maps API key not configured")
	}

	timestamp := time.Now().Unix()
	url := fmt.Sprintf("https://maps.googleapis.com/maps/api/timezone/json?location=%.6f,%.6f&timestamp=%d&key=%s",
		lat, lng, timestamp, d.mapsAPIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
// It uses percentiles to exclude outliers (e.g., occasional early starts or late nights).

// findSleepHours looks for extended periods of zero or near-zero activity
// This is more reliable than finding "quiet" hours which might just be evening time.

func (d *Detector) queryUnifiedGeminiForTimezone(ctx context.Context, contextData map[string]interface{}, verbose bool) (string, string, float64, string, string, error) {
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

	// Use the official genai SDK (handles both API key and ADC)
	geminiResp, err := d.callGeminiWithSDK(ctx, fullPrompt, verbose)
	if err != nil {
		return "", "", 0, "", "", fmt.Errorf("Gemini API call failed: %w", err)
	}
	return geminiResp.Timezone, geminiResp.Location, geminiResp.Confidence, fullPrompt, geminiResp.Reasoning, nil
}

type geminiResponse struct {
	Timezone   string  `json:"timezone"`
	Location   string  `json:"location"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// callGeminiWithSDK uses the official Google AI SDK which handles authentication automatically.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (d *Detector) callGeminiWithSDK(ctx context.Context, prompt string, verbose bool) (*geminiResponse, error) {
	// Check cache first if available
	cacheKey := fmt.Sprintf("genai:%s:%s", d.geminiModel, prompt)
	if d.cache != nil {
		if cachedData, found := d.cache.APICall(cacheKey, []byte(prompt)); found {
			d.logger.Debug("Gemini SDK cache hit", "cache_data_length", len(cachedData))
			var result geminiResponse
			if err := json.Unmarshal(cachedData, &result); err != nil {
				d.logger.Debug("Failed to unmarshal cached Gemini response", "error", err)
			} else if result.Timezone != "" && result.Location != "" {
				// Validate the cached result has actual data
				d.logger.Debug("Using cached Gemini response",
					"timezone", result.Timezone,
					"location", result.Location,
					"confidence", result.Confidence)
				return &result, nil
			} else {
				d.logger.Warn("Cached Gemini response is invalid/empty, fetching fresh",
					"timezone", result.Timezone,
					"location", result.Location)
				// Continue to make a fresh API call
			}
		}
	}

	// Create client based on authentication method
	var client *genai.Client
	var err error
	var config *genai.ClientConfig

	if d.geminiAPIKey != "" {
		// When using API key, use Gemini API backend (not Vertex AI)
		// API keys work with Gemini API, not Vertex AI
		config = &genai.ClientConfig{
			Backend: genai.BackendGeminiAPI,
			APIKey:  d.geminiAPIKey,
		}
		d.logger.Info("Using Gemini API with API key")
	} else {
		// When using ADC, use Vertex AI backend
		projectID := d.gcpProject
		if projectID == "" {
			// Try to get from environment
			projectID = os.Getenv("GCP_PROJECT")
			if projectID == "" {
				projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
			}
			if projectID == "" {
				// Default project for ghuTZ
				projectID = "ghutz-468911"
			}
		}

		config = &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  projectID,
			Location: "us-central1",
		}
		d.logger.Info("Using Vertex AI with Application Default Credentials", "project", projectID, "location", "us-central1")
	}

	client, err = genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	// Select model for Vertex AI
	modelName := d.geminiModel
	if modelName == "" {
		modelName = "gemini-2.5-flash-lite"
	}

	// Vertex AI expects the model name without "models/" prefix
	modelName = strings.TrimPrefix(modelName, "models/")

	d.logger.Debug("Using model", "model", modelName)

	// Prepare content with user role
	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: prompt},
			},
		},
	}

	// Configure generation
	maxTokens := int32(100)
	if verbose {
		maxTokens = 300
	}

	temperature := float32(0.1)
	genConfig := &genai.GenerateContentConfig{
		Temperature:      &temperature,
		MaxOutputTokens:  maxTokens,
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"timezone": {
					Type:        genai.TypeString,
					Description: "IANA timezone identifier",
				},
				"location": {
					Type:        genai.TypeString,
					Description: "Specific location name (city, region) - NEVER return UNKNOWN",
				},
				"confidence": {
					Type:        genai.TypeString,
					Description: "Confidence level: high, medium, or low",
				},
				"reasoning": {
					Type:        genai.TypeString,
					Description: "Brief explanation of the decision and evidence used",
				},
			},
			Required: []string{"timezone", "location", "confidence", "reasoning"},
		},
	}

	// Generate content with retry logic for transient errors
	var resp *genai.GenerateContentResponse
	err = retry.Do(
		func() error {
			var genErr error
			resp, genErr = client.Models.GenerateContent(ctx, modelName, contents, genConfig)
			if genErr != nil {
				// Check if it's a context timeout or similar transient error
				if strings.Contains(genErr.Error(), "context deadline exceeded") ||
					strings.Contains(genErr.Error(), "timeout") ||
					strings.Contains(genErr.Error(), "temporary failure") ||
					strings.Contains(genErr.Error(), "503") ||
					strings.Contains(genErr.Error(), "502") ||
					strings.Contains(genErr.Error(), "500") {
					d.logger.Warn("Gemini API transient error, retrying", "error", genErr)
					return genErr // Retry
				}
				// For non-transient errors, don't retry
				d.logger.Error("Gemini API non-transient error", "error", genErr)
				return retry.Unrecoverable(genErr)
			}
			return nil
		},
		retry.Attempts(3),
		retry.Delay(time.Second*2),
		retry.MaxDelay(time.Second*10),
		retry.DelayType(retry.BackOffDelay),
		retry.OnRetry(func(n uint, err error) {
			d.logger.Info("Retrying Gemini API call", "attempt", n+1, "error", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content after retries: %w", err)
	}

	// Parse response
	if len(resp.Candidates) == 0 {
		d.logger.Error("Gemini returned no candidates")
		return nil, errors.New("no response from Gemini")
	}

	if len(resp.Candidates[0].Content.Parts) == 0 {
		d.logger.Error("Gemini returned no content parts", "candidates_count", len(resp.Candidates))
		return nil, errors.New("no content in Gemini response")
	}

	// The response should be JSON text
	jsonText := ""
	if textPart := resp.Candidates[0].Content.Parts[0]; textPart != nil && textPart.Text != "" {
		jsonText = textPart.Text
	} else {
		return nil, errors.New("unexpected response type from Gemini")
	}

	// Log the raw response for debugging
	d.logger.Debug("Gemini raw response", "response_length", len(jsonText))

	// Log first 500 chars of response for debugging (in case it's truncated)
	if jsonText != "" {
		previewLen := len(jsonText)
		if previewLen > 500 {
			previewLen = 500
		}
		d.logger.Debug("Gemini response preview", "first_500_chars", jsonText[:previewLen])
	}

	// Clean up the response - Gemini sometimes wraps JSON in markdown code blocks
	jsonText = strings.TrimSpace(jsonText)

	// Check for empty response
	if jsonText == "" || jsonText == "{}" {
		d.logger.Error("Gemini returned empty response")
		return nil, errors.New("gemini returned empty response")
	}

	// Remove markdown code block wrapper if present
	if strings.HasPrefix(jsonText, "```json") {
		jsonText = strings.TrimPrefix(jsonText, "```json")
		jsonText = strings.TrimSuffix(jsonText, "```")
		jsonText = strings.TrimSpace(jsonText)
		d.logger.Debug("Stripped markdown JSON wrapper from Gemini response")
	} else if strings.HasPrefix(jsonText, "```") {
		jsonText = strings.TrimPrefix(jsonText, "```")
		jsonText = strings.TrimSuffix(jsonText, "```")
		jsonText = strings.TrimSpace(jsonText)
		d.logger.Debug("Stripped markdown wrapper from Gemini response")
	}

	// Check if response looks like an error message instead of JSON
	if !strings.HasPrefix(jsonText, "{") {
		d.logger.Error("Gemini response doesn't look like JSON",
			"response_preview", jsonText[:min(200, len(jsonText))])
		return nil, errors.New("gemini response is not valid JSON")
	}

	var result struct {
		Timezone   string `json:"timezone"`
		Location   string `json:"location"`
		Confidence string `json:"confidence"`
		Reasoning  string `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		// Log the full response when parsing fails
		d.logger.Error("Failed to parse Gemini JSON response",
			"error", err,
			"response_length", len(jsonText),
			"raw_response", jsonText)
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	// Validate the response has required fields
	if result.Timezone == "" {
		d.logger.Error("Gemini response missing timezone field",
			"parsed_result", result,
			"raw_response", jsonText)
		return nil, errors.New("gemini response missing timezone field")
	}

	if result.Location == "" {
		d.logger.Error("Gemini response missing location field",
			"parsed_result", result,
			"raw_response", jsonText)
		return nil, errors.New("gemini response missing location field")
	}

	// Convert confidence to float
	var confidence float64
	switch strings.ToLower(result.Confidence) {
	case "high":
		confidence = 0.9
	case "medium":
		confidence = 0.7
	case "low":
		confidence = 0.5
	default:
		confidence = 0.6
	}

	d.logger.Info("Gemini detection via SDK successful",
		"timezone", result.Timezone,
		"location", result.Location,
		"confidence", confidence,
		"reasoning", result.Reasoning)

	response := &geminiResponse{
		Timezone:   result.Timezone,
		Location:   result.Location,
		Confidence: confidence,
		Reasoning:  result.Reasoning,
	}

	// Cache the successful response
	if d.cache != nil {
		if responseData, err := json.Marshal(response); err == nil {
			if err := d.cache.SetAPICall(cacheKey, []byte(prompt), responseData); err != nil {
				d.logger.Error("Failed to cache Gemini SDK response", "error", err)
			} else {
				d.logger.Info("Gemini SDK response cached for 20 days")
			}
		}
	}

	return response, nil
}

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

	return string(body)
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis.
