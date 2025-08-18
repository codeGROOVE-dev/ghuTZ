package ghutz

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/gemini"
	"github.com/codeGROOVE-dev/ghuTZ/pkg/github"
)

// geminiQueryResult holds the result from a Gemini API query
type geminiQueryResult struct {
	Timezone   string
	Reasoning  string
	Confidence float64
	Location   string
	Prompt     string
}

// queryUnifiedGeminiForTimezone queries Gemini AI for timezone detection.
func (d *Detector) queryUnifiedGeminiForTimezone(ctx context.Context, contextData map[string]interface{}, verbose bool) (*geminiQueryResult, error) {
	// Check if we have activity data for confidence scoring later
	hasActivityData := false
	if hourCounts, ok := contextData["hour_counts"].(map[int]int); ok && len(hourCounts) > 0 {
		hasActivityData = true
	}

	// Format all evidence into a comprehensive prompt
	evidence := d.formatEvidenceForGemini(contextData)

	// Use the unified prompt template and inject evidence
	promptTemplate := gemini.UnifiedPrompt()
	prompt := fmt.Sprintf(promptTemplate, evidence)

	// Verbose prompt display removed - now handled in main CLI

	// Pass verbose if DEBUG logging is enabled
	isVerbose := d.logger.Enabled(ctx, slog.LevelDebug)
	
	// Create Gemini client and call API
	client := gemini.NewClient(d.geminiAPIKey, d.geminiModel, d.gcpProject)
	resp, err := client.CallWithSDK(ctx, prompt, isVerbose, d.cache, d.logger)
	if err != nil {
		return nil, fmt.Errorf("Gemini API call failed: %w", err)
	}

	// Handle both new and old response formats
	timezone := resp.DetectedTimezone
	if timezone == "" {
		timezone = resp.Timezone // Fallback to old field
	}
	
	location := resp.DetectedLocation
	if location == "" {
		location = resp.LocationSource // Fallback to old field
		if location == "" {
			location = resp.Location // Another fallback
		}
	}
	
	reasoning := resp.DetectionReasoning
	if reasoning == "" {
		reasoning = resp.Reasoning // Fallback to old field
	}
	
	// Parse confidence from string or number
	confidence := 0.5
	if resp.ConfidenceLevel != "" {
		switch strings.ToLower(resp.ConfidenceLevel) {
		case "high":
			confidence = 0.85
		case "medium":
			confidence = 0.6
		case "low":
			confidence = 0.3
		default:
			confidence = 0.5
		}
	} else if resp.Confidence != nil {
		// Handle old format with numeric confidence
		switch v := resp.Confidence.(type) {
		case float64:
			confidence = v
		case string:
			switch strings.ToLower(v) {
			case "high":
				confidence = 0.85
			case "medium":
				confidence = 0.6
			case "low":
				confidence = 0.3
			}
		}
	}
	
	// Verbose response display removed - now handled in main CLI

	// Adjust confidence based on data availability
	if !hasActivityData && confidence > 0.7 {
		// Without activity data, cap confidence at 70%
		confidence = 0.7
	}

	// If we have strong activity patterns, boost confidence slightly
	if hasActivityData && confidence < 0.9 {
		confidence = math.Min(0.9, confidence*1.1)
	}

	// Return the result
	return &geminiQueryResult{
		Timezone:   timezone,
		Reasoning:  reasoning,
		Confidence: confidence,
		Location:   location,
		Prompt:     prompt,
	}, nil
}


