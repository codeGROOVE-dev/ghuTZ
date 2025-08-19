package social

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
