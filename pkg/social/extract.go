// Package social provides extraction of social media information.
package social

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/codeGROOVE-dev/retry"
)

// Extract processes a map of social media URLs/data and returns structured content for each
// The map key is the type (e.g., "mastodon", "twitter", "website", "linkedin")
// The map value is the URL or identifier.
func Extract(ctx context.Context, data map[string]string, logger *slog.Logger) []Content {
	if logger == nil {
		logger = slog.Default()
	}

	var results []Content
	var mu sync.Mutex
	var wg sync.WaitGroup
	
	// SECURITY: Limit concurrent requests to prevent amplification attacks
	const maxConcurrent = 3
	const maxURLsToProcess = 10 // SECURITY: Cap total URLs to prevent abuse
	semaphore := make(chan struct{}, maxConcurrent)

	// Process each URL with concurrency limit
	urlCount := 0
	for kind, urlStr := range data {
		if urlCount >= maxURLsToProcess {
			logger.Debug("reached maximum URL processing limit", "limit", maxURLsToProcess)
			break
		}
		if urlStr == "" {
			continue
		}
		
		urlCount++
		wg.Add(1)
		go func(k, u string) {
			defer wg.Done()
			
			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			var content *Content

			switch strings.ToLower(k) {
			case "mastodon":
				content = extractMastodon(ctx, u, logger)
			case "twitter", "x":
				content = extractTwitter(ctx, u, logger)
			case "bluesky", "bsky":
				content = extractBlueSky(ctx, u, logger)
			case "website", "blog", "homepage":
				content = extractWebsite(ctx, u, logger)
			case "linkedin":
				content = extractLinkedIn(ctx, u, logger)
			default:
				// For unknown types, try to extract as a generic website
				content = extractWebsite(ctx, u, logger)
				if content != nil {
					content.Kind = k // Preserve the original kind
				}
			}

			if content != nil {
				mu.Lock()
				results = append(results, *content)
				mu.Unlock()
			}
		}(kind, urlStr)
	}

	wg.Wait()
	return results
}

// extractMastodon extracts content from a Mastodon profile.
func extractMastodon(ctx context.Context, mastodonURL string, logger *slog.Logger) *Content {
	// First try API, then fall back to HTML scraping
	profileData := fetchMastodonProfileViaAPI(ctx, mastodonURL, logger)
	if profileData == nil {
		profileData = fetchMastodonProfile(ctx, mastodonURL, logger)
	}

	if profileData == nil {
		return nil
	}

	content := &Content{
		Kind:     "mastodon",
		URL:      mastodonURL,
		Bio:      profileData.Bio,
		Name:     profileData.DisplayName,
		Username: profileData.Username,
		Tags:     profileData.Hashtags,
		Joined:   profileData.JoinedDate,
		Fields:   profileData.ProfileFields,
	}

	// Build markdown content
	var md strings.Builder
	md.WriteString("# Mastodon Profile\n\n")
	if profileData.DisplayName != "" {
		md.WriteString("**Name:** " + profileData.DisplayName + "\n")
	}
	if profileData.Username != "" {
		md.WriteString("**Username:** @" + profileData.Username + "\n")
	}
	if profileData.Bio != "" {
		md.WriteString("\n## Bio\n" + profileData.Bio + "\n")
	}
	if len(profileData.ProfileFields) > 0 {
		md.WriteString("\n## Profile Fields\n")
		for key, value := range profileData.ProfileFields {
			md.WriteString("- **" + key + ":** " + value + "\n")
		}
	}
	if len(profileData.Hashtags) > 0 {
		md.WriteString("\n## Hashtags\n")
		md.WriteString(strings.Join(profileData.Hashtags, ", ") + "\n")
	}
	if profileData.JoinedDate != "" {
		md.WriteString("\n**Joined:** " + profileData.JoinedDate + "\n")
	}

	content.Markdown = md.String()

	// Extract location from profile fields if present
	for key, value := range profileData.ProfileFields {
		lowerKey := strings.ToLower(key)
		if strings.Contains(lowerKey, "location") || strings.Contains(lowerKey, "city") ||
			strings.Contains(lowerKey, "country") || strings.Contains(lowerKey, "place") {
			content.Location = value
			break
		}
	}

	return content
}

