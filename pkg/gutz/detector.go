// Package gutz provides GitHub user timezone detection functionality.
package gutz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/codeGROOVE-dev/guTZ/pkg/github"
	"github.com/codeGROOVE-dev/guTZ/pkg/httpcache"
	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
	"github.com/codeGROOVE-dev/retry"
)

// SECURITY: Compiled regex patterns for validation and extraction.
var (
	// Timezone extraction patterns.
	timezoneDataAttrRegex = regexp.MustCompile(`data-timezone="([^"]+)"`)
	timezoneJSONRegex     = regexp.MustCompile(`"timezone":"([^"]+)"`)
	timezoneFieldRegex    = regexp.MustCompile(`timezone:([^,}]+)`)
)

// UserContext holds all fetched data for a user to avoid redundant API calls.
type UserContext struct {
	User             *github.User
	FromCache        map[string]bool
	Username         string
	ProfileHTML      string
	PullRequests     []github.PullRequest
	StarredRepos     []github.Repository
	Repositories     []github.Repository
	Issues           []github.Issue
	Comments         []github.Comment
	Gists            []github.Gist // Full gist objects with descriptions
	Commits          []time.Time
	CommitActivities []github.CommitActivity // Enhanced commit data with repository info
	Organizations    []github.Organization
	Events           []github.PublicEvent
	SSHKeys          []github.SSHKey // SSH public keys with creation timestamps
}

// Detector performs timezone detection for GitHub users.
type Detector struct {
	logger        *slog.Logger
	httpClient    *http.Client
	cache         *httpcache.OtterCache
	githubClient  *github.Client
	githubToken   string
	mapsAPIKey    string
	geminiAPIKey  string
	geminiModel   string
	gcpProject    string
	forceActivity bool
}

