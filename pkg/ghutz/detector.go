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

type Detector struct {
	githubToken   string
	mapsAPIKey    string
	geminiAPIKey  string
	geminiModel   string
	gcpProject    string
	logger        *slog.Logger
	httpClient    *http.Client
	forceActivity bool
	cache         *DiskCache
}

// retryableHTTPDo performs an HTTP request with exponential backoff and jitter
func (d *Detector) retryableHTTPDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var lastErr error
	
	err := retry.Do(
		func() error {
			var err error
			resp, err = d.httpClient.Do(req.WithContext(ctx))
			if err != nil {
				// Network errors are retryable
				lastErr = err
				return err
			}
			
			// Check for rate limiting or server errors
			if resp.StatusCode == 429 || resp.StatusCode == 403 || resp.StatusCode >= 500 {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
				d.logger.Debug("retryable HTTP error", 
					"status", resp.StatusCode, 
					"url", req.URL.String(),
					"body", string(body))
				return lastErr
			}
			
			// Success or non-retryable error
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.BackOffDelay),
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

func New(opts ...Option) *Detector {
	return NewWithLogger(slog.Default(), opts...)
}

func NewWithLogger(logger *slog.Logger, opts ...Option) *Detector {
	optHolder := &OptionHolder{}
	for _, opt := range opts {
		opt(optHolder)
	}
	
	// Initialize cache
	var cache *DiskCache
	if userCacheDir, err := os.UserCacheDir(); err == nil {
		cacheDir := filepath.Join(userCacheDir, "ghutz")
		cache, err = NewDiskCache(cacheDir, 7*24*time.Hour, logger)
		if err != nil {
			logger.Debug("cache initialization failed", "error", err)
			// Cache is optional, continue without it
			cache = nil
		}
	} else {
		logger.Debug("could not determine user cache directory", "error", err)
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
	
	validUsername := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)
	if !validUsername.MatchString(username) {
		return nil, fmt.Errorf("invalid username format")
	}
	
	d.logger.Info("detecting timezone", "username", username)
	
	// Perform activity analysis if flag is set (for comparison)
	var activityResult *Result
	if d.forceActivity {
		d.logger.Debug("performing activity pattern analysis", "username", username)
		activityResult = d.tryActivityPatterns(ctx, username)
	}
	
	// Try quick detection methods first
	{
		d.logger.Debug("trying profile HTML scraping", "username", username)
		if result := d.tryProfileScraping(ctx, username); result != nil {
			d.logger.Info("detected from profile HTML", "username", username, "timezone", result.Timezone)
			// Add activity data if we have it
			if activityResult != nil {
				result.ActivityTimezone = activityResult.ActivityTimezone
				result.QuietHoursUTC = activityResult.QuietHoursUTC
				result.ActiveHoursLocal = activityResult.ActiveHoursLocal
			}
			return result, nil
		}
		d.logger.Debug("profile HTML scraping failed", "username", username)
		
		d.logger.Debug("trying location field analysis", "username", username)
		if result := d.tryLocationField(ctx, username); result != nil {
			d.logger.Info("detected from location field", "username", username, "timezone", result.Timezone, "location", result.LocationName)
			// Add activity data if we have it
			if activityResult != nil {
				result.ActivityTimezone = activityResult.ActivityTimezone
				result.QuietHoursUTC = activityResult.QuietHoursUTC
				result.ActiveHoursLocal = activityResult.ActiveHoursLocal
			}
			return result, nil
		}
		d.logger.Debug("location field analysis failed", "username", username)
	}
	
	// Now try activity patterns if we haven't already
	if activityResult == nil {
		d.logger.Debug("trying activity pattern analysis", "username", username)
		activityResult = d.tryActivityPatterns(ctx, username)
	}
	
	d.logger.Debug("trying Gemini analysis with contextual data", "username", username, "has_activity_data", activityResult != nil)
	if result := d.tryUnifiedGeminiAnalysis(ctx, username, activityResult); result != nil {
		if activityResult != nil {
			// Preserve activity data in the final result
			result.ActivityTimezone = activityResult.ActivityTimezone
			result.QuietHoursUTC = activityResult.QuietHoursUTC
			result.ActiveHoursLocal = activityResult.ActiveHoursLocal
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
		return activityResult, nil
	}
	
	return nil, fmt.Errorf("could not determine timezone for %s", username)
}

func (d *Detector) tryProfileScraping(ctx context.Context, username string) *Result {
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

func (d *Detector) tryLocationField(ctx context.Context, username string) *Result {
	user := d.fetchUser(ctx, username)
	if user == nil || user.Location == "" {
		d.logger.Debug("no location field found", "username", username)
		return nil
	}
	
	d.logger.Debug("analyzing location field", "username", username, "location", user.Location)
	
	if d.isLocationTooVague(user.Location) {
		d.logger.Debug("location too vague for geocoding", "username", username, "location", user.Location)
		return nil
	}
	
	coords, err := d.geocodeLocation(ctx, user.Location)
	if err != nil {
		d.logger.Debug("geocoding failed", "username", username, "location", user.Location, "error", err)
		return nil
	}
	
	d.logger.Debug("geocoded location", "username", username, "location", user.Location, 
		"latitude", coords.Latitude, "longitude", coords.Longitude)
	
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


// tryUnifiedGeminiAnalysis uses Gemini with all available data (activity + context) in a single call
func (d *Detector) tryUnifiedGeminiAnalysis(ctx context.Context, username string, activityResult *Result) *Result {
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
			"title":      pr.Title,
			"created_at": pr.CreatedAt,
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
		
		// Add timezone candidates based on the detected offset
		if strings.HasPrefix(activityResult.Timezone, "UTC") {
			// Extract offset from UTC format (e.g., "UTC+5", "UTC-8")
			offsetStr := strings.TrimPrefix(activityResult.Timezone, "UTC")
			if offset, err := strconv.Atoi(offsetStr); err == nil {
				candidates := getTimezoneCandidatesForOffset(float64(offset))
				if len(candidates) > 0 {
					contextData["timezone_candidates"] = candidates
				}
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
	
	return &Result{
		Username:                username,
		Timezone:                timezone,
		GeminiSuggestedLocation: location,
		Confidence:              confidence,
		Method:                  method,
	}
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
	
	quietHours := findQuietHours(hourCounts)
	if len(quietHours) < 4 {
		d.logger.Debug("insufficient quiet hours", "username", username, "quiet_hours", len(quietHours))
		return nil
	}
	
	d.logger.Debug("activity pattern summary", "username", username, 
		"total_activity", totalActivity,
		"quiet_hours", quietHours, 
		"most_active_hours", mostActiveHours,
		"max_activity_count", maxActivity)
	
	hourlyActivity := make([]int, 24)
	for hour := 0; hour < 24; hour++ {
		hourlyActivity[hour] = hourCounts[hour]
	}
	d.logger.Debug("hourly activity distribution", "username", username, "hours_utc", hourlyActivity)
	
	// Find the middle of quiet hours, handling wrap-around
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
		"quiet_hours", quietHours,
		"mid_quiet_utc", midQuiet,
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
	
	return &Result{
		Username:         username,
		Timezone:         timezone,
		ActivityTimezone: timezone, // Pure activity-based result
		QuietHoursUTC:    quietHours,
		ActiveHoursLocal: struct {
			Start int `json:"start"`
			End   int `json:"end"`
		}{
			Start: activeStart,
			End:   activeEnd,
		},
		Confidence: 0.8,
		Method:     "activity_patterns",
	}
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
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	if d.githubToken != "" {
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
		body, _ := io.ReadAll(resp.Body)
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
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	if d.githubToken != "" {
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
		body, _ := io.ReadAll(resp.Body)
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
		body, _ := io.ReadAll(resp.Body)
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
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	
	if d.githubToken != "" {
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
		body, _ := io.ReadAll(resp.Body)
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
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	
	if d.githubToken != "" {
		req.Header.Set("Authorization", "token "+d.githubToken)
	}
	
	resp, err := d.cachedHTTPDo(ctx, req)
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

func (d *Detector) geocodeLocation(ctx context.Context, location string) (*Location, error) {
	if d.mapsAPIKey == "" {
		return nil, fmt.Errorf("Google Maps API key not configured")
	}
	
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
		return "", fmt.Errorf("Google Maps API key not configured")
	}
	
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

func (d *Detector) isLocationTooVague(location string) bool {
	location = strings.ToLower(strings.TrimSpace(location))
	
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
	// Return a simple UTC offset format for activity-only detection
	// This avoids making assumptions about specific locations
	if offsetHours >= 0 {
		return fmt.Sprintf("UTC+%d", offsetHours)
	}
	return fmt.Sprintf("UTC%d", offsetHours) // Negative sign is already included
}

func getTimezoneCandidatesForOffset(offsetHours float64) []string {
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
	activityTimezone := ""
	hasActivityData := false
	if tz, ok := contextData["activity_detected_timezone"].(string); ok && tz != "" {
		activityTimezone = tz
		hasActivityData = true
	}
	
	var prompt string
	if hasActivityData {
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
- Pull request titles and issue body text (may contain location/conference references, language patterns)
- Website/blog content (may contain explicit location info, language, regional context)
- Username patterns that might indicate nationality/region (e.g., "wojciechka" suggests Polish origin)
- Company location vs user location (remote work is common)
- Language patterns in PR/issue text (American vs British English spelling, local idioms)
- Known tech company locations (e.g., Chainguard is US-based)

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

SPECIAL CASES TO WATCH:
- If location says "Canada" but activity suggests Europe, they likely moved to Europe or work European hours
- Canada spans UTC-3.5 to UTC-8, so Europe/Berlin (UTC+1/+2) is incompatible - trust the activity
- If someone at Google has "Canada" location but European activity, they're likely in Europe now
- For Etc/GMT timezones, these are rarely real - consider if the person might have unusual hours in a standard timezone

IMPORTANT: For Etc/GMT timezones (like Etc/GMT-4), these are generic offset-based zones. Please suggest the most likely specific city or region based on:
- The user's name/username origin (e.g., Polish names often indicate Poland location)
- Company locations that match this offset
- Common tech hubs in this timezone
- Any contextual clues from their activity

Note: If timezone_candidates are provided in the context, these are the common timezones that match the detected UTC offset. Consider which is most likely based on the user's name, company, and other context.

Respond with your reasoning followed by your conclusion on separate lines:
"TIMEZONE:" followed by ONLY a valid IANA timezone identifier or "UNKNOWN"
"LOCATION:" followed by a specific location name (city, region) that best matches the timezone and context, or "UNKNOWN"

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
- Pull request titles and issue body text (may contain location/conference references, language patterns)
- Website/blog content (may contain explicit location info, language, regional context)
- Username patterns that might indicate nationality/region (e.g., "wojciechka" suggests Polish origin)
- Company location vs user location (remote work is common)
- Language patterns in PR/issue text (American vs British English spelling, local idioms)
- Known tech company locations (e.g., Chainguard is US-based)

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

IMPORTANT: For Etc/GMT timezones (like Etc/GMT-4), these are generic offset-based zones. Please suggest the most likely specific city or region based on:
- The user's name/username origin (e.g., Polish names often indicate Poland location)
- Company locations that match this offset
- Common tech hubs in this timezone
- Any contextual clues from their activity

Note: If timezone_candidates are provided in the context, these are the common timezones that match the detected UTC offset. Consider which is most likely based on the user's name, company, and other context.

Respond with your reasoning followed by your conclusion on separate lines:
"TIMEZONE:" followed by ONLY a valid IANA timezone identifier or "UNKNOWN"
"LOCATION:" followed by a specific location name (city, region) that best matches the timezone and context, or "UNKNOWN"

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
	
	d.logger.Debug("Gemini full response", "full_response", fullResponse)
	
	var timezone, location string
	if strings.Contains(fullResponse, "TIMEZONE:") {
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
		timezone = fullResponse
	}
	
	d.logger.Debug("Gemini unified response", "extracted_timezone", timezone, "extracted_location", location, "had_activity_data", hasActivityData, "verbose_mode", verbose)
	if hasActivityData {
		d.logger.Debug("Gemini activity comparison", "original_activity_timezone", activityTimezone, "gemini_response", timezone, "gemini_location", location)
	}
	
	d.logger.Debug("Gemini unified context", "context_json", string(contextJSON))
	
	if timezone == "UNKNOWN" || timezone == "" {
		return "", "", 0, nil
	}
	
	if location == "UNKNOWN" {
		location = ""
	}
	
	if !strings.Contains(timezone, "/") {
		d.logger.Debug("invalid timezone format from unified Gemini", "timezone", timezone)
		return "", "", 0, nil
	}
	
	_, err = time.LoadLocation(timezone)
	if err != nil {
		d.logger.Debug("invalid timezone from unified Gemini", "timezone", timezone, "error", err)
		return "", "", 0, nil
	}
	
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


func (d *Detector) fetchWebsiteContent(ctx context.Context, blogURL string) string {
	if blogURL == "" {
		return ""
	}
	
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
	
	body := make([]byte, 50*1024)
	n, _ := io.ReadFull(resp.Body, body)
	content := string(body[:n])
	
	content = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(content, " ")
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	content = strings.TrimSpace(content)
	
	if len(content) > 2000 {
		content = content[:2000] + "..."
	}
	
	d.logger.Debug("fetched website content", "url", blogURL, "content_length", len(content))
	return content
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}