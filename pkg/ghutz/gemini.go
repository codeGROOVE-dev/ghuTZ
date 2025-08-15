package ghutz

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

type geminiResponse struct {
	Timezone       string
	OffsetUTC      string
	Confidence     float64
	LocationSource string
	DST            string
	Error          string
}

// queryUnifiedGeminiForTimezone queries Gemini AI for timezone detection.
func (d *Detector) queryUnifiedGeminiForTimezone(ctx context.Context, contextData map[string]interface{}, verbose bool) (string, string, float64, string, string, error) {
	// Check if we have activity data for confidence scoring later
	hasActivityData := false
	if hourCounts, ok := contextData["hour_counts"].(map[int]int); ok && len(hourCounts) > 0 {
		hasActivityData = true
	}

	// Format all evidence into a comprehensive prompt
	evidence := d.formatEvidenceForGemini(contextData)

	// Use the unified prompt template and inject evidence
	promptTemplate := unifiedGeminiPrompt()
	prompt := fmt.Sprintf(promptTemplate, evidence)

	if verbose {
		d.logger.Debug("querying Gemini with comprehensive context", "prompt_length", len(prompt))
	}

	resp, err := d.callGeminiWithSDK(ctx, prompt, verbose)
	if err != nil {
		return "", "", 0, "", "", fmt.Errorf("Gemini API call failed: %w", err)
	}

	// Check for API error
	if resp.Error != "" {
		return "", "", 0, "", "", fmt.Errorf("Gemini returned error: %s", resp.Error)
	}

	// Calculate confidence based on response and available data
	confidence := resp.Confidence

	// Adjust confidence based on data availability
	if !hasActivityData && confidence > 0.7 {
		// Without activity data, cap confidence at 70%
		confidence = 0.7
	}

	// If we have strong activity patterns, boost confidence slightly
	if hasActivityData && confidence < 0.9 {
		confidence = math.Min(0.9, confidence*1.1)
	}

	return resp.Timezone, resp.OffsetUTC, confidence, resp.LocationSource, resp.DST, nil
}


// tryUnifiedGeminiAnalysisWithEvents attempts timezone detection using Gemini AI with event context.
func (d *Detector) tryUnifiedGeminiAnalysisWithEvents(ctx context.Context, username string, activityResult *Result, events []PublicEvent) *Result {
	// Skip if no Gemini API key
	if d.geminiAPIKey == "" {
		d.logger.Debug("skipping Gemini analysis - no API key configured")
		return nil
	}

	user := d.fetchUser(ctx, username)
	if user == nil {
		d.logger.Debug("could not fetch user for Gemini analysis", "username", username)
		return nil
	}

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
		
		// Add active hours if available
		if activityResult.ActiveHoursLocal.Start > 0 || activityResult.ActiveHoursLocal.End > 0 {
			contextData["work_hours"] = []float64{
				activityResult.ActiveHoursLocal.Start,
				activityResult.ActiveHoursLocal.End,
			}
		}
		
		// Add lunch break if detected
		if activityResult.LunchHoursLocal.Confidence > 0 {
			contextData["lunch_break"] = []float64{
				activityResult.LunchHoursLocal.Start,
				activityResult.LunchHoursLocal.End,
			}
			contextData["lunch_confidence"] = activityResult.LunchHoursLocal.Confidence
		}
		
		// Add peak productivity if available
		if activityResult.PeakProductivity.Count > 0 {
			contextData["peak_productivity"] = []float64{
				activityResult.PeakProductivity.Start,
				activityResult.PeakProductivity.End,
			}
		}
	}

	// Fetch supplemental data for comprehensive analysis
	wg := sync.WaitGroup{}
	var organizations []Organization
	var repos []Repository
	var pullRequests []PullRequest
	var issues []Issue

	// Fetch organizations
	wg.Add(1)
	go func() {
		defer wg.Done()
		orgs, err := d.fetchOrganizations(ctx, username)
		if err == nil {
			organizations = orgs
		}
	}()

	// Fetch repositories
	wg.Add(1)
	go func() {
		defer wg.Done()
		pinnedRepos, _ := d.fetchPinnedRepositories(ctx, username)
		popularRepos, _ := d.fetchPopularRepositories(ctx, username)

		// Combine and deduplicate
		repoMap := make(map[string]Repository)
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		prs, err := d.fetchPullRequests(ctx, username)
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		iss, err := d.fetchIssues(ctx, username)
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

	wg.Wait()

	// Add to context
	if len(organizations) > 0 {
		contextData["organizations"] = organizations
	}
	if len(repos) > 0 {
		contextData["repositories"] = repos
	}
	if len(pullRequests) > 0 {
		contextData["pull_requests"] = pullRequests
	}
	if len(issues) > 0 {
		contextData["issues"] = issues
	}

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

	// Extract social media URLs
	socialURLs := extractSocialMediaURLs(user)
	if len(socialURLs) > 0 {
		contextData["social_media_urls"] = socialURLs

		// Check for country TLDs
		tlds := extractCountryTLDs(socialURLs...)
		if len(tlds) > 0 {
			contextData["country_tlds"] = tlds
		}
	}

	// Name-based hints
	if user.Name != "" {
		if isPolishName(user.Name) {
			contextData["name_hint"] = "Polish name detected - likely in Poland (Europe/Warsaw)"
		}
	}

	// Query Gemini with all context
	timezone, offsetStr, confidence, locationSource, dstInfo, err := d.queryUnifiedGeminiForTimezone(ctx, contextData, false)
	if err != nil {
		d.logger.Debug("Gemini analysis failed", "error", err)
		return nil
	}

	// If Gemini returns low confidence, don't use it
	if confidence < 0.3 {
		d.logger.Debug("Gemini confidence too low", "confidence", confidence)
		return nil
	}

	result := &Result{
		Username:                username,
		Timezone:                timezone,
		TimezoneConfidence:      confidence,
		Method:                  "gemini_analysis",
		GeminiSuggestedLocation: locationSource,
		GeminiReasoning:         fmt.Sprintf("Gemini AI analysis (%s)", locationSource),
	}

	// Add offset and DST information to reasoning
	if offsetStr != "" || (dstInfo != "" && dstInfo != "unknown") {
		reasoning := []string{result.GeminiReasoning}
		if offsetStr != "" {
			reasoning = append(reasoning, fmt.Sprintf("UTC offset: %s", offsetStr))
		}
		if dstInfo != "" && dstInfo != "unknown" {
			reasoning = append(reasoning, fmt.Sprintf("DST: %s", dstInfo))
		}
		result.GeminiReasoning = strings.Join(reasoning, "; ")
	}

	// If we have coordinates, add them
	if detectedLocation != nil {
		result.Location = detectedLocation
	}

	// Merge with activity result if available
	if activityResult != nil {
		result.ActiveHoursLocal = activityResult.ActiveHoursLocal
		result.QuietHoursUTC = activityResult.QuietHoursUTC
		result.HourlyActivityUTC = activityResult.HourlyActivityUTC
		result.LunchHoursLocal = activityResult.LunchHoursLocal
		result.PeakProductivity = activityResult.PeakProductivity
	}

	return result
}