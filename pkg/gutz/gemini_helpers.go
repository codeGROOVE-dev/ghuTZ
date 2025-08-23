package gutz

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
)

// Constants for data limits and thresholds.
const (
	maxUserRepos          = 40
	maxStarredRepos       = 15
	maxExternalContribs   = 15
	maxRecentPRs          = 10
	maxRecentIssues       = 10
	maxRecentCommits      = 10
	maxTextSamples        = 8
	maxLocationIndicators = 5
	maxTopCandidates      = 4
	maxDetailedCandidates = 3
	commitMessageMaxLen   = 132
	websiteContentMaxLen  = 4000
	mastodonContentMaxLen = 3000
	workStartEarliest     = 5
	workStartLatest       = 10
)

// repoContribution tracks contributions to a repository.
type repoContribution struct {
	Name  string
	Count int
}

// formatEvidenceForGemini formats detection evidence for Gemini API analysis.
// It organizes the evidence by signal strength: direct location signals,
// activity-based timezone constraints, repository geography, recent activity,
// work patterns, and website content for hobby detection.
//
//nolint:gocognit,revive,maintidx // Comprehensive evidence formatting requires detailed analysis
func (d *Detector) formatEvidenceForGemini(contextData map[string]any) string {
	var sb strings.Builder

	// Section 1: Direct location evidence (highest priority).
	sb.WriteString("=== PRIMARY LOCATION SIGNALS ===\n\n")

	// User profile is the most direct signal.
	if user, ok := contextData["user"].(*github.User); ok && user != nil {
		sb.WriteString("GitHub Profile:\n")
		if user.Name != "" {
			fmt.Fprintf(&sb, "- Name: %s\n", user.Name)
		}
		if user.Location != "" {
			fmt.Fprintf(&sb, "- Location: %s\n", user.Location)
		}
		if user.Company != "" {
			fmt.Fprintf(&sb, "- Company: %s\n", user.Company)
		}
		if user.Bio != "" {
			fmt.Fprintf(&sb, "- Bio: %s\n", user.Bio)
		}
		if user.Blog != "" {
			fmt.Fprintf(&sb, "- Website: %s\n", user.Blog)
		}
		if user.TwitterHandle != "" {
			fmt.Fprintf(&sb, "- Twitter: @%s\n", user.TwitterHandle)
		}
		if user.Email != "" {
			fmt.Fprintf(&sb, "- Email: %s\n", user.Email)
		}
		sb.WriteString("\n")
	}

	// Display deduped emails from all sources if available
	if emails, ok := contextData["emails"].([]string); ok && len(emails) > 0 {
		sb.WriteString("Collected Email Addresses:\n")
		for _, email := range emails {
			fmt.Fprintf(&sb, "- %s\n", email)
		}
		sb.WriteString("\n")
	}

	// Display social accounts from GraphQL
	if socialAccounts, ok := contextData["social_accounts"].([]github.SocialAccount); ok && len(socialAccounts) > 0 {
		sb.WriteString("Social Media Accounts:\n")
		for _, account := range socialAccounts {
			fmt.Fprintf(&sb, "- %s: %s", account.Provider, account.URL)
			if account.DisplayName != "" {
				fmt.Fprintf(&sb, " (%s)", account.DisplayName)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Organizations with their locations and descriptions.
	if orgs, ok := contextData["organizations"].([]github.Organization); ok && len(orgs) > 0 {
		sb.WriteString("GitHub Organizations:\n")
		for _, org := range orgs {
			if org.Name != "" && org.Name != org.Login {
				fmt.Fprintf(&sb, "- %s (%s)", org.Login, org.Name)
			} else {
				fmt.Fprintf(&sb, "- %s", org.Login)
			}
			if org.Location != "" {
				fmt.Fprintf(&sb, " - Location: %s", org.Location)
			}
			if org.Description != "" {
				fmt.Fprintf(&sb, " - %s", org.Description)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Country-specific domains provide strong location signals.
	if tlds, ok := contextData["country_tlds"].([]CountryTLD); ok && len(tlds) > 0 {
		sb.WriteString("Country domains: ")
		for i, tld := range tlds {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%s (%s)", tld.TLD, tld.Country)
		}
		sb.WriteString("\n\n")
	}

	// Social media profiles can reveal location information.
	if socialURLs, ok := contextData["social_media_urls"].([]string); ok && len(socialURLs) > 0 {
		sb.WriteString("Social media profiles:\n")
		for _, url := range socialURLs {
			switch {
			case strings.Contains(url, "twitter.com") || strings.Contains(url, "x.com"):
				fmt.Fprintf(&sb, "- Twitter/X: %s\n", url)
			case strings.Contains(url, "linkedin.com"):
				fmt.Fprintf(&sb, "- LinkedIn: %s\n", url)
			case strings.Contains(url, "@") || strings.Contains(url, "mastodon") || strings.Contains(url, "fosstodon"):
				fmt.Fprintf(&sb, "- Mastodon: %s\n", url)
			default:
				fmt.Fprintf(&sb, "- %s\n", url)
			}
		}
		sb.WriteString("\n")
	}

	// Twitter profile details if available.
	if twitterProfile, ok := contextData["twitter_profile"].(map[string]string); ok && twitterProfile != nil {
		sb.WriteString("Twitter/X profile details:\n")
		if username := twitterProfile["username"]; username != "" {
			fmt.Fprintf(&sb, "- Username: @%s\n", username)
		}
		if name := twitterProfile["name"]; name != "" {
			fmt.Fprintf(&sb, "- Name: %s\n", name)
		}
		if location := twitterProfile["location"]; location != "" {
			fmt.Fprintf(&sb, "- Location: %s\n", location)
		}
		if bio := twitterProfile["bio"]; bio != "" {
			fmt.Fprintf(&sb, "- Bio: %s\n", bio)
		}
		sb.WriteString("\n")
	}

	// BlueSky profile details if available.
	if blueSkyProfile, ok := contextData["bluesky_profile"].(map[string]string); ok && blueSkyProfile != nil {
		sb.WriteString("BlueSky profile details:\n")
		if handle := blueSkyProfile["handle"]; handle != "" {
			fmt.Fprintf(&sb, "- Handle: @%s\n", handle)
		}
		if name := blueSkyProfile["name"]; name != "" {
			fmt.Fprintf(&sb, "- Name: %s\n", name)
		}
		if bio := blueSkyProfile["bio"]; bio != "" {
			fmt.Fprintf(&sb, "- Bio: %s\n", bio)
		}
		sb.WriteString("\n")
	}

	// Mastodon profile details if available.
	if mastodonProfile, ok := contextData["mastodon_profile"].(*MastodonProfileData); ok && mastodonProfile != nil {
		sb.WriteString("Mastodon profile details:\n")
		if mastodonProfile.Username != "" {
			fmt.Fprintf(&sb, "- Username: @%s\n", mastodonProfile.Username)
		}
		if mastodonProfile.DisplayName != "" {
			fmt.Fprintf(&sb, "- Display name: %s\n", mastodonProfile.DisplayName)
		}
		if mastodonProfile.Bio != "" {
			fmt.Fprintf(&sb, "- Bio: %s\n", mastodonProfile.Bio)
		}
		if mastodonProfile.JoinedDate != "" {
			fmt.Fprintf(&sb, "- Joined: %s\n", mastodonProfile.JoinedDate)
		}

		// Display profile fields (these often contain location, pronouns, websites, etc.)
		if len(mastodonProfile.ProfileFields) > 0 {
			sb.WriteString("- Profile fields:\n")
			for key, value := range mastodonProfile.ProfileFields {
				fmt.Fprintf(&sb, "  • %s: %s\n", key, value)
			}
		}

		// Display extracted websites
		if len(mastodonProfile.Websites) > 0 {
			sb.WriteString("- Websites found:\n")
			for _, website := range mastodonProfile.Websites {
				fmt.Fprintf(&sb, "  • %s\n", website)
			}
		}

		// Display hashtags if present (can indicate interests/location)
		if len(mastodonProfile.Hashtags) > 0 {
			sb.WriteString("- Hashtags: ")
			for i, tag := range mastodonProfile.Hashtags {
				if i > 0 {
					sb.WriteString(", ")
				}
				fmt.Fprintf(&sb, "#%s", tag)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Section 2: Activity-based timezone constraints (mandatory).
	sb.WriteString("=== ACTIVITY TIMEZONE ANALYSIS ===\n\n")

	// Timezone candidates are critical constraints that must be respected.
	if candidates, ok := contextData["timezone_candidates"].([]timezone.Candidate); ok && len(candidates) > 0 { //nolint:nestif // Complex but necessary for accurate timezone detection
		// Summary line shows top candidates
		// Show top 5 candidates (or more if claimed timezone is lower)
		maxToShow := 5
		for i := range candidates {
			c := &candidates[i]
			if c.IsProfile && i >= maxToShow {
				maxToShow = i + 1 // Include the claimed timezone
				break
			}
		}

		sb.WriteString("Top candidates: ")
		for i := range candidates {
			if i >= maxToShow {
				break
			}
			candidate := &candidates[i]
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "UTC%+.0f", candidate.Offset)
		}
		sb.WriteString("\n")

		// Add time range analyzed and DST warning if applicable
		if dateRange, ok := contextData["activity_date_range"].(map[string]any); ok {
			if oldest, ok := dateRange["oldest"].(time.Time); ok {
				if newest, ok := dateRange["newest"].(time.Time); ok {
					fmt.Fprintf(&sb, "Time range analyzed: %s to %s",
						oldest.Format("2006-01-02"), newest.Format("2006-01-02"))

					// Add DST transition warning if applicable
					if spansDST, ok := dateRange["spans_dst_transitions"].(bool); ok && spansDST {
						sb.WriteString(" ⚠️ WARNING: Analysis spans daylight saving time transitions")

						// Determine if it's US or EU DST transitions based on date ranges
						oldestMonth := oldest.Month()
						newestMonth := newest.Month()

						// Check for US DST transitions (March/November)
						if (oldestMonth <= 3 && newestMonth >= 3) || (oldestMonth <= 11 && newestMonth >= 11) {
							sb.WriteString(" (US: March/November)")
						}

						// Check for EU DST transitions (March/October)
						if (oldestMonth <= 3 && newestMonth >= 3) || (oldestMonth <= 10 && newestMonth >= 10) {
							sb.WriteString(" (EU: March/October)")
						}
					}
					sb.WriteString("\n")
				}
			}
		}
		sb.WriteString("\n")

		// Show detailed signals for top candidates (including claimed if not in top 3)
		maxDetailed := maxDetailedCandidates
		for i := range candidates {
			c := &candidates[i]
			if c.IsProfile && i >= maxDetailedCandidates {
				maxDetailed = i + 1 // Include the claimed timezone in details
				break
			}
		}

		for i := range candidates {
			if i >= maxDetailed {
				break
			}
			candidate := &candidates[i]
			fmt.Fprintf(&sb, "%d. UTC%+.1f (%.0f%% confidence)\n",
				i+1, candidate.Offset, candidate.Confidence)

			// Calculate local times for this candidate
			offset := int(candidate.Offset)

			// Work hours - convert UTC to this candidate's local time
			if workHours, ok := contextData["work_hours_utc"].([]float64); ok && len(workHours) == 2 {
				localStart := math.Mod(workHours[0]+float64(offset)+24, 24)
				localEnd := math.Mod(workHours[1]+float64(offset)+24, 24)

				// Format with minutes if there are any
				startHour := int(localStart)
				startMin := int((localStart - float64(startHour)) * 60)
				endHour := int(localEnd)
				endMin := int((localEnd - float64(endHour)) * 60)

				if startMin == 0 && endMin == 0 {
					fmt.Fprintf(&sb, "   Work hours: %02d:00-%02d:00 local", startHour, endHour)
				} else {
					fmt.Fprintf(&sb, "   Work hours: %02d:%02d-%02d:%02d local", startHour, startMin, endHour, endMin)
				}
				if localStart < 6 {
					sb.WriteString(" ⚠️ very early")
				}
				sb.WriteString("\n")
			}

			// Lunch break - use the candidate's own lunch calculation
			if candidate.LunchStartUTC >= 0 {
				// Convert candidate's UTC lunch time to local time for this candidate
				localLunchStart := math.Mod(candidate.LunchStartUTC+float64(offset)+24, 24)
				localLunchEnd := math.Mod(candidate.LunchEndUTC+float64(offset)+24, 24)

				// Format the times properly
				startHour := int(localLunchStart)
				startMin := int((localLunchStart - float64(startHour)) * 60)
				endHour := int(localLunchEnd)
				endMin := int((localLunchEnd - float64(endHour)) * 60)

				fmt.Fprintf(&sb, "   Lunch: %02d:%02d-%02d:%02d local", startHour, startMin, endHour, endMin)
				if localLunchStart >= 11 && localLunchStart <= 14 {
					sb.WriteString(" ✓")
				} else {
					sb.WriteString(" ⚠️ unusual")
				}
				if candidate.LunchConfidence > 0 {
					fmt.Fprintf(&sb, " (%.0f%% conf)", candidate.LunchConfidence*100)
				}
				sb.WriteString("\n")
			}

			// Sleep hours - find the longest continuous sequence
			if sleepHours, ok := contextData["sleep_hours"].([]int); ok && len(sleepHours) > 0 {
				// Find the longest continuous sequence of sleep hours
				longestStart := sleepHours[0]
				longestEnd := sleepHours[0]
				currentStart := sleepHours[0]
				currentEnd := sleepHours[0]
				maxLength := 1
				currentLength := 1

				for i := 1; i < len(sleepHours); i++ {
					// Check if this hour continues the sequence (considering wrap-around)
					expectedNext := (currentEnd + 1) % 24
					if sleepHours[i] == expectedNext {
						currentEnd = sleepHours[i]
						currentLength++
						if currentLength > maxLength {
							maxLength = currentLength
							longestStart = currentStart
							longestEnd = currentEnd
						}
					} else {
						// Start new sequence
						currentStart = sleepHours[i]
						currentEnd = sleepHours[i]
						currentLength = 1
					}
				}

				// Calculate local sleep hours
				localSleepStart := (longestStart + offset + 24) % 24
				localSleepEnd := ((longestEnd+1)%24 + offset + 24) % 24 // Add 1 to get end of sleep period

				// Format the sleep display with more detail
				fmt.Fprintf(&sb, "   Sleep: %02d:00-%02d:00 local (%d hrs)",
					localSleepStart, localSleepEnd, maxLength)

				// Check if sleep is at night - normal sleep starts 8pm-2am and ends 4am-10am
				nighttimeStart := (localSleepStart >= 20 && localSleepStart <= 23) || (localSleepStart >= 0 && localSleepStart <= 2)
				nighttimeEnd := (localSleepEnd >= 4 && localSleepEnd <= 10) || (localSleepEnd >= 0 && localSleepEnd <= 3)
				if nighttimeStart && nighttimeEnd {
					sb.WriteString(" ✓ nighttime")
				} else {
					fmt.Fprintf(&sb, " ⚠️ unusual (start:%02d end:%02d)", localSleepStart, localSleepEnd)
				}
				sb.WriteString("\n")
			}

			// Evening activity
			if candidate.EveningActivity > 0 {
				fmt.Fprintf(&sb, "   Evening activity (7-11pm): %d events", candidate.EveningActivity)
				if candidate.EveningActivity > 20 {
					sb.WriteString(" (high)")
				}
				sb.WriteString("\n")
			}

			// Peak productivity
			if peakHours, ok := contextData["peak_productivity_utc"].([]int); ok && len(peakHours) >= 2 {
				localPeakStart := (peakHours[0] + offset + 24) % 24
				localPeakEnd := (peakHours[1] + offset + 24) % 24
				fmt.Fprintf(&sb, "   Peak productivity: %02d:00-%02d:00 local", localPeakStart, localPeakEnd)
				if !candidate.PeakTimeReasonable {
					sb.WriteString(" ⚠️ unusual peak time")
				}
				sb.WriteString("\n")
			}

			// Add validation indicators
			if candidate.WorkHoursReasonable && candidate.LunchReasonable && candidate.SleepReasonable && candidate.PeakTimeReasonable {
				sb.WriteString("   ✓ All patterns align well with this timezone\n")
			} else {
				var issues []string
				if !candidate.WorkHoursReasonable {
					issues = append(issues, "unusual work hours")
				}
				if !candidate.LunchReasonable {
					issues = append(issues, "unusual lunch time")
				}
				if !candidate.SleepReasonable {
					issues = append(issues, fmt.Sprintf("unusual sleep (mid: %.1f)", candidate.SleepMidLocal))
				}
				if !candidate.PeakTimeReasonable {
					issues = append(issues, "unusual peak time")
				}
				if len(issues) > 0 {
					fmt.Fprintf(&sb, "   ⚠️ Issues: %s\n", strings.Join(issues, ", "))
				}
			}

			// Include human-readable scoring analysis for AI
			if len(candidate.ScoringDetails) > 0 {
				var positives, negatives []string
				for _, detail := range candidate.ScoringDetails {
					if strings.HasPrefix(detail, "+") {
						// Convert "+8 (good work start 7am)" to "Good work start at 7am"
						reason := strings.TrimSpace(strings.Split(detail, "(")[1])
						reason = strings.TrimSuffix(reason, ")")
						positives = append(positives, reason)
					} else if strings.HasPrefix(detail, "-") {
						// Convert "-15 (suspicious 5-6pm activity)" to "Suspicious 5-6pm activity"
						reason := strings.TrimSpace(strings.Split(detail, "(")[1])
						reason = strings.TrimSuffix(reason, ")")
						negatives = append(negatives, reason)
					}
				}

				if len(positives) > 0 {
					sb.WriteString("   ✓ Reasons this timezone fits: ")
					sb.WriteString(strings.Join(positives, "; "))
					sb.WriteString("\n")
				}

				if len(negatives) > 0 {
					sb.WriteString("   ⚠️ Reasons this may not be correct: ")
					sb.WriteString(strings.Join(negatives, "; "))
					sb.WriteString("\n")
				}
			}
		}
		sb.WriteString("\n")
	}

	// Activity summary shows overall engagement.
	if dateRange, ok := contextData["activity_date_range"].(map[string]any); ok {
		if totalEvents, ok := dateRange["total_events"].(int); ok {
			if totalDays, ok := dateRange["total_days"].(int); ok {
				fmt.Fprintf(&sb, "Activity: %d events over %d days\n", totalEvents, totalDays)
			}
		}
	}

	// Time patterns help validate timezone candidates.
	if workHours, ok := contextData["work_hours_utc"].([]float64); ok && len(workHours) == 2 {
		startHour := int(workHours[0])
		startMin := int((workHours[0] - float64(startHour)) * 60)
		endHour := int(workHours[1])
		endMin := int((workHours[1] - float64(endHour)) * 60)

		if startMin == 0 && endMin == 0 {
			fmt.Fprintf(&sb, "Active hours UTC: %02d:00-%02d:00\n", startHour, endHour)
		} else {
			fmt.Fprintf(&sb, "Active hours UTC: %02d:%02d-%02d:%02d\n", startHour, startMin, endHour, endMin)
		}
	}
	if quietHours, ok := contextData["quiet_hours"].([]int); ok && len(quietHours) > 0 {
		fmt.Fprintf(&sb, "Quiet hours UTC: %v\n", quietHours)
	}

	sb.WriteString("\n")

	// Section 3: Repository geography and interests.
	sb.WriteString("=== REPOSITORY SIGNALS ===\n\n")

	// List user's repositories first.
	if repos, ok := contextData["repositories"].([]github.Repository); ok && len(repos) > 0 {
		sb.WriteString("User's repositories:\n")
		count := 0
		for i := range repos {
			if !repos[i].Fork && count < maxUserRepos { // Show up to 20 non-fork repos.
				if repos[i].Description != "" {
					fmt.Fprintf(&sb, "- %s: %s\n", repos[i].Name, repos[i].Description)
				} else {
					fmt.Fprintf(&sb, "- %s\n", repos[i].Name)
				}
				count++
			}
		}
		sb.WriteString("\n")
	}

	// Starred repositories reveal interests and potential location clues.
	if starredRepos, ok := contextData["starred_repositories"].([]github.Repository); ok && len(starredRepos) > 0 {
		sb.WriteString("Starred repositories (interests/location clues):\n")
		for i := range starredRepos {
			if i >= maxStarredRepos { // Limit to 15 starred repos.
				break
			}
			if starredRepos[i].Description != "" {
				fmt.Fprintf(&sb, "- %s: %s\n", starredRepos[i].Name, starredRepos[i].Description)
			} else {
				fmt.Fprintf(&sb, "- %s\n", starredRepos[i].Name)
			}
		}
		sb.WriteString("\n")
	}

	// Analyze repositories for location and hobby clues.
	locationRepos := make(map[string]string)
	hobbyIndicators := make(map[string]bool)

	// analyzeRepo extracts location and hobby indicators from a repository.
	analyzeRepo := func(repo github.Repository, _ string) {
		repoLower := strings.ToLower(repo.Name + " " + repo.Description)

		// Check for location-specific keywords in repository names and descriptions.
		locations := map[string][]string{
			"Brazil":     {"brazil", "brasil", "bvsp", "bovespa", "são paulo", "sao paulo", "rio de janeiro"},
			"Argentina":  {"argentina", "buenos aires"},
			"Canada":     {"canada", "toronto", "montreal", "vancouver", "ottawa", "calgary"},
			"Colorado":   {"colorado", "denver", "boulder"},
			"California": {"california", "san francisco", "los angeles", "san diego"},
			"Texas":      {"texas", "austin", "houston", "dallas"},
			"New York":   {"new york", "nyc", "manhattan", "brooklyn"},
		}

		for location, keywords := range locations {
			for _, keyword := range keywords {
				if strings.Contains(repoLower, keyword) {
					locationRepos[repo.Name] = location
					break
				}
			}
		}

		// Check for hobby and interest indicators.
		hobbies := map[string][]string{
			"Caving/Spelunking (US Mountain timezone likely)": {"caving", "spelunk", "cave map"},
			"Rock climbing (Mountain/Pacific US)":             {"climbing", "climb", "boulder", "crag"},
			"Winter sports (Mountain states)":                 {"skiing", "ski", "snowboard", "snow"},
			"Hiking/Outdoors (Western US)":                    {"hiking", "trail", "backpack", "camping"},
			"Surfing (Coastal)":                               {"surf", "wave", "beach"},
			"Desert activities (Southwest US)":                {"desert", "canyon", "mesa"},
		}

		for hobby, keywords := range hobbies {
			for _, keyword := range keywords {
				if strings.Contains(repoLower, keyword) {
					hobbyIndicators[hobby] = true
					break
				}
			}
		}
	}

	// Analyze user's own repositories.
	if repos, ok := contextData["repositories"].([]github.Repository); ok {
		for i := range repos {
			if !repos[i].Fork { // Skip forks.
				analyzeRepo(repos[i], "owned")
			}
		}
	}

	// Analyze starred repositories for interests.
	if starredRepos, ok := contextData["starred_repositories"].([]github.Repository); ok {
		for i := range starredRepos {
			analyzeRepo(starredRepos[i], "starred")
		}
	}

	// Output location-specific repositories if found.
	if len(locationRepos) > 0 {
		sb.WriteString("Location indicators found in repos:\n")
		// Sort by repository name for consistent output.
		var repoNames []string
		for name := range locationRepos {
			repoNames = append(repoNames, name)
		}
		sort.Strings(repoNames)
		count := 0
		for _, name := range repoNames {
			if count >= maxLocationIndicators { // Limit to 5 location indicators.
				break
			}
			fmt.Fprintf(&sb, "- %s → %s\n", name, locationRepos[name])
			count++
		}
		sb.WriteString("\n")
	}

	// Output hobby indicators if found.
	if len(hobbyIndicators) > 0 {
		sb.WriteString("Hobby/Interest indicators:\n")
		// Sort hobbies for deterministic output
		var hobbies []string
		for hobby := range hobbyIndicators {
			hobbies = append(hobbies, hobby)
		}
		sort.Strings(hobbies)
		for _, hobby := range hobbies {
			fmt.Fprintf(&sb, "- %s\n", hobby)
		}
		sb.WriteString("\n")
	}

	// External contributions show collaboration patterns.
	if contribs, ok := contextData["contributed_repositories"].([]repoContribution); ok && len(contribs) > 0 {
		sb.WriteString("External contributions (repos not owned by user):\n")
		for i, contrib := range contribs {
			if i >= maxExternalContribs { // Show up to 15 external contributions.
				break
			}
			fmt.Fprintf(&sb, "- %s (%d contributions)\n", contrib.Name, contrib.Count)
		}
		sb.WriteString("\n")
	}

	// Section 4: Recent activity and contributions.
	sb.WriteString("=== RECENT ACTIVITY ===\n\n")

	// Recent pull requests show current work focus.
	if prs, ok := contextData["pull_requests"].([]github.PullRequest); ok && len(prs) > 0 {
		sb.WriteString("Recent Pull Requests:\n")
		for i := range prs {
			if i >= maxRecentPRs {
				break
			}
			fmt.Fprintf(&sb, "- %s\n", prs[i].Title)
		}
		sb.WriteString("\n")
	}

	// Recent issues show areas of interest and collaboration.
	if issues, ok := contextData["issues"].([]github.Issue); ok && len(issues) > 0 {
		sb.WriteString("Recent Issues:\n")
		for i := range issues {
			if i >= maxRecentIssues {
				break
			}
			fmt.Fprintf(&sb, "- %s\n", issues[i].Title)
		}
		sb.WriteString("\n")
	}

	// Commit messages can reveal language patterns and work style.
	if commitSamples, ok := contextData["commit_message_samples"].([]CommitMessageSample); ok && len(commitSamples) > 0 {
		sb.WriteString("Recent Commit Messages:\n")
		for i, sample := range commitSamples {
			if i >= maxRecentCommits {
				break
			}
			msg := sample.Message
			if len(msg) > commitMessageMaxLen {
				msg = msg[:commitMessageMaxLen] + "..."
			}
			fmt.Fprintf(&sb, "- %s\n", msg)
		}
		sb.WriteString("\n")
	}

	// Text samples provide language and cultural indicators.
	if textSamples, ok := contextData["text_samples"].([]string); ok && len(textSamples) > 0 {
		sb.WriteString("Text samples from PRs/issues/comments:\n")
		for i, sample := range textSamples {
			if i >= maxTextSamples {
				break
			}
			fmt.Fprintf(&sb, "- %s\n", sample)
		}
		sb.WriteString("\n")
	}

	// Recent gist descriptions can reveal location, interests, and language preferences.
	if recentGists, ok := contextData["recent_gists"].([]github.Gist); ok && len(recentGists) > 0 {
		sb.WriteString("Recent Gist Descriptions (interests/location clues):\n")
		for i, gist := range recentGists {
			if i >= 5 { // Limit to 5 gists as requested
				break
			}
			description := gist.Description
			if description == "" {
				description = "[No description]"
			}
			// Truncate very long descriptions
			if len(description) > 100 {
				description = description[:100] + "..."
			}
			fmt.Fprintf(&sb, "- %s (created: %s)\n", description, gist.CreatedAt.Format("2006-01-02"))
		}
		sb.WriteString("\n")
	}

	// Section 5: Additional patterns (weekend/weekday ratio)
	// Only show this if we have the data
	if weekendActivity, ok := contextData["weekend_activity_ratio"].(float64); ok {
		sb.WriteString("\n=== ADDITIONAL PATTERNS ===\n\n")
		fmt.Fprintf(&sb, "Weekend activity: %.1f%% of weekday activity\n", weekendActivity*100)
		switch {
		case weekendActivity < 0.3:
			sb.WriteString("Pattern: Strong work/life separation (typical employee)\n")
		case weekendActivity > 0.7:
			sb.WriteString("Pattern: Continuous activity (OSS maintainer)\n")
		default:
			sb.WriteString("Pattern: Moderate weekend activity\n")
		}
		sb.WriteString("\n")
	}

	// Section 6: Website content (kept full for hobby detection).
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		sb.WriteString("=== WEBSITE CONTENT ===\n\n")
		contentPreview := websiteContent
		if len(contentPreview) > websiteContentMaxLen {
			contentPreview = contentPreview[:websiteContentMaxLen] + "...\n[TRUNCATED]"
		}
		sb.WriteString(contentPreview)
		sb.WriteString("\n\n")
	}

	// Mastodon-linked website content.
	if websiteContents, ok := contextData["mastodon_website_contents"].(map[string]string); ok && len(websiteContents) > 0 {
		// Sort websites for deterministic output
		var websites []string
		for website := range websiteContents {
			websites = append(websites, website)
		}
		sort.Strings(websites)
		for _, website := range websites {
			content := websiteContents[website]
			fmt.Fprintf(&sb, "Content from %s:\n", website)
			contentPreview := content
			if len(contentPreview) > mastodonContentMaxLen {
				contentPreview = contentPreview[:mastodonContentMaxLen] + "...\n[TRUNCATED]"
			}
			sb.WriteString(contentPreview)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}
