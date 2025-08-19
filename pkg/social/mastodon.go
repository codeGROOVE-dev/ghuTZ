package social

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

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

// MastodonAccount represents the API response from Mastodon.
type MastodonAccount struct {
	ID             string          `json:"id"`
	Username       string          `json:"username"`
	Acct           string          `json:"acct"`
	DisplayName    string          `json:"display_name"`
	Note           string          `json:"note"` // Bio in HTML
	URL            string          `json:"url"`  // Profile URL
	CreatedAt      time.Time       `json:"created_at"`
	Fields         []MastodonField `json:"fields"` // Custom profile fields
	FollowersCount int             `json:"followers_count"`
	FollowingCount int             `json:"following_count"`
	StatusesCount  int             `json:"statuses_count"`
}

// MastodonField represents a custom field on a Mastodon profile.
type MastodonField struct {
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	Name       string     `json:"name"`
	Value      string     `json:"value"`
}

// fetchMastodonProfileViaAPI fetches profile data using the Mastodon API.
func fetchMastodonProfileViaAPI(ctx context.Context, mastodonURL string, logger *slog.Logger) *MastodonProfileData {
	// Parse the Mastodon URL to extract hostname and username
	parsedURL, err := url.Parse(mastodonURL)
	if err != nil {
		logger.Debug("failed to parse Mastodon URL", "url", mastodonURL, "error", err)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback to HTML scraping
	}

	hostname := parsedURL.Host

	// Extract username from path (e.g., "/@username" or "/users/username")
	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	var username string
	for _, part := range pathParts {
		if strings.HasPrefix(part, "@") {
			username = strings.TrimPrefix(part, "@")
			break
		} else if len(pathParts) >= 2 && pathParts[0] == "users" {
			username = pathParts[1]
			break
		}
	}

	if username == "" {
		// Try to extract from the last part of the path
		if len(pathParts) > 0 {
			lastPart := pathParts[len(pathParts)-1]
			if lastPart != "" && !strings.Contains(lastPart, ".") {
				username = strings.TrimPrefix(lastPart, "@")
			}
		}
	}

	if username == "" {
		logger.Debug("could not extract username from Mastodon URL", "url", mastodonURL)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback to HTML scraping
	}

	// Construct the API URL
	apiURL := fmt.Sprintf("https://%s/api/v1/accounts/lookup?acct=%s", hostname, username)

	logger.Debug("fetching Mastodon profile via API", "api_url", apiURL, "username", username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		logger.Debug("failed to create API request", "url", apiURL, "error", err)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Debug("failed to fetch Mastodon API", "url", apiURL, "error", err)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		logger.Debug("Mastodon API returned non-200 status", "status", resp.StatusCode, "url", apiURL)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		logger.Debug("failed to read API response", "error", err)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback
	}

	var account MastodonAccount
	if err := json.Unmarshal(body, &account); err != nil {
		logger.Debug("failed to parse Mastodon API response", "error", err)
		return fetchMastodonProfile(ctx, mastodonURL, logger) // Fallback
	}

	// Convert to our profile data structure
	profileData := &MastodonProfileData{
		Username:      account.Username,
		DisplayName:   account.DisplayName,
		ProfileFields: make(map[string]string),
		Websites:      []string{},
		Hashtags:      []string{},
	}

	// Extract bio (strip HTML)
	profileData.Bio = stripHTML(account.Note)

	// Log the bio content
	logger.Debug("extracted Mastodon bio",
		"username", account.Username,
		"bio", profileData.Bio,
		"bio_length", len(profileData.Bio))

	// Extract hashtags from bio
	hashtagPattern := regexp.MustCompile(`#(\w+)`)
	hashtagMatches := hashtagPattern.FindAllStringSubmatch(profileData.Bio, -1)
	for _, match := range hashtagMatches {
		if len(match) > 1 {
			profileData.Hashtags = append(profileData.Hashtags, match[1])
			logger.Debug("found hashtag in Mastodon bio", "hashtag", match[1])
		}
	}

	// Set joined date
	profileData.JoinedDate = account.CreatedAt.Format("January 2006")

	// Process custom fields
	for _, field := range account.Fields {
		fieldName := stripHTML(field.Name)
		fieldValue := stripHTML(field.Value)

		profileData.ProfileFields[fieldName] = fieldValue

		// Extract URLs from field value HTML
		urls := extractURLsFromHTML(field.Value)
		for _, urlStr := range urls {
			// Check if this looks like a personal website/blog
			// Common field names for websites
			lowerFieldName := strings.ToLower(fieldName)
			if strings.Contains(lowerFieldName, "website") ||
				strings.Contains(lowerFieldName, "blog") ||
				strings.Contains(lowerFieldName, "home") ||
				strings.Contains(lowerFieldName, "url") ||
				strings.Contains(lowerFieldName, "site") ||
				strings.Contains(lowerFieldName, "www") ||
				strings.Contains(lowerFieldName, "web") ||
				field.VerifiedAt != nil { // Verified fields are often personal websites
				// This is likely a personal website
				if !containsMastodonDomain(urlStr) {
					profileData.Websites = append(profileData.Websites, urlStr)
					logger.Debug("found website in Mastodon field",
						"field", fieldName,
						"url", urlStr,
						"verified", field.VerifiedAt != nil)
				}
			}
		}
	}

	// Also check bio for URLs
	bioURLs := extractURLsFromHTML(account.Note)
	for _, urlStr := range bioURLs {
		if !containsMastodonDomain(urlStr) {
			// Check if URL already exists in websites
			found := false
			for _, existing := range profileData.Websites {
				if existing == urlStr {
					found = true
					break
				}
			}
			if !found {
				profileData.Websites = append(profileData.Websites, urlStr)
			}
		}
	}

	logger.Debug("fetched Mastodon profile via API",
		"username", account.Username,
		"display_name", account.DisplayName,
		"bio_length", len(profileData.Bio),
		"fields_count", len(account.Fields),
		"websites_found", len(profileData.Websites),
		"hashtags_found", len(profileData.Hashtags),
		"hashtags", profileData.Hashtags)

	return profileData
}

