package gutz //nolint:revive // Multiple public structs needed for API

import (
	"math"
	"strconv"
	"strings"
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

// WithMemoryOnlyCache configures the detector to use memory-only HTTP caching.
func WithMemoryOnlyCache() Option {
	return func(o *OptionHolder) {
		o.memoryOnlyCache = true
	}
}

// WithNoCache disables all caching (both memory and disk).
func WithNoCache() Option {
	return func(o *OptionHolder) {
		o.noCache = true
	}
}

// OptionHolder holds configuration options.
type OptionHolder struct {
	githubToken     string
	mapsAPIKey      string
	geminiAPIKey    string
	geminiModel     string
	gcpProject      string
	cacheDir        string
	forceActivity   bool
	memoryOnlyCache bool
	noCache         bool // Explicitly disable all caching
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

// SleepRange represents a sleep period.
type SleepRange struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Duration float64 `json:"duration"`
}

// CalculateSleepRanges converts UTC sleep hours to local time and groups them into ranges.
func CalculateSleepRanges(sleepHoursUTC []int, tz string) []SleepRange {
	if len(sleepHoursUTC) == 0 {
		return nil
	}

	// Convert UTC sleep hours to local hours
	var localSleepHours []int
	for _, utcHour := range sleepHoursUTC {
		localHour := int(convertUTCToLocalFloat(float64(utcHour), tz))
		localSleepHours = append(localSleepHours, localHour)
	}

	// Group consecutive sleep hours into ranges
	ranges := groupSleepHours(localSleepHours)

	// Convert to SleepRange structs and filter valid ones
	var sleepRanges []SleepRange
	for _, r := range ranges {
		// Calculate duration of this sleep range
		duration := float64(r.end - r.start)
		if duration <= 0 {
			// Handle wraparound (e.g., 22:00 - 6:00)
			duration = (24 - float64(r.start)) + float64(r.end)
		}

		// Only include ranges that are 4-12 hours (reasonable sleep periods)
		if duration >= 4 && duration <= 12 {
			sleepRanges = append(sleepRanges, SleepRange{
				Start:    float64(r.start),
				End:      float64(r.end),
				Duration: duration,
			})
		}
	}

	return sleepRanges
}

type sleepRange struct {
	start, end int
}

func convertUTCToLocalFloat(utcHour float64, tz string) float64 {
	// Use Go's native timezone conversion (same as histogram package)
	if loc, err := time.LoadLocation(tz); err == nil {
		today := time.Now().UTC().Truncate(24 * time.Hour)
		hour := int(utcHour)
		minutes := int((utcHour - float64(hour)) * 60)
		utcTime := today.Add(time.Duration(hour)*time.Hour + time.Duration(minutes)*time.Minute)
		localTime := utcTime.In(loc)
		return float64(localTime.Hour()) + float64(localTime.Minute())/60.0
	}
	// Fallback for UTC+/- format
	if strings.HasPrefix(tz, "UTC") {
		offsetStr := strings.TrimPrefix(tz, "UTC")
		if offsetStr == "" {
			return utcHour // UTC+0
		}
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			return math.Mod(utcHour+float64(offset)+24, 24)
		}
	}
	return utcHour // Default to UTC if parsing fails
}

func groupSleepHours(localSleepHours []int) []sleepRange {
	var ranges []sleepRange
	if len(localSleepHours) == 0 {
		return ranges
	}

	currentStart := localSleepHours[0]
	currentEnd := localSleepHours[0]

	for i := 1; i < len(localSleepHours); i++ {
		hour := localSleepHours[i]

		if isConsecutiveHour(currentEnd, hour) {
			currentEnd = hour
		} else {
			// End current range and start new one
			ranges = append(ranges, sleepRange{currentStart, (currentEnd + 1) % 24})
			currentStart = hour
			currentEnd = hour
		}
	}

	// Add the final range
	ranges = append(ranges, sleepRange{currentStart, (currentEnd + 1) % 24})
	return ranges
}

func isConsecutiveHour(currentEnd, hour int) bool {
	// Check if this hour is consecutive (handle day wraparound)
	return hour == (currentEnd+1)%24 || (currentEnd == 23 && hour == 0)
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
	SleepRanges                []SleepRange           `json:"sleep_ranges,omitempty"`
	TopOrganizations           []OrgActivity          `json:"top_organizations"`
	SleepBucketsUTC            []float64              `json:"sleep_buckets_utc,omitempty"`
	TimezoneCandidates         []timezone.Candidate   `json:"timezone_candidates,omitempty"`
	DataSources                []string               `json:"data_sources,omitempty"`
	LunchHoursUTC              LunchBreak             `json:"lunch_hours_utc,omitempty"`
	LunchHoursLocal            LunchBreak             `json:"lunch_hours_local,omitempty"`
	PeakProductivity           PeakTime               `json:"peak_productivity"`
	ActiveHoursLocal           ActiveHours            `json:"active_hours_local,omitempty"`
	LocationConfidence         float64                `json:"location_confidence,omitempty"`
	TimezoneConfidence         float64                `json:"timezone_confidence,omitempty"`
	Confidence                 float64                `json:"confidence"`
	GeminiActivityMismatch     bool                   `json:"gemini_activity_mismatch,omitempty"`
	Verification               *VerificationResult    `json:"verification,omitempty"`
	GeminiSuspiciousMismatch   bool                   `json:"gemini_suspicious_mismatch,omitempty"`
	GeminiMismatchReason       string                 `json:"gemini_mismatch_reason,omitempty"`
	GeminiActivityOffsetHours  float64                `json:"gemini_activity_offset_hours,omitempty"`
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
