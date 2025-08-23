// Package gutz provides GitHub user timezone detection functionality.
package gutz

import (
	"context"
	"crypto/tls"
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
	"strconv"
	"strings"
	"sync"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/codeGROOVE-dev/guTZ/pkg/github"
	"github.com/codeGROOVE-dev/guTZ/pkg/httpcache"
	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
	"github.com/codeGROOVE-dev/guTZ/pkg/tzconvert"
	"github.com/codeGROOVE-dev/retry"
)

// SECURITY: Compiled regex patterns for validation and extraction.
var (
	// Timezone extraction patterns.
	timezoneDataAttrRegex = regexp.MustCompile(`data-timezone="([^"]+)"`)
	timezoneJSONRegex     = regexp.MustCompile(`"timezone":"([^"]+)"`)
	timezoneFieldRegex    = regexp.MustCompile(`timezone:([^,}]+)`)
	// GitHub profile timezone element pattern - extracts the UTC offset hours.
	profileTimezoneRegex = regexp.MustCompile(`<profile-timezone[^>]*data-hours-ahead-of-utc="([^"]*)"`)
	// Fallback pattern for when data-hours-ahead-of-utc is empty - matches text like (UTC +02:00) or (UTC -05:00).
	profileTimezoneTextRegex = regexp.MustCompile(`<profile-timezone[^>]*>.*?\(UTC\s*([+-]?\d{2}):(\d{2})\).*?</profile-timezone>`)
)