// fetchMastodonProfile fetches comprehensive info from a Mastodon profile via HTML scraping.
func fetchMastodonProfile(ctx context.Context, mastodonURL string, logger *slog.Logger) *MastodonProfileData {
	// Mastodon profiles often have metadata in the HTML
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mastodonURL, http.NoBody)
	if err != nil {
		logger.Debug("failed to create Mastodon request", "url", mastodonURL, "error", err)
		return nil
	}

	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Debug("failed to fetch Mastodon profile", "url", mastodonURL, "error", err)
		return nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Debug("failed to close response body", "error", err)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		logger.Debug("failed to read Mastodon response", "url", mastodonURL, "error", err)
		return nil
	}

	content := string(body)

	profileData := &MastodonProfileData{
		ProfileFields: make(map[string]string),
		Websites:      []string{},
		Hashtags:      []string{},
	}

	// Extract bio/description from meta tags
	bioPattern := regexp.MustCompile(`<meta\s+(?:property|name)=["'](?:og:)?description["']\s+content=["']([^"']+)["']`)
	if matches := bioPattern.FindStringSubmatch(content); len(matches) > 1 {
		profileData.Bio = html.UnescapeString(matches[1])

		// Extract hashtags from bio
		hashtagPattern := regexp.MustCompile(`#(\w+)`)
		hashtagMatches := hashtagPattern.FindAllStringSubmatch(profileData.Bio, -1)
		for _, match := range hashtagMatches {
			if len(match) > 1 {
				profileData.Hashtags = append(profileData.Hashtags, match[1])
			}
		}
	}

	// Extract all profile metadata fields (the key-value pairs shown on profile)
	// Pattern: <dt>FieldName</dt><dd>FieldValue</dd>
	// This is more complex because values can contain HTML
	fieldPattern := regexp.MustCompile(`<dt[^>]*>([^<]+)</dt>\s*<dd[^>]*>(.*?)</dd>`)
	fieldMatches := fieldPattern.FindAllStringSubmatch(content, -1)

	for _, match := range fieldMatches {
		if len(match) <= 2 {
			continue
		}
		fieldName := strings.TrimSpace(html.UnescapeString(match[1]))
		fieldValue := match[2]

		// Extract text and links from field value
		// Remove HTML tags but preserve link URLs
		linkPattern := regexp.MustCompile(`<a[^>]+href=["']([^"']+)["'][^>]*>([^<]*)</a>`)
		links := linkPattern.FindAllStringSubmatch(fieldValue, -1)

		cleanValue := fieldValue
		for _, link := range links {
			if len(link) > 2 {
				urlStr := link[1]
				linkText := link[2]
				if linkText != "" {
					cleanValue = strings.Replace(cleanValue, link[0], linkText, 1)
				} else {
					cleanValue = strings.Replace(cleanValue, link[0], urlStr, 1)
				}

				// Add to websites list if it's a website
				if strings.HasPrefix(urlStr, "http") && !strings.Contains(urlStr, "mastodon") &&
					!strings.Contains(urlStr, ".social") && !strings.Contains(urlStr, "infosec.exchange") {
					profileData.Websites = append(profileData.Websites, urlStr)
				}
			}
		}

		// Remove remaining HTML tags
		tagPattern := regexp.MustCompile(`<[^>]+>`)
		cleanValue = tagPattern.ReplaceAllString(cleanValue, "")
		cleanValue = strings.TrimSpace(html.UnescapeString(cleanValue))

		if fieldName != "" && cleanValue != "" {
			profileData.ProfileFields[fieldName] = cleanValue
		}
	}

	// Also look for rel="me" links (verified links)
	websitePattern := regexp.MustCompile(`<a[^>]+rel=["']me["'][^>]+href=["']([^"']+)["']`)
	websiteMatches := websitePattern.FindAllStringSubmatch(content, -1)
	for _, match := range websiteMatches {
		if len(match) > 1 {
			urlStr := match[1]
			// Add non-social media links to websites
			if !strings.Contains(urlStr, "twitter.com") &&
				!strings.Contains(urlStr, "github.com") &&
				!strings.Contains(urlStr, "linkedin.com") &&
				!strings.Contains(urlStr, "mastodon") &&
				!strings.Contains(urlStr, ".social") &&
				!strings.Contains(urlStr, "infosec.exchange") {
				// Check if not already in list
				found := false
				for _, w := range profileData.Websites {
					if w == urlStr {
						found = true
						break
					}
				}
				if !found {
					profileData.Websites = append(profileData.Websites, urlStr)
				}
			}
		}
	}

	// Try to extract joined date
	joinedPattern := regexp.MustCompile(`(?:Joined|Member since)[:\s]*([^<]+)`)
	if matches := joinedPattern.FindStringSubmatch(content); len(matches) > 1 {
		profileData.JoinedDate = strings.TrimSpace(matches[1])
	}

	logger.Debug("extracted from Mastodon profile",
		"url", mastodonURL,
		"bio_length", len(profileData.Bio),
		"fields_count", len(profileData.ProfileFields),
		"websites_count", len(profileData.Websites),
		"hashtags_count", len(profileData.Hashtags))

	return profileData
}

