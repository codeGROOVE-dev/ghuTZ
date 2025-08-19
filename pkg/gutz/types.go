package gutz

import (
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/github"
	"github.com/codeGROOVE-dev/guTZ/pkg/timezone"
)

// Option configures a Detector.
type Option func(*OptionHolder)

// WithGitHubToken sets the GitHub API token for the Detector.
func WithGitHubToken(token string) Option {
	return func(o *OptionHolder) {
		o.githubToken = token
	}
}

// WithMapsAPIKey sets the Google Maps API key for geocoding services.
func WithMapsAPIKey(key string) Option {
	return func(o *OptionHolder) {
		o.mapsAPIKey = key
	}
}

// WithGeminiAPIKey sets the Gemini API key for AI-based timezone detection.
func WithGeminiAPIKey(key string) Option {
	return func(o *OptionHolder) {
		o.geminiAPIKey = key
	}
}

// WithGeminiModel sets the Gemini model to use for AI-based detection.
func WithGeminiModel(model string) Option {
	return func(o *OptionHolder) {
		o.geminiModel = model
	}
}

// WithGCPProject sets the GCP project ID for Gemini API access.
func WithGCPProject(projectID string) Option {
	return func(o *OptionHolder) {
		o.gcpProject = projectID
	}
}

// WithHTTPClient sets the HTTP client (kept for compatibility, not implemented).
func WithHTTPClient(_ any) Option {
	return func(_ *OptionHolder) {
		// Not implemented, keeping for compatibility
	}
}

// WithLogger sets the logger (kept for compatibility, handled differently).
func WithLogger(_ any) Option {
	return func(_ *OptionHolder) {
		// Logger is handled differently
	}
}

// WithActivityAnalysis enables or disables activity analysis.
func WithActivityAnalysis(enabled bool) Option {
	return func(o *OptionHolder) {
		o.forceActivity = enabled
	}
}

// WithCacheDir sets the cache directory for HTTP requests.
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

// LunchBreak represents detected lunch break times.
type LunchBreak struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
}

// PeakTime represents peak productivity periods.
type PeakTime struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Count int     `json:"count"`
}

// ActiveHours represents working hours.
type ActiveHours struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// DateRange represents activity date range.
type DateRange struct {
	OldestActivity      time.Time `json:"oldest_activity,omitempty"`
	NewestActivity      time.Time `json:"newest_activity,omitempty"`
	TotalDays           int       `json:"total_days,omitempty"`
	SpansDSTTransitions bool      `json:"spans_dst_transitions,omitempty"`
}

// OrgActivity represents organization activity counts.
type OrgActivity struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Result represents timezone detection results.
type Result struct {
	DetectionTime              time.Time              `json:"detection_time"`
	Location                   *Location              `json:"location,omitempty"`
	HourlyOrganizationActivity map[int]map[string]int `json:"hourly_organization_activity,omitempty"`
	HourlyActivityUTC          map[int]int            `json:"hourly_activity_utc"`
	HalfHourlyActivityUTC      map[float64]int        `json:"-"`
	Method                     string                 `json:"method"`
	LocationName               string                 `json:"location_name,omitempty"`
	GeminiSuggestedLocation    string                 `json:"gemini_suggested_location,omitempty"`
	Name                       string                 `json:"name,omitempty"`
	Timezone                   string                 `json:"timezone"`
	GeminiReasoning            string                 `json:"gemini_reasoning,omitempty"`
	Username                   string                 `json:"username"`
	CreatedAt                  *time.Time             `json:"created_at,omitempty"`
	ActivityTimezone           string                 `json:"activity_timezone,omitempty"`
	GeminiPrompt               string                 `json:"gemini_prompt,omitempty"`
	ActivityDateRange          DateRange              `json:"activity_date_range,omitempty"`
	SleepHoursUTC              []int                  `json:"sleep_hours_utc,omitempty"`
	TopOrganizations           []OrgActivity          `json:"top_organizations"`
	SleepBucketsUTC            []float64              `json:"sleep_buckets_utc,omitempty"`
	TimezoneCandidates         []timezone.Candidate   `json:"timezone_candidates,omitempty"`
	DataSources                []string               `json:"data_sources,omitempty"`
	LunchHoursUTC              LunchBreak             `json:"lunch_hours_utc,omitempty"`
	PeakProductivity           PeakTime               `json:"peak_productivity"`
	ActiveHoursLocal           ActiveHours            `json:"active_hours_local,omitempty"`
	LocationConfidence         float64                `json:"location_confidence,omitempty"`
	TimezoneConfidence         float64                `json:"timezone_confidence,omitempty"`
	Confidence                 float64                `json:"confidence"`
}

// Location represents geographic coordinates.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Note: GitHub-related types (User, PullRequest, Issue, Comment, Organization, Repository)
// have been moved to the github package.

// ActivityData holds all activity data for timezone detection.
type ActivityData struct {
	PullRequests []github.PullRequest
	Issues       []github.Issue
	Comments     []github.Comment
	StarredRepos []github.Repository
}
