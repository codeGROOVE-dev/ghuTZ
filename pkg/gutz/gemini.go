package gutz

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/gemini"
	"github.com/codeGROOVE-dev/guTZ/pkg/github"
	"github.com/codeGROOVE-dev/guTZ/pkg/social"
)

// geminiQueryResult holds the result from a Gemini API query.
//
//nolint:govet // fieldalignment is a minor optimization, struct clarity is preferred
type geminiQueryResult struct {
	// Place float64 first for alignment (8 bytes)
	Confidence float64
	// Place pointer next (8 bytes on 64-bit)
	Response *gemini.Response // Full response from Gemini including GPS coords
	// Strings are pointers (8 bytes each), group together
	Timezone  string
	Reasoning string
	Location  string
	Prompt    string
}

// queryUnifiedGeminiForTimezone queries Gemini AI for timezone detection.
func (d *Detector) queryUnifiedGeminiForTimezone(ctx context.Context, contextData map[string]any) (*geminiQueryResult, error) {
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

	// Create Gemini client and call API
	client := gemini.NewClient(d.geminiAPIKey, d.geminiModel, d.gcpProject)
	resp, err := client.CallWithSDK(ctx, prompt, d.cache, d.logger)
	if err != nil {
		return nil, fmt.Errorf("🚩 Gemini API SDK call failed: %w (prompt_length: %d, has_activity: %t)",
			err, len(prompt), hasActivityData)
	}

	// Extract response fields
	timezone := resp.DetectedTimezone
	location := resp.DetectedLocation
	reasoning := resp.DetectionReasoning

	// Parse confidence from string
	var confidence float64
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

	// Verbose response display removed - now handled in main CLI

	// Adjust confidence based on data availability
	if !hasActivityData && confidence > 0.5 {
		// Without activity data, cap confidence at 50%
		confidence = 0.5
		d.logger.Debug("capped confidence due to no activity data",
			"original", confidence, "capped", 0.5)
	}

	// If we have strong activity patterns, apply a small boost (5% max)
	if hasActivityData && confidence > 0.5 {
		// Only boost if confidence is already decent
		originalConfidence := confidence
		confidence = math.Min(confidence+0.05, 0.9) // Add max 5%, cap at 90%
		if confidence != originalConfidence {
			d.logger.Debug("applied activity data confidence boost",
				"original", originalConfidence, "boosted", confidence)
		}
	}

	// Return the result
	return &geminiQueryResult{
		Timezone:   timezone,
		Reasoning:  reasoning,
		Confidence: confidence,
		Location:   location,
		Prompt:     prompt,
		Response:   resp,
	}, nil
}