// extractTwitterBasic extracts basic content from a Twitter/X profile without scraping.
func extractTwitterBasic(ctx context.Context, twitterURL string, logger *slog.Logger) *Content {
	_ = ctx // Context not used in current implementation
	// Extract username from URL
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

	// TODO: In the future, this could use Twitter API or scraping
	// For now, return a basic structure showing what we could extract
	content := &Content{
		Kind:     "twitter",
		URL:      twitterURL,
		Username: username,
		Name:     username,   // Would be fetched from profile
		Bio:      "",         // Would be fetched from profile
		Location: "",         // Would be fetched from profile
		Tags:     []string{}, // Would extract hashtags from bio
		Fields:   make(map[string]string),
	}

	// Build markdown with what we have
	var md strings.Builder
	md.WriteString("# Twitter/X Profile\n\n")
	if username != "" {
		md.WriteString("**Username:** @" + username + "\n")
		md.WriteString("\n*Note: Full profile data extraction not yet implemented*\n")
	}

	content.Markdown = md.String()

	logger.Debug("extracted Twitter profile (dummy)", "url", twitterURL, "username", username)

	return content
}

// extractLinkedIn extracts content from a LinkedIn profile
// This is a dummy implementation for now.
func extractLinkedIn(ctx context.Context, linkedinURL string, logger *slog.Logger) *Content {
	_ = ctx // Context not used in current implementation
	// Extract username/profile ID from URL
	profileID := ""
	if strings.Contains(linkedinURL, "linkedin.com/in/") {
		parts := strings.Split(linkedinURL, "linkedin.com/in/")
		if len(parts) > 1 {
			profileID = strings.Split(parts[1], "/")[0]
			profileID = strings.Split(profileID, "?")[0]
		}
	}

	// TODO: LinkedIn scraping is complex due to authentication requirements
	content := &Content{
		Kind:     "linkedin",
		URL:      linkedinURL,
		Username: profileID,
		Name:     "", // Would be fetched from profile
		Bio:      "", // Would be fetched from profile
		Location: "", // Would be fetched from profile
		Fields:   make(map[string]string),
	}

	// Build markdown
	var md strings.Builder
	md.WriteString("# LinkedIn Profile\n\n")
	if profileID != "" {
		md.WriteString("**Profile:** " + profileID + "\n")
	}
	md.WriteString("\n*Note: LinkedIn profile data extraction not yet implemented*\n")

	content.Markdown = md.String()

	logger.Debug("extracted LinkedIn profile (dummy)", "url", linkedinURL, "profile_id", profileID)

	return content
}

// extractWebsite extracts content from a generic website.
func extractWebsite(ctx context.Context, websiteURL string, logger *slog.Logger) *Content {
	// Fetch website content
	htmlContent := fetchWebsiteContent(ctx, websiteURL, logger)
	if htmlContent == "" {
		return nil
	}

	// Convert HTML to markdown
	markdown := htmlToMarkdown(htmlContent)

	content := &Content{
		Kind:     "website",
		URL:      websiteURL,
		Markdown: markdown,
		Fields:   make(map[string]string),
	}

	// Try to extract title from HTML
	title := extractTitle(htmlContent)
	if title != "" {
		content.Name = title
	}

	// Try to extract description
	description := extractMetaDescription(htmlContent)
	if description != "" {
		content.Bio = description
	}

	// Extract any social media links from the website
	socialLinks := extractSocialMediaFromHTML(htmlContent)
	if len(socialLinks) > 0 {
		content.Fields["social_links"] = strings.Join(socialLinks, ", ")
	}

	logger.Debug("extracted website content", "url", websiteURL, "title", title, "markdown_length", len(markdown))

	return content
}

