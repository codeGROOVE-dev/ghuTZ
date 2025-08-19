package ghutz

import (
	"fmt"
	"sort"
	"strings"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/github"
	"github.com/codeGROOVE-dev/ghuTZ/pkg/timezone"
)

// Constants for data limits and thresholds.
const (
	maxUserRepos          = 20
	maxStarredRepos       = 15
	maxExternalContribs   = 15
	maxRecentPRs          = 10
	maxRecentIssues       = 10
	maxRecentCommits      = 10
	maxTextSamples        = 8
	maxLocationIndicators = 5
	maxTopCandidates      = 5
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
		if mastodonProfile.Bio != "" {
			fmt.Fprintf(&sb, "- Bio: %s\n", mastodonProfile.Bio)
		}
		if mastodonProfile.DisplayName != "" {
			fmt.Fprintf(&sb, "- Display name: %s\n", mastodonProfile.DisplayName)
		}
		for _, website := range mastodonProfile.Websites {
			fmt.Fprintf(&sb, "- Website: %s\n", website)
		}
		sb.WriteString("\n")
	}

	// Section 2: Activity-based timezone constraints (mandatory).
	sb.WriteString("=== ACTIVITY TIMEZONE ANALYSIS ===\n\n")

	// Timezone candidates are critical constraints that must be respected.
	if candidates, ok := contextData["timezone_candidates"].([]timezone.Candidate); ok && len(candidates) > 0 {
		// Summary line shows all viable candidates.
		sb.WriteString("Top 5 candidates: ")
		for i, candidate := range candidates {
			if i >= maxTopCandidates {
				break
			}
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "UTC%+.0f", candidate.Offset)
		}
		sb.WriteString("\n\n")

		// Show detailed signals for top 3 candidates.
		for i, candidate := range candidates {
			if i >= maxDetailedCandidates {
				break
			}
			fmt.Fprintf(&sb, "%d. UTC%+.1f (%.0f%% confidence)\n",
				i+1, candidate.Offset, candidate.Confidence)

			// Format key signals compactly.
			signals := []string{}
			if candidate.WorkStartLocal >= workStartEarliest && candidate.WorkStartLocal <= workStartLatest {
				signals = append(signals, fmt.Sprintf("work %dam", candidate.WorkStartLocal))
			}
			if candidate.LunchReasonable && candidate.LunchLocalTime > 0 {
				signals = append(signals, fmt.Sprintf("lunch %d:00", int(candidate.LunchLocalTime)))
			}
			if candidate.EveningActivity > 0 {
				signals = append(signals, fmt.Sprintf("evening activity %d", candidate.EveningActivity))
			}
			if len(signals) > 0 {
				sb.WriteString("   → " + strings.Join(signals, ", ") + "\n")
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
	if workHours, ok := contextData["work_hours_utc"].([]int); ok && len(workHours) == 2 {
		fmt.Fprintf(&sb, "Active hours UTC: %02d:00-%02d:00\n", workHours[0], workHours[1])
	}
	if quietHours, ok := contextData["quiet_hours"].([]int); ok && len(quietHours) > 0 {
		fmt.Fprintf(&sb, "Quiet hours UTC: %v\n", quietHours)
	}

	// Hourly activity distribution helps validate timezone candidates.
	if hourCounts, ok := contextData["hour_counts"].(map[int]int); ok && len(hourCounts) > 0 {
		sb.WriteString("\nHourly activity (UTC):\n")
		for hour := range 24 {
			if count, exists := hourCounts[hour]; exists && count > 0 {
				fmt.Fprintf(&sb, "%02d:00: %d events\n", hour, count)
			}
		}
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

	// Section 5: Work patterns.
	sb.WriteString("\n=== WORK PATTERNS ===\n\n")

	// Weekend versus weekday activity reveals work style.
	if weekendActivity, ok := contextData["weekend_activity_ratio"].(float64); ok {
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

	// Day of week activity shows work schedule patterns.
	if dayActivity, ok := contextData["day_of_week_activity"].(map[string]int); ok && len(dayActivity) > 0 {
		sb.WriteString("Activity by day of week:\n")
		days := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
		for _, day := range days {
			if count, exists := dayActivity[day]; exists && count > 0 {
				fmt.Fprintf(&sb, "- %s: %d events\n", day, count)
			}
		}
		sb.WriteString("\n")
	}

	// Lunch break patterns help validate timezone.
	if lunchHours, ok := contextData["lunch_break_utc"].([]int); ok && len(lunchHours) >= 2 {
		if confidence, ok := contextData["lunch_confidence"].(float64); ok {
			fmt.Fprintf(&sb, "Lunch break UTC: %02d:00-%02d:00 (%.0f%% confidence)\n",
				lunchHours[0], lunchHours[1], confidence*100)
		}
	}

	// Peak productivity hours indicate work style.
	if peakHours, ok := contextData["peak_productivity_utc"].([]int); ok && len(peakHours) >= 2 {
		fmt.Fprintf(&sb, "Peak productivity UTC: %02d:00-%02d:00\n", peakHours[0], peakHours[1])
	}

	// Sleep hours indicate timezone alignment
	if sleepHours, ok := contextData["sleep_hours"].([]int); ok && len(sleepHours) > 0 {
		// Group consecutive hours for cleaner display
		var sleepRanges []string
		if len(sleepHours) > 0 {
			start := sleepHours[0]
			end := sleepHours[0]
			for i := 1; i < len(sleepHours); i++ {
				if sleepHours[i] == (end+1)%24 {
					end = sleepHours[i]
				} else {
					if start == end {
						sleepRanges = append(sleepRanges, fmt.Sprintf("%02d:00", start))
					} else {
						sleepRanges = append(sleepRanges, fmt.Sprintf("%02d:00-%02d:00", start, (end+1)%24))
					}
					start = sleepHours[i]
					end = sleepHours[i]
				}
			}
			// Add final range
			if start == end {
				sleepRanges = append(sleepRanges, fmt.Sprintf("%02d:00", start))
			} else {
				sleepRanges = append(sleepRanges, fmt.Sprintf("%02d:00-%02d:00", start, (end+1)%24))
			}
		}
		fmt.Fprintf(&sb, "Detected sleep hours UTC: %s\n", strings.Join(sleepRanges, ", "))
		fmt.Fprintf(&sb, "Note: Primary sleep period should be 4-8 continuous hours between 10pm-8am local time\n")
	}

	// Evening activity indicates personal coding time.
	if eveningHours, ok := contextData["evening_activity_hours"].([]int); ok && len(eveningHours) > 0 {
		fmt.Fprintf(&sb, "Evening activity hours (7-11pm window): %v\n", eveningHours)
		if eveningPct, ok := contextData["evening_activity_percentage"].(float64); ok {
			fmt.Fprintf(&sb, "Evening activity percentage: %.1f%%\n", eveningPct)
		}
	}
	sb.WriteString("\n")

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
