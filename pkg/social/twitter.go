package social

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	twitterscraper "github.com/imperatrona/twitter-scraper"
)

// extractTwitter extracts content from a Twitter/X profile using the scraper.
func extractTwitter(ctx context.Context, twitterURL string, logger *slog.Logger) *Content {
	// Extract username from URL
	username := extractTwitterUsername(twitterURL)
	if username == "" {
		logger.Debug("could not extract Twitter username", "url", twitterURL)
		return extractTwitterBasic(ctx, twitterURL, logger) // Fall back to basic extraction
	}

	// Create scraper instance
	scraper := twitterscraper.New()
	// Try to get profile
	profile, err := scraper.GetProfile(username)
	if err != nil {
		logger.Debug("failed to scrape Twitter profile", "username", username, "error", err)
		return extractTwitterBasic(ctx, twitterURL, logger) // Fall back to basic extraction
	}

	// Create content structure with enhanced data
	content := &Content{
		Kind:     "twitter",
		URL:      twitterURL,
		Username: username,
		Name:     profile.Name,
		Bio:      profile.Biography,
		Location: profile.Location,
		Tags:     extractHashtags(profile.Biography),
		Fields:   make(map[string]string),
	}

	// Add additional fields
	if profile.Website != "" {
		content.Fields["website"] = profile.Website
	}
	if profile.Joined != nil {
		content.Fields["joined"] = profile.Joined.Format("2006-01-02")
	}
	content.Fields["followers"] = formatNumber(profile.FollowersCount)
	content.Fields["following"] = formatNumber(profile.FollowingCount)

	// Build enhanced markdown
	var md strings.Builder
	md.WriteString("# Twitter/X Profile\n\n")
	md.WriteString("**Username:** @" + username + "\n")
	if profile.Name != "" {
		md.WriteString("**Name:** " + profile.Name + "\n")
	}
	if profile.Location != "" {
		md.WriteString("**Location:** " + profile.Location + "\n")
	}
	if profile.Biography != "" {
		md.WriteString("\n**Bio:**\n" + profile.Biography + "\n")
	}
	if profile.Website != "" {
		md.WriteString("\n**Website:** " + profile.Website + "\n")
	}
	if len(content.Tags) > 0 {
		md.WriteString("\n**Hashtags:** " + strings.Join(content.Tags, ", ") + "\n")
	}

	content.Markdown = md.String()

	logger.Debug("successfully extracted Twitter profile",
		"url", twitterURL,
		"username", username,
		"name", profile.Name,
		"location", profile.Location,
		"bio_length", len(profile.Biography))

	return content
}

// extractTwitterUsername extracts the username from a Twitter/X URL.
func extractTwitterUsername(twitterURL string) string {
	username := ""
	if strings.Contains(twitterURL, "twitter.com/") {
		parts := strings.Split(twitterURL, "twitter.com/")
		if len(parts) > 1 {
			username = strings.TrimPrefix(parts[1], "@")
			username = strings.Split(username, "/")[0]
			username = strings.Split(username, "?")[0]
		}
	} else if strings.Contains(twitterURL, "x.com/") {
		parts := strings.Split(twitterURL, "x.com/")
		if len(parts) > 1 {
			username = strings.TrimPrefix(parts[1], "@")
			username = strings.Split(username, "/")[0]
			username = strings.Split(username, "?")[0]
		}
	}
	return strings.TrimSpace(username)
}

// formatNumber formats a number for display.
func formatNumber(n int) string {
	switch {
	case n >= 1000000:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000000.0), ".0"), "0") + "M"
	case n >= 1000:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000.0), ".0"), "0") + "K"
	default:
		return strconv.Itoa(n)
	}
}
