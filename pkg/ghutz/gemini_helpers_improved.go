package ghutz

import (
	"fmt"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/github"
	"github.com/codeGROOVE-dev/ghuTZ/pkg/timezone"
)

// formatEvidenceForGeminiImproved formats detection evidence for Gemini API analysis with better structure.
func (d *Detector) formatEvidenceForGeminiImproved(contextData map[string]interface{}) string {
	var sb strings.Builder

	// SECTION 1: PRIMARY LOCATION SIGNALS
	sb.WriteString("=== PRIMARY LOCATION SIGNALS ===\n\n")

	// User profile information (most direct signals)
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
			// Highlight .ca domains
			if strings.Contains(user.Blog, ".ca") {
				sb.WriteString("  ⚠️ CANADIAN DOMAIN (.ca)\n")
			}
		}
		if user.TwitterHandle != "" {
			sb.WriteString(fmt.Sprintf("- Twitter: @%s\n", user.TwitterHandle))
		}
		sb.WriteString("\n")
	}

	// Extract and highlight Canadian indicators from repositories
	canadianIndicators := []string{}
	if repos, ok := contextData["repositories"].([]github.Repository); ok && len(repos) > 0 {
		for _, repo := range repos {
			repoLower := strings.ToLower(repo.Name + " " + repo.Description)
			if strings.Contains(repoLower, "canada") ||
				strings.Contains(repoLower, "canadian") ||
				strings.Contains(repoLower, "toronto") ||
				strings.Contains(repoLower, "montreal") ||
				strings.Contains(repoLower, "vancouver") ||
				strings.Contains(repoLower, "ottawa") ||
				strings.Contains(repoLower, "calgary") ||
				strings.Contains(repoLower, "halifax") ||
				strings.Contains(repo.Name, ".ca") ||
				strings.Contains(repoLower, "pycon.ca") {
				canadianIndicators = append(canadianIndicators,
					fmt.Sprintf("- %s: %s", repo.Name, repo.Description))
			}
		}
	}

	if len(canadianIndicators) > 0 {
		sb.WriteString("🇨🇦 CANADIAN INDICATORS IN REPOSITORIES:\n")
		for _, indicator := range canadianIndicators {
			sb.WriteString(indicator + "\n")
		}
		sb.WriteString("\n")
	}

	// Website content - check for Canadian references
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		websiteLower := strings.ToLower(websiteContent)
		canadianRefs := []string{}

		// Check for Canadian cities
		canadianCities := []string{"toronto", "montreal", "vancouver", "ottawa", "calgary",
			"edmonton", "winnipeg", "quebec", "hamilton", "kitchener", "waterloo", "halifax"}
		for _, city := range canadianCities {
			if strings.Contains(websiteLower, city) {
				canadianRefs = append(canadianRefs, city)
			}
		}

		// Check for Canadian provinces
		canadianProvinces := []string{"ontario", "quebec", "british columbia", "alberta",
			"manitoba", "saskatchewan", "nova scotia", "newfoundland"}
		for _, province := range canadianProvinces {
			if strings.Contains(websiteLower, province) {
				canadianRefs = append(canadianRefs, province)
			}
		}

		if strings.Contains(websiteLower, "canada") || strings.Contains(websiteLower, "canadian") {
			canadianRefs = append(canadianRefs, "Canada/Canadian mentioned")
		}

		if len(canadianRefs) > 0 {
			sb.WriteString("🇨🇦 CANADIAN REFERENCES IN WEBSITE:\n")
			for _, ref := range canadianRefs {
				sb.WriteString(fmt.Sprintf("- %s\n", ref))
			}
			sb.WriteString("\n")
		}
	}

	// Mastodon profile data
	if mastodonProfile, ok := contextData["mastodon_profile"].(*MastodonProfileData); ok && mastodonProfile != nil {
		sb.WriteString("Mastodon Profile:\n")
		if mastodonProfile.Username != "" {
			sb.WriteString(fmt.Sprintf("- Username: @%s\n", mastodonProfile.Username))
		}
		if mastodonProfile.Bio != "" {
			sb.WriteString(fmt.Sprintf("- Bio: %s\n", mastodonProfile.Bio))
		}

		// Check for .ca websites in Mastodon
		for _, website := range mastodonProfile.Websites {
			if strings.Contains(website, ".ca") {
				sb.WriteString(fmt.Sprintf("- Website: %s ⚠️ CANADIAN DOMAIN\n", website))
			} else {
				sb.WriteString(fmt.Sprintf("- Website: %s\n", website))
			}
		}
		sb.WriteString("\n")
	}

	// Country TLDs - highlight .ca
	if tlds, ok := contextData["country_tlds"].([]CountryTLD); ok && len(tlds) > 0 {
		sb.WriteString("Country-specific domains found:\n")
		for _, tld := range tlds {
			if tld.TLD == ".ca" {
				sb.WriteString(fmt.Sprintf("- %s (%s) ⚠️ CANADIAN DOMAIN\n", tld.TLD, tld.Country))
			} else {
				sb.WriteString(fmt.Sprintf("- %s (%s)\n", tld.TLD, tld.Country))
			}
		}
		sb.WriteString("\n")
	}

	// SECTION 2: ACTIVITY PATTERNS
	sb.WriteString("=== ACTIVITY PATTERNS ===\n\n")

	// Activity date range
	if dateRange, ok := contextData["activity_date_range"].(map[string]interface{}); ok {
		if oldest, ok := dateRange["oldest"].(time.Time); ok {
			if newest, ok := dateRange["newest"].(time.Time); ok {
				if totalDays, ok := dateRange["total_days"].(int); ok {
					if totalEvents, ok := dateRange["total_events"].(int); ok {
						sb.WriteString(fmt.Sprintf("Activity: %d events from %s to %s (%d days)\n",
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

	// Timezone candidates with Canadian timezone info
	if candidates, ok := contextData["timezone_candidates"].([]timezone.TimezoneCandidate); ok && len(candidates) > 0 {
		sb.WriteString("Timezone Analysis:\n")
		sb.WriteString("NOTE: Canadian timezones include:\n")
		sb.WriteString("- Pacific (BC): UTC-8/UTC-7\n")
		sb.WriteString("- Mountain (AB): UTC-7/UTC-6\n")
		sb.WriteString("- Central (MB): UTC-6/UTC-5\n")
		sb.WriteString("- Eastern (ON/QC): UTC-5/UTC-4 ← Toronto is here\n")
		sb.WriteString("- Atlantic (NS/NB): UTC-4/UTC-3\n")
		sb.WriteString("- Newfoundland: UTC-3:30/UTC-2:30\n\n")

		sb.WriteString("Activity-based timezone candidates:\n")
		for i, candidate := range candidates {
			if i >= 5 {
				break
			}
			// Use %.1f to handle fractional offsets like UTC+5.5 (India), UTC+3.5 (Iran)
			sb.WriteString(fmt.Sprintf("%d. UTC%+.1f - %.1f%% confidence\n",
				i+1, candidate.Offset, candidate.Confidence))
		}
		sb.WriteString("\n")
	}

	// Quiet hours and active hours
	if quietHours, ok := contextData["quiet_hours"].([]int); ok && len(quietHours) > 0 {
		sb.WriteString(fmt.Sprintf("Quiet Hours (UTC): %v\n", quietHours))
	}

	if workHours, ok := contextData["work_hours_utc"].([]int); ok && len(workHours) == 2 {
		sb.WriteString(fmt.Sprintf("Active Hours (UTC): %02d:00 - %02d:00\n", workHours[0], workHours[1]))
	}
	sb.WriteString("\n")

	// SECTION 3: REPOSITORIES (show all, not just Canadian ones)
	sb.WriteString("=== REPOSITORIES ===\n\n")

	if repos, ok := contextData["repositories"].([]github.Repository); ok && len(repos) > 0 {
		sb.WriteString("User's Repositories:\n")

		// Show non-fork repos first
		count := 0
		for _, repo := range repos {
			if !repo.Fork && count < 30 {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", repo.Name, repo.Description))
				count++
			}
		}
		sb.WriteString("\n")
	}

	// SECTION 4: WEBSITE CONTENT (don't truncate as aggressively)
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		sb.WriteString("=== WEBSITE CONTENT ===\n\n")
		contentPreview := websiteContent
		if len(contentPreview) > 4000 { // Increased from 2000
			contentPreview = contentPreview[:4000] + "...\n[TRUNCATED]"
		}
		sb.WriteString(contentPreview)
		sb.WriteString("\n\n")
	}

	// Mastodon website content
	if websiteContents, ok := contextData["mastodon_website_contents"].(map[string]string); ok && len(websiteContents) > 0 {
		for website, content := range websiteContents {
			sb.WriteString(fmt.Sprintf("Content from %s:\n", website))
			contentPreview := content
			if len(contentPreview) > 3000 { // Increased from 1500
				contentPreview = contentPreview[:3000] + "...\n[TRUNCATED]"
			}
			sb.WriteString(contentPreview)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}