// tryUnifiedGeminiAnalysisWithContext attempts timezone detection using Gemini AI with UserContext.
//
//nolint:gocognit,nestif,revive,maintidx // Complex AI-based analysis requires comprehensive data processing
func (d *Detector) tryUnifiedGeminiAnalysisWithContext(ctx context.Context, userCtx *UserContext, activityResult *Result) *Result {
	if userCtx.User == nil {
		d.logger.Warn("🚩 User Profile Unavailable - Proceeding with Gemini analysis using available data", "username", userCtx.Username,
			"issue", "GitHub user profile fetch failed - likely token scope issues or user not found")
	}

	// Prepare comprehensive context for Gemini
	contextData := make(map[string]any)
	var dataSources []string

	contextData["user"] = userCtx.User
	if userCtx.User != nil {
		dataSources = append(dataSources, "Profile")

		// Include social accounts from GraphQL
		if userCtx.User != nil && len(userCtx.User.SocialAccounts) > 0 {
			contextData["social_accounts"] = userCtx.User.SocialAccounts
			dataSources = append(dataSources, "Social Accounts")
		}
	}

	contextData["recent_events"] = userCtx.Events
	if len(userCtx.Events) > 0 {
		dataSources = append(dataSources, "Events")
	}

	// Add activity result if available
	if activityResult != nil {
		contextData["activity_result"] = activityResult

		if activityResult.SleepHoursUTC != nil {
			contextData["sleep_hours"] = activityResult.SleepHoursUTC
		}

		if activityResult.HourlyActivityUTC != nil {
			contextData["hour_counts"] = activityResult.HourlyActivityUTC
		}

		if len(activityResult.TimezoneCandidates) > 0 {
			contextData["timezone_candidates"] = activityResult.TimezoneCandidates
		}

		if activityResult.ActivityDateRange.TotalDays > 0 {
			totalEvents := 0
			if activityResult.HourlyActivityUTC != nil {
				for _, count := range activityResult.HourlyActivityUTC {
					totalEvents += count
				}
			}

			contextData["activity_date_range"] = map[string]any{
				"oldest":                activityResult.ActivityDateRange.OldestActivity,
				"newest":                activityResult.ActivityDateRange.NewestActivity,
				"total_days":            activityResult.ActivityDateRange.TotalDays,
				"total_events":          totalEvents,
				"spans_dst_transitions": activityResult.ActivityDateRange.SpansDSTTransitions,
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

					if activityResult.PeakProductivityUTC.Count > 0 {
						peakStartUTC := int(activityResult.PeakProductivityUTC.Start)
						peakEndUTC := int(activityResult.PeakProductivityUTC.End)
						contextData["peak_productivity_utc"] = []int{peakStartUTC, peakEndUTC}
					}
				}
			}
		}
	}

	// Use data from UserContext instead of fetching again
	if len(userCtx.Organizations) > 0 {
		contextData["organizations"] = userCtx.Organizations
		dataSources = append(dataSources, "Organizations")
	}
	if len(userCtx.Repositories) > 0 {
		contextData["repositories"] = userCtx.Repositories
		dataSources = append(dataSources, "Repositories")

		// Check for github.io repositories (personal websites)
		for i := range userCtx.Repositories {
			// Check if this is a GitHub Pages site
			if !strings.HasSuffix(userCtx.Repositories[i].Name, ".github.io") &&
				!strings.EqualFold(userCtx.Repositories[i].Name, userCtx.Username+".github.io") {
				continue
			}
			// This is a personal GitHub Pages site
			githubPagesURL := fmt.Sprintf("https://%s.github.io", userCtx.Username)

			// Add to website if not already set
			if userCtx.User != nil && userCtx.User.Blog == "" {
				userCtx.User.Blog = githubPagesURL
				d.logger.Debug("found GitHub Pages site", "url", githubPagesURL, "repo", userCtx.Repositories[i].Name)
			}

			// Fetch content from the GitHub Pages site
			websiteContent := d.fetchWebsiteContent(ctx, githubPagesURL)
			if websiteContent != "" {
				if existingContent, ok := contextData["website_content"].(string); !ok || existingContent == "" {
					contextData["website_content"] = websiteContent
					contextData["github_pages_url"] = githubPagesURL
					dataSources = append(dataSources, "Pages")
					d.logger.Debug("fetched GitHub Pages content", "url", githubPagesURL, "content_length", len(websiteContent))
				}
			}
			break // Found the main github.io site
		}
	}
	if len(userCtx.StarredRepos) > 0 {
		contextData["starred_repositories"] = userCtx.StarredRepos
		dataSources = append(dataSources, "Starred Repos")
	}

	// Add gist information with descriptions (last 5 gists for location/interest hints)
	if len(userCtx.Gists) > 0 {
		contextData["gist_count"] = len(userCtx.Gists)

		// Include last 5 gists with descriptions for location/interest analysis
		recentGists := userCtx.Gists
		if len(recentGists) > 5 {
			recentGists = recentGists[:5]
		}
		contextData["recent_gists"] = recentGists
		dataSources = append(dataSources, "Gists")
	}

	// Include recent PRs and Issues (limited to save context)
	if len(userCtx.PullRequests) > 0 {
		// Include up to 20 recent PRs
		limit := 20
		if len(userCtx.PullRequests) < limit {
			limit = len(userCtx.PullRequests)
		}
		contextData["recent_pull_requests"] = userCtx.PullRequests[:limit]
		dataSources = append(dataSources, "Pull Requests")
	}

	if len(userCtx.Issues) > 0 {
		// Include up to 20 recent issues
		limit := 20
		if len(userCtx.Issues) < limit {
			limit = len(userCtx.Issues)
		}
		contextData["recent_issues"] = userCtx.Issues[:limit]
		dataSources = append(dataSources, "Issues")
	}

	if len(userCtx.Comments) > 0 {
		contextData["comments"] = userCtx.Comments
		dataSources = append(dataSources, "Comments")
	}

	// Collect commit message samples for Gemini to analyze
	commitSamples := collectCommitMessageSamples(userCtx.Events, 15)
	if len(commitSamples) > 0 {
		contextData["commit_message_samples"] = commitSamples
		dataSources = append(dataSources, "Commits")
	}

	// Collect text samples from PRs/issues for Gemini to analyze
	textSamples := collectTextSamples(userCtx.PullRequests, userCtx.Issues, userCtx.Comments, 10)
	if len(textSamples) > 0 {
		contextData["text_samples"] = textSamples
	}

	// Check for location field and try to geocode
	var detectedLocation *Location
	if userCtx.User != nil && userCtx.User.Location != "" {
		if loc, err := d.geocodeLocation(ctx, userCtx.User.Location); err == nil {
			detectedLocation = loc
			contextData["location"] = loc
		}
	}

	// Try to extract additional context from profile
	if userCtx.User != nil && userCtx.User.Blog != "" {
		// Try to fetch website content for more context
		websiteContent := d.fetchWebsiteContent(ctx, userCtx.User.Blog)
		if websiteContent != "" {
			contextData["website_content"] = websiteContent
		}
	}

	// Extract repository contributions from all sources
	contributedRepos := extractRepositoryContributions(userCtx)
	if len(contributedRepos) > 0 {
		contextData["contributed_repositories"] = contributedRepos
		dataSources = append(dataSources, "External Contributions")
	}

	// Extract and dedupe emails from profile and commits
	emails := extractAndDedupeEmails(userCtx)
	if len(emails) > 0 {
		contextData["emails"] = emails
		dataSources = append(dataSources, "Emails")
	}

	// Extract SSH key creation timestamps as additional timezone signals
	if len(userCtx.SSHKeys) > 0 {
		sshKeyTimes := make([]time.Time, 0, len(userCtx.SSHKeys))
		for _, key := range userCtx.SSHKeys {
			if !key.CreatedAt.IsZero() {
				sshKeyTimes = append(sshKeyTimes, key.CreatedAt)
			}
		}
		if len(sshKeyTimes) > 0 {
			contextData["ssh_key_creation_times"] = sshKeyTimes
			dataSources = append(dataSources, "SSH Keys")
		}
	}

	// Extract social media URLs from GitHub profile
	socialURLs := extractSocialMediaURLs(userCtx.User)

	// Also extract social media URLs from website content
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		websiteSocialURLs := github.ExtractSocialMediaFromHTML(websiteContent)
		var blogURL string
		if userCtx.User != nil {
			blogURL = userCtx.User.Blog
		}
		d.logger.Debug("extracted social media URLs from website",
			"website", blogURL,
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
	//nolint:nestif // Social media URL processing requires conditional logic
	if len(socialURLs) > 0 {
		contextData["social_media_urls"] = socialURLs

		// Check for country TLDs
		tlds := extractCountryTLDs(socialURLs...)
		if len(tlds) > 0 {
			contextData["country_tlds"] = tlds
		}

		// Extract data from social media profiles
		socialProfiles := make(map[string]string)
		for _, socialURL := range socialURLs {
			// Determine the type of social media
			switch {
			case strings.Contains(socialURL, "twitter.com") || strings.Contains(socialURL, "x.com"):
				socialProfiles["twitter"] = socialURL
				d.logger.Debug("identified Twitter URL", "url", socialURL)
			case strings.Contains(socialURL, "bsky.app"):
				socialProfiles["bluesky"] = socialURL
				d.logger.Debug("identified BlueSky URL", "url", socialURL)
			case strings.Contains(socialURL, "/@") || strings.Contains(socialURL, "infosec.exchange") ||
				strings.Contains(socialURL, "mastodon") || strings.Contains(socialURL, ".social"):
				socialProfiles["mastodon"] = socialURL
				d.logger.Debug("identified Mastodon URL", "url", socialURL)
			}
		}

		d.logger.Debug("social profiles to extract", "profiles", socialProfiles, "count", len(socialProfiles))

		// Extract social media data using the social package
		d.logger.Debug("calling social.Extract", "profiles", socialProfiles)
		extractedProfiles := social.Extract(ctx, socialProfiles, d.logger)

		d.logger.Debug("extracted social profiles", "count", len(extractedProfiles), "profiles", extractedProfiles)

		// Process extracted profiles
		for idx := range extractedProfiles {
			switch extractedProfiles[idx].Kind {
			case "twitter":
				// Add Twitter profile data to context
				if extractedProfiles[idx].Location != "" || extractedProfiles[idx].Bio != "" {
					twitterData := map[string]string{
						"username": extractedProfiles[idx].Username,
						"name":     extractedProfiles[idx].Name,
						"bio":      extractedProfiles[idx].Bio,
						"location": extractedProfiles[idx].Location,
					}
					contextData["twitter_profile"] = twitterData
					dataSources = append(dataSources, "Twitter/X")
					d.logger.Debug("extracted Twitter profile",
						"username", extractedProfiles[idx].Username,
						"location", extractedProfiles[idx].Location,
						"bio_length", len(extractedProfiles[idx].Bio))
				}

			case "bluesky":
				// Add BlueSky profile data to context
				if extractedProfiles[idx].Bio != "" {
					blueSkyData := map[string]string{
						"handle": extractedProfiles[idx].Username,
						"name":   extractedProfiles[idx].Name,
						"bio":    extractedProfiles[idx].Bio,
					}
					contextData["bluesky_profile"] = blueSkyData
					dataSources = append(dataSources, "BlueSky")
					d.logger.Debug("extracted BlueSky profile",
						"handle", extractedProfiles[idx].Username,
						"bio_length", len(extractedProfiles[idx].Bio))
				}

			case "mastodon":
				// Convert to old format for compatibility
				mastodonData := &MastodonProfileData{
					Username:      extractedProfiles[idx].Username,
					DisplayName:   extractedProfiles[idx].Name,
					Bio:           extractedProfiles[idx].Bio,
					ProfileFields: extractedProfiles[idx].Fields,
					Hashtags:      extractedProfiles[idx].Tags,
					JoinedDate:    extractedProfiles[idx].Joined,
					Websites:      []string{},
				}

				// Extract websites from fields
				for key, value := range extractedProfiles[idx].Fields {
					lowerKey := strings.ToLower(key)
					if strings.Contains(lowerKey, "website") || strings.Contains(lowerKey, "blog") ||
						strings.Contains(lowerKey, "home") || strings.Contains(lowerKey, "url") {
						if strings.HasPrefix(value, "http") {
							mastodonData.Websites = append(mastodonData.Websites, value)
							d.logger.Debug("found website in Mastodon field",
								"field", key, "url", value, "username", extractedProfiles[idx].Username)
						}
					}
				}

				contextData["mastodon_profile"] = mastodonData
				dataSources = append(dataSources, "Mastodon")

				for _, website := range mastodonData.Websites {
					d.logger.Debug("processing Mastodon website", "url", website)
					if userCtx.User != nil && userCtx.User.Blog == "" {
						userCtx.User.Blog = website
						d.logger.Debug("set user blog from Mastodon", "url", website)
					}

					websiteContent := d.fetchWebsiteContent(ctx, website)
					if websiteContent != "" {
						d.logger.Debug("fetched Mastodon website content", "url", website, "length", len(websiteContent))
						if websiteContents, ok := contextData["mastodon_website_contents"].(map[string]string); ok {
							websiteContents[website] = websiteContent
						} else {
							contextData["mastodon_website_contents"] = map[string]string{
								website: websiteContent,
							}
						}
					} else {
						d.logger.Debug("no content fetched from Mastodon website", "url", website)
					}
				}
			}
		}
	}

	// Query Gemini with all context
	geminiResult, err := d.queryUnifiedGeminiForTimezone(ctx, contextData)
	if err != nil {
		d.logger.Warn("🚩 Gemini API Analysis Failed", "username", userCtx.Username,
			"error", err,
			"data_sources", dataSources,
			"context_keys", func() []string {
				keys := make([]string, 0, len(contextData))
				for k := range contextData {
					keys = append(keys, k)
				}
				return keys
			}(),
			"fallback", "using activity-only patterns")
		return nil
	}

	if geminiResult.Confidence < 0.3 {
		d.logger.Warn("🚩 Gemini Analysis Rejected: Low Confidence", "username", userCtx.Username,
			"confidence", geminiResult.Confidence,
			"timezone_detected", geminiResult.Timezone,
			"reasoning", func() string {
				if len(geminiResult.Reasoning) <= 100 {
					return geminiResult.Reasoning
				}
				return geminiResult.Reasoning[:100] + "..."
			}(),
			"threshold", 0.3)
		return nil
	}

	// Log successful Gemini response at INFO level
	d.logger.Info("Gemini response received",
		"username", userCtx.Username,
		"timezone", geminiResult.Timezone,
		"location", geminiResult.Location,
		"confidence", geminiResult.Confidence,
		"data_sources", dataSources)

	// Sort data sources alphabetically for consistent display
	sort.Strings(dataSources)

	result := &Result{
		Username:                userCtx.Username,
		Timezone:                geminiResult.Timezone,
		TimezoneConfidence:      geminiResult.Confidence,
		Confidence:              geminiResult.Confidence,
		Method:                  "gemini_analysis",
		GeminiSuggestedLocation: geminiResult.Location,
		GeminiReasoning:         geminiResult.Reasoning,
		GeminiPrompt:            geminiResult.Prompt,
		DataSources:             dataSources,
		CreatedAt:               createdAtFromUser(userCtx.User),
	}

	// Add suspicious mismatch detection from Gemini
	if geminiResult.Response != nil {
		result.GeminiSuspiciousMismatch = geminiResult.Response.SuspiciousMismatch
		result.GeminiMismatchReason = geminiResult.Response.MismatchReason

		// Overwrite detectedLocation with Gemini's detected coordinates
		if geminiResult.Response.Latitude != 0 && geminiResult.Response.Longitude != 0 {
			detectedLocation = &Location{
				Latitude:  geminiResult.Response.Latitude,
				Longitude: geminiResult.Response.Longitude,
			}
			result.LocationName = geminiResult.Response.DetectedLocation
			result.GeminiSuggestedLocation = geminiResult.Response.DetectedLocation
			d.logger.Debug("Updated detectedLocation with Gemini GPS coordinates",
				"lat", geminiResult.Response.Latitude,
				"lng", geminiResult.Response.Longitude,
				"location", geminiResult.Response.DetectedLocation)
		}
	}

	// If Gemini provided a location string but no coordinates, try to geocode it
	if detectedLocation == nil && geminiResult.Location != "" && geminiResult.Location != "unknown" {
		if coords, err := d.geocodeLocation(ctx, geminiResult.Location); err == nil {
			detectedLocation = coords
			result.LocationName = geminiResult.Location
			result.GeminiSuggestedLocation = geminiResult.Location
			d.logger.Debug("Updated detectedLocation via geocoding Gemini location", "location", geminiResult.Location)
		}
	}

	// Set the final location
	if detectedLocation != nil {
		result.Location = detectedLocation
		d.logger.Debug("Using final detected location",
			"lat", detectedLocation.Latitude,
			"lng", detectedLocation.Longitude,
			"location", result.LocationName)
	}

	if activityResult != nil {
		result.ActiveHoursLocal = activityResult.ActiveHoursLocal
		result.SleepHoursUTC = activityResult.SleepHoursUTC
		result.SleepRangesLocal = activityResult.SleepRangesLocal
		result.SleepBucketsUTC = activityResult.SleepBucketsUTC
		result.HourlyActivityUTC = activityResult.HourlyActivityUTC
		result.HalfHourlyActivityUTC = activityResult.HalfHourlyActivityUTC
		result.LunchHoursUTC = activityResult.LunchHoursUTC
		result.LunchHoursLocal = activityResult.LunchHoursLocal
		result.PeakProductivityUTC = activityResult.PeakProductivityUTC
		result.PeakProductivityLocal = activityResult.PeakProductivityLocal
		result.TopOrganizations = activityResult.TopOrganizations
		result.HourlyOrganizationActivity = activityResult.HourlyOrganizationActivity
		result.ActivityDateRange = activityResult.ActivityDateRange

		// Check if Gemini's timezone differs significantly from activity-based detection
		if len(activityResult.TimezoneCandidates) > 0 {
			// Get the UTC offset from Gemini's timezone
			geminiOffset := utcOffsetFromTimezone(geminiResult.Timezone)

			// Get the top activity-based candidate offset
			topActivityOffset := activityResult.TimezoneCandidates[0].Offset

			// Calculate the difference in hours
			offsetDiff := math.Abs(geminiOffset - topActivityOffset)

			// Flag if difference is > 2 hours
			if offsetDiff > 2.0 {
				result.GeminiActivityMismatch = true
				result.GeminiActivityOffsetHours = offsetDiff

				d.logger.Warn("Gemini timezone differs significantly from activity pattern",
					"username", userCtx.Username,
					"gemini_timezone", geminiResult.Timezone,
					"gemini_offset", geminiOffset,
					"activity_offset", topActivityOffset,
					"difference_hours", offsetDiff)
			}
		}
	}

	return result
}

