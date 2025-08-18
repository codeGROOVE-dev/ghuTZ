package ghutz

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	
	"github.com/codeGROOVE-dev/ghuTZ/pkg/github"
)

// formatEvidenceForGemini formats detection evidence for Gemini API analysis.
func (d *Detector) formatEvidenceForGemini(contextData map[string]interface{}) string {
	var sb strings.Builder

	// User profile information
	if user, ok := contextData["user"].(*github.GitHubUser); ok && user != nil {
		sb.WriteString("GitHub Profile:\n")
		if user.Name != "" {
			sb.WriteString(fmt.Sprintf("- Name: %s\n", user.Name))
		}
		if user.Location != "" {
			sb.WriteString(fmt.Sprintf("- Location: %s\n", user.Location))
		}
		if user.Company != "" {
			sb.WriteString(fmt.Sprintf("- Company: %s\n", user.Company))
		}
		if user.Bio != "" {
			sb.WriteString(fmt.Sprintf("- Bio: %s\n", user.Bio))
		}
		if user.Blog != "" {
			sb.WriteString(fmt.Sprintf("- Website: %s\n", user.Blog))
		}
		if user.TwitterHandle != "" {
			sb.WriteString(fmt.Sprintf("- Twitter: @%s\n", user.TwitterHandle))
		}
		sb.WriteString("\n")
	} else {
		// Debug why user is not showing
		if user == nil {
			sb.WriteString("GitHub Profile: (user data not available)\n\n")
		} else {
			sb.WriteString("GitHub Profile: (type assertion failed)\n\n")
		}
	}

	// Activity date range
	if dateRange, ok := contextData["activity_date_range"].(map[string]interface{}); ok {
		if oldest, ok := dateRange["oldest"].(time.Time); ok {
			if newest, ok := dateRange["newest"].(time.Time); ok {
				if totalDays, ok := dateRange["total_days"].(int); ok {
					if totalEvents, ok := dateRange["total_events"].(int); ok {
						sb.WriteString(fmt.Sprintf("Activity date range: %d events from %s to %s (%d days)\n",
							totalEvents,
							oldest.Format("2006-01-02"),
							newest.Format("2006-01-02"),
							totalDays))
					}
				}
			}
		}
		sb.WriteString("\n")
	}
	
	// Activity patterns
	if quietHours, ok := contextData["quiet_hours"].([]int); ok && len(quietHours) > 0 {
		sb.WriteString(fmt.Sprintf("Quiet Hours (UTC): %v\n", quietHours))
	}
	
	// Weekend vs weekday activity patterns (cultural work week indicator)
	if weekendActivity, ok := contextData["weekend_activity_ratio"].(float64); ok {
		sb.WriteString(fmt.Sprintf("\nWeekend vs Weekday Activity:\n"))
		sb.WriteString(fmt.Sprintf("- Weekend activity: %.1f%% of weekday activity\n", weekendActivity*100))
		
		// Interpret the pattern
		if weekendActivity < 0.3 {
			sb.WriteString("- Pattern: Strong work/life separation (typical employee)\n")
		} else if weekendActivity > 0.7 {
			sb.WriteString("- Pattern: Continuous activity (OSS maintainer or flexible schedule)\n")
		} else {
			sb.WriteString("- Pattern: Moderate weekend activity\n")
		}
	}
	
	// Day of week patterns
	if dayActivity, ok := contextData["day_of_week_activity"].(map[string]int); ok && len(dayActivity) > 0 {
		sb.WriteString("Activity by day of week:\n")
		days := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
		for _, day := range days {
			if count, exists := dayActivity[day]; exists && count > 0 {
				sb.WriteString(fmt.Sprintf("- %s: %d events\n", day, count))
			}
		}
	}

	// Convert work hours to UTC if we have them and the offset
	if workHours, ok := contextData["work_hours_utc"].([]int); ok && len(workHours) == 2 {
		sb.WriteString(fmt.Sprintf("Active Hours (UTC): %02d:00 - %02d:00\n", workHours[0], workHours[1]))
	}

	// Hourly activity distribution (UTC) - very revealing pattern
	if hourCounts, ok := contextData["hour_counts"].(map[int]int); ok && len(hourCounts) > 0 {
		sb.WriteString("\nHourly Activity Distribution (UTC):\n")
		for hour := 0; hour < 24; hour++ {
			if count, exists := hourCounts[hour]; exists && count > 0 {
				sb.WriteString(fmt.Sprintf("- %02d:00: %d events\n", hour, count))
			}
		}
	}
	
	// Use unified timezone candidates if available
	if candidates, ok := contextData["timezone_candidates"].([]TimezoneCandidate); ok && len(candidates) > 0 {
		sb.WriteString("\nTimezone Candidates (unified analysis):\n")
		for i, candidate := range candidates {
			if i >= 5 {
				break // Only show top 5
			}
			// Display the confidence with enhanced dynamic range
			// Raw scores are typically 15-45, with small differences being significant
			displayConfidence := candidate.Confidence
			
			// Normalize scores relative to the best candidate for better differentiation
			// This amplifies small differences which are actually meaningful
			bestScore := candidates[0].Confidence
			if bestScore > 0 {
				// Calculate relative score (0-1 range, where 1 = best candidate)
				relativeScore := displayConfidence / bestScore
				
				// Apply non-linear scaling to amplify differences
				// This makes small differences more visible
				// Best candidate gets ~85-95%, second best ~60-75%, third ~40-60%
				if i == 0 {
					// Best candidate: 85-95% range
					displayConfidence = 85 + (relativeScore * 10)
				} else {
					// Other candidates: scale based on how far they are from best
					// Use exponential scaling to amplify differences
					displayConfidence = math.Pow(relativeScore, 2.5) * 95
				}
			} else {
				// Fallback to simple scaling if no best score
				displayConfidence = displayConfidence * 2.0
			}
			
			// Ensure reasonable bounds
			displayConfidence = math.Max(10, math.Min(95, displayConfidence))
			
			sb.WriteString(fmt.Sprintf("%d. %s - %.1f%% confidence\n", 
				i+1, candidate.Timezone, displayConfidence))
			
			// Show detailed timings
			sb.WriteString(fmt.Sprintf("   → Sleep midpoint: %.1f local\n", candidate.SleepMidLocal))
			sb.WriteString(fmt.Sprintf("   → Work starts: %d:00am local\n", candidate.WorkStartLocal))
			
			// Show lunch with specific time and dip strength
			if candidate.LunchReasonable && candidate.LunchLocalTime > 0 {
				lunchHour := int(candidate.LunchLocalTime)
				lunchMin := int((candidate.LunchLocalTime - float64(lunchHour)) * 60)
				dipPercent := candidate.LunchDipStrength * 100
				sb.WriteString(fmt.Sprintf("   → Lunch: %d:%02d local (%.0f%% activity drop)\n", 
					lunchHour, lunchMin, dipPercent))
			} else if candidate.LunchLocalTime > 0 {
				lunchHour := int(candidate.LunchLocalTime)
				lunchMin := int((candidate.LunchLocalTime - float64(lunchHour)) * 60)
				sb.WriteString(fmt.Sprintf("   → Lunch: %d:%02d local (⚠️ unusual time)\n", 
					lunchHour, lunchMin))
			} else {
				sb.WriteString("   → Lunch: ❌ No detectable pattern (suggests wrong timezone)\n")
			}
			
			// Evening activity
			if candidate.EveningActivity > 0 {
				sb.WriteString(fmt.Sprintf("   → Evening activity (7-11pm local): %d events\n", candidate.EveningActivity))
			}
			
			// Summary indicators
			if candidate.WorkHoursNormal && candidate.LunchReasonable {
				sb.WriteString("   → Pattern: ✓ Consistent with timezone\n")
			} else if candidate.WorkHoursNormal || candidate.LunchReasonable {
				sb.WriteString("   → Pattern: ⚠️ Partially consistent\n")
			} else {
				sb.WriteString("   → Pattern: ❌ Inconsistent signals\n")
			}
		}
	}
	
	// The activity timezone is now the same as the top candidate, no conflict to report
	
	// Check for potential mismatches between profile and detected timezone
	if candidates, ok := contextData["timezone_candidates"].([]TimezoneCandidate); ok && len(candidates) > 0 {
		topCandidate := candidates[0]
		
		// Check if website content suggests different location than detected
		if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
			websiteLower := strings.ToLower(websiteContent)
			
			// Check for location mentions in website
			hasEuropeanMention := strings.Contains(websiteLower, "spain") || 
			                      strings.Contains(websiteLower, "asturias") ||
			                      strings.Contains(websiteLower, "europe") ||
			                      strings.Contains(websiteLower, "amsterdam") ||
			                      strings.Contains(websiteLower, "netherlands")
			                      
			hasUSMention := strings.Contains(websiteLower, "silicon valley") ||
			                strings.Contains(websiteLower, "california") ||
			                strings.Contains(websiteLower, "san francisco")
			
			if topCandidate.Offset < 0 && hasEuropeanMention {
				sb.WriteString("\n⚠️ Location Mismatch: Website mentions European locations but activity suggests US timezone\n")
				sb.WriteString("  Possible explanations:\n")
				sb.WriteString("  • Remote work for US company from Europe\n")
				sb.WriteString("  • Recently relocated but website not updated\n")
				sb.WriteString("  • Shifted schedule to collaborate with US team\n")
			} else if topCandidate.Offset >= 0 && hasUSMention {
				sb.WriteString("\n⚠️ Location Mismatch: Website mentions US locations but activity suggests European/Asian timezone\n")
				sb.WriteString("  Possible explanations:\n")
				sb.WriteString("  • Returned to Europe/Asia after US work\n")
				sb.WriteString("  • Remote work for European/Asian company from US\n")
				sb.WriteString("  • Natural night owl schedule\n")
			}
		}
		
		// Check for US company with non-US timezone pattern
		if user, ok := contextData["user"].(*github.GitHubUser); ok && user != nil {
			if strings.Contains(strings.ToLower(user.Company), "chainguard") ||
			   strings.Contains(strings.ToLower(user.Company), "google") ||
			   strings.Contains(strings.ToLower(user.Company), "meta") ||
			   strings.Contains(strings.ToLower(user.Company), "microsoft") {
				if topCandidate.Offset >= 0 {
					sb.WriteString("\n⚠️ Company Mismatch: Works for US tech company but shows non-US activity pattern\n")
					sb.WriteString("  Possible explanations:\n")
					sb.WriteString("  • Remote employee in different timezone\n")
					sb.WriteString("  • International office location\n")
				}
			}
		}
	}

	// Location evidence
	if location, ok := contextData["location"].(*Location); ok && location != nil {
		sb.WriteString("\nGeocoded Location:\n")
		sb.WriteString(fmt.Sprintf("- Coordinates: %.4f, %.4f\n", location.Latitude, location.Longitude))
	}

	// Organizations
	if orgs, ok := contextData["organizations"]; ok && orgs != nil {
		if orgsList, ok := orgs.([]github.Organization); ok && len(orgsList) > 0 {
			sb.WriteString("\nGitHub Organizations:\n")
			for _, org := range orgsList {
				if org.Location != "" {
					sb.WriteString(fmt.Sprintf("- %s (Location: %s)\n", org.Login, org.Location))
				} else {
					sb.WriteString(fmt.Sprintf("- %s\n", org.Login))
				}
				if org.Description != "" {
					sb.WriteString(fmt.Sprintf("  %s\n", org.Description))
				}
			}
		}
	}

	// Repository evidence - show user's own repositories with descriptions
	if repos, ok := contextData["repositories"].([]github.Repository); ok && len(repos) > 0 {
		// Separate pinned, non-fork, and fork repos
		var pinnedRepos []github.Repository
		var nonForkRepos []github.Repository
		var forkRepos []github.Repository
		
		for _, repo := range repos {
			// Note: We don't have IsPinned field anymore, so we'll just split by Fork status
			if !repo.Fork {
				nonForkRepos = append(nonForkRepos, repo)
			} else {
				forkRepos = append(forkRepos, repo)
			}
		}
		
		sb.WriteString("\nUser's Repositories:\n")
		
		// First show pinned repos (if any)
		for _, repo := range pinnedRepos {
			if repo.Fork {
				sb.WriteString(fmt.Sprintf("- %s: %s [FORK, PINNED]\n", repo.Name, repo.Description))
			} else {
				sb.WriteString(fmt.Sprintf("- %s: %s [PINNED]\n", repo.Name, repo.Description))
			}
		}
		
		// Then show all non-fork repos (these are most likely to have location clues)
		for _, repo := range nonForkRepos {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", repo.Name, repo.Description))
		}
		
		// If we have room and few non-forks, show some forks too (limit total to ~30)
		totalShown := len(pinnedRepos) + len(nonForkRepos)
		if totalShown < 30 {
			remaining := 30 - totalShown
			for i, repo := range forkRepos {
				if i >= remaining {
					break
				}
				sb.WriteString(fmt.Sprintf("- %s: %s [FORK]\n", repo.Name, repo.Description))
			}
		}
	}

	// Starred repositories - these can be very revealing for location/interests
	if starredRepos, ok := contextData["starred_repositories"].([]github.Repository); ok && len(starredRepos) > 0 {
		sb.WriteString("\nRecently Starred Repositories (location/interest clues):\n")
		for i, repo := range starredRepos {
			if i >= 25 { // Limit to 25 most recent
				break
			}
			if repo.Description != "" {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", repo.Name, repo.Description))
			} else {
				sb.WriteString(fmt.Sprintf("- %s\n", repo.Name))
			}
		}
	}

	// Recent activity date range (just the time span, not the event counts)
	if events, ok := contextData["recent_events"].([]github.PublicEvent); ok && len(events) > 0 {
		var firstEvent, lastEvent time.Time
		
		for i, event := range events {
			if i == 0 {
				firstEvent = event.CreatedAt
			}
			lastEvent = event.CreatedAt
		}
		
		sb.WriteString(fmt.Sprintf("\nActivity Date Range: %s to %s\n", 
			lastEvent.Format("2006-01-02"), 
			firstEvent.Format("2006-01-02")))
	}

	// Track repositories they're contributing to with counts
	repoContributions := make(map[string]int)
	
	// Pull requests
	if prs, ok := contextData["pull_requests"].([]github.PullRequest); ok && len(prs) > 0 {
		// Show recent PR titles for context
		sb.WriteString("\nRecent PR titles:\n")
		for i, pr := range prs {
			if i >= 10 { // Show up to 10 recent PRs
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", pr.Title))
			// Track repositories they're contributing to
			if pr.RepoName != "" {
				repoContributions[pr.RepoName]++
			}
		}
		
		// Track more repositories from issues
		if issues, ok := contextData["issues"].([]github.Issue); ok {
			for _, issue := range issues {
				if issue.RepoName != "" {
					repoContributions[issue.RepoName]++
				}
			}
		}
		
		// Show repositories they're contributing to (excluding already shown repos)
		if len(repoContributions) > 0 {
			// Build a set of already shown repos
			alreadyShown := make(map[string]bool)
			if repos, ok := contextData["repositories"].([]github.Repository); ok {
				for _, repo := range repos {
					alreadyShown[repo.FullName] = true
				}
			}
			
			// Sort repos by contribution count
			type repoCount struct {
				name  string
				count int
			}
			var sortedRepos []repoCount
			for repo, count := range repoContributions {
				// Skip if already shown in user's repositories
				if alreadyShown[repo] {
					continue
				}
				sortedRepos = append(sortedRepos, repoCount{repo, count})
			}
			
			// Sort by count descending
			for i := 0; i < len(sortedRepos); i++ {
				for j := i + 1; j < len(sortedRepos); j++ {
					if sortedRepos[j].count > sortedRepos[i].count {
						sortedRepos[i], sortedRepos[j] = sortedRepos[j], sortedRepos[i]
					}
				}
			}
			
			sb.WriteString("\nOther projects they've been working on (sorted by activity):\n")
			for i, repo := range sortedRepos {
				if i >= 25 {
					break
				}
				sb.WriteString(fmt.Sprintf("- %s (%d contributions)\n", repo.name, repo.count))
			}
		}
		
		// Also check for timezone mentions
		for _, pr := range prs {
			if pr.Body != "" && (strings.Contains(strings.ToLower(pr.Body), "timezone") || 
				strings.Contains(strings.ToLower(pr.Body), "time zone") ||
				strings.Contains(strings.ToLower(pr.Body), "local time")) {
				sb.WriteString(fmt.Sprintf("- PR with timezone mention: %s\n", pr.Title))
				break
			}
		}
	}

	// Issues
	if issues, ok := contextData["issues"].([]github.Issue); ok && len(issues) > 0 {
		// Show recent issue titles for context
		sb.WriteString("\nRecent issue titles:\n")
		for i, issue := range issues {
			if i >= 10 { // Show up to 10 recent issues
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", issue.Title))
		}
	}

	// Find longest PR body (excluding templates) as a writing sample
	if prs, ok := contextData["pull_requests"].([]github.PullRequest); ok && len(prs) > 0 {
		longestBody := ""
		maxLength := 0
		for _, pr := range prs {
			// Skip likely templates
			if strings.Contains(pr.Body, "<!-- ") || strings.Contains(pr.Body, "## Checklist") {
				continue
			}
			if len(pr.Body) > maxLength && len(pr.Body) > 100 {
				maxLength = len(pr.Body)
				longestBody = pr.Body
			}
		}
		if longestBody != "" {
			// Truncate if too long
			if len(longestBody) > 500 {
				longestBody = longestBody[:500] + "..."
			}
			sb.WriteString(fmt.Sprintf("\nLongest PR description (%d chars):\n%s\n", maxLength, longestBody))
		}
	}
	
	// Website content
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		sb.WriteString("\nWebsite/Blog Content:\n")
		// Just pass the raw content to Gemini, truncated if too long
		contentPreview := websiteContent
		if len(contentPreview) > 2000 {
			contentPreview = contentPreview[:2000] + "..."
		}
		sb.WriteString(contentPreview)
		sb.WriteString("\n")
	}
	
	// Comprehensive Mastodon profile data
	if mastodonProfile, ok := contextData["mastodon_profile"].(*MastodonProfileData); ok && mastodonProfile != nil {
		sb.WriteString("\nMastodon Profile Data:\n")
		
		// Username and display name
		if mastodonProfile.Username != "" {
			sb.WriteString(fmt.Sprintf("Username: @%s\n", mastodonProfile.Username))
		}
		if mastodonProfile.DisplayName != "" {
			sb.WriteString(fmt.Sprintf("Display Name: %s\n", mastodonProfile.DisplayName))
		}
		
		// Bio
		if mastodonProfile.Bio != "" {
			sb.WriteString(fmt.Sprintf("Bio: %s\n", mastodonProfile.Bio))
		}
		
		// Profile fields (key-value pairs) - sort keys for deterministic output
		if len(mastodonProfile.ProfileFields) > 0 {
			sb.WriteString("Profile Fields:\n")
			// Sort keys for deterministic iteration order
			var fieldKeys []string
			for key := range mastodonProfile.ProfileFields {
				fieldKeys = append(fieldKeys, key)
			}
			sort.Strings(fieldKeys)
			for _, key := range fieldKeys {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", key, mastodonProfile.ProfileFields[key]))
			}
		}
		
		// Hashtags
		if len(mastodonProfile.Hashtags) > 0 {
			sb.WriteString(fmt.Sprintf("Hashtags: %s\n", strings.Join(mastodonProfile.Hashtags, ", ")))
		}
		
		// Joined date
		if mastodonProfile.JoinedDate != "" {
			sb.WriteString(fmt.Sprintf("Mastodon Joined: %s\n", mastodonProfile.JoinedDate))
		}
		
		// Websites found
		if len(mastodonProfile.Websites) > 0 {
			sb.WriteString(fmt.Sprintf("Websites from Mastodon: %s\n", strings.Join(mastodonProfile.Websites, ", ")))
		}
	}
	
	// Content from Mastodon-discovered websites
	if websiteContents, ok := contextData["mastodon_website_contents"].(map[string]string); ok && len(websiteContents) > 0 {
		sb.WriteString("\nContent from Mastodon-linked websites:\n")
		for website, content := range websiteContents {
			sb.WriteString(fmt.Sprintf("\nFrom %s:\n", website))
			contentPreview := content
			if len(contentPreview) > 1500 {
				contentPreview = contentPreview[:1500] + "..."
			}
			sb.WriteString(contentPreview)
			sb.WriteString("\n")
		}
	}

	// Social media
	if socialURLs, ok := contextData["social_media_urls"].([]string); ok && len(socialURLs) > 0 {
		sb.WriteString("\nSocial Media Profiles:\n")
		for _, url := range socialURLs {
			if strings.Contains(url, "twitter.com") || strings.Contains(url, "x.com") {
				sb.WriteString(fmt.Sprintf("- Twitter/X: %s\n", url))
			} else if strings.Contains(url, "linkedin.com") {
				sb.WriteString(fmt.Sprintf("- LinkedIn: %s\n", url))
			} else if strings.Contains(url, "@") {
				sb.WriteString(fmt.Sprintf("- Mastodon: %s\n", url))
			} else {
				sb.WriteString(fmt.Sprintf("- %s\n", url))
			}
		}
	}

	// Country TLDs
	if tlds, ok := contextData["country_tlds"].([]CountryTLD); ok && len(tlds) > 0 {
		sb.WriteString("\nCountry-specific domains found:\n")
		for _, tld := range tlds {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", tld.TLD, tld.Country))
		}
	}

	// Evening activity pattern (strong timezone signal)
	if eveningHours, ok := contextData["evening_activity_hours"].([]int); ok && len(eveningHours) > 0 {
		sb.WriteString(fmt.Sprintf("\nEvening Activity Hours (7-11pm window): %v\n", eveningHours))
		if eveningPct, ok := contextData["evening_activity_percentage"].(float64); ok {
			sb.WriteString(fmt.Sprintf("Evening Activity Percentage: %.1f%%\n", eveningPct))
		}
	}

	// Lunch break pattern in UTC
	if lunchHours, ok := contextData["lunch_break_utc"].([]int); ok && len(lunchHours) >= 2 {
		sb.WriteString(fmt.Sprintf("\nLunch Break Pattern (UTC): %02d:00 - %02d:00\n", lunchHours[0], lunchHours[1]))
		if confidence, ok := contextData["lunch_confidence"].(float64); ok {
			sb.WriteString(fmt.Sprintf("Lunch Break Confidence: %.0f%%\n", confidence*100))
		}
	}

	// Peak productivity hours in UTC
	if peakHours, ok := contextData["peak_productivity_utc"].([]int); ok && len(peakHours) >= 2 {
		sb.WriteString(fmt.Sprintf("\nPeak Productivity (UTC): %02d:00 - %02d:00\n", peakHours[0], peakHours[1]))
	}

	// Name hints removed - Gemini analyzes names directly
	
	// Commit message samples for language analysis
	if commitSamples, ok := contextData["commit_message_samples"].([]CommitMessageSample); ok && len(commitSamples) > 0 {
		sb.WriteString("\nSample Commit Messages:\n")
		for i, sample := range commitSamples {
			if i >= 10 { // Limit to 10 samples
				break
			}
			// Truncate long messages
			msg := sample.Message
			if len(msg) > 100 {
				msg = msg[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf("- %s\n", msg))
		}
	}
	
	// Text samples from PRs/issues
	if textSamples, ok := contextData["text_samples"].([]string); ok && len(textSamples) > 0 {
		sb.WriteString("\nSample PR/Issue/Comment Text:\n")
		for i, sample := range textSamples {
			if i >= 8 { // Limit to 8 samples
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", sample))
		}
	}
	
	// Skip collaborator analysis to reduce complexity

	// Additional context from profile scraping
	if scrapedTimezone, ok := contextData["scraped_timezone"].(string); ok && scrapedTimezone != "" {
		sb.WriteString(fmt.Sprintf("\nTimezone from profile scraping: %s\n", scrapedTimezone))
	}

	return sb.String()
}