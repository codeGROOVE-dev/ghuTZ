package social

import (
	"regexp"
)

// Content represents structured content extracted from a social media profile or website.
type Content struct {
	Fields   map[string]string
	Kind     string
	URL      string
	Bio      string
	Markdown string
	Location string
	Name     string
	Username string
	Joined   string
	Tags     []string
}

// extractHashtags extracts hashtags from text.
func extractHashtags(text string) []string {
	if text == "" {
		return nil
	}

	// Find all hashtags (word boundary followed by # and alphanumeric chars)
	re := regexp.MustCompile(`#\w+`)
	matches := re.FindAllString(text, -1)

	// De-duplicate
	seen := make(map[string]bool)
	var tags []string
	for _, match := range matches {
		if !seen[match] {
			seen[match] = true
			tags = append(tags, match)
		}
	}

	return tags
}
