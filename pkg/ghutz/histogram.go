package ghutz

import (
	"fmt"
	"math"
	"strings"
	
	"github.com/fatih/color"
)

// ActivityHistogram represents activity data with visual representation
type ActivityHistogram struct {
	Username     string
	Timezone     string
	UTCOffset    int
	HourlyActivity map[int]int  // UTC hours -> activity count
	WorkStart    int
	WorkEnd      int
	LunchStart   float64
	LunchEnd     float64
	QuietHours   []int
}

// getOrgColorFunc returns a color function for an organization
func getOrgColorFunc(org string, topOrgs []struct{ Name string `json:"name"`; Count int `json:"count"` }) *color.Color {
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

// GenerateHistogram creates a visual representation of user activity
func GenerateHistogram(result *Result, hourCounts map[int]int, utcOffset int) string {
	var output strings.Builder
	
	// Check if we have organization data
	hasOrgData := result.HourlyOrganizationActivity != nil && len(result.HourlyOrganizationActivity) > 0
	
	// Modern, clean header
	output.WriteString("📊 Activity Pattern\n")
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
	
	// Find max activity for scaling
	maxActivity := 0
	for _, count := range hourCounts {
		if count > maxActivity {
			maxActivity = count
		}
	}
	
	if maxActivity == 0 {
		return output.String() + "No activity data available\n"
	}
	
	// Build the histogram with redesigned layout
	for localHour := 0; localHour < 24; localHour++ {
		// Convert local hour to UTC
		utcHour := localHour - utcOffset
		if utcHour < 0 {
			utcHour += 24
		}
		if utcHour >= 24 {
			utcHour -= 24
		}
		
		count := hourCounts[utcHour]
		
		// Determine what type of hour this is first
		hourType := ""
		hourColor := color.New(color.Reset) // Default no color
		
		// Check if it's a quiet/sleep hour
		for _, qh := range result.QuietHoursUTC {
			localQuietHour := qh + utcOffset
			if localQuietHour < 0 {
				localQuietHour += 24
			}
			if localQuietHour >= 24 {
				localQuietHour -= 24
			}
			if localHour == localQuietHour {
				hourType = "z"
				hourColor = color.New(color.FgBlue)
				break
			}
		}
		
		// Check for peak time first (highest priority)
		if hourType == "" && result.PeakProductivity.Count > 0 {
			// Convert peak times from UTC to display timezone
			peakStart := math.Mod(result.PeakProductivity.Start + float64(utcOffset) + 24, 24)
			peakEnd := math.Mod(result.PeakProductivity.End + float64(utcOffset) + 24, 24)
			localHourFloat := float64(localHour)
			
			// Check if the current hour overlaps with peak time
			hourStart := localHourFloat
			hourEnd := localHourFloat + 1.0
			
			// Check for overlap between [hourStart, hourEnd) and [peakStart, peakEnd)
			if hourEnd > peakStart && hourStart < peakEnd {
				hourType = "^"
				hourColor = color.New(color.FgYellow)
			}
		}
		
		// Check for lunch hour
		if hourType == "" && (result.LunchHoursLocal.Start != 0 || result.LunchHoursLocal.End != 0) {
			// Convert lunch times from UTC to display timezone
			lunchStartHour := math.Mod(result.LunchHoursLocal.Start + float64(utcOffset) + 24, 24)
			lunchEndHour := math.Mod(result.LunchHoursLocal.End + float64(utcOffset) + 24, 24)
			localHourFloat := float64(localHour)
			
			// Check if the current hour block overlaps with lunch time
			hourStart := localHourFloat
			hourEnd := localHourFloat + 1.0
			
			// Check for overlap between [hourStart, hourEnd) and [lunchStart, lunchEnd)
			if hourEnd > lunchStartHour && hourStart < lunchEndHour {
				hourType = "L"
				hourColor = color.New(color.FgGreen)
			}
		}
		
		// Start building the line
		line := fmt.Sprintf("%02d:00 ", localHour)
		
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
			// Scale to max 20 characters for the bar
			barLength := (count * 20) / maxActivity
			if barLength == 0 {
				barLength = 1 // Ensure at least one character for any activity
			}
			
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
	
	return output.String()
}

func formatHour(decimalHour float64) string {
	hour := int(decimalHour)
	minutes := int((decimalHour - float64(hour)) * 60)
	return fmt.Sprintf("%02d:%02d", hour, minutes)
}