// UserContext holds all fetched data for a user to avoid redundant API calls.
type UserContext struct {
	User                    *github.User
	GitHubTimezone          string // Profile timezone from GitHub profile UTC offset (e.g., "UTC-7")
	ProfileLocationTimezone string // Profile location timezone from geocoding location (e.g., "UTC-8" for Seattle in winter)
	FromCache               map[string]bool
	Username                string
	ProfileHTML             string
	PullRequests            []github.PullRequest
	StarredRepos            []github.Repository
	Repositories            []github.Repository
	Issues                  []github.Issue
	Comments                []github.Comment
	Gists                   []github.Gist // Full gist objects with descriptions
	Commits                 []time.Time
	CommitActivities        []github.CommitActivity // Enhanced commit data with repository info
	Organizations           []github.Organization
	Events                  []github.PublicEvent
	SSHKeys                 []github.SSHKey // SSH public keys with creation timestamps
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
			cache, err = httpcache.NewOtterCache(ctx, cacheDir, 14*24*time.Hour, logger)
			if err != nil {
				logger.Warn("cache initialization failed", "error", err, "cache_dir", cacheDir)
				// Cache is optional, continue without it
				cache = nil
			}
		}
	}

	detector := &Detector{
		githubToken:  optHolder.githubToken,
		mapsAPIKey:   optHolder.mapsAPIKey,
		geminiAPIKey: optHolder.geminiAPIKey,
		geminiModel:  optHolder.geminiModel,
		gcpProject:   optHolder.gcpProject,
		logger:       logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false, // Enable keep-alive for connection reuse
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // Explicitly requested to ignore SSL errors for personal websites
				},
			},
		},
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
// fetchPersonalWebsite fetches personal websites with minimal retries and ignoring SSL errors.
func (d *Detector) fetchPersonalWebsite(ctx context.Context, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var lastErr error

	err := retry.Do(
		func() error {
			var err error
			resp, err = d.httpClient.Do(req) //nolint:bodyclose // Body is closed by caller on success, closed here on error
			if err != nil {
				lastErr = err
				return err
			}

			// Don't retry on client errors (4xx) except rate limiting
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return nil // Don't retry client errors
			}

			// Check for rate limiting or server errors
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
				body, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				if readErr != nil {
					d.logger.Debug("failed to read error response body", "error", readErr)
				}
				if closeErr != nil {
					d.logger.Debug("failed to close error response body", "error", closeErr)
				}
				lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
				return lastErr
			}

			// Success - response body will be handled by caller
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(2),                 // Only try twice for personal websites
		retry.Delay(100*time.Millisecond), // Short delay between attempts
		retry.DelayType(retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)), // Add jitter
		retry.MaxJitter(50*time.Millisecond),                                       // Small jitter for personal websites
		retry.OnRetry(func(n uint, err error) {
			d.logger.Debug("retrying personal website fetch",
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
		return nil, fmt.Errorf("personal website fetch failed: %w", lastErr)
	}

	return resp, nil
}

func (d *Detector) retryableHTTPDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Give up after 15 seconds total
	deadline := time.Now().Add(15 * time.Second)
	var resp *http.Response
	var lastErr error

	err := retry.Do(
		func() error {
			// Check if we've exceeded the 15-second deadline
			if time.Now().After(deadline) {
				return retry.Unrecoverable(errors.New("timeout after 15 seconds"))
			}

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
				// Check if this is a secondary rate limit error
				bodyStr := string(body)
				isSecondaryRateLimit := strings.Contains(bodyStr, "secondary rate limit")

				if isSecondaryRateLimit {
					// Secondary rate limits need minutes to reset, but we only have 15 seconds
					d.logger.Warn("GitHub secondary rate limit detected, giving up due to 15-second timeout",
						"status", resp.StatusCode,
						"url", req.URL.String())
					return retry.Unrecoverable(fmt.Errorf("GitHub secondary rate limit (retry would exceed timeout): %s", bodyStr))
				} else {
					d.logger.Debug("retryable HTTP error",
						"status", resp.StatusCode,
						"url", req.URL.String(),
						"body", bodyStr)
				}
				return lastErr
			}

			// Success - response body will be handled by caller
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(10),                // More attempts with shorter delays
		retry.Delay(100*time.Millisecond), // Start with 100ms
		retry.MaxDelay(3*time.Second),     // Cap at 3 seconds to fit within 15-second limit
		retry.DelayType(retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)), // Exponential backoff with jitter
		retry.MaxJitter(100*time.Millisecond),                                      // Add up to 100ms of jitter
		retry.OnRetry(func(n uint, err error) {
			remainingTime := time.Until(deadline)
			d.logger.Info("retrying HTTP request",
				"attempt", n+1,
				"url", req.URL.String(),
				"remaining_time", remainingTime,
				"error", err)
		}),
		retry.RetryIf(func(err error) bool {
			// Don't retry if we've hit the deadline
			if time.Now().After(deadline) {
				return false
			}
			// Retry on network errors and rate limits (but not secondary rate limits)
			return err != nil && !strings.Contains(err.Error(), "retry would exceed timeout")
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
	result.SleepRangesLocal = activityResult.SleepRangesLocal
	result.SleepBucketsUTC = activityResult.SleepBucketsUTC
	result.ActiveHoursLocal = activityResult.ActiveHoursLocal
	result.ActiveHoursUTC = activityResult.ActiveHoursUTC
	result.LunchHoursUTC = activityResult.LunchHoursUTC
	result.LunchHoursLocal = activityResult.LunchHoursLocal
	result.PeakProductivityUTC = activityResult.PeakProductivityUTC
	result.PeakProductivityLocal = activityResult.PeakProductivityLocal
	result.TopOrganizations = activityResult.TopOrganizations
	result.HourlyActivityUTC = activityResult.HourlyActivityUTC
	result.HalfHourlyActivityUTC = activityResult.HalfHourlyActivityUTC
	result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
	result.TimezoneCandidates = activityResult.TimezoneCandidates
	result.ActivityDateRange = activityResult.ActivityDateRange

	// Calculate timezone offset for the new timezone (needed for recalculation)
	newOffset := offsetFromNamedTimezone(result.Timezone)

	// Always use the lunch times for the final chosen timezone
	// First check if we already calculated lunch for this timezone in our candidates
	// This is needed because Gemini might pick a named timezone like America/Los_Angeles
	// but our activity analysis used UTC-8, and they might have different lunch calculations
	if activityResult.HalfHourlyActivityUTC != nil && activityResult.TimezoneCandidates != nil {
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
					// Convert lunch hours from UTC to local
					result.LunchHoursLocal = struct {
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
					}{
						Start:      tzconvert.UTCToLocal(candidate.LunchStartUTC, newOffset),
						End:        tzconvert.UTCToLocal(candidate.LunchEndUTC, newOffset),
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
			// Convert lunch hours from UTC to local
			result.LunchHoursLocal = struct {
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
			}{
				Start:      tzconvert.UTCToLocal(lunchStart, newOffset),
				End:        tzconvert.UTCToLocal(lunchEnd, newOffset),
				Confidence: lunchConfidence,
			}
		}
	}

	// Recalculate LunchHoursLocal if we have UTC lunch hours but they weren't recalculated above
	if result.LunchHoursUTC.Start != 0 || result.LunchHoursUTC.End != 0 {
		// Check if lunch hours weren't already recalculated in the conditional block above
		if activityResult.HalfHourlyActivityUTC == nil || activityResult.TimezoneCandidates == nil {
			d.logger.Debug("recalculating LunchHoursLocal with correct offset",
				"timezone", result.Timezone,
				"offset", newOffset,
				"lunchStartUTC", result.LunchHoursUTC.Start,
				"lunchEndUTC", result.LunchHoursUTC.End)
			result.LunchHoursLocal = struct {
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
			}{
				Start:      tzconvert.UTCToLocal(result.LunchHoursUTC.Start, newOffset),
				End:        tzconvert.UTCToLocal(result.LunchHoursUTC.End, newOffset),
				Confidence: result.LunchHoursUTC.Confidence,
			}
		}
	}

	// Also recalculate ActiveHoursLocal and PeakProductivityLocal with the correct timezone offset
	if result.ActiveHoursUTC.Start != 0 || result.ActiveHoursUTC.End != 0 {
		d.logger.Debug("recalculating ActiveHoursLocal with correct offset",
			"timezone", result.Timezone,
			"offset", newOffset,
			"activeStartUTC", result.ActiveHoursUTC.Start,
			"activeEndUTC", result.ActiveHoursUTC.End)
		result.ActiveHoursLocal = struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		}{
			Start: tzconvert.UTCToLocal(result.ActiveHoursUTC.Start, newOffset),
			End:   tzconvert.UTCToLocal(result.ActiveHoursUTC.End, newOffset),
		}
	}

	// Recalculate PeakProductivityLocal with correct offset
	if result.PeakProductivityUTC.Start != 0 || result.PeakProductivityUTC.End != 0 {
		d.logger.Debug("recalculating PeakProductivityLocal with correct offset",
			"timezone", result.Timezone,
			"offset", newOffset,
			"peakStartUTC", result.PeakProductivityUTC.Start,
			"peakEndUTC", result.PeakProductivityUTC.End)
		result.PeakProductivityLocal = struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Count int     `json:"count"`
		}{
			Start: tzconvert.UTCToLocal(result.PeakProductivityUTC.Start, newOffset),
			End:   tzconvert.UTCToLocal(result.PeakProductivityUTC.End, newOffset),
			Count: result.PeakProductivityUTC.Count,
		}
	}

	// Recalculate SleepRangesLocal with correct timezone
	if len(result.SleepBucketsUTC) > 0 {
		d.logger.Debug("recalculating SleepRangesLocal with correct timezone",
			"timezone", result.Timezone,
			"sleepBucketsUTC", result.SleepBucketsUTC)
		result.SleepRangesLocal = CalculateSleepRangesFromBuckets(result.SleepBucketsUTC, result.Timezone)
	}
}