// NewWithLogger creates a new Detector with a custom logger.
func NewWithLogger(ctx context.Context, logger *slog.Logger, opts ...Option) *Detector {
	optHolder := &OptionHolder{}
	for _, opt := range opts {
		opt(optHolder)
	}

	// Initialize cache
	var cache *httpcache.OtterCache

	switch {
	case optHolder.noCache:
		// Explicitly disable all caching
		logger.Info("caching disabled by --no-cache flag")
		cache = nil
	case optHolder.memoryOnlyCache:
		// Use memory-only cache (for web server)
		var err error
		cache, err = httpcache.NewMemoryOnlyCache(12*time.Hour, logger)
		if err != nil {
			logger.Warn("memory-only cache initialization failed", "error", err)
			// Cache is optional, continue without it
			cache = nil
		}
	default:
		// Use disk-backed cache (for CLI)
		var cacheDir string
		if optHolder.cacheDir != "" {
			// Use custom cache directory
			cacheDir = optHolder.cacheDir
		} else if userCacheDir, err := os.UserCacheDir(); err == nil {
			// Use default user cache directory
			cacheDir = filepath.Join(userCacheDir, "gutz")
		} else {
			logger.Debug("could not determine user cache directory", "error", err)
		}

		if cacheDir != "" {
			var err error
			cache, err = httpcache.NewOtterCache(ctx, cacheDir, 30*24*time.Hour, logger)
			if err != nil {
				logger.Warn("cache initialization failed", "error", err, "cache_dir", cacheDir)
				// Cache is optional, continue without it
				cache = nil
			}
		}
	}

	detector := &Detector{
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

	// Create GitHub client with cached HTTP
	if cache != nil {
		detector.githubClient = github.NewClient(logger, detector.httpClient, optHolder.githubToken, detector.cachedHTTPDo)
	} else {
		detector.githubClient = github.NewClient(logger, detector.httpClient, optHolder.githubToken, detector.retryableHTTPDo)
	}

	return detector
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
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden ||
				resp.StatusCode >= http.StatusInternalServerError {
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

// IsValidGitHubUsername validates GitHub username format for security.
// This is exported for use by the server to prevent path traversal attacks.
func IsValidGitHubUsername(username string) bool {
	// SECURITY: Validate username to prevent injection attacks
	username = strings.TrimSpace(username)

	if username == "" {
		return false
	}

	// GitHub username rules:
	// - Max 39 characters
	// - May contain alphanumeric characters and hyphens
	// - Cannot start or end with hyphen
	// - Cannot have consecutive hyphens
	if len(username) > 39 {
		return false
	}

	if username[0] == '-' || username[len(username)-1] == '-' {
		return false
	}

	if strings.Contains(username, "--") {
		return false
	}

	// Only allow alphanumeric and hyphens
	for _, ch := range username {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') &&
			(ch < '0' || ch > '9') && ch != '-' {
			return false
		}
	}

	return true
}

// New creates a new Detector with default logger.
func New(ctx context.Context, opts ...Option) *Detector {
	return NewWithLogger(ctx, slog.Default(), opts...)
}

// Close properly shuts down the detector, including saving the cache to disk.
func (d *Detector) Close() error {
	if d.cache != nil {
		return d.cache.Close()
	}
	return nil
}

// mergeActivityData copies activity analysis data into the result.
func (d *Detector) mergeActivityData(result, activityResult *Result) {
	if activityResult == nil || result == nil {
		return
	}

	// Log to see what's being merged
	d.logger.Debug("mergeActivityData called",
		"result.Timezone", result.Timezone,
		"activityResult.Timezone", activityResult.Timezone,
		"has_candidates", activityResult.TimezoneCandidates != nil)
	result.ActivityTimezone = activityResult.ActivityTimezone
	result.SleepHoursUTC = activityResult.SleepHoursUTC
	result.SleepRanges = activityResult.SleepRanges
	result.SleepBucketsUTC = activityResult.SleepBucketsUTC
	result.ActiveHoursLocal = activityResult.ActiveHoursLocal
	result.LunchHoursUTC = activityResult.LunchHoursUTC
	result.LunchHoursLocal = activityResult.LunchHoursLocal
	result.PeakProductivity = activityResult.PeakProductivity
	result.TopOrganizations = activityResult.TopOrganizations
	result.HourlyActivityUTC = activityResult.HourlyActivityUTC
	result.HalfHourlyActivityUTC = activityResult.HalfHourlyActivityUTC
	result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
	result.TimezoneCandidates = activityResult.TimezoneCandidates
	result.ActivityDateRange = activityResult.ActivityDateRange

	// Always use the lunch times for the final chosen timezone
	// First check if we already calculated lunch for this timezone in our candidates
	// This is needed because Gemini might pick a named timezone like America/Los_Angeles
	// but our activity analysis used UTC-8, and they might have different lunch calculations
	if activityResult.HalfHourlyActivityUTC != nil && activityResult.TimezoneCandidates != nil {
		// Calculate timezone offset for the new timezone
		newOffset := offsetFromNamedTimezone(result.Timezone)
		oldOffset := offsetFromNamedTimezone(activityResult.Timezone)

		d.logger.Debug("mergeActivityData checking candidates",
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
		switch result.Timezone {
		case "America/Los_Angeles":
			// Currently August, so PDT (-7) is active
			possibleOffsets = []int{-7, -8}
		case "America/New_York":
			// Currently August, so EDT (-4) is active
			possibleOffsets = []int{-4, -5}
		case "America/Chicago":
			// Currently August, so CDT (-5) is active
			possibleOffsets = []int{-5, -6}
		case "America/Denver":
			// Currently August, so MDT (-6) is active
			possibleOffsets = []int{-6, -7}
		default:
			// For other timezones, stick with the calculated offset
		}

		// Look through all candidates for a matching offset
		// We check offsets in order of preference (current DST offset first)
		for _, offset := range possibleOffsets {
			for i := range result.TimezoneCandidates {
				candidate := &result.TimezoneCandidates[i]
				if int(candidate.Offset) == offset && candidate.LunchStartUTC >= 0 {
					// Reuse the lunch calculation from this candidate
					d.logger.Debug("reusing lunch from candidate",
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
					// Set LunchHoursLocal to the same values (like ActiveHoursLocal, it's actually UTC)
					result.LunchHoursLocal = result.LunchHoursUTC
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
			d.logger.Debug("no matching candidate lunch found, calculating new lunch",
				"timezone", result.Timezone,
				"offset", newOffset)
			lunchStart, lunchEnd, lunchConfidence := lunch.DetectLunchBreakNoonCentered(activityResult.HalfHourlyActivityUTC, newOffset)
			result.LunchHoursUTC = struct {
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
			}{
				Start:      lunchStart,
				End:        lunchEnd,
				Confidence: lunchConfidence,
			}
			// Set LunchHoursLocal to the same values (like ActiveHoursLocal, it's actually UTC)
			result.LunchHoursLocal = result.LunchHoursUTC
		}
	}
}

// fetchAllUserData fetches all data for a user at once to avoid redundant API calls.
func (d *Detector) fetchAllUserData(ctx context.Context, username string) (*UserContext, error) { //nolint:revive,maintidx // Long function but organized logically
	userCtx := &UserContext{
		Username:  username,
		FromCache: make(map[string]bool),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex     // For safe concurrent writes to userCtx
	var criticalErr error // Track critical errors that should fail the entire detection

	// Fetch user profile (with GraphQL for social accounts)
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking user profile", "username", username)
		user, err := d.githubClient.FetchUserEnhancedGraphQL(ctx, username)
		if err != nil {
			// User not found is expected, not critical
			if errors.Is(err, github.ErrUserNotFound) {
				d.logger.Debug("user not found", "username", username)
				return
			}
			// No token is acceptable, we can work without it
			if errors.Is(err, github.ErrNoGitHubToken) {
				d.logger.Debug("no GitHub token available", "username", username)
				return
			}
			// Any other error (like JSON unmarshaling) is critical - API is broken
			d.logger.Error("Critical: User profile fetch failed", "username", username, "error", err)
			mu.Lock()
			criticalErr = fmt.Errorf("failed to fetch GitHub user profile: %w", err)
			mu.Unlock()
			return
		}
		mu.Lock()
		userCtx.User = user
		mu.Unlock()
	}()

	// Fetch public events
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking public events", "username", username)
		events, err := d.githubClient.FetchPublicEvents(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch public events", "error", err)
			events = []github.PublicEvent{}
		}
		mu.Lock()
		userCtx.Events = events
		mu.Unlock()
	}()

	// Fetch profile HTML for scraping
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking profile HTML", "username", username)
		html := d.githubClient.FetchProfileHTML(ctx, username)
		mu.Lock()
		userCtx.ProfileHTML = html
		mu.Unlock()
	}()

	// Fetch organizations
	wg.Add(1)
	go func() {
		defer wg.Done()
		orgs, err := d.githubClient.FetchOrganizations(ctx, username)
		if err == nil {
			d.logger.Debug("fetched organizations", "username", username, "count", len(orgs))
		} else {
			d.logger.Debug("failed to fetch organizations", "username", username, "error", err)
		}
		mu.Lock()
		userCtx.Organizations = orgs
		mu.Unlock()
	}()

	// Fetch repositories (pinned and popular) - do these in parallel too
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking repositories", "username", username)

		var pinnedRepos, popularRepos []github.Repository
		var repoWg sync.WaitGroup

		repoWg.Add(2)
		go func() {
			defer repoWg.Done()
			var err error
			pinnedRepos, err = d.githubClient.FetchPinnedRepositories(ctx, username)
			if err != nil {
				d.logger.Debug("failed to fetch pinned repositories", "username", username, "error", err)
			}
		}()
		go func() {
			defer repoWg.Done()
			var err error
			popularRepos, err = d.githubClient.FetchPopularRepositories(ctx, username)
			if err != nil {
				d.logger.Debug("failed to fetch popular repositories", "username", username, "error", err)
			}
		}()
		repoWg.Wait()

		// Combine and deduplicate repos
		repoMap := make(map[string]github.Repository)
		for i := range pinnedRepos {
			repoMap[pinnedRepos[i].FullName] = pinnedRepos[i]
		}
		for i := range popularRepos {
			if _, exists := repoMap[popularRepos[i].FullName]; !exists {
				repoMap[popularRepos[i].FullName] = popularRepos[i]
			}
		}
		// Sort repo names for deterministic order
		var repoNames []string
		for name := range repoMap {
			repoNames = append(repoNames, name)
		}
		sort.Strings(repoNames)
		var repos []github.Repository
		for _, name := range repoNames {
			repos = append(repos, repoMap[name])
		}

		mu.Lock()
		userCtx.Repositories = repos
		mu.Unlock()
	}()

	// Fetch starred repositories
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking starred repositories", "username", username)
		_, starredRepos, err := d.githubClient.FetchStarredRepositories(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch starred repositories", "username", username, "error", err)
		}
		mu.Lock()
		userCtx.StarredRepos = starredRepos
		mu.Unlock()
	}()

	// Fetch pull requests and issues using GraphQL (combined in single query!)
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("fetching PRs and Issues via GraphQL", "username", username)

		// Use the new GraphQL method that fetches both PRs and Issues together
		prs, issues, err := d.githubClient.FetchActivityWithGraphQL(ctx, username)
		if err != nil {
			d.logger.Warn("🚩 GraphQL Activity Fetch Failed", "username", username, "error", err)
			// No fallback - GraphQL is the primary method now
			prs = []github.PullRequest{}
			issues = []github.Issue{}
		}

		mu.Lock()
		userCtx.PullRequests = prs
		userCtx.Issues = issues
		mu.Unlock()

		d.logger.Info("activity data fetched",
			"username", username,
			"prs", len(prs),
			"issues", len(issues),
			"method", "GraphQL")
	}()

	// Fetch comments
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking comments", "username", username)
		comments, err := d.githubClient.FetchUserComments(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch user comments", "username", username, "error", err)
		}
		mu.Lock()
		userCtx.Comments = comments
		mu.Unlock()
	}()

	// Fetch gists
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking gists", "username", username)
		gists, err := d.githubClient.FetchUserGistsDetails(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch user gists", "username", username, "error", err)
		}
		mu.Lock()
		userCtx.Gists = gists
		mu.Unlock()
	}()

	// Fetch commit activities with repository information (using GraphQL for better quota)
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking commit activities", "username", username)
		commitActivities, err := d.githubClient.FetchUserCommitActivitiesGraphQL(ctx, username, 100)
		if err != nil {
			d.logger.Debug("failed to fetch commit activities", "username", username, "error", err)
		}
		mu.Lock()
		userCtx.CommitActivities = commitActivities
		mu.Unlock()
	}()

	// Fetch SSH keys (cheap public API call with timezone hints from creation times)
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking SSH keys", "username", username)
		keys, err := d.githubClient.FetchUserSSHKeys(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch SSH keys", "username", username, "error", err)
			keys = []github.SSHKey{}
		}
		mu.Lock()
		userCtx.SSHKeys = keys
		mu.Unlock()
	}()

	// Wait for all fetches to complete
	wg.Wait()

	// Check for critical errors
	if criticalErr != nil {
		return nil, criticalErr
	}

	// Log summary
	d.logger.Info("fetched all user data",
		"username", username,
		"events", len(userCtx.Events),
		"orgs", len(userCtx.Organizations),
		"repos", len(userCtx.Repositories),
		"starred", len(userCtx.StarredRepos),
		"prs", len(userCtx.PullRequests),
		"issues", len(userCtx.Issues),
		"comments", len(userCtx.Comments),
		"gists", len(userCtx.Gists),
		"commit_activities", len(userCtx.CommitActivities),
		"ssh_keys", len(userCtx.SSHKeys))

	return userCtx, nil
}

