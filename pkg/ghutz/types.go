package ghutz

import "time"

// Option configures a Detector
type Option func(*OptionHolder)

// Options for Detector
func WithGitHubToken(token string) Option {
	return func(o *OptionHolder) {
		o.githubToken = token
	}
}

func WithMapsAPIKey(key string) Option {
	return func(o *OptionHolder) {
		o.mapsAPIKey = key
	}
}

func WithGeminiAPIKey(key string) Option {
	return func(o *OptionHolder) {
		o.geminiAPIKey = key
	}
}

func WithGeminiModel(model string) Option {
	return func(o *OptionHolder) {
		o.geminiModel = model
	}
}

func WithGCPProject(projectID string) Option {
	return func(o *OptionHolder) {
		o.gcpProject = projectID
	}
}

func WithHTTPClient(client interface{}) Option {
	return func(o *OptionHolder) {
		// Not implemented, keeping for compatibility
	}
}

func WithLogger(logger interface{}) Option {
	return func(o *OptionHolder) {
		// Logger is handled differently
	}
}

// OptionHolder holds configuration options
type OptionHolder struct {
	githubToken  string
	mapsAPIKey   string
	geminiAPIKey string
	geminiModel  string
	gcpProject   string
}

// Result represents timezone detection results
type Result struct {
	Username             string    `json:"username"`
	Timezone             string    `json:"timezone"`
	Location             *Location `json:"location,omitempty"`
	LocationName         string    `json:"location_name,omitempty"`
	GeminiSuggestedLocation string `json:"gemini_suggested_location,omitempty"`
	Confidence           float64   `json:"confidence"`
	TimezoneConfidence   float64   `json:"timezone_confidence,omitempty"`
	LocationConfidence   float64   `json:"location_confidence,omitempty"`
	Method               string    `json:"method"`
	DetectionTime        time.Time `json:"detection_time"`
}

// Location represents geographic coordinates
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GitHubUser represents basic GitHub user info
type GitHubUser struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	Location  string `json:"location"`
	Company   string `json:"company"`
	Blog      string `json:"blog"`
	Email     string `json:"email"`
	Bio       string `json:"bio"`
	CreatedAt string `json:"created_at"`
}

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
}

// Issue represents a GitHub issue
type Issue struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
}

// Comment represents a GitHub comment
type Comment struct {
	CreatedAt time.Time `json:"created_at"`
	Type      string    `json:"type"` // "issue" or "commit"
}

// Organization represents a GitHub organization
type Organization struct {
	Login       string `json:"login"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

// ActivityData holds all activity data for timezone detection
type ActivityData struct {
	PullRequests []PullRequest
	Issues       []Issue
	Comments     []Comment
}

// TimezoneCandidate represents a timezone detection result with evidence
type TimezoneCandidate struct {
	Timezone   string
	Confidence float64
	Evidence   []string
}