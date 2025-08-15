package ghutz

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// CountryTLD represents a country top-level domain with its associated country.
type CountryTLD struct {
	TLD     string
	Country string
}

// extractCountryTLDs extracts country-specific TLDs from URLs.
func extractCountryTLDs(urls ...string) []CountryTLD {
	// Map of country TLDs to their countries
	countryTLDs := map[string]string{
		".uk":    "United Kingdom",
		".ca":    "Canada",
		".au":    "Australia",
		".nz":    "New Zealand",
		".de":    "Germany",
		".fr":    "France",
		".nl":    "Netherlands",
		".se":    "Sweden",
		".no":    "Norway",
		".fi":    "Finland",
		".dk":    "Denmark",
		".pl":    "Poland",
		".es":    "Spain",
		".it":    "Italy",
		".pt":    "Portugal",
		".br":    "Brazil",
		".mx":    "Mexico",
		".ar":    "Argentina",
		".jp":    "Japan",
		".kr":    "South Korea",
		".cn":    "China",
		".in":    "India",
		".sg":    "Singapore",
		".hk":    "Hong Kong",
		".tw":    "Taiwan",
		".ru":    "Russia",
		".za":    "South Africa",
		".il":    "Israel",
		".ae":    "UAE",
		".ch":    "Switzerland",
		".at":    "Austria",
		".be":    "Belgium",
		".cz":    "Czech Republic",
		".ie":    "Ireland",
	}

	var tlds []CountryTLD
	seen := make(map[string]bool)

	for _, u := range urls {
		if u == "" {
			continue
		}

		// Parse the URL
		parsed, err := url.Parse(u)
		if err != nil {
			continue
		}

		host := strings.ToLower(parsed.Host)
		if host == "" {
			host = strings.ToLower(u) // Fallback for malformed URLs
		}

		// Check each TLD
		for tld, country := range countryTLDs {
			if strings.HasSuffix(host, tld) && !seen[tld] {
				tlds = append(tlds, CountryTLD{TLD: tld, Country: country})
				seen[tld] = true
			}
		}
	}

	return tlds
}

// extractSocialMediaURLs extracts social media profile URLs from GitHub user data.
func extractSocialMediaURLs(user *GitHubUser) []string {
	if user == nil {
		return nil
	}

	var urls []string

	// Check bio for social media links
	if user.Bio != "" {
		// Common patterns for social media in bios
		patterns := []string{
			`https?://(?:www\.)?twitter\.com/[\w]+`,
			`https?://(?:www\.)?x\.com/[\w]+`,
			`https?://(?:www\.)?linkedin\.com/in/[\w-]+`,
			`https?://(?:www\.)?instagram\.com/[\w.]+`,
			`https?://(?:www\.)?facebook\.com/[\w.]+`,
			`https?://[\w.-]+\.social/@[\w]+`,      // Mastodon instances
			`https?://mastodon\.[\w.-]+/@[\w]+`,    // Mastodon instances
			`https?://fosstodon\.org/@[\w]+`,       // Popular Mastodon instance
			`https?://techhub\.social/@[\w]+`,      // Tech Mastodon instance
			`https?://infosec\.exchange/@[\w]+`,    // InfoSec Mastodon instance
			`https?://(?:www\.)?youtube\.com/c/[\w-]+`,
			`https?://(?:www\.)?twitch\.tv/[\w]+`,
		}

		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindAllString(user.Bio, -1)
			urls = append(urls, matches...)
		}
	}

	// Check blog field
	if user.Blog != "" {
		urls = append(urls, user.Blog)
	}

	// Check Twitter username field
	if user.TwitterUsername != "" {
		urls = append(urls, fmt.Sprintf("https://twitter.com/%s", user.TwitterUsername))
	}

	return urls
}

// fetchMastodonWebsite fetches and extracts website from Mastodon profile.
func (d *Detector) fetchMastodonWebsite(ctx context.Context, mastodonURL string) string {
	// Validate Mastodon URL format
	if !strings.Contains(mastodonURL, "@") || !strings.Contains(mastodonURL, "://") {
		return ""
	}

	req, err := http.NewRequestWithContext(ctx, "GET", mastodonURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("User-Agent", "ghuTZ/1.0")
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			d.logger.Debug("Failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024)) // Limit to 100KB
	if err != nil {
		return ""
	}

	html := string(body)

	// Look for website in profile metadata
	// Mastodon profiles often have structured data
	websiteRegex := regexp.MustCompile(`<a[^>]+rel="me"[^>]+href="([^"]+)"`)
	matches := websiteRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		website := matches[1]
		// Filter out other social media links
		if !strings.Contains(website, "twitter.com") &&
			!strings.Contains(website, "github.com") &&
			!strings.Contains(website, "linkedin.com") &&
			!strings.Contains(website, "instagram.com") {
			return website
		}
	}

	// Alternative pattern for Mastodon metadata table
	metadataRegex := regexp.MustCompile(`<th>Website</th>\s*<td[^>]*>.*?href="([^"]+)"`)
	matches = metadataRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	// Try to find API endpoint from the HTML and make API call
	instanceRegex := regexp.MustCompile(`https?://([^/]+)/@([\w]+)`)
	instanceMatches := instanceRegex.FindStringSubmatch(mastodonURL)
	if len(instanceMatches) == 3 {
		instance := instanceMatches[1]
		username := instanceMatches[2]
		apiURL := fmt.Sprintf("https://%s/api/v1/accounts/lookup?acct=%s", instance, username)

		apiReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err == nil {
			apiReq.Header.Set("User-Agent", "ghuTZ/1.0")
			apiResp, err := http.Get(apiURL)
			if err == nil && apiResp.StatusCode == http.StatusOK {
				defer func() {
					if closeErr := apiResp.Body.Close(); closeErr != nil {
						slog.Debug("Failed to close API response body", "error", closeErr)
					}
				}()
				// Would need JSON parsing here, but keeping it simple for now
			}
		}
	}

	return ""
}

// isPolishName checks if a name appears to be Polish based on common patterns.
func isPolishName(name string) bool {
	if name == "" {
		return false
	}

	nameLower := strings.ToLower(name)

	// Check for Polish special characters
	polishChars := []string{"ł", "ą", "ć", "ę", "ń", "ó", "ś", "ź", "ż"}
	for _, char := range polishChars {
		if strings.Contains(nameLower, char) {
			return true
		}
	}

	// Check for common Polish name endings
	polishEndings := []string{"ski", "cki", "wicz", "czak", "czyk", "owski", "ewski", "iński"}
	for _, ending := range polishEndings {
		if strings.HasSuffix(nameLower, ending) {
			return true
		}
	}

	// Check for common Polish first names
	polishFirstNames := []string{
		"łukasz", "paweł", "michał", "piotr", "wojciech",
		"krzysztof", "andrzej", "marek", "tomasz", "jan", "stanisław", "zbigniew",
		"anna", "maria", "katarzyna", "małgorzata", "agnieszka", "barbara", "ewa",
		"elżbieta", "zofia", "teresa", "magdalena", "joanna", "aleksandra",
	}

	nameWords := strings.Fields(nameLower)
	for _, word := range nameWords {
		for _, firstName := range polishFirstNames {
			if word == firstName {
				return true
			}
		}
	}

	return false
}

// extractSocialMediaFromHTML extracts social media links from GitHub profile HTML.
func extractSocialMediaFromHTML(html string) []string {
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