// getUTCOffsetFromTimezone returns the UTC offset in hours for a given timezone string.
// It handles IANA timezone strings like "America/New_York" or "Europe/London".
func utcOffsetFromTimezone(tzString string) float64 {
	// Try to load the timezone
	loc, err := time.LoadLocation(tzString)
	if err != nil {
		// If it fails, try to parse as UTC±N format
		if strings.HasPrefix(tzString, "UTC") {
			offsetStr := strings.TrimPrefix(tzString, "UTC")
			if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
				return offset
			}
		}
		// Default to 0 if we can't parse
		return 0
	}

	// Get the offset for the current time
	// This handles DST correctly for the current date
	now := time.Now().In(loc)
	_, offset := now.Zone()

	// Convert seconds to hours
	return float64(offset) / 3600.0
}

// extractRepositoryContributions aggregates repository contributions from all sources.
func extractRepositoryContributions(userCtx *UserContext) []repoContribution {
	contributedRepos := make(map[string]int)

	// Extract from PRs
	for i := range userCtx.PullRequests {
		pr := &userCtx.PullRequests[i]
		if pr.RepoName != "" && !strings.HasPrefix(pr.RepoName, userCtx.Username+"/") {
			contributedRepos[pr.RepoName]++
		}
	}

	// Extract from Issues
	for i := range userCtx.Issues {
		issue := &userCtx.Issues[i]
		if issue.RepoName != "" && !strings.HasPrefix(issue.RepoName, userCtx.Username+"/") {
			contributedRepos[issue.RepoName]++
		}
	}

	// Extract from Comments (parse repository from HTML URL)
	for _, comment := range userCtx.Comments {
		if repoFullName := extractRepoFromGitHubURL(comment.HTMLURL); repoFullName != "" {
			// Only count external contributions (not user's own repos)
			if !strings.HasPrefix(repoFullName, userCtx.Username+"/") {
				contributedRepos[repoFullName]++
			}
		}
	}

	// Extract from Commit Activities (using enhanced GraphQL commit data)
	for _, commitActivity := range userCtx.CommitActivities {
		// Only count external contributions (not user's own repos)
		if !strings.HasPrefix(commitActivity.Repository, userCtx.Username+"/") {
			contributedRepos[commitActivity.Repository]++
		}
	}

	// Convert to sorted list
	if len(contributedRepos) == 0 {
		return nil
	}

	var contribs []repoContribution
	for repo, count := range contributedRepos {
		contribs = append(contribs, repoContribution{Name: repo, Count: count})
	}

	// Sort by contribution count descending, then by name for deterministic ordering
	for i := 0; i < len(contribs); i++ {
		for j := i + 1; j < len(contribs); j++ {
			if contribs[i].Count < contribs[j].Count ||
				(contribs[i].Count == contribs[j].Count && contribs[i].Name > contribs[j].Name) {
				contribs[i], contribs[j] = contribs[j], contribs[i]
			}
		}
	}

	// Limit to top 15 contributions to avoid overwhelming Gemini
	if len(contribs) > 15 {
		contribs = contribs[:15]
	}

	return contribs
}

