package gutz //nolint:revive // Multiple public structs needed for API

import (
	"math"
	"sort"
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

// CalculateSleepRangesFromBuckets converts UTC sleep buckets (half-hourly) into local sleep ranges.
func CalculateSleepRangesFromBuckets(sleepBucketsUTC []float64, tz string) []SleepRange {
	if len(sleepBucketsUTC) == 0 {
		return nil
	}

	localSleepBuckets := convertBucketsToLocal(sleepBucketsUTC, tz)

	// Check for wraparound case first (evening >= 22 and morning <= 6)
	if wraparoundRange := tryCreateWraparoundRange(localSleepBuckets); wraparoundRange != nil {
		return []SleepRange{*wraparoundRange}
	}

	// Process as normal consecutive ranges
	return groupConsecutiveBuckets(localSleepBuckets)
}

func convertBucketsToLocal(sleepBucketsUTC []float64, tz string) []float64 {
	localSleepBuckets := make([]float64, len(sleepBucketsUTC))
	for i, utcBucket := range sleepBucketsUTC {
		localSleepBuckets[i] = convertUTCToLocalFloat(utcBucket, tz)
	}
	return localSleepBuckets
}

func tryCreateWraparoundRange(localSleepBuckets []float64) *SleepRange {
	var eveningBuckets, morningBuckets []float64
	for _, bucket := range localSleepBuckets {
		if bucket >= 22 {
			eveningBuckets = append(eveningBuckets, bucket)
		} else if bucket <= 6 {
			morningBuckets = append(morningBuckets, bucket)
		}
	}

	if len(eveningBuckets) == 0 || len(morningBuckets) == 0 {
		return nil
	}

	sort.Float64s(eveningBuckets)
	sort.Float64s(morningBuckets)

	sleepStart := eveningBuckets[0]
	sleepEnd := morningBuckets[len(morningBuckets)-1] + 0.5
	duration := (24 - sleepStart) + sleepEnd

	if duration >= 4 && duration <= 12 {
		return &SleepRange{
			Start:    sleepStart,
			End:      sleepEnd,
			Duration: duration,
		}
	}
	return nil
}

func groupConsecutiveBuckets(localSleepBuckets []float64) []SleepRange {
	sort.Float64s(localSleepBuckets)
	if len(localSleepBuckets) == 0 {
		return nil
	}

	var ranges []SleepRange
	currentStart := localSleepBuckets[0]
	currentEnd := localSleepBuckets[0] + 0.5

	for i := 1; i < len(localSleepBuckets); i++ {
		bucket := localSleepBuckets[i]
		if math.Abs(bucket-currentEnd) < 0.1 {
			currentEnd = bucket + 0.5
		} else {
			if range_ := createRangeIfValid(currentStart, currentEnd); range_ != nil {
				ranges = append(ranges, *range_)
			}
			currentStart = bucket
			currentEnd = bucket + 0.5
		}
	}

	// Add the last range
	if range_ := createRangeIfValid(currentStart, currentEnd); range_ != nil {
		ranges = append(ranges, *range_)
	}

	return ranges
}

func createRangeIfValid(start, end float64) *SleepRange {
	duration := end - start
	if duration <= 0 {
		duration = (24 - start) + end
	}
	if duration >= 4 && duration <= 12 {
		return &SleepRange{
			Start:    start,
			End:      end,
			Duration: duration,
		}
	}
	return nil
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
	CreatedAt                  *time.Time             `json:"created_at,omitempty"`
	HourlyOrganizationActivity map[int]map[string]int `json:"hourly_organization_activity,omitempty"`
	HourlyActivityUTC          map[int]int            `json:"hourly_activity_utc"`
	HalfHourlyActivityUTC      map[float64]int        `json:"-"`
	Location                   *Location              `json:"location,omitempty"`
	Verification               *VerificationResult    `json:"verification,omitempty"`
	GeminiReasoning            string                 `json:"gemini_reasoning,omitempty"`
	Name                       string                 `json:"name,omitempty"`
	GeminiSuggestedLocation    string                 `json:"gemini_suggested_location,omitempty"`
	Username                   string                 `json:"username"`
	Timezone                   string                 `json:"timezone"`
	LocationName               string                 `json:"location_name,omitempty"`
	ActivityTimezone           string                 `json:"activity_timezone,omitempty"`
	GeminiPrompt               string                 `json:"gemini_prompt,omitempty"`
	Method                     string                 `json:"method"`
	GeminiMismatchReason       string                 `json:"gemini_mismatch_reason,omitempty"`
	ActivityDateRange          DateRange              `json:"activity_date_range,omitempty"`
	SleepHoursUTC              []int                  `json:"sleep_hours_utc,omitempty"`
	TopOrganizations           []OrgActivity          `json:"top_organizations"`
	TimezoneCandidates         []timezone.Candidate   `json:"timezone_candidates,omitempty"`
	DataSources                []string               `json:"data_sources,omitempty"`
	SleepRangesLocal           []SleepRange           `json:"sleep_ranges_local,omitempty"`
	SleepBucketsUTC            []float64              `json:"sleep_buckets_utc,omitempty"`
	LunchHoursLocal            LunchBreak             `json:"lunch_hours_local,omitempty"`
	PeakProductivityLocal      PeakTime               `json:"peak_productivity_local"`
	PeakProductivityUTC        PeakTime               `json:"peak_productivity_utc"`
	LunchHoursUTC              LunchBreak             `json:"lunch_hours_utc,omitempty"`
	ActiveHoursLocal           ActiveHours            `json:"active_hours_local,omitempty"`
	ActiveHoursUTC             ActiveHours            `json:"active_hours_utc,omitempty"`
	LocationConfidence         float64                `json:"location_confidence,omitempty"`
	TimezoneConfidence         float64                `json:"timezone_confidence,omitempty"`
	Confidence                 float64                `json:"confidence"`
	GeminiActivityOffsetHours  float64                `json:"gemini_activity_offset_hours,omitempty"`
	GeminiActivityMismatch     bool                   `json:"gemini_activity_mismatch,omitempty"`
	GeminiSuspiciousMismatch   bool                   `json:"gemini_suspicious_mismatch,omitempty"`
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
