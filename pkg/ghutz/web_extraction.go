package ghutz

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/github"
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
		".uk": "United Kingdom",
		".ca": "Canada",
		".au": "Australia",
		".nz": "New Zealand",
		".de": "Germany",
		".fr": "France",
		".nl": "Netherlands",
		".se": "Sweden",
		".no": "Norway",
		".fi": "Finland",
		".dk": "Denmark",
		".pl": "Poland",
		".es": "Spain",
		".it": "Italy",
		".pt": "Portugal",
		".br": "Brazil",
		".mx": "Mexico",
		".ar": "Argentina",
		".jp": "Japan",
		".kr": "South Korea",
		".cn": "China",
		".in": "India",
		".sg": "Singapore",
		".hk": "Hong Kong",
		".tw": "Taiwan",
		".ru": "Russia",
		".za": "South Africa",
		".il": "Israel",
		".ae": "UAE",
		".ch": "Switzerland",
		".at": "Austria",
		".be": "Belgium",
		".cz": "Czech Republic",
		".ie": "Ireland",
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

// MastodonProfileData represents all extracted data from a Mastodon profile.
type MastodonProfileData struct {
	ProfileFields map[string]string
	Username      string
	DisplayName   string
	Bio           string
	JoinedDate    string
	Websites      []string
	Hashtags      []string
}

// extractSocialMediaURLs extracts social media profile URLs from GitHub user data.
func extractSocialMediaURLs(user *github.GitHubUser) []string {
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
			`https?://[\w.-]+\.social/@[\w]+`,     // Mastodon instances
			`https?://mastodon\.[\w.-]+/@[\w]+`,   // Mastodon instances
			`https?://fosstodon\.org/@[\w]+`,      // Popular Mastodon instance
			`https?://techhub\.social/@[\w]+`,     // Tech Mastodon instance
			`https?://infosec\.exchange/@[\w]+`,   // InfoSec Mastodon instance
			`https?://triangletoot\.party/@[\w]+`, // Triangle area Mastodon instance
			`https?://[\w.-]+\.party/@[\w]+`,      // .party Mastodon instances
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
	if user.TwitterHandle != "" {
		urls = append(urls, fmt.Sprintf("https://twitter.com/%s", user.TwitterHandle))
	}

	// Add URLs from GraphQL social accounts (critical for puerco.mx detection)
	for _, account := range user.SocialAccounts {
		if account.URL != "" {
			urls = append(urls, account.URL)
		}
	}

	return urls
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
