package ghutz

import "time"

// Option configures a Detector.
type Option func(*OptionHolder)

// Options for Detector.
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

func WithActivityAnalysis(enabled bool) Option {
	return func(o *OptionHolder) {
		o.forceActivity = enabled
	}
}

func WithCacheDir(dir string) Option {
	return func(o *OptionHolder) {
		o.cacheDir = dir
	}
}

// OptionHolder holds configuration options.
type OptionHolder struct {
	githubToken   string
	mapsAPIKey    string
	geminiAPIKey  string
	geminiModel   string
	gcpProject    string
	cacheDir      string
	forceActivity bool
}

// Result represents timezone detection results.
type Result struct {
	DetectionTime              time.Time              `json:"detection_time"`
	Location                   *Location              `json:"location,omitempty"`
	HourlyOrganizationActivity map[int]map[string]int `json:"hourly_organization_activity,omitempty"`
	HourlyActivityUTC          map[int]int            `json:"hourly_activity_utc"`
	Method                     string                 `json:"method"`
	LocationName               string                 `json:"location_name,omitempty"`
	GeminiSuggestedLocation    string                 `json:"gemini_suggested_location,omitempty"`
	Name                       string                 `json:"name,omitempty"`
	Timezone                   string                 `json:"timezone"`
	GeminiReasoning            string                 `json:"gemini_reasoning,omitempty"`
	Username                   string                 `json:"username"`
	ActivityTimezone           string                 `json:"activity_timezone,omitempty"`
	GeminiPrompt               string                 `json:"gemini_prompt,omitempty"`
	ActivityDateRange          struct {
		OldestActivity      time.Time `json:"oldest_activity,omitempty"`
		NewestActivity      time.Time `json:"newest_activity,omitempty"`
		TotalDays           int       `json:"total_days,omitempty"`
		SpansDSTTransitions bool      `json:"spans_dst_transitions,omitempty"`
	} `json:"activity_date_range,omitempty"`
	TopOrganizations []struct // Oldest activity timestamp
	// Whether data spans DST transitions
	{
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"top_organizations"`
	QuietHoursUTC []int `json:"quiet_hours_utc,omitempty"` // Organization name
	// Activity count

	LunchHoursLocal struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Confidence float64 `json:"confidence"`
	} `json:"lunch_hours_local,omitempty"`
	PeakProductivity struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Count int     `json:"count"`
	} `json:"peak_productivity"`
	ActiveHoursLocal struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"active_hours_local,omitempty"`
	LocationConfidence float64 `json:"location_confidence,omitempty"`
	TimezoneConfidence float64 `json:"timezone_confidence,omitempty"`
	Confidence         float64 `json:"confidence"` // Lunch start time in UTC (supports 30-min increments)
	// Work end time in UTC (supports 30-min increments)
}

// Location represents geographic coordinates.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GitHubUser represents basic GitHub user info.
type GitHubUser struct {
	Login           string `json:"login"`
	Name            string `json:"name"`
	Location        string `json:"location"`
	Company         string `json:"company"`
	Blog            string `json:"blog"`
	Email           string `json:"email"`
	Bio             string `json:"bio"`
	TwitterUsername string `json:"twitter_username"`
	CreatedAt       string `json:"created_at"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	HTMLURL    string    `json:"html_url"`
	Repository string    `json:"repository,omitempty"` // owner/repo format
}

// Issue represents a GitHub issue.
type Issue struct {
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	HTMLURL    string    `json:"html_url"`
	Repository string    `json:"repository,omitempty"` // owner/repo format
}

// Comment represents a GitHub comment.
type Comment struct {
	CreatedAt time.Time `json:"created_at"`
	Type      string    `json:"type"` // "issue" or "commit"
}

// Organization represents a GitHub organization.
type Organization struct {
	Login       string `json:"login"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

// Repository represents a GitHub repository with location indicators.
type Repository struct {
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	Description     string `json:"description"`
	Language        string `json:"language"`
	HTMLURL         string `json:"html_url"`
	StargazersCount int    `json:"stargazers_count"`
	IsPinned        bool   `json:"is_pinned"`
}

// ActivityData holds all activity data for timezone detection.
type ActivityData struct {
	PullRequests []PullRequest
	Issues       []Issue
	Comments     []Comment
}

// TimezoneCandidate represents a timezone detection result with evidence.
type TimezoneCandidate struct {
	Timezone   string
	Evidence   []string
	Confidence float64
}