// fetchWebsiteContent fetches the content of a website.
func fetchWebsiteContent(ctx context.Context, websiteURL string, logger *slog.Logger) string {
	if websiteURL == "" {
		return ""
	}

	if !strings.HasPrefix(websiteURL, "http://") && !strings.HasPrefix(websiteURL, "https://") {
		websiteURL = "https://" + websiteURL
	}

	// SECURITY: Parse URL to validate it's safe to fetch
	parsedURL, err := url.Parse(websiteURL)
	if err != nil {
		logger.Debug("invalid URL format", "url", websiteURL, "error", err)
		return ""
	}

	// SECURITY: Prevent SSRF attacks by blocking internal/private IPs and local URLs
	host := strings.ToLower(parsedURL.Hostname())

	// Block localhost and local domains
	if host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		logger.Debug("blocked fetch to local/internal host", "host", host)
		return ""
	}

	// Block private IP ranges (RFC 1918)
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			logger.Debug("blocked fetch to private IP", "ip", host)
			return ""
		}
	}

	// Block metadata service endpoints (AWS, GCP, Azure)
	if host == "169.254.169.254" || host == "metadata.google.internal" ||
		host == "metadata.azure.com" {
		logger.Debug("blocked fetch to metadata service", "host", host)
		return ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, websiteURL, http.NoBody)
	if err != nil {
		logger.Debug("failed to create website request", "url", websiteURL, "error", err)
		return ""
	}

	req.Header.Set("User-Agent", "GitHub-Timezone-Detector/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	
	// Use retry logic with exponential backoff and jitter
	var resp *http.Response
	err = retry.Do(
		func() error {
			var doErr error
			resp, doErr = client.Do(req)
			if doErr != nil {
				return doErr
			}
			// Retry on server errors and rate limiting
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(5),
		retry.Delay(time.Second),
		retry.MaxDelay(2*time.Minute),
		retry.DelayType(retry.FullJitterBackoffDelay),
		retry.OnRetry(func(n uint, err error) {
			logger.Debug("retrying website fetch", "attempt", n+1, "url", websiteURL, "error", err)
		}),
	)
	
	if err != nil {
		logger.Debug("failed to fetch website after retries", "url", websiteURL, "error", err)
		return ""
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Debug("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		logger.Debug("website returned non-200 status", "url", websiteURL, "status", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB limit
	if err != nil {
		logger.Debug("failed to read website body", "url", websiteURL, "error", err)
		return ""
	}

	return string(body)
}

// htmlToMarkdown converts HTML content to markdown format.
func htmlToMarkdown(htmlContent string) string {
	if htmlContent == "" {
		return ""
	}

	// First unescape HTML entities
	content := html.UnescapeString(htmlContent)

	// Remove script and style tags with their content
	scriptPattern := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	content = scriptPattern.ReplaceAllString(content, "")
	stylePattern := regexp.MustCompile(`(?i)<style[^>]*>.*?</style>`)
	content = stylePattern.ReplaceAllString(content, "")

	// Convert headers
	h1Pattern := regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	content = h1Pattern.ReplaceAllString(content, "\n# $1\n")
	h2Pattern := regexp.MustCompile(`(?i)<h2[^>]*>(.*?)</h2>`)
	content = h2Pattern.ReplaceAllString(content, "\n## $1\n")
	h3Pattern := regexp.MustCompile(`(?i)<h3[^>]*>(.*?)</h3>`)
	content = h3Pattern.ReplaceAllString(content, "\n### $1\n")

	// Convert links
	linkPattern := regexp.MustCompile(`(?i)<a[^>]+href=["']([^"']+)["'][^>]*>([^<]+)</a>`)
	content = linkPattern.ReplaceAllString(content, "[$2]($1)")

	// Convert paragraphs and line breaks
	content = strings.ReplaceAll(content, "</p>", "\n\n")
	content = strings.ReplaceAll(content, "<p>", "")
	content = strings.ReplaceAll(content, "<br>", "\n")
	content = strings.ReplaceAll(content, "<br/>", "\n")
	content = strings.ReplaceAll(content, "<br />", "\n")

	// Convert lists
	content = strings.ReplaceAll(content, "<li>", "- ")
	content = strings.ReplaceAll(content, "</li>", "\n")
	content = strings.ReplaceAll(content, "<ul>", "\n")
	content = strings.ReplaceAll(content, "</ul>", "\n")
	content = strings.ReplaceAll(content, "<ol>", "\n")
	content = strings.ReplaceAll(content, "</ol>", "\n")

	// Convert bold and italic
	boldPattern := regexp.MustCompile(`(?i)<(?:b|strong)[^>]*>(.*?)</(?:b|strong)>`)
	content = boldPattern.ReplaceAllString(content, "**$1**")
	italicPattern := regexp.MustCompile(`(?i)<(?:i|em)[^>]*>(.*?)</(?:i|em)>`)
	content = italicPattern.ReplaceAllString(content, "*$1*")

	// Remove all remaining HTML tags
	tagPattern := regexp.MustCompile(`<[^>]+>`)
	content = tagPattern.ReplaceAllString(content, "")

	// Clean up excessive whitespace
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	content = strings.TrimSpace(content)

	return content
}

// extractTitle extracts the title from HTML content.
func extractTitle(htmlContent string) string {
	// Try to extract from <title> tag
	titlePattern := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	if matches := titlePattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
		return strings.TrimSpace(html.UnescapeString(matches[1]))
	}

	// Try to extract from og:title meta tag
	ogTitlePattern := regexp.MustCompile(`<meta\s+property=["']og:title["']\s+content=["']([^"']+)["']`)
	if matches := ogTitlePattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
		return strings.TrimSpace(html.UnescapeString(matches[1]))
	}

	// Try to extract from h1 tag
	h1Pattern := regexp.MustCompile(`(?i)<h1[^>]*>([^<]+)</h1>`)
	if matches := h1Pattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
		return strings.TrimSpace(html.UnescapeString(matches[1]))
	}

	return ""
}