// getCreatedAtFromUser safely extracts the created_at time from a user, returning nil if not available.
func getCreatedAtFromUser(user *github.User) *time.Time {
	if user == nil || user.CreatedAt.IsZero() {
		return nil
	}
	return &user.CreatedAt
}

// Detect performs timezone detection for the given GitHub username.
func (d *Detector) Detect(ctx context.Context, username string) (*Result, error) {
	// SECURITY: Validate username to prevent injection attacks
	if !IsValidGitHubUsername(username) {
		return nil, errors.New("invalid GitHub username format")
	}

	d.logger.Info("detecting timezone", "username", username)

	// Fetch ALL data at once to avoid redundant API calls
	userCtx, err := d.fetchAllUserData(ctx, username)
	if err != nil {
		// Critical error fetching user data - API is broken
		d.logger.Error("Failed to fetch user data", "username", username, "error", err)
		return nil, fmt.Errorf("GitHub API error: %w", err)
	}

	// Get the full name from the fetched user
	var fullName string
	if userCtx.User != nil && userCtx.User.Name != "" {
		fullName = userCtx.User.Name
	}

	// Always perform activity analysis for fun and comparison
	d.logger.Debug("performing activity pattern analysis", "username", username)
	activityResult := d.tryActivityPatternsWithContext(ctx, userCtx)

	// Try quick detection methods first
	d.logger.Debug("trying profile HTML scraping", "username", username)
	if result := d.tryProfileScrapingWithContext(ctx, userCtx); result != nil {
		d.logger.Info("detected from profile HTML", "username", username, "timezone", result.Timezone)
		result.Name = fullName
		d.mergeActivityData(result, activityResult)
		return result, nil
	}
	d.logger.Debug("profile HTML scraping failed", "username", username)

	d.logger.Debug("trying location field analysis", "username", username)
	if result := d.tryLocationFieldWithContext(ctx, userCtx); result != nil {
		d.logger.Info("detected from location field", "username", username, "timezone", result.Timezone, "location", result.LocationName)
		result.Name = fullName
		d.mergeActivityData(result, activityResult)
		return result, nil
	}
	d.logger.Debug("location field analysis failed", "username", username)

	d.logger.Debug("trying Gemini analysis with contextual data", "username", username, "has_activity_data", activityResult != nil)
	if result := d.tryUnifiedGeminiAnalysisWithContext(ctx, userCtx, activityResult); result != nil {
		result.Name = fullName
		// Use mergeActivityData to properly handle lunch time reuse from candidates
		d.mergeActivityData(result, activityResult)
		if activityResult != nil {
			d.logger.Info("timezone detected with Gemini + activity", "username", username,
				"activity_timezone", activityResult.Timezone, "final_timezone", result.Timezone)
		} else {
			d.logger.Info("timezone detected with Gemini only", "username", username, "timezone", result.Timezone)
		}
		return result, nil
	}
	d.logger.Warn("Gemini location detection failed - using activity-only fallback", "username", username)

	if activityResult != nil {
		d.logger.Info("using activity-only result as fallback", "username", username, "timezone", activityResult.Timezone)
		activityResult.Name = fullName
		return activityResult, nil
	}

	return nil, fmt.Errorf("could not determine timezone for %s", username)
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

	// SECURITY: Only auto-prefix https:// for well-formed domain names
	if !strings.HasPrefix(blogURL, "http://") && !strings.HasPrefix(blogURL, "https://") {
		// Validate it looks like a domain before auto-prefixing
		if !strings.Contains(blogURL, ".") || strings.Contains(blogURL, " ") ||
			strings.Contains(blogURL, "://") || strings.HasPrefix(blogURL, "//") {
			d.logger.Debug("invalid URL format, not auto-prefixing", "url", blogURL)
			return ""
		}
		blogURL = "https://" + blogURL
	}

	// SECURITY: Parse URL to validate it's safe to fetch
	parsedURL, err := url.Parse(blogURL)
	if err != nil {
		d.logger.Debug("invalid URL format", "url", blogURL, "error", err)
		return ""
	}

	// SECURITY: Prevent SSRF attacks by blocking internal/private IPs and local URLs
	host := strings.ToLower(parsedURL.Hostname())

	// Block localhost and local domains
	if host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		d.logger.Debug("blocked fetch to local/internal host", "host", host)
		return ""
	}

	// Block private IP ranges (RFC 1918)
	// SECURITY: Resolve hostname to IP first to prevent DNS rebinding attacks
	ips, err := net.LookupIP(host) //nolint:noctx // DNS lookup doesn't need context
	if err == nil && len(ips) > 0 {
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				d.logger.Debug("blocked fetch to private IP", "ip", ip.String(), "host", host)
				return ""
			}
		}
	} else if ip := net.ParseIP(host); ip != nil {
		// Direct IP address provided
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			d.logger.Debug("blocked fetch to private IP", "ip", host)
			return ""
		}
	}

	// Block metadata service endpoints (AWS, GCP, Azure)
	if host == "169.254.169.254" || host == "metadata.google.internal" ||
		host == "metadata.azure.com" {
		d.logger.Debug("blocked fetch to metadata service", "host", host)
		return ""
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

	// Create a cache key for markdown conversion based on HTML content
	// This ensures consistent markdown output for the same HTML input
	htmlStr := string(body)
	markdownCacheKey := fmt.Sprintf("markdown:%s", blogURL)

	// Check if we have a cached markdown conversion
	// Use the HTML content as request body for cache key generation
	if cachedData, found := d.cache.APICall(markdownCacheKey, []byte(htmlStr)); found {
		d.logger.Debug("using cached markdown conversion", "url", blogURL, "cached_length", len(cachedData))
		return string(cachedData)
	}

	d.logger.Debug("markdown cache miss, converting HTML", "url", blogURL, "html_length", len(htmlStr))

	// Convert HTML to markdown for better text extraction
	markdown, err := md.ConvertString(htmlStr)
	if err != nil {
		// If conversion fails, cache and return the raw HTML
		d.logger.Debug("failed to convert HTML to markdown", "url", blogURL, "error", err)
		// Cache the raw HTML as fallback
		if cacheErr := d.cache.SetAPICall(markdownCacheKey, []byte(htmlStr), []byte(htmlStr)); cacheErr != nil {
			d.logger.Debug("failed to cache raw HTML", "error", cacheErr)
		}
		return htmlStr
	}

	// Cache the markdown conversion for consistency
	if err := d.cache.SetAPICall(markdownCacheKey, []byte(htmlStr), []byte(markdown)); err != nil {
		d.logger.Debug("failed to cache markdown conversion", "error", err)
	}
	return markdown
}