// fetchAllUserData fetches all data for a user at once to avoid redundant API calls.
func (d *Detector) fetchAllUserData(ctx context.Context, username string) (*UserContext, error) { //nolint:revive,maintidx // Long function but organized logically
	userCtx := &UserContext{
		Username:  username,
		FromCache: make(map[string]bool),
	}

	// STEP 1: First fetch profile HTML to verify username exists
	d.logger.Debug("checking profile HTML", "username", username)
	html := d.githubClient.FetchProfileHTML(ctx, username)
	userCtx.ProfileHTML = html

	// Check if user exists by looking for 404 indicators
	if strings.Contains(html, "Not Found") || strings.Contains(html, "This is not the web page you are looking for") {
		d.logger.Info("GitHub user not found", "username", username)
		return nil, github.ErrUserNotFound
	}

	var wg sync.WaitGroup
	var mu sync.Mutex     // For safe concurrent writes to userCtx
	var criticalErr error // Track critical errors that should fail the entire detection

	// STEP 2: Fetch user profile with GraphQL (includes social accounts)
	// This is second priority after HTML verification
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.logger.Debug("checking user profile with GraphQL", "username", username)
		user, err := d.githubClient.FetchUserEnhancedGraphQL(ctx, username)
		if err != nil {
			// No token is acceptable, we can work without it
			if errors.Is(err, github.ErrNoGitHubToken) {
				d.logger.Debug("no GitHub token available, will parse from HTML instead", "username", username)
				// Try to extract basic user info from HTML as fallback
				parsedUser := d.parseUserFromHTML(html, username)
				if parsedUser != nil {
					mu.Lock()
					userCtx.User = parsedUser
					mu.Unlock()
				}
				return
			}
			// Any other GraphQL error is critical - fail the detection
			d.logger.Error("GraphQL user profile fetch failed", "username", username, "error", err)
			mu.Lock()
			criticalErr = fmt.Errorf("unable to fetch GitHub profile: %w", err)
			mu.Unlock()
			return
		}
		mu.Lock()
		userCtx.User = user
		mu.Unlock()
	}()

	// STEP 3: Fetch other data in parallel after verification
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