// tryUnifiedGeminiAnalysisWithContext attempts timezone detection using Gemini AI with UserContext
func (d *Detector) tryUnifiedGeminiAnalysisWithContext(ctx context.Context, userCtx *UserContext, activityResult *Result) *Result {
	// Skip if no Gemini API key
	if d.geminiAPIKey == "" {
		d.logger.Debug("skipping Gemini analysis - no API key configured")
		return nil
	}

	if userCtx.User == nil {
		d.logger.Debug("could not fetch user for Gemini analysis", "username", userCtx.Username)
		return nil
	}

	// Prepare comprehensive context for Gemini
	contextData := make(map[string]interface{})
	contextData["user"] = userCtx.User
	contextData["recent_events"] = userCtx.Events

	// Add activity result if available
	if activityResult != nil {
		contextData["activity_result"] = activityResult
		
		if activityResult.QuietHoursUTC != nil {
			contextData["quiet_hours"] = activityResult.QuietHoursUTC
		}
		
		if activityResult.HourlyActivityUTC != nil {
			contextData["hour_counts"] = activityResult.HourlyActivityUTC
		}
		
		if activityResult.TimezoneCandidates != nil && len(activityResult.TimezoneCandidates) > 0 {
			contextData["timezone_candidates"] = activityResult.TimezoneCandidates
		}
		
		if activityResult.ActivityDateRange.TotalDays > 0 {
			totalEvents := 0
			if activityResult.HourlyActivityUTC != nil {
				for _, count := range activityResult.HourlyActivityUTC {
					totalEvents += count
				}
			}
			
			contextData["activity_date_range"] = map[string]interface{}{
				"oldest":      activityResult.ActivityDateRange.OldestActivity,
				"newest":      activityResult.ActivityDateRange.NewestActivity,
				"total_days":  activityResult.ActivityDateRange.TotalDays,
				"total_events": totalEvents,
			}
		}
		
		if activityResult.ActivityTimezone != "" {
			contextData["activity_timezone"] = activityResult.ActivityTimezone
			
			if strings.HasPrefix(activityResult.ActivityTimezone, "UTC") {
				offsetStr := strings.TrimPrefix(activityResult.ActivityTimezone, "UTC")
				if offset, err := strconv.Atoi(offsetStr); err == nil {
					contextData["utc_offset"] = offset
					
					if activityResult.ActiveHoursLocal.Start > 0 || activityResult.ActiveHoursLocal.End > 0 {
						startUTC := int(activityResult.ActiveHoursLocal.Start)
						endUTC := int(activityResult.ActiveHoursLocal.End)
						contextData["work_hours_utc"] = []int{startUTC, endUTC}
					}
					
					if activityResult.LunchHoursUTC.Confidence > 0 {
						lunchStartUTC := int(activityResult.LunchHoursUTC.Start)
						lunchEndUTC := int(activityResult.LunchHoursUTC.End)
						contextData["lunch_break_utc"] = []int{lunchStartUTC, lunchEndUTC}
						contextData["lunch_confidence"] = activityResult.LunchHoursUTC.Confidence
					}
					
					if activityResult.PeakProductivity.Count > 0 {
						peakStartUTC := int(activityResult.PeakProductivity.Start)
						peakEndUTC := int(activityResult.PeakProductivity.End)
						contextData["peak_productivity_utc"] = []int{peakStartUTC, peakEndUTC}
					}
				}
			}
		}
	}

	// Use data from UserContext instead of fetching again
	if len(userCtx.Organizations) > 0 {
		contextData["organizations"] = userCtx.Organizations
	}
	if len(userCtx.Repositories) > 0 {
		contextData["repositories"] = userCtx.Repositories
	}
	if len(userCtx.StarredRepos) > 0 {
		contextData["starred_repositories"] = userCtx.StarredRepos
	}
	
	// Filter recent PRs
	var recentPRs []github.PullRequest
	cutoff := time.Now().AddDate(0, -3, 0)
	for _, pr := range userCtx.PullRequests {
		if pr.CreatedAt.After(cutoff) {
			recentPRs = append(recentPRs, pr)
			if len(recentPRs) >= 20 {
				break
			}
		}
	}
	if len(recentPRs) > 0 {
		contextData["pull_requests"] = recentPRs
	}
	
	// Filter recent issues
	var recentIssues []github.Issue
	for _, issue := range userCtx.Issues {
		if issue.CreatedAt.After(cutoff) {
			recentIssues = append(recentIssues, issue)
			if len(recentIssues) >= 20 {
				break
			}
		}
	}
	if len(recentIssues) > 0 {
		contextData["issues"] = recentIssues
	}
	
	if len(userCtx.Comments) > 0 {
		contextData["comments"] = userCtx.Comments
	}
	
	// Collect commit message samples for Gemini to analyze
	commitSamples := collectCommitMessageSamples(userCtx.Events, 15)
	if len(commitSamples) > 0 {
		contextData["commit_message_samples"] = commitSamples
	}
	
	// Collect text samples from PRs/issues for Gemini to analyze  
	textSamples := collectTextSamples(recentPRs, recentIssues, userCtx.Comments, 10)
	if len(textSamples) > 0 {
		contextData["text_samples"] = textSamples
	}

	// Check for location field and try to geocode
	var detectedLocation *Location
	if userCtx.User.Location != "" {
		if loc, err := d.geocodeLocation(ctx, userCtx.User.Location); err == nil {
			detectedLocation = loc
			contextData["location"] = loc
		}
	}

	// Try to extract additional context from profile
	if userCtx.User.Blog != "" {
		// Try to fetch website content for more context
		websiteContent := d.fetchWebsiteContent(ctx, userCtx.User.Blog)
		if websiteContent != "" {
			contextData["website_content"] = websiteContent
		}
	}

	// Extract social media URLs from GitHub profile
	socialURLs := extractSocialMediaURLs(userCtx.User)
	
	// Also extract social media URLs from website content
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		websiteSocialURLs := github.ExtractSocialMediaFromHTML(websiteContent)
		d.logger.Debug("extracted social media URLs from website", 
			"website", userCtx.User.Blog, 
			"found_urls", len(websiteSocialURLs))
		
		// Merge with existing social URLs, avoiding duplicates
		for _, url := range websiteSocialURLs {
			found := false
			for _, existing := range socialURLs {
				if existing == url {
					found = true
					break
				}
			}
			if !found {
				socialURLs = append(socialURLs, url)
				d.logger.Debug("added social URL from website", "url", url)
			}
		}
	}
	if len(socialURLs) > 0 {
		contextData["social_media_urls"] = socialURLs

		// Check for country TLDs
		tlds := extractCountryTLDs(socialURLs...)
		if len(tlds) > 0 {
			contextData["country_tlds"] = tlds
		}
		
		// Follow Mastodon links to get comprehensive profile data
		for _, socialURL := range socialURLs {
			isMastodon := false
			
			if strings.Contains(userCtx.User.Bio, "[MASTODON] " + socialURL) {
				isMastodon = true
			} else if strings.Contains(socialURL, "/@") {
				isMastodon = true
			}
			
			if isMastodon {
				d.logger.Debug("following Mastodon link", "url", socialURL)
				mastodonData := fetchMastodonProfileViaAPI(ctx, socialURL, d.logger)
				if mastodonData != nil {
					contextData["mastodon_profile"] = mastodonData
					
					for _, website := range mastodonData.Websites {
						if userCtx.User.Blog == "" {
							userCtx.User.Blog = website
						}
						
						websiteContent := d.fetchWebsiteContent(ctx, website)
						if websiteContent != "" {
							if websiteContents, ok := contextData["mastodon_website_contents"].(map[string]string); ok {
								websiteContents[website] = websiteContent
							} else {
								contextData["mastodon_website_contents"] = map[string]string{
									website: websiteContent,
								}
							}
						}
					}
				}
			}
		}
	}

	// Query Gemini with all context
	// Check if verbose mode is enabled
	isVerbose := d.logger.Enabled(ctx, slog.LevelDebug)
	geminiResult, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, isVerbose)
	if err != nil {
		d.logger.Debug("Gemini analysis failed", "error", err)
		return nil
	}

	if geminiResult.Confidence < 0.3 {
		d.logger.Debug("Gemini confidence too low", "confidence", geminiResult.Confidence)
		return nil
	}

	result := &Result{
		Username:                userCtx.Username,
		Timezone:                geminiResult.Timezone,
		TimezoneConfidence:      geminiResult.Confidence,
		Confidence:              geminiResult.Confidence,
		Method:                  "gemini_analysis",
		GeminiSuggestedLocation: geminiResult.Location,
		GeminiReasoning:         geminiResult.Reasoning,
		GeminiPrompt:            geminiResult.Prompt,
	}

	if detectedLocation != nil {
		result.Location = detectedLocation
	} else if geminiResult.Location != "" && geminiResult.Location != "unknown" {
		if coords, err := d.geocodeLocation(ctx, geminiResult.Location); err == nil {
			result.Location = coords
			result.LocationName = geminiResult.Location
		}
	}

	if activityResult != nil {
		result.ActiveHoursLocal = activityResult.ActiveHoursLocal
		result.QuietHoursUTC = activityResult.QuietHoursUTC
		result.SleepBucketsUTC = activityResult.SleepBucketsUTC
		result.HourlyActivityUTC = activityResult.HourlyActivityUTC
		result.HalfHourlyActivityUTC = activityResult.HalfHourlyActivityUTC
		result.LunchHoursUTC = activityResult.LunchHoursUTC
		result.PeakProductivity = activityResult.PeakProductivity
		result.TopOrganizations = activityResult.TopOrganizations
		result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
		result.ActivityDateRange = activityResult.ActivityDateRange
	}

	return result
}