// extractMetaDescription extracts the meta description from HTML content.
func extractMetaDescription(htmlContent string) string {
	// Try to extract from meta description tag
	descPattern := regexp.MustCompile(`<meta\s+name=["']description["']\s+content=["']([^"']+)["']`)
	if matches := descPattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
		return strings.TrimSpace(html.UnescapeString(matches[1]))
	}

	// Try to extract from og:description meta tag
	ogDescPattern := regexp.MustCompile(`<meta\s+property=["']og:description["']\s+content=["']([^"']+)["']`)
	if matches := ogDescPattern.FindStringSubmatch(htmlContent); len(matches) > 1 {
		return strings.TrimSpace(html.UnescapeString(matches[1]))
	}

	return ""
}

// extractSocialMediaFromHTML extracts social media links from HTML content.
func extractSocialMediaFromHTML(htmlContent string) []string {
	var urls []string

	// Extract Mastodon links (format: @username@instance.domain)
	mastodonRegex := regexp.MustCompile(`href="(https?://[^"]+/@[^"]+)"[^>]*>@[^@]+@[^<]+`)
	mastodonMatches := mastodonRegex.FindAllStringSubmatch(htmlContent, -1)
	for _, match := range mastodonMatches {
		if len(match) > 1 {
			urls = append(urls, match[1])
		}
	}

	// Extract other social media links
	socialPatterns := []string{
		`https?://(?:www\.)?twitter\.com/[\w]+`,
		`https?://(?:www\.)?x\.com/[\w]+`,
		`https?://(?:www\.)?linkedin\.com/in/[\w-]+`,
		`https?://(?:www\.)?instagram\.com/[\w.]+`,
		`https?://(?:www\.)?facebook\.com/[\w.]+`,
		`https?://(?:www\.)?youtube\.com/[\w/-]+`,
		`https?://(?:www\.)?twitch\.tv/[\w]+`,
		`https?://[\w.-]+\.social/@[\w]+`,   // Generic Mastodon pattern
		`https?://mastodon\.[\w.-]+/@[\w]+`, // Mastodon instances
		`https?://fosstodon\.org/@[\w]+`,    // Popular Mastodon instance
		`https?://techhub\.social/@[\w]+`,   // Tech Mastodon instance
		`https?://infosec\.exchange/@[\w]+`, // InfoSec Mastodon instance
	}

	for _, pattern := range socialPatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllString(htmlContent, -1)
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