// createdAtFromUser safely extracts the created_at time from a user, returning nil if not available.
func createdAtFromUser(user *github.User) *time.Time {
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

	// Extract GitHub profile timezone early if we have the HTML
	if userCtx.ProfileHTML != "" {
		d.extractGitHubTimezoneFromHTML(userCtx)
	}

	// Always perform activity analysis for fun and comparison
	d.logger.Debug("performing activity pattern analysis", "username", username)
	activityResult := d.tryActivityPatternsWithContext(ctx, userCtx)
	if activityResult == nil {
		d.logger.Error("activity pattern analysis returned nil unexpectedly",
			"username", username,
			"has_events", len(userCtx.Events) > 0,
			"has_user", userCtx.User != nil)
	}

	// Try quick detection methods first
	d.logger.Debug("trying profile HTML scraping", "username", username)
	if result := d.tryProfileScrapingWithContext(ctx, userCtx); result != nil {
		d.logger.Info("detected from profile HTML", "username", username, "timezone", result.Timezone)
		result.Name = fullName
		d.mergeActivityData(result, activityResult)
		// Add verification using centralized function
		result.Verification = d.createVerification(ctx, userCtx, result.Timezone,
			userCtx.ProfileLocationTimezone, result.ActivityTimezone, result.Location)
		return result, nil
	}
	d.logger.Debug("profile HTML scraping failed", "username", username)

	d.logger.Debug("trying location field analysis", "username", username)
	locationResult := d.tryLocationFieldWithContext(ctx, userCtx)
	if locationResult != nil {
		d.logger.Info("detected from location field", "username", username, "timezone", locationResult.Timezone, "location", locationResult.LocationName)
		locationResult.Name = fullName
		d.mergeActivityData(locationResult, activityResult)

		// Always run Gemini analysis for better detection, even when location field succeeds
		d.logger.Debug("running additional Gemini analysis for better detection", "username", username)
		geminiResult := d.tryUnifiedGeminiAnalysisWithContext(ctx, userCtx, activityResult)
		if geminiResult != nil {
			d.logger.Info("Gemini returned result", "timezone", geminiResult.Timezone)
			// Create verification using centralized function
			// Note: locationResult.Timezone is the timezone from geocoding the location field
			var activityTz string
			if activityResult != nil {
				activityTz = activityResult.Timezone
			}
			verification := d.createVerification(ctx, userCtx, geminiResult.Timezone,
				locationResult.Timezone, activityTz, geminiResult.Location)

			// Use Gemini's detected values as the actual result
			locationResult.Timezone = geminiResult.Timezone
			locationResult.Location = geminiResult.Location
			locationResult.LocationName = geminiResult.GeminiSuggestedLocation
			locationResult.GeminiSuggestedLocation = geminiResult.GeminiSuggestedLocation
			locationResult.GeminiSuspiciousMismatch = geminiResult.GeminiSuspiciousMismatch
			locationResult.GeminiMismatchReason = geminiResult.GeminiMismatchReason
			locationResult.GeminiReasoning = geminiResult.GeminiReasoning // Copy the reasoning for tooltip
			locationResult.GeminiPrompt = geminiResult.GeminiPrompt       // Copy the prompt for verbose mode
			// Preserve timezone candidates from activity analysis - Gemini doesn't generate these
			// locationResult.TimezoneCandidates already has the candidates from mergeActivityData
			locationResult.Method = "gemini_enhanced" // Update method to indicate Gemini enhanced the detection

			// Find the matching timezone candidate to use its pre-calculated values
			newOffset := tzconvert.ParseTimezoneOffset(locationResult.Timezone)
			d.logger.Info("looking for matching candidate after Gemini",
				"timezone", locationResult.Timezone,
				"offset", newOffset,
				"num_candidates", len(locationResult.TimezoneCandidates))

			// Try to find a matching candidate with the same offset
			candidateFound := false
			for i := range locationResult.TimezoneCandidates {
				candidate := &locationResult.TimezoneCandidates[i]
				if int(candidate.Offset) == newOffset {
					d.logger.Info("found matching candidate",
						"offset", newOffset,
						"lunch_local", candidate.LunchLocalTime)

					// Use the pre-calculated lunch hours from the candidate
					if candidate.LunchStartUTC > 0 && candidate.LunchEndUTC > 0 {
						locationResult.LunchHoursUTC = struct {
							Start      float64 `json:"start"`
							End        float64 `json:"end"`
							Confidence float64 `json:"confidence"`
						}{
							Start:      candidate.LunchStartUTC,
							End:        candidate.LunchEndUTC,
							Confidence: candidate.LunchConfidence,
						}
						locationResult.LunchHoursLocal = struct {
							Start      float64 `json:"start"`
							End        float64 `json:"end"`
							Confidence float64 `json:"confidence"`
						}{
							Start:      tzconvert.UTCToLocal(candidate.LunchStartUTC, newOffset),
							End:        tzconvert.UTCToLocal(candidate.LunchEndUTC, newOffset),
							Confidence: candidate.LunchConfidence,
						}
					}

					// Note: Work hours aren't stored in candidates, keep existing values

					candidateFound = true
					break
				}
			}

			if !candidateFound {
				d.logger.Info("no matching candidate found, keeping existing values",
					"offset", newOffset)
			}

			// Recalculate ActiveHoursLocal and PeakProductivityLocal with the Gemini-corrected timezone
			if locationResult.ActiveHoursUTC.Start != 0 || locationResult.ActiveHoursUTC.End != 0 {
				d.logger.Debug("recalculating ActiveHoursLocal after Gemini correction",
					"timezone", locationResult.Timezone,
					"offset", newOffset,
					"activeStartUTC", locationResult.ActiveHoursUTC.Start,
					"activeEndUTC", locationResult.ActiveHoursUTC.End)
				locationResult.ActiveHoursLocal = struct {
					Start float64 `json:"start"`
					End   float64 `json:"end"`
				}{
					Start: tzconvert.UTCToLocal(locationResult.ActiveHoursUTC.Start, newOffset),
					End:   tzconvert.UTCToLocal(locationResult.ActiveHoursUTC.End, newOffset),
				}
			}

			// Recalculate PeakProductivityLocal with Gemini-corrected offset
			if locationResult.PeakProductivityUTC.Start != 0 || locationResult.PeakProductivityUTC.End != 0 {
				d.logger.Debug("recalculating PeakProductivityLocal after Gemini correction",
					"timezone", locationResult.Timezone,
					"offset", newOffset,
					"peakStartUTC", locationResult.PeakProductivityUTC.Start,
					"peakEndUTC", locationResult.PeakProductivityUTC.End)
				locationResult.PeakProductivityLocal = struct {
					Start float64 `json:"start"`
					End   float64 `json:"end"`
					Count int     `json:"count"`
				}{
					Start: tzconvert.UTCToLocal(locationResult.PeakProductivityUTC.Start, newOffset),
					End:   tzconvert.UTCToLocal(locationResult.PeakProductivityUTC.End, newOffset),
					Count: locationResult.PeakProductivityUTC.Count,
				}
			}

			// Recalculate SleepRangesLocal with Gemini-corrected timezone
			if len(locationResult.SleepBucketsUTC) > 0 {
				d.logger.Debug("recalculating SleepRangesLocal after Gemini correction",
					"timezone", locationResult.Timezone,
					"sleepBucketsUTC", locationResult.SleepBucketsUTC)
				locationResult.SleepRangesLocal = CalculateSleepRangesFromBuckets(locationResult.SleepBucketsUTC, locationResult.Timezone)
			}

			// Add location distance calculation if coordinates are available
			if verification.ProfileLocation != "" && geminiResult.Location != nil && locationResult.Location != nil {
				distance := haversineDistance(locationResult.Location.Latitude, locationResult.Location.Longitude,
					geminiResult.Location.Latitude, geminiResult.Location.Longitude)
				if distance > 0 {
					verification.LocationDistanceKm = distance
					if distance > 1000 {
						verification.LocationMismatch = "major"
					} else if distance > 400 {
						verification.LocationMismatch = "minor"
					}
				}
			}

			locationResult.Verification = verification
		} else {
			d.logger.Info("Gemini returned nil result")

			// When Gemini isn't available, prefer: profile timezone > profile location timezone > activity timezone
			// Check if we should use profile timezone or activity timezone instead of location
			if userCtx.GitHubTimezone != "" {
				// Profile timezone takes highest priority
				d.logger.Info("using profile timezone when Gemini unavailable",
					"profile_tz", userCtx.GitHubTimezone,
					"location_tz", locationResult.Timezone)

				// Parse the profile timezone to a standard format if needed
				profileTz := userCtx.GitHubTimezone
				locationResult.Timezone = profileTz

				// Recalculate all local times with the profile timezone
				newOffset := offsetFromNamedTimezone(profileTz)

				// Recalculate ActiveHoursLocal
				if locationResult.ActiveHoursUTC.Start != 0 || locationResult.ActiveHoursUTC.End != 0 {
					locationResult.ActiveHoursLocal = struct {
						Start float64 `json:"start"`
						End   float64 `json:"end"`
					}{
						Start: tzconvert.UTCToLocal(locationResult.ActiveHoursUTC.Start, newOffset),
						End:   tzconvert.UTCToLocal(locationResult.ActiveHoursUTC.End, newOffset),
					}
				}

				// Recalculate PeakProductivityLocal
				if locationResult.PeakProductivityUTC.Start != 0 || locationResult.PeakProductivityUTC.End != 0 {
					locationResult.PeakProductivityLocal = struct {
						Start float64 `json:"start"`
						End   float64 `json:"end"`
						Count int     `json:"count"`
					}{
						Start: tzconvert.UTCToLocal(locationResult.PeakProductivityUTC.Start, newOffset),
						End:   tzconvert.UTCToLocal(locationResult.PeakProductivityUTC.End, newOffset),
						Count: locationResult.PeakProductivityUTC.Count,
					}
				}

				// Recalculate LunchHoursLocal
				if locationResult.LunchHoursUTC.Start != 0 || locationResult.LunchHoursUTC.End != 0 {
					locationResult.LunchHoursLocal = struct {
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
					}{
						Start:      tzconvert.UTCToLocal(locationResult.LunchHoursUTC.Start, newOffset),
						End:        tzconvert.UTCToLocal(locationResult.LunchHoursUTC.End, newOffset),
						Confidence: locationResult.LunchHoursUTC.Confidence,
					}
				}

				// Recalculate SleepRangesLocal
				if len(locationResult.SleepBucketsUTC) > 0 {
					d.logger.Debug("recalculating SleepRangesLocal with profile timezone",
						"timezone", profileTz,
						"sleepBucketsUTC", locationResult.SleepBucketsUTC)
					locationResult.SleepRangesLocal = CalculateSleepRangesFromBuckets(locationResult.SleepBucketsUTC, profileTz)
				}
			}
			// Profile location timezone (from geocoding) is already set as locationResult.Timezone
			// Activity timezone is third priority and is already in activityResult if we need to fall back to it
		}

		if geminiResult != nil {
			d.logger.Info("Gemini enhanced location detection",
				"claimed", userCtx.User.Location,
				"detected", geminiResult.GeminiSuggestedLocation,
				"timezone", geminiResult.Timezone,
				"suspicious", geminiResult.GeminiSuspiciousMismatch,
				"mismatch_reason", geminiResult.GeminiMismatchReason)
		}

		// Add verification if we didn't already add it above
		if locationResult.Verification == nil {
			activityTz := ""
			if activityResult != nil {
				activityTz = activityResult.Timezone
			}
			locationResult.Verification = d.createVerification(ctx, userCtx, locationResult.Timezone,
				locationResult.Timezone, activityTz, locationResult.Location)
		}
		return locationResult, nil
	}
	d.logger.Debug("location field analysis failed", "username", username)

	d.logger.Debug("trying Gemini analysis with contextual data", "username", username, "has_activity_data", activityResult != nil)
	if result := d.tryUnifiedGeminiAnalysisWithContext(ctx, userCtx, activityResult); result != nil {
		result.Name = fullName
		// Use mergeActivityData to properly handle lunch time reuse from candidates
		d.mergeActivityData(result, activityResult)
		// Add verification using centralized function
		activityTz := ""
		if activityResult != nil {
			activityTz = activityResult.Timezone
		}
		result.Verification = d.createVerification(ctx, userCtx, result.Timezone,
			userCtx.ProfileLocationTimezone, activityTz, result.Location)
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
		// Add verification for activity-only result
		activityResult.Verification = d.createVerification(ctx, userCtx, activityResult.Timezone,
			userCtx.ProfileLocationTimezone, activityResult.Timezone, activityResult.Location)
		return activityResult, nil
	}

	return nil, fmt.Errorf("could not determine timezone for %s", username)
}