// tryUnifiedGeminiAnalysisWithEvents attempts timezone detection using Gemini AI with event context.
func (d *Detector) tryUnifiedGeminiAnalysisWithEvents(ctx context.Context, username string, activityResult *Result, events []github.PublicEvent) *Result {
	// Skip if no Gemini API key
	if d.geminiAPIKey == "" {
		d.logger.Debug("skipping Gemini analysis - no API key configured")
		return nil
	}

	user := d.githubClient.FetchUser(ctx, username)
	if user == nil {
		d.logger.Debug("could not fetch user for Gemini analysis", "username", username)
		return nil
	}
	
	// Debug log user data
	d.logger.Debug("fetched user for Gemini", 
		"username", username,
		"name", user.Name,
		"location", user.Location,
		"company", user.Company,
		"bio", user.Bio)

	// Prepare comprehensive context for Gemini
	contextData := make(map[string]interface{})
	contextData["user"] = user
	contextData["recent_events"] = events

	// Add activity result if available
	if activityResult != nil {
		contextData["activity_result"] = activityResult
		
		// Add available fields from Result struct
		if activityResult.QuietHoursUTC != nil {
			contextData["quiet_hours"] = activityResult.QuietHoursUTC
		}
		
		if activityResult.HourlyActivityUTC != nil {
			contextData["hour_counts"] = activityResult.HourlyActivityUTC
		}
		
		// Add timezone candidates for unified analysis
		if activityResult.TimezoneCandidates != nil && len(activityResult.TimezoneCandidates) > 0 {
			contextData["timezone_candidates"] = activityResult.TimezoneCandidates
		}
		
		// Add activity date range information
		if activityResult.ActivityDateRange.TotalDays > 0 {
			// Calculate total events from hourly activity
			totalEvents := 0
			if activityResult.HourlyActivityUTC != nil {
				for _, count := range activityResult.HourlyActivityUTC {
					totalEvents += count
				}
			}
			
			contextData["activity_date_range"] = map[string]interface{}{
				"oldest":      activityResult.ActivityDateRange.OldestActivity,
				"newest":      activityResult.ActivityDateRange.NewestActivity,
				"total_days":  activityResult.ActivityDateRange.TotalDays,
				"total_events": totalEvents,
			}
		}
		
		// Add activity timezone result
		if activityResult.ActivityTimezone != "" {
			contextData["activity_timezone"] = activityResult.ActivityTimezone
			
			// Extract offset from activity timezone (e.g., "UTC-5" -> -5)
			if strings.HasPrefix(activityResult.ActivityTimezone, "UTC") {
				offsetStr := strings.TrimPrefix(activityResult.ActivityTimezone, "UTC")
				if offset, err := strconv.Atoi(offsetStr); err == nil {
					contextData["utc_offset"] = offset
					
					// Hours are already in UTC (despite the field names saying "Local")
					if activityResult.ActiveHoursLocal.Start > 0 || activityResult.ActiveHoursLocal.End > 0 {
						startUTC := int(activityResult.ActiveHoursLocal.Start)
						endUTC := int(activityResult.ActiveHoursLocal.End)
						contextData["work_hours_utc"] = []int{startUTC, endUTC}
					}
					
					// Lunch break is already in UTC
					if activityResult.LunchHoursUTC.Confidence > 0 {
						lunchStartUTC := int(activityResult.LunchHoursUTC.Start)
						lunchEndUTC := int(activityResult.LunchHoursUTC.End)
						contextData["lunch_break_utc"] = []int{lunchStartUTC, lunchEndUTC}
						contextData["lunch_confidence"] = activityResult.LunchHoursUTC.Confidence
					}
					
					// Peak productivity is already in UTC
					if activityResult.PeakProductivity.Count > 0 {
						peakStartUTC := int(activityResult.PeakProductivity.Start)
						peakEndUTC := int(activityResult.PeakProductivity.End)
						contextData["peak_productivity_utc"] = []int{peakStartUTC, peakEndUTC}
					}
				}
			}
		}
	}

	// Fetch supplemental data for comprehensive analysis
	wg := sync.WaitGroup{}
	var organizations []github.Organization
	var repos []github.Repository
	var starredRepos []github.Repository
	var pullRequests []github.PullRequest
	var issues []github.Issue
	var comments []github.Comment

	// Fetch organizations
	wg.Add(6) // Total goroutines: orgs, repos, PRs, issues, comments, starred
	go func() {
		defer wg.Done()
		orgs, err := d.githubClient.FetchOrganizations(ctx, username)
		if err == nil {
			organizations = orgs
		}
	}()

	// Fetch repositories
	go func() {
		defer wg.Done()
		pinnedRepos, err := d.githubClient.FetchPinnedRepositories(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch pinned repositories", "error", err)
		}
		popularRepos, err := d.githubClient.FetchPopularRepositories(ctx, username)
		if err != nil {
			d.logger.Debug("failed to fetch popular repositories", "error", err)
		}

		// Combine and deduplicate
		repoMap := make(map[string]github.Repository)
		for _, repo := range pinnedRepos {
			repoMap[repo.FullName] = repo
		}
		for _, repo := range popularRepos {
			if _, exists := repoMap[repo.FullName]; !exists {
				repoMap[repo.FullName] = repo
			}
		}

		for _, repo := range repoMap {
			repos = append(repos, repo)
		}
	}()

	// Fetch recent PRs
	go func() {
		defer wg.Done()
		prs, err := d.githubClient.FetchPullRequests(ctx, username)
		if err == nil && len(prs) > 0 {
			// Limit to recent PRs
			cutoff := time.Now().AddDate(0, -3, 0)
			for _, pr := range prs {
				if pr.CreatedAt.After(cutoff) {
					pullRequests = append(pullRequests, pr)
					if len(pullRequests) >= 20 {
						break
					}
				}
			}
		}
	}()

	// Fetch recent issues
	go func() {
		defer wg.Done()
		iss, err := d.githubClient.FetchIssues(ctx, username)
		if err == nil && len(iss) > 0 {
			// Limit to recent issues
			cutoff := time.Now().AddDate(0, -3, 0)
			for _, issue := range iss {
				if issue.CreatedAt.After(cutoff) {
					issues = append(issues, issue)
					if len(issues) >= 20 {
						break
					}
				}
			}
		}
	}()

	// Fetch recent comments
	go func() {
		defer wg.Done()
		cmts, err := d.githubClient.FetchUserComments(ctx, username)
		if err == nil {
			comments = cmts
		}
	}()

	// Fetch starred repositories
	go func() {
		defer wg.Done()
		_, starred, err := d.githubClient.FetchStarredRepositories(ctx, username)
		if err == nil {
			starredRepos = starred
		}
	}()

	wg.Wait()

	// Add to context
	if len(organizations) > 0 {
		contextData["organizations"] = organizations
	}
	if len(repos) > 0 {
		contextData["repositories"] = repos
	}
	if len(starredRepos) > 0 {
		contextData["starred_repositories"] = starredRepos
	}
	if len(pullRequests) > 0 {
		contextData["pull_requests"] = pullRequests
	}
	if len(issues) > 0 {
		contextData["issues"] = issues
	}
	if len(comments) > 0 {
		contextData["comments"] = comments
	}
	
	// Collect commit message samples for Gemini to analyze
	commitSamples := collectCommitMessageSamples(events, 15)
	if len(commitSamples) > 0 {
		contextData["commit_message_samples"] = commitSamples
	}
	
	// Collect text samples from PRs/issues for Gemini to analyze  
	textSamples := collectTextSamples(pullRequests, issues, comments, 10)
	if len(textSamples) > 0 {
		contextData["text_samples"] = textSamples
	}
	
	// Skip collaborator analysis to reduce API calls

	// Check for location field and try to geocode
	var detectedLocation *Location
	if user.Location != "" {
		if loc, err := d.geocodeLocation(ctx, user.Location); err == nil {
			detectedLocation = loc
			contextData["location"] = loc
		}
	}

	// Try to extract additional context from profile
	if user.Blog != "" {
		// Try to fetch website content for more context
		websiteContent := d.fetchWebsiteContent(ctx, user.Blog)
		if websiteContent != "" {
			contextData["website_content"] = websiteContent
		}
	}

	// Extract social media URLs from GitHub profile
	socialURLs := extractSocialMediaURLs(user)
	
	// Also extract social media URLs from website content
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		websiteSocialURLs := github.ExtractSocialMediaFromHTML(websiteContent)
		d.logger.Debug("extracted social media URLs from website", 
			"website", user.Blog, 
			"found_urls", len(websiteSocialURLs))
		
		// Merge with existing social URLs, avoiding duplicates
		for _, url := range websiteSocialURLs {
			found := false
			for _, existing := range socialURLs {
				if existing == url {
					found = true
					break
				}
			}
			if !found {
				socialURLs = append(socialURLs, url)
				d.logger.Debug("added social URL from website", "url", url)
			}
		}
	}
	if len(socialURLs) > 0 {
		contextData["social_media_urls"] = socialURLs

		// Check for country TLDs
		tlds := extractCountryTLDs(socialURLs...)
		if len(tlds) > 0 {
			contextData["country_tlds"] = tlds
		}
		
		// Follow Mastodon links to get comprehensive profile data
		for _, socialURL := range socialURLs {
			// Check if URL is a Mastodon instance
			// More authoritative: check if the bio contains [MASTODON] provider tag
			// or if URL contains common Mastodon patterns
			isMastodon := false
			
			// First check if the bio indicates this is a Mastodon link (from GraphQL provider field)
			if strings.Contains(user.Bio, "[MASTODON] " + socialURL) {
				isMastodon = true
			} else if strings.Contains(socialURL, "/@") {
				// Fallback: Mastodon URLs typically have /@username pattern
				// This is more authoritative than hardcoding specific domains
				isMastodon = true
			}
			
			if isMastodon {
				d.logger.Debug("following Mastodon link", "url", socialURL)
				// Try API first, falls back to HTML scraping if needed
				mastodonData := fetchMastodonProfileViaAPI(ctx, socialURL, d.logger)
				if mastodonData != nil {
					// Store all Mastodon profile data
					contextData["mastodon_profile"] = mastodonData
					
					// Process all discovered websites
					for _, website := range mastodonData.Websites {
						// If we didn't already have a blog URL, use the first website
						if user.Blog == "" {
							user.Blog = website
						}
						
						// Fetch content from each Mastodon-discovered website
						websiteContent := d.fetchWebsiteContent(ctx, website)
						if websiteContent != "" {
							// Store website content with the URL as key
							if websiteContents, ok := contextData["mastodon_website_contents"].(map[string]string); ok {
								websiteContents[website] = websiteContent
							} else {
								contextData["mastodon_website_contents"] = map[string]string{
									website: websiteContent,
								}
							}
							d.logger.Debug("fetched Mastodon website content", "website", website, "content_length", len(websiteContent))
						}
					}
					
					d.logger.Debug("extracted comprehensive Mastodon data", 
						"mastodon", socialURL, 
						"websites", len(mastodonData.Websites),
						"fields", len(mastodonData.ProfileFields),
						"hashtags", len(mastodonData.Hashtags))
				}
			}
		}
	}

	// Let Gemini figure out name patterns on its own from the provided name

	// Query Gemini with all context
	// Check if verbose mode is enabled
	isVerbose := d.logger.Enabled(ctx, slog.LevelDebug)
	geminiResult, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, isVerbose)
	if err != nil {
		d.logger.Debug("Gemini analysis failed", "error", err)
		return nil
	}

	// If Gemini returns low confidence, don't use it
	if geminiResult.Confidence < 0.3 {
		d.logger.Debug("Gemini confidence too low", "confidence", geminiResult.Confidence)
		return nil
	}


	result := &Result{
		Username:                username,
		Timezone:                geminiResult.Timezone,
		TimezoneConfidence:      geminiResult.Confidence,
		Confidence:              geminiResult.Confidence,  // Set both fields for compatibility
		Method:                  "gemini_analysis",
		GeminiSuggestedLocation: geminiResult.Location,
		GeminiReasoning:         geminiResult.Reasoning,
		GeminiPrompt:            geminiResult.Prompt,
	}

	// If we have coordinates from user's profile location, add them
	if detectedLocation != nil {
		result.Location = detectedLocation
	} else if geminiResult.Location != "" && geminiResult.Location != "unknown" {
		// Try to geocode the Gemini-suggested location
		if coords, err := d.geocodeLocation(ctx, geminiResult.Location); err == nil {
			result.Location = coords
			result.LocationName = geminiResult.Location
			d.logger.Debug("successfully geocoded Gemini location", "username", username, 
				"location", geminiResult.Location, "lat", coords.Latitude, "lng", coords.Longitude)
		} else {
			d.logger.Debug("failed to geocode Gemini location", "username", username, 
				"location", geminiResult.Location, "error", err)
		}
	}

	// Merge with activity result if available
	if activityResult != nil {
		result.ActiveHoursLocal = activityResult.ActiveHoursLocal
		result.QuietHoursUTC = activityResult.QuietHoursUTC
		result.SleepBucketsUTC = activityResult.SleepBucketsUTC // Include 30-minute resolution sleep periods
		result.HourlyActivityUTC = activityResult.HourlyActivityUTC
		result.HalfHourlyActivityUTC = activityResult.HalfHourlyActivityUTC
		result.LunchHoursUTC = activityResult.LunchHoursUTC
		result.PeakProductivity = activityResult.PeakProductivity
		result.TopOrganizations = activityResult.TopOrganizations
		result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
		result.ActivityDateRange = activityResult.ActivityDateRange
	}

	return result
}

// extractUTCOffset extracts the UTC offset from a timezone string
// Handles formats like "UTC+10", "Europe/Moscow", "America/New_York"