// extractRepoFromGitHubURL extracts repository owner/name from a GitHub URL
// Examples:
//
//	https://github.com/owner/repo/issues/123 -> "owner/repo"
//	https://github.com/owner/repo/pull/456#issuecomment-789 -> "owner/repo"
//	https://github.com/owner/repo/commit/abc123 -> "owner/repo"
func extractRepoFromGitHubURL(htmlURL string) string {
	if htmlURL == "" {
		return ""
	}

	// Parse the URL to extract the path
	// Expected format: https://github.com/owner/repo/...
	// We want to extract "owner/repo"

	// Simple string parsing approach for GitHub URLs
	const githubPrefix = "https://github.com/"
	if !strings.HasPrefix(htmlURL, githubPrefix) {
		return ""
	}

	// Remove the github.com prefix
	path := strings.TrimPrefix(htmlURL, githubPrefix)

	// Split by '/' and take first two parts (owner/repo)
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}

	owner := parts[0]
	repo := parts[1]

	// Basic validation - owner and repo should not be empty
	if owner == "" || repo == "" {
		return ""
	}

	return owner + "/" + repo
}

// extractAndDedupeEmails collects emails from user profile and commits, returning deduplicated list.
func extractAndDedupeEmails(userCtx *UserContext) []string {
	emailSet := make(map[string]bool)
	var emails []string

	// Add user profile email if available
	if userCtx.User != nil && userCtx.User.Email != "" {
		email := strings.ToLower(strings.TrimSpace(userCtx.User.Email))
		if isValidEmail(email) && !emailSet[email] {
			emailSet[email] = true
			emails = append(emails, email)
		}
	}

	// Add emails from commit activities
	for _, commitActivity := range userCtx.CommitActivities {
		// Add author email
		if commitActivity.AuthorEmail != "" {
			email := strings.ToLower(strings.TrimSpace(commitActivity.AuthorEmail))
			if isValidEmail(email) && !emailSet[email] {
				emailSet[email] = true
				emails = append(emails, email)
			}
		}

		// Add committer email (often different from author)
		if commitActivity.CommitterEmail != "" {
			email := strings.ToLower(strings.TrimSpace(commitActivity.CommitterEmail))
			if isValidEmail(email) && !emailSet[email] {
				emailSet[email] = true
				emails = append(emails, email)
			}
		}
	}

	return emails
}

// isValidEmail performs basic email validation.
func isValidEmail(email string) bool {
	if email == "" || len(email) > 254 {
		return false
	}

	// Basic validation - must contain @ and domain
	if !strings.Contains(email, "@") {
		return false
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}

	// Skip noreply and bot emails that aren't useful for location detection
	if strings.Contains(email, "noreply") ||
		strings.Contains(email, "users.noreply.github.com") ||
		strings.Contains(email, "+bot@") ||
		strings.Contains(email, "bot@") {
		return false
	}

	return true
}

// getContextDataKeys extracts the keys from context data for debugging.
