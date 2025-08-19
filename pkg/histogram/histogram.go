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
		min := int((utcHour - float64(hour)) * 60)
		utcTime := today.Add(time.Duration(hour)*time.Hour + time.Duration(min)*time.Minute)
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

// GenerateHistogram creates a visual representation of user activity.
func GenerateHistogram(result *Result, hourCounts map[int]int, timezone string) string {
	var output strings.Builder

	// Check if we have organization data
	hasOrgData := len(result.HourlyOrganizationActivity) > 0

	// Modern, clean header
	output.WriteString("📊 Activity Pattern (30-minute resolution)\n")
	output.WriteString(strings.Repeat("─", 50) + "\n")

	// Count total events for confidence indicator
	totalEvents := 0
	for _, count := range hourCounts {
		totalEvents += count
	}

	// Add a note if we have limited data
	if totalEvents < 20 {
		output.WriteString(fmt.Sprintf("⚠️  Limited data: only %d events available\n", totalEvents))
		output.WriteString(strings.Repeat("─", 50) + "\n")
	}

	// Find max activity for scaling (need to check both hourly and half-hourly data)
	maxActivity := 0
	// First check hourly data
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	// Use the half-hour data (always available)
	halfHourCounts := result.HalfHourlyActivityUTC
	if len(halfHourCounts) == 0 {
		return output.String() + "No half-hour activity data available\n"
	}

	for _, count := range halfHourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}

	if maxActivity == 0 {
		return output.String() + "No activity data available\n"
	}

	// Build the histogram with 30-minute buckets in UTC order
	for utcHour := range 24 {
		// Process both half-hour buckets for each hour
		for halfHour := range 2 {
			bucket := float64(utcHour) + float64(halfHour)*0.5
			count := halfHourCounts[bucket]

			// Convert UTC time to local time for display
			localTime := convertUTCToLocal(bucket, timezone)
			localHour := int(localTime)
			localMin := int((localTime - float64(localHour)) * 60)

			// Determine what type of hour this is first
			hourType := ""
			hourColor := color.New(color.Reset) // Default no color

			// Check if it's a quiet/sleep hour using 30-minute resolution
			// First try the new SleepBucketsUTC if available
			if len(result.SleepBucketsUTC) > 0 {
				for _, sleepBucket := range result.SleepBucketsUTC {
					if bucket == sleepBucket {
						hourType = "z"
						hourColor = color.New(color.FgBlue)
						break
					}
				}
			} else {
				// Fall back to hourly quiet hours for backward compatibility
				for _, qh := range result.QuietHoursUTC {
					// Convert UTC quiet hour to local time using consistent conversion
					localQuietTime := convertUTCToLocal(float64(qh), timezone)
					localQuietHour := int(localQuietTime)
					if localHour == localQuietHour {
						hourType = "z"
						hourColor = color.New(color.FgBlue)
						break
					}
				}
			}

			// Check for peak time (with 30-minute precision)
			if hourType == "" && result.PeakProductivity.Count > 0 {
				// Convert peak times from UTC to display timezone using consistent conversion
				peakStart := convertUTCToLocal(result.PeakProductivity.Start, timezone)
				peakEnd := convertUTCToLocal(result.PeakProductivity.End, timezone)

				// Check if the current 30-minute bucket overlaps with peak time
				if localTime >= peakStart && localTime < peakEnd {
					hourType = "^"
					hourColor = color.New(color.FgYellow)
				}
			}

			// Check for lunch hour (with 30-minute precision)
			if hourType == "" && (result.LunchHoursUTC.Start != 0 || result.LunchHoursUTC.End != 0) {
				// Convert lunch times from UTC to display timezone using consistent conversion
				lunchStart := convertUTCToLocal(result.LunchHoursUTC.Start, timezone)
				lunchEnd := convertUTCToLocal(result.LunchHoursUTC.End, timezone)

				// Check if the current 30-minute bucket overlaps with lunch time
				if localTime >= lunchStart && localTime < lunchEnd {
					hourType = "L"
					hourColor = color.New(color.FgGreen)
				}
			}

			// Start building the line with 30-minute precision
			line := fmt.Sprintf("%02d:%02d ", localHour, localMin)

			// Add hour type indicator with fixed width (single character + space)
			if hourType != "" {
				line += hourColor.Sprint(hourType) + " " // Colored character + 1 space
			} else {
				line += "  " // 2 spaces to match character + space width
			}

			// Add count with consistent width
			if count > 0 {
				line += fmt.Sprintf("(%2d) ", count)
			} else {
				line += "     " // 5 spaces to match "(nn) "
			}

			// Create visual bar
			if count > 0 {
				// Use event count directly as bar length
				barLength := count

				if hasOrgData && result.HourlyOrganizationActivity[utcHour] != nil {
					// Build color-coded bar based on organization activity
					orgActivity := result.HourlyOrganizationActivity[utcHour]

					bar := ""
					remaining := barLength

					// Add segments for each top organization
					for _, topOrg := range result.TopOrganizations {
						if orgCount, exists := orgActivity[topOrg.Name]; exists && remaining > 0 {
							// Calculate this org's proportion
							segmentLength := (orgCount * barLength) / count
							if segmentLength == 0 && orgCount > 0 {
								segmentLength = 1
							}
							if segmentLength > remaining {
								segmentLength = remaining
							}

							colorFunc := getOrgColorFunc(topOrg.Name, result.TopOrganizations)
							bar += colorFunc.Sprint(strings.Repeat("█", segmentLength))
							remaining -= segmentLength
						}
					}

					// Add any remaining as "other" activity
					if remaining > 0 {
						greyColor := color.New(color.FgHiBlack)
						bar += greyColor.Sprint(strings.Repeat("█", remaining))
					}

					line += bar
				} else {
					// No org data, use simple grey bar
					greyColor := color.New(color.FgHiBlack)
					if barLength == 1 {
						line += greyColor.Sprint("·")
					} else {
						line += greyColor.Sprint(strings.Repeat("█", barLength))
					}
				}
			}

			output.WriteString(line + "\n")
		}
	}

	return output.String()
}
