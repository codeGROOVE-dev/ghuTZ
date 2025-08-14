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

// OptionHolder holds configuration options
type OptionHolder struct {
	githubToken   string
	mapsAPIKey    string
	geminiAPIKey  string
	geminiModel   string
	gcpProject    string
	cacheDir      string
	forceActivity bool
}

// Result represents timezone detection results
type Result struct {
	Username                string    `json:"username"`
	Name                    string    `json:"name,omitempty"`
	Timezone                string    `json:"timezone"`
	ActivityTimezone        string    `json:"activity_timezone,omitempty"` // Pure activity-based timezone
	Location                *Location `json:"location,omitempty"`
	LocationName            string    `json:"location_name,omitempty"`
	GeminiSuggestedLocation string    `json:"gemini_suggested_location,omitempty"`
	Confidence              float64   `json:"confidence"`
	TimezoneConfidence      float64   `json:"timezone_confidence,omitempty"`
	LocationConfidence      float64   `json:"location_confidence,omitempty"`
	Method                  string    `json:"method"`
	DetectionTime           time.Time `json:"detection_time"`
	ActivityDateRange       struct {
		OldestActivity      time.Time `json:"oldest_activity,omitempty"`       // Oldest activity timestamp
		NewestActivity      time.Time `json:"newest_activity,omitempty"`       // Newest activity timestamp
		TotalDays           int       `json:"total_days,omitempty"`            // Days covered by activity data
		SpansDSTTransitions bool      `json:"spans_dst_transitions,omitempty"` // Whether data spans DST transitions
	} `json:"activity_date_range,omitempty"`
	QuietHoursUTC           []int     `json:"quiet_hours_utc,omitempty"` // Hours when user is typically inactive (UTC)
	
	// IMPORTANT: All time fields below store UTC values despite their names
	// The "Local" suffix is kept for backward compatibility but is misleading
	// Frontend must convert these UTC values to local timezone for display
	ActiveHoursLocal        struct {
		Start float64 `json:"start"` // Work start time in UTC (supports 30-min increments)
		End   float64 `json:"end"`   // Work end time in UTC (supports 30-min increments)
	} `json:"active_hours_local,omitempty"` // WARNING: Contains UTC times, not local!
	LunchHoursLocal struct {
		Start      float64 `json:"start"`      // Lunch start time in UTC (supports 30-min increments)
		End        float64 `json:"end"`        // Lunch end time in UTC (supports 30-min increments)
		Confidence float64 `json:"confidence"` // Confidence level of lunch detection (0.0-1.0)
	} `json:"lunch_hours_local,omitempty"` // WARNING: Contains UTC times, not local!
	PeakProductivity struct {
		Start float64 `json:"start"` // Peak productivity start in UTC (30-min resolution)
		End   float64 `json:"end"`   // Peak productivity end in UTC (30-min resolution)
		Count int     `json:"count"` // Activity count in this window
	} `json:"peak_productivity"` // Stored in UTC
	TopOrganizations []struct {
		Name  string `json:"name"`  // Organization name
		Count int    `json:"count"` // Activity count
	} `json:"top_organizations"`
	
	// Gemini AI Analysis Details
	GeminiPrompt    string `json:"gemini_prompt,omitempty"`    // The prompt sent to Gemini (for debugging)
	GeminiReasoning string `json:"gemini_reasoning,omitempty"` // Gemini's reasoning for its decision
	
	// HourlyActivityUTC stores the raw activity counts by UTC hour for histogram generation
	HourlyActivityUTC map[int]int `json:"hourly_activity_utc"` // Raw activity counts by UTC hour
	
	// HourlyOrganizationActivity stores organization-specific activity by UTC hour
	HourlyOrganizationActivity map[int]map[string]int `json:"hourly_organization_activity,omitempty"` // UTC hour -> org -> count
}

// Location represents geographic coordinates
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GitHubUser represents basic GitHub user info
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

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	HTMLURL    string    `json:"html_url"`
	Repository string    `json:"repository,omitempty"` // owner/repo format
}

// Issue represents a GitHub issue
type Issue struct {
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	HTMLURL    string    `json:"html_url"`
	Repository string    `json:"repository,omitempty"` // owner/repo format
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

// Repository represents a GitHub repository with location indicators
type Repository struct {
	Name            string `json:"name"`             // Repository name
	FullName        string `json:"full_name"`        // owner/repo format
	Description     string `json:"description"`      // Repository description
	Language        string `json:"language"`         // Primary language
	StargazersCount int    `json:"stargazers_count"` // Number of stars
	IsPinned        bool   `json:"is_pinned"`        // Whether this is a pinned repo
	HTMLURL         string `json:"html_url"`         // GitHub URL
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