// createVerification creates a consistent VerificationResult from the various timezone sources.
// This centralizes the logic to avoid duplication and ensure consistent handling.
func (d *Detector) createVerification(ctx context.Context, userCtx *UserContext, detectedTimezone string, locationTimezone string, activityTimezone string, detectedLocation *Location) *VerificationResult {
	if userCtx == nil || userCtx.User == nil {
		return nil
	}

	verification := &VerificationResult{
		ProfileLocation:         userCtx.User.Location,
		ProfileTimezone:         userCtx.GitHubTimezone,          // From GitHub profile UTC offset
		ProfileLocationTimezone: userCtx.ProfileLocationTimezone, // From geocoding the location field
	}

	// If ProfileLocationTimezone is empty but we have a locationTimezone from geocoding, use it
	if verification.ProfileLocationTimezone == "" && locationTimezone != "" {
		verification.ProfileLocationTimezone = locationTimezone
	}

	// Calculate difference between profile timezone and profile location timezone
	if verification.ProfileTimezone != "" && verification.ProfileLocationTimezone != "" {
		offsetDiff := d.calculateTimezoneOffsetDiff(verification.ProfileTimezone, verification.ProfileLocationTimezone)
		if offsetDiff != 0 {
			if offsetDiff < 0 {
				verification.ProfileLocationDiff = -offsetDiff
			} else {
				verification.ProfileLocationDiff = offsetDiff
			}
		}
	}

	// Calculate difference between detected timezone and profile timezone
	if detectedTimezone != "" && verification.ProfileTimezone != "" && detectedTimezone != verification.ProfileTimezone {
		offsetDiff := d.calculateTimezoneOffsetDiff(verification.ProfileTimezone, detectedTimezone)
		if offsetDiff != 0 {
			absOffsetDiff := offsetDiff
			if absOffsetDiff < 0 {
				absOffsetDiff = -absOffsetDiff
			}
			verification.TimezoneOffsetDiff = absOffsetDiff
			if absOffsetDiff > 3 {
				verification.TimezoneMismatch = "major"
			} else if absOffsetDiff > 1 {
				verification.TimezoneMismatch = "minor"
			}
		}
	}

	// Calculate location distance if we have both profile location and detected location
	if verification.ProfileLocation != "" && detectedLocation != nil {
		distance := d.calculateLocationDistanceFromCoords(ctx, verification.ProfileLocation,
			detectedLocation.Latitude, detectedLocation.Longitude)
		if distance > 0 {
			verification.LocationDistanceKm = distance
			if distance > 1000 {
				verification.LocationMismatch = "major"
			} else if distance > 400 {
				verification.LocationMismatch = "minor"
			}
		}
	}

	return verification
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

	// Use a custom retry for personal websites with only 2 attempts and short delay
	resp, err := d.fetchPersonalWebsite(ctx, req)
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

// parseUserFromHTML extracts user information from GitHub profile HTML.
// This is used as a fallback when GraphQL is not available (no token).
func (d *Detector) parseUserFromHTML(html string, username string) *github.User {
	if html == "" {
		return nil
	}

	user := &github.User{
		Login: username,
	}

	// Extract name - look for itemprop="name"
	nameRegex := regexp.MustCompile(`<span[^>]*itemprop="name"[^>]*>([^<]+)</span>`)
	if matches := nameRegex.FindStringSubmatch(html); len(matches) > 1 {
		user.Name = strings.TrimSpace(matches[1])
	}

	// Extract location - look for itemprop="homeLocation"
	locationRegex := regexp.MustCompile(`<span[^>]*itemprop="homeLocation"[^>]*>([^<]+)</span>`)
	if matches := locationRegex.FindStringSubmatch(html); len(matches) > 1 {
		user.Location = strings.TrimSpace(matches[1])
	}

	// Extract bio - look for the bio element
	bioRegex := regexp.MustCompile(`<div[^>]*data-bio-text[^>]*>([^<]+)</div>`)
	if matches := bioRegex.FindStringSubmatch(html); len(matches) > 1 {
		user.Bio = strings.TrimSpace(matches[1])
	}

	// Extract company - look for itemprop="worksFor"
	companyRegex := regexp.MustCompile(`<span[^>]*itemprop="worksFor"[^>]*>([^<]+)</span>`)
	if matches := companyRegex.FindStringSubmatch(html); len(matches) > 1 {
		user.Company = strings.TrimSpace(matches[1])
	}

	// Extract blog/website - look for itemprop="url"
	blogRegex := regexp.MustCompile(`<a[^>]*itemprop="url"[^>]*href="([^"]+)"`)
	if matches := blogRegex.FindStringSubmatch(html); len(matches) > 1 {
		user.Blog = strings.TrimSpace(matches[1])
	}

	// Extract social accounts - look for itemprop="social" links
	// These include Twitter, Mastodon, LinkedIn, etc.
	socialRegex := regexp.MustCompile(`<li[^>]*itemprop="social"[^>]*>.*?<a[^>]*href="([^"]+)"[^>]*>([^<]*)</a>`)
	socialMatches := socialRegex.FindAllStringSubmatch(html, -1)

	var socialAccounts []github.SocialAccount
	for _, match := range socialMatches {
		if len(match) > 1 {
			socialURL := strings.TrimSpace(match[1])

			// Determine provider from URL
			var provider string
			switch {
			case strings.Contains(socialURL, "twitter.com"):
				provider = "twitter"
			case strings.Contains(socialURL, "mastodon"):
				provider = "mastodon"
			case strings.Contains(socialURL, "linkedin.com"):
				provider = "linkedin"
			case strings.Contains(socialURL, "facebook.com"):
				provider = "facebook"
			case strings.Contains(socialURL, "instagram.com"):
				provider = "instagram"
			case strings.Contains(socialURL, "youtube.com"):
				provider = "youtube"
			default:
				provider = "generic"
			}

			socialAccounts = append(socialAccounts, github.SocialAccount{
				Provider: provider,
				URL:      socialURL,
			})
		}
	}
	user.SocialAccounts = socialAccounts

	d.logger.Debug("parsed user from HTML",
		"username", username,
		"name", user.Name,
		"location", user.Location,
		"company", user.Company,
		"blog", user.Blog,
		"social_accounts", len(socialAccounts))

	return user
}

// extractGitHubTimezoneFromHTML extracts the GitHub profile timezone from HTML and stores it in userCtx.
// This is separate from tryProfileScrapingWithContext so we can extract the timezone early.
func (d *Detector) extractGitHubTimezoneFromHTML(userCtx *UserContext) {
	html := userCtx.ProfileHTML
	if html == "" {
		return
	}

	// First check for GitHub's profile-timezone element (most reliable)
	d.logger.Debug("checking for GitHub profile timezone element", "username", userCtx.Username, "html_length", len(html))

	// Log if we find the profile-timezone element at all
	if strings.Contains(html, "profile-timezone") {
		d.logger.Debug("found profile-timezone element in HTML", "username", userCtx.Username)
		// Extract a sample around it for debugging
		if idx := strings.Index(html, "profile-timezone"); idx >= 0 {
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + 200
			if end > len(html) {
				end = len(html)
			}
			d.logger.Debug("profile-timezone context", "username", userCtx.Username, "sample", html[start:end])
		}
	} else {
		d.logger.Debug("no profile-timezone element found in HTML", "username", userCtx.Username)
	}

	if matches := profileTimezoneRegex.FindStringSubmatch(html); len(matches) > 1 {
		hoursStr := strings.TrimSpace(matches[1])
		d.logger.Debug("found data-hours-ahead-of-utc attribute", "username", userCtx.Username, "value", hoursStr)
		if hoursStr != "" { // Only process if not empty
			if hours, err := strconv.ParseFloat(hoursStr, 64); err == nil {
				// Convert hours to UTC offset format
				// GitHub uses negative for west of UTC (e.g., -12.0 for UTC-12)
				// Handle fractional hours (e.g., India is UTC+5.5, Nepal is UTC+5.75, Newfoundland is UTC-2.5)
				var tz string
				switch {
				case hours == 0:
					tz = "UTC"
				case hours < 0:
					// Check if it's a fractional hour
					if hours != float64(int(hours)) {
						// Format with decimal for fractional hours
						tz = fmt.Sprintf("UTC%.1f", hours)
					} else {
						tz = fmt.Sprintf("UTC%d", int(hours))
					}
				default:
					// Positive offset
					if hours != float64(int(hours)) {
						// Format with decimal for fractional hours
						tz = fmt.Sprintf("UTC+%.1f", hours)
					} else {
						tz = fmt.Sprintf("UTC+%d", int(hours))
					}
				}

				// Store the GitHub timezone in userCtx for verification
				userCtx.GitHubTimezone = tz

				// Don't return this as the detected timezone - it's the user's profile timezone
				// But we'll use it for verification later
				d.logger.Debug("found GitHub profile timezone", "username", userCtx.Username, "timezone", tz, "hours_offset", hoursStr)
			}
		} else {
			d.logger.Debug("data-hours-ahead-of-utc was empty, trying fallback", "username", userCtx.Username)
		}
	} else {
		d.logger.Debug("no profile-timezone element found", "username", userCtx.Username)
	}

	// Fallback: If data-hours-ahead-of-utc was empty, try parsing the text content
	if userCtx.GitHubTimezone == "" {
		d.logger.Debug("trying fallback regex for timezone text", "username", userCtx.Username)
		if matches := profileTimezoneTextRegex.FindStringSubmatch(html); len(matches) > 2 {
			hoursStr := strings.TrimSpace(matches[1])
			minutesStr := strings.TrimSpace(matches[2])
			if hours, err := strconv.Atoi(hoursStr); err == nil {
				if minutes, err := strconv.Atoi(minutesStr); err == nil {
					// Convert to decimal hours
					decimalHours := float64(hours) + float64(minutes)/60.0
					if hours < 0 {
						decimalHours = float64(hours) - float64(minutes)/60.0
					}

					// Format the timezone string
					var tz string
					switch {
					case decimalHours == 0:
						tz = "UTC"
					case decimalHours == float64(int(decimalHours)):
						// Whole number of hours
						if decimalHours > 0 {
							tz = fmt.Sprintf("UTC+%d", int(decimalHours))
						} else {
							tz = fmt.Sprintf("UTC%d", int(decimalHours))
						}
					default:
						// Fractional hours
						if decimalHours > 0 {
							tz = fmt.Sprintf("UTC+%.1f", decimalHours)
						} else {
							tz = fmt.Sprintf("UTC%.1f", decimalHours)
						}
					}

					userCtx.GitHubTimezone = tz
					d.logger.Debug("found GitHub profile timezone from text", "username", userCtx.Username, "timezone", tz, "hours", hoursStr, "minutes", minutesStr)
				}
			}
		}
	}
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

	// GitHub timezone extraction has already been done in extractGitHubTimezoneFromHTML

	// Try extracting timezone from HTML using other pre-compiled regex patterns
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
		CreatedAt:    createdAtFromUser(userCtx.User),
	}
}

// formatEvidenceForGemini formats contextual data into a readable, structured format for Gemini analysis.
