// Package histogram provides visualization of activity patterns.
package histogram

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

// Result represents timezone detection results - imported type needed for histogram.
type Result struct {
	HalfHourlyActivityUTC      map[float64]int        `json:"-"`
	HourlyOrganizationActivity map[int]map[string]int `json:"hourly_organization_activity,omitempty"`
	TopOrganizations           []OrgActivity          `json:"top_organizations"`
	QuietHoursUTC              []int                  `json:"quiet_hours_utc"`
	SleepBucketsUTC            []float64              `json:"sleep_buckets_utc,omitempty"`
	PeakProductivity           PeakProductivity       `json:"peak_productivity,omitempty"`
	LunchHoursUTC              LunchBreak             `json:"lunch_hours_utc,omitempty"`
}

// OrgActivity represents activity for an organization.
type OrgActivity struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// PeakProductivity represents peak productivity hours.
type PeakProductivity struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Count int     `json:"count"`
}

// LunchBreak represents lunch break times.
type LunchBreak struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
}

// ActivityHistogram represents activity data with visual representation.
type ActivityHistogram struct {
	HourlyActivity map[int]int
	Username       string
	Timezone       string
	QuietHours     []int
	UTCOffset      int
	WorkStart      int
	WorkEnd        int
	LunchStart     float64
	LunchEnd       float64
}

// getOrgColorFunc returns a color function for an organization.
func getOrgColorFunc(org string, topOrgs []OrgActivity) *color.Color {
	// Define colors for top 3 orgs only
	colors := []*color.Color{
		color.New(color.FgBlue),   // Blue for top org
		color.New(color.FgYellow), // Yellow for 2nd
		color.New(color.FgRed),    // Red for 3rd
	}

	// Find org position in top orgs
	for i, topOrg := range topOrgs {
		if i < 3 && topOrg.Name == org {
			return colors[i]
		}
	}

	// Grey for all other orgs (4th, 5th, etc.)
	return color.New(color.FgHiBlack)
}

// convertUTCToLocal converts a UTC hour (float) to local time using Go's timezone database.
func convertUTCToLocal(utcHour float64, timezone string) float64 {
	if loc, err := time.LoadLocation(timezone); err == nil {
		// Use Go's native timezone conversion
		today := time.Now().UTC().Truncate(24 * time.Hour)
		hour := int(utcHour)
		minutes := int((utcHour - float64(hour)) * 60)
		utcTime := today.Add(time.Duration(hour)*time.Hour + time.Duration(minutes)*time.Minute)
		localTime := utcTime.In(loc)
		return float64(localTime.Hour()) + float64(localTime.Minute())/60.0
	}
	// Fallback for UTC+/- format
	if strings.HasPrefix(timezone, "UTC") {
		offsetStr := strings.TrimPrefix(timezone, "UTC")
		if offsetStr == "" {
			return utcHour // UTC+0
		}
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			return math.Mod(utcHour+float64(offset)+24, 24)
		}
	}
	return utcHour // No conversion possible
}

// histogramData holds the data needed to generate a histogram.
type histogramData struct {
	result       *Result
	hourCounts   map[int]int
	timezone     string
	hasOrgData   bool
	totalEvents  int
	maxActivity  int
}

// GenerateHistogram creates a visual representation of user activity.
func GenerateHistogram(result *Result, hourCounts map[int]int, timezone string) string {
	data := prepareHistogramData(result, hourCounts, timezone)
	if data == nil {
		return "No activity data available\n"
	}

	var output strings.Builder
	writeHistogramHeader(&output, data.totalEvents)
	writeHistogramBars(&output, data)
	return output.String()
}

// prepareHistogramData initializes and validates the data for histogram generation.
func prepareHistogramData(result *Result, hourCounts map[int]int, timezone string) *histogramData {
	data := &histogramData{
		result:     result,
		hourCounts: hourCounts,
		timezone:   timezone,
		hasOrgData: len(result.HourlyOrganizationActivity) > 0,
	}

	// Count total events
	for _, count := range hourCounts {
		data.totalEvents += count
	}

	// Find max activity for scaling
	data.maxActivity = findMaxActivity(hourCounts, result.HalfHourlyActivityUTC)
	if data.maxActivity == 0 || len(result.HalfHourlyActivityUTC) == 0 {
		return nil
	}

	return data
}

// findMaxActivity determines the maximum activity count across all time periods.
func findMaxActivity(hourCounts map[int]int, halfHourCounts map[float64]int) int {
	maxActivity := 0
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	for _, count := range halfHourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	return maxActivity
}

// writeHistogramHeader writes the header and warnings to the output.
func writeHistogramHeader(output *strings.Builder, totalEvents int) {
	output.WriteString("📊 Activity Pattern (30-minute resolution)\n")
	output.WriteString(strings.Repeat("─", 50) + "\n")

	if totalEvents < 20 {
		fmt.Fprintf(output, "⚠️  Limited data: only %d events available\n", totalEvents)
		output.WriteString(strings.Repeat("─", 50) + "\n")
	}
}

