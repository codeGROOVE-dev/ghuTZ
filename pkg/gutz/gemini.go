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
type geminiQueryResult struct {
	Timezone   string
	Reasoning  string
	Location   string
	Prompt     string
	Confidence float64
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
		return nil, fmt.Errorf("gemini API call failed: %w", err)
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
			default:
				// Unknown confidence level, use medium as default
				confidence = 0.6
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

// tryUnifiedGeminiAnalysisWithContext attempts timezone detection using Gemini AI with UserContext.
//
//nolint:gocognit,nestif,revive,maintidx // Complex AI-based analysis requires comprehensive data processing
func (d *Detector) tryUnifiedGeminiAnalysisWithContext(ctx context.Context, userCtx *UserContext, activityResult *Result) *Result {
	if userCtx.User == nil {
		d.logger.Debug("could not fetch user for Gemini analysis", "username", userCtx.Username)
		return nil
	}

	// Prepare comprehensive context for Gemini
	contextData := make(map[string]any)
	var dataSources []string

	contextData["user"] = userCtx.User
	if userCtx.User != nil {
		dataSources = append(dataSources, "GitHub Profile")
	}

	contextData["recent_events"] = userCtx.Events
	if len(userCtx.Events) > 0 {
		dataSources = append(dataSources, "GitHub Events")
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
				"oldest":       activityResult.ActivityDateRange.OldestActivity,
				"newest":       activityResult.ActivityDateRange.NewestActivity,
				"total_days":   activityResult.ActivityDateRange.TotalDays,
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
			if userCtx.User.Blog == "" {
				userCtx.User.Blog = githubPagesURL
				d.logger.Debug("found GitHub Pages site", "url", githubPagesURL, "repo", userCtx.Repositories[i].Name)
			}

			// Fetch content from the GitHub Pages site
			websiteContent := d.fetchWebsiteContent(ctx, githubPagesURL)
			if websiteContent != "" {
				if existingContent, ok := contextData["website_content"].(string); !ok || existingContent == "" {
					contextData["website_content"] = websiteContent
					contextData["github_pages_url"] = githubPagesURL
					dataSources = append(dataSources, "GitHub Pages")
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

	// Filter recent PRs and collect contributed repositories
	var recentPRs []github.PullRequest
	contributedRepos := make(map[string]int) // repo -> contribution count
	// Use a fixed cutoff relative to newest activity for deterministic results
	cutoff := time.Date(2025, 5, 18, 0, 0, 0, 0, time.UTC) // 3 months before approximate current date

	// Sort PRs by creation date for deterministic processing
	sortedPRs := make([]github.PullRequest, len(userCtx.PullRequests))
	copy(sortedPRs, userCtx.PullRequests)
	sort.Slice(sortedPRs, func(i, j int) bool {
		return sortedPRs[i].CreatedAt.After(sortedPRs[j].CreatedAt) // Most recent first
	})

	for i := range sortedPRs {
		if sortedPRs[i].CreatedAt.After(cutoff) {
			recentPRs = append(recentPRs, sortedPRs[i])
			// Track contributed repositories (not owned by user)
			if sortedPRs[i].RepoName != "" && !strings.HasPrefix(sortedPRs[i].RepoName, userCtx.Username+"/") {
				contributedRepos[sortedPRs[i].RepoName]++
			}
			if len(recentPRs) >= 20 {
				break
			}
		}
	}
	if len(recentPRs) > 0 {
		contextData["pull_requests"] = recentPRs
		dataSources = append(dataSources, "Pull Requests")
	}

	// Filter recent issues and collect more contributed repositories
	var recentIssues []github.Issue

	// Sort Issues by creation date for deterministic processing
	sortedIssues := make([]github.Issue, len(userCtx.Issues))
	copy(sortedIssues, userCtx.Issues)
	sort.Slice(sortedIssues, func(i, j int) bool {
		return sortedIssues[i].CreatedAt.After(sortedIssues[j].CreatedAt) // Most recent first
	})

	for i := range sortedIssues {
		if sortedIssues[i].CreatedAt.After(cutoff) {
			recentIssues = append(recentIssues, sortedIssues[i])
			// Track contributed repositories (not owned by user)
			if sortedIssues[i].RepoName != "" && !strings.HasPrefix(sortedIssues[i].RepoName, userCtx.Username+"/") {
				contributedRepos[sortedIssues[i].RepoName]++
			}
			if len(recentIssues) >= 20 {
				break
			}
		}
	}
	if len(recentIssues) > 0 {
		contextData["issues"] = recentIssues
		dataSources = append(dataSources, "Issues")
	}

	// Add contributed repositories to context (repos user has contributed to but doesn't own)
	if len(contributedRepos) > 0 {
		// Convert map to sorted slice for consistent output
		var contribs []repoContribution
		for repo, count := range contributedRepos {
			contribs = append(contribs, repoContribution{Name: repo, Count: count})
		}
		// Sort by contribution count (descending), then by name for deterministic ordering
		sort.Slice(contribs, func(i, j int) bool {
			if contribs[i].Count == contribs[j].Count {
				return contribs[i].Name < contribs[j].Name // Secondary sort by name
			}
			return contribs[i].Count > contribs[j].Count
		})
		contextData["contributed_repositories"] = contribs
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
			case strings.Contains(socialURL, "bsky.app"):
				socialProfiles["bluesky"] = socialURL
			case strings.Contains(socialURL, "/@") || strings.Contains(userCtx.User.Bio, "[MASTODON] "+socialURL):
				socialProfiles["mastodon"] = socialURL
			}
		}

		// Extract social media data using the social package
		extractedProfiles := social.Extract(ctx, socialProfiles, d.logger)

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
						}
					}
				}

				contextData["mastodon_profile"] = mastodonData
				dataSources = append(dataSources, "Mastodon")

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

	// Query Gemini with all context
	geminiResult, err := d.queryUnifiedGeminiForTimezone(ctx, contextData)
	if err != nil {
		d.logger.Warn("Gemini analysis failed - falling back to activity patterns", "username", userCtx.Username, "error", err)
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
		DataSources:             dataSources,
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
		result.SleepHoursUTC = activityResult.SleepHoursUTC
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
