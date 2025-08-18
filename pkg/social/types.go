package social

// Content represents structured content extracted from a social media profile or website
type Content struct {
	Kind     string   // "mastodon", "twitter", "website", "linkedin", etc.
	URL      string   // Original URL
	Bio      string   // Biography/description
	Markdown string   // Full content in markdown format
	Tags     []string // Hashtags or keywords
	Location string   // Location if found
	Name     string   // Display name or title
	Username string   // Username/handle if applicable
	Joined   string   // Join date if available
	Fields   map[string]string // Additional metadata fields
}