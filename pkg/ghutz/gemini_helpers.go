package ghutz

import (
	"fmt"
	"strings"
	"time"
)

// formatEvidenceForGemini formats detection evidence for Gemini API analysis.
func (d *Detector) formatEvidenceForGemini(contextData map[string]interface{}) string {
	var sb strings.Builder

	// User profile information
	if user, ok := contextData["user"].(*GitHubUser); ok && user != nil {
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
		sb.WriteString("\n")
	}

	// Activity patterns
	if quietHours, ok := contextData["quiet_hours"].([]int); ok && len(quietHours) > 0 {
		sb.WriteString(fmt.Sprintf("Quiet Hours (UTC): %v\n", quietHours))
	}

	if workHours, ok := contextData["work_hours"].([]int); ok && len(workHours) == 2 {
		sb.WriteString(fmt.Sprintf("Active Hours (UTC): %02d:00 - %02d:00\n", workHours[0], workHours[1]))
	}

	// UTC offset information
	if offset, ok := contextData["utc_offset"].(int); ok {
		offsetStr := fmt.Sprintf("UTC%+d", offset)
		if offset == 0 {
			offsetStr = "UTC"
		}
		sb.WriteString(fmt.Sprintf("Detected UTC Offset: %s\n", offsetStr))

		// Add DST context if available
		if dstContext, ok := contextData["dst_context"].(string); ok && dstContext != "" {
			sb.WriteString(fmt.Sprintf("DST Context: %s\n", dstContext))
		}
	}

	// Location evidence
	if location, ok := contextData["location"].(*Location); ok && location != nil {
		sb.WriteString("\nGeocoded Location:\n")
		sb.WriteString(fmt.Sprintf("- Coordinates: %.4f, %.4f\n", location.Latitude, location.Longitude))
	}

	// Organizations
	if orgs, ok := contextData["organizations"]; ok && orgs != nil {
		if orgsList, ok := orgs.([]Organization); ok {
			var orgsWithLocation []string
			for _, org := range orgsList {
				if org.Location != "" {
					orgsWithLocation = append(orgsWithLocation, fmt.Sprintf("%s (%s)", org.Login, org.Location))
				}
			}
			if len(orgsWithLocation) > 0 {
				sb.WriteString("\nOrganizations with locations:\n")
				for _, org := range orgsWithLocation {
					sb.WriteString(fmt.Sprintf("- %s\n", org))
				}
			}
		}
	}

	// Repository evidence
	if repos, ok := contextData["repositories"].([]Repository); ok && len(repos) > 0 {
		sb.WriteString("\nTop Repositories:\n")
		count := 0
		for _, repo := range repos {
			if count >= 5 {
				break
			}
			if repo.Description != "" {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", repo.Name, repo.Description))
				count++
			}
		}
	}

	// Recent activity
	if events, ok := contextData["recent_events"].([]PublicEvent); ok && len(events) > 0 {
		sb.WriteString("\nRecent Activity Summary:\n")
		
		// Count events by type
		eventCounts := make(map[string]int)
		var firstEvent, lastEvent time.Time
		
		for i, event := range events {
			eventCounts[event.Type]++
			if i == 0 {
				firstEvent = event.CreatedAt
			}
			lastEvent = event.CreatedAt
		}
		
		sb.WriteString(fmt.Sprintf("- Total events: %d\n", len(events)))
		sb.WriteString(fmt.Sprintf("- Date range: %s to %s\n", 
			lastEvent.Format("2006-01-02"), 
			firstEvent.Format("2006-01-02")))
		
		// Top event types
		sb.WriteString("- Event types: ")
		first := true
		for eventType, count := range eventCounts {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s (%d)", eventType, count))
			first = false
		}
		sb.WriteString("\n")
	}

	// Pull requests
	if prs, ok := contextData["pull_requests"].([]PullRequest); ok && len(prs) > 0 {
		sb.WriteString(fmt.Sprintf("\nPull Requests: %d total\n", len(prs)))
		
		// Sample PR for timezone clues
		for i, pr := range prs {
			if i >= 3 {
				break
			}
			if pr.Body != "" && (strings.Contains(strings.ToLower(pr.Body), "timezone") || 
				strings.Contains(strings.ToLower(pr.Body), "time zone") ||
				strings.Contains(strings.ToLower(pr.Body), "local time")) {
				sb.WriteString(fmt.Sprintf("- PR with timezone mention: %s\n", pr.Title))
			}
		}
	}

	// Issues
	if issues, ok := contextData["issues"].([]Issue); ok && len(issues) > 0 {
		sb.WriteString(fmt.Sprintf("\nIssues: %d total\n", len(issues)))
	}

	// Website content
	if websiteContent, ok := contextData["website_content"].(string); ok && websiteContent != "" {
		sb.WriteString("\nWebsite/Blog content extracted\n")
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

	// Lunch break pattern
	if lunchHours, ok := contextData["lunch_break"].([]int); ok && len(lunchHours) > 0 {
		sb.WriteString(fmt.Sprintf("\nLunch Break Pattern (UTC): %02d:00 - %02d:00\n", lunchHours[0], lunchHours[1]))
		if confidence, ok := contextData["lunch_confidence"].(float64); ok {
			sb.WriteString(fmt.Sprintf("Lunch Break Confidence: %.0f%%\n", confidence*100))
		}
	}

	// Peak productivity hours
	if peakHours, ok := contextData["peak_productivity"].([]float64); ok && len(peakHours) >= 2 {
		sb.WriteString(fmt.Sprintf("\nPeak Productivity (local): %.0f:00 - %.0f:00\n", peakHours[0], peakHours[1]))
	}

	// Name-based hints
	if nameHint, ok := contextData["name_hint"].(string); ok && nameHint != "" {
		sb.WriteString(fmt.Sprintf("\nName-based hint: %s\n", nameHint))
	}

	// Additional context from profile scraping
	if scrapedTimezone, ok := contextData["scraped_timezone"].(string); ok && scrapedTimezone != "" {
		sb.WriteString(fmt.Sprintf("\nTimezone from profile scraping: %s\n", scrapedTimezone))
	}

	return sb.String()
}