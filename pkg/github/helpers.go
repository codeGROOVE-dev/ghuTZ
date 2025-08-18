package github

import (
	"regexp"
)

// ExtractSocialMediaFromHTML extracts social media links from GitHub profile HTML.
func ExtractSocialMediaFromHTML(html string) []string {
	var urls []string

	// Extract Mastodon links (format: @username@instance.domain)
	// Look for pattern like: href="https://infosec.exchange/@jamon">@jamon@infosec.exchange
	mastodonRegex := regexp.MustCompile(`href="(https?://[^"]+/@[^"]+)"[^>]*>@[^@]+@[^<]+`)
	mastodonMatches := mastodonRegex.FindAllStringSubmatch(html, -1)
	for _, match := range mastodonMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}

	// Extract other social media links from the profile
	socialPatterns := []string{
		`https?://(?:www\.)?twitter\.com/[\w]+`,
		`https?://(?:www\.)?x\.com/[\w]+`,
		`https?://(?:www\.)?linkedin\.com/in/[\w-]+`,
		`https?://(?:www\.)?instagram\.com/[\w.]+`,
		`https?://(?:www\.)?facebook\.com/[\w.]+`,
		`https?://(?:www\.)?youtube\.com/[\w/-]+`,
		`https?://(?:www\.)?twitch\.tv/[\w]+`,
		`https?://[\w.-]+\.social/@[\w]+`,      // Generic Mastodon pattern
		`https?://mastodon\.[\w.-]+/@[\w]+`,    // Mastodon instances
		`https?://fosstodon\.org/@[\w]+`,       // Popular Mastodon instance
		`https?://techhub\.social/@[\w]+`,      // Tech Mastodon instance
		`https?://infosec\.exchange/@[\w]+`,    // InfoSec Mastodon instance
		`https?://triangletoot\.party/@[\w]+`,  // Triangle area Mastodon instance
		`https?://[\w.-]+\.party/@[\w]+`,       // .party Mastodon instances
	}

	for _, pattern := range socialPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllString(html, -1)
		urls = append(urls, matches...)
	}

	// Deduplicate URLs
	seen := make(map[string]bool)
	var unique []string
	for _, u := range urls {
		if !seen[u] {
			seen[u] = true
			unique = append(unique, u)
		}
	}

	return unique
}