// stripHTML removes HTML tags from a string.
func stripHTML(htmlStr string) string {
	// First unescape HTML entities
	unescaped := html.UnescapeString(htmlStr)

	// Remove <br> and <p> tags with newlines
	unescaped = strings.ReplaceAll(unescaped, "<br>", "\n")
	unescaped = strings.ReplaceAll(unescaped, "<br/>", "\n")
	unescaped = strings.ReplaceAll(unescaped, "<br />", "\n")
	unescaped = strings.ReplaceAll(unescaped, "</p>", "\n")
	unescaped = strings.ReplaceAll(unescaped, "<p>", "")

	// Remove all other HTML tags
	tagPattern := regexp.MustCompile(`<[^>]+>`)
	cleaned := tagPattern.ReplaceAllString(unescaped, "")

	// Clean up multiple newlines
	cleaned = regexp.MustCompile(`\n+`).ReplaceAllString(cleaned, "\n")

	return strings.TrimSpace(cleaned)
}

// extractURLsFromHTML extracts all URLs from HTML content.
func extractURLsFromHTML(htmlContent string) []string {
	var urls []string

	// Pattern to find href attributes
	hrefPattern := regexp.MustCompile(`href=["']([^"']+)["']`)
	matches := hrefPattern.FindAllStringSubmatch(htmlContent, -1)

	for _, match := range matches {
		if len(match) > 1 {
			urlStr := match[1]
			if strings.HasPrefix(urlStr, "http") {
				urls = append(urls, urlStr)
			}
		}
	}

	return urls
}

// containsMastodonDomain checks if a URL is a Mastodon instance.
func containsMastodonDomain(urlStr string) bool {
	mastodonDomains := []string{
		"mastodon",
		".social",
		"infosec.exchange",
		"fosstodon",
		"mstdn",
		"toot",
		"fediverse",
	}

	lowerURL := strings.ToLower(urlStr)
	for _, domain := range mastodonDomains {
		if strings.Contains(lowerURL, domain) {
			return true
		}
	}

	return false
}