// tryProfileScrapingWithContext tries to extract timezone from profile HTML using UserContext.
func (d *Detector) tryProfileScrapingWithContext(_ context.Context, userCtx *UserContext) *Result {
	html := userCtx.ProfileHTML
	if html == "" {
		return nil
	}

	// Check if user exists - look for 404 indicators in HTML
	if strings.Contains(html, "This is not the web page you are looking for") {
		d.logger.Info("GitHub user not found", "username", userCtx.Username)
		return &Result{
			Username:   userCtx.Username,
			Timezone:   "UTC",
			Confidence: 0,
			Method:     "user_not_found",
		}
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
					Username:   userCtx.Username,
					Timezone:   tz,
					Confidence: 0.95,
					Method:     "github_profile",
				}
			}
		}
	}

	return nil
}

// tryLocationFieldWithContext tries to detect timezone from user location field using UserContext.
func (d *Detector) tryLocationFieldWithContext(ctx context.Context, userCtx *UserContext) *Result {
	if userCtx.User == nil || userCtx.User.Location == "" {
		d.logger.Debug("no location field found", "username", userCtx.Username)
		return nil
	}

	d.logger.Debug("analyzing location field", "username", userCtx.Username, "location", userCtx.User.Location)

	// Check if location is too vague for geocoding
	location := strings.ToLower(strings.TrimSpace(userCtx.User.Location))
	vagueLocations := []string{
		"united states", "usa", "us", "america",
		"canada", "europe", "asia", "africa", "australia",
		"remote", "worldwide", "global", "earth", "world",
		"internet", "online", "cyberspace", "metaverse",
		"home", "somewhere", "everywhere", "nowhere",
	}

	for _, vague := range vagueLocations {
		if location == vague {
			d.logger.Debug("location too vague for geocoding", "location", location)
			return nil
		}
	}

	// Try to geocode the location
	coords, err := d.geocodeLocation(ctx, userCtx.User.Location)
	if err != nil {
		d.logger.Debug("geocoding failed", "location", userCtx.User.Location, "error", err)
		return nil
	}

	// Get timezone from coordinates
	tz, err := d.timezoneForCoordinates(ctx, coords.Latitude, coords.Longitude)
	if err != nil {
		d.logger.Debug("timezone lookup failed", "lat", coords.Latitude, "lng", coords.Longitude, "error", err)
		return nil
	}

	return &Result{
		Username:     userCtx.Username,
		Timezone:     tz,
		Location:     coords,
		LocationName: userCtx.User.Location,
		Confidence:   0.8,
		Method:       "location_field",
		CreatedAt:    getCreatedAtFromUser(userCtx.User),
	}
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis.