// writeHistogramBars generates and writes all the histogram bars.
func writeHistogramBars(output *strings.Builder, data *histogramData) {
	for utcHour := range 24 {
		// Process both half-hour buckets for each hour
		for halfHour := range 2 {
			bucket := float64(utcHour) + float64(halfHour)*0.5
			count := data.result.HalfHourlyActivityUTC[bucket]
			line := generateHistogramLine(data, bucket, count, utcHour)
			output.WriteString(line + "\n")
		}
	}
}

// generateHistogramLine creates a single line of the histogram.
func generateHistogramLine(data *histogramData, bucket float64, count, utcHour int) string {
	localTime := convertUTCToLocal(bucket, data.timezone)
	localHour := int(localTime)
	localMin := int((localTime - float64(localHour)) * 60)

	hourType, hourColor := determineHourType(data.result, bucket, localTime, localHour, data.timezone)
	line := buildTimeAndTypeIndicator(localHour, localMin, hourType, hourColor)
	line += buildCountIndicator(count)
	line += buildActivityBar(data, count, utcHour)

	return line
}

// determineHourType identifies what type of time period this bucket represents.
func determineHourType(result *Result, bucket, localTime float64, localHour int, timezone string) (string, *color.Color) {
	// Check for sleep time
	if len(result.SleepBucketsUTC) > 0 {
		for _, sleepBucket := range result.SleepBucketsUTC {
			if bucket == sleepBucket {
				return "z", color.New(color.FgBlue)
			}
		}
	} else {
		// Fall back to hourly quiet hours
		for _, qh := range result.QuietHoursUTC {
			localQuietTime := convertUTCToLocal(float64(qh), timezone)
			if localHour == int(localQuietTime) {
				return "z", color.New(color.FgBlue)
			}
		}
	}

	// Check for peak time
	if result.PeakProductivity.Count > 0 {
		peakStart := convertUTCToLocal(result.PeakProductivity.Start, timezone)
		peakEnd := convertUTCToLocal(result.PeakProductivity.End, timezone)
		if localTime >= peakStart && localTime < peakEnd {
			return "^", color.New(color.FgYellow)
		}
	}

	// Check for lunch time
	if result.LunchHoursUTC.Start != 0 || result.LunchHoursUTC.End != 0 {
		lunchStart := convertUTCToLocal(result.LunchHoursUTC.Start, timezone)
		lunchEnd := convertUTCToLocal(result.LunchHoursUTC.End, timezone)
		if localTime >= lunchStart && localTime < lunchEnd {
			return "L", color.New(color.FgGreen)
		}
	}

	return "", color.New(color.Reset)
}

// buildTimeAndTypeIndicator creates the time and type indicator part of the line.
func buildTimeAndTypeIndicator(localHour, localMin int, hourType string, hourColor *color.Color) string {
	line := fmt.Sprintf("%02d:%02d ", localHour, localMin)
	if hourType != "" {
		line += hourColor.Sprint(hourType) + " "
	} else {
		line += "  "
	}
	return line
}

// buildCountIndicator creates the count display part of the line.
func buildCountIndicator(count int) string {
	if count > 0 {
		return fmt.Sprintf("(%2d) ", count)
	}
	return "     "
}

// buildActivityBar creates the visual bar representation.
func buildActivityBar(data *histogramData, count, utcHour int) string {
	if count == 0 {
		return ""
	}

	barLength := count
	if data.hasOrgData && data.result.HourlyOrganizationActivity[utcHour] != nil {
		return buildOrganizationBar(data.result, barLength, count, utcHour)
	}
	return buildSimpleBar(barLength)
}

// buildOrganizationBar creates a color-coded bar based on organization activity.
func buildOrganizationBar(result *Result, barLength, count, utcHour int) string {
	orgActivity := result.HourlyOrganizationActivity[utcHour]
	var bar strings.Builder
	remaining := barLength

	// Add segments for each top organization
	for _, topOrg := range result.TopOrganizations {
		orgCount, exists := orgActivity[topOrg.Name]
		if !exists || remaining <= 0 {
			continue
		}

		segmentLength := (orgCount * barLength) / count
		if segmentLength == 0 && orgCount > 0 {
			segmentLength = 1
		}
		if segmentLength > remaining {
			segmentLength = remaining
		}

		colorFunc := getOrgColorFunc(topOrg.Name, result.TopOrganizations)
		bar.WriteString(colorFunc.Sprint(strings.Repeat("█", segmentLength)))
		remaining -= segmentLength
	}

	// Add any remaining as "other" activity
	if remaining > 0 {
		greyColor := color.New(color.FgHiBlack)
		bar.WriteString(greyColor.Sprint(strings.Repeat("█", remaining)))
	}

	return bar.String()
}

// buildSimpleBar creates a simple grey bar without organization data.
func buildSimpleBar(barLength int) string {
	greyColor := color.New(color.FgHiBlack)
	if barLength == 1 {
		return greyColor.Sprint("·")
	}
	return greyColor.Sprint(strings.Repeat("█", barLength))
}
