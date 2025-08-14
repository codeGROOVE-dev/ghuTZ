package ghutz

import (
	"fmt"
	"strings"
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

// GenerateHistogram creates a visual representation of user activity
func GenerateHistogram(result *Result, hourCounts map[int]int, utcOffset int) string {
	var output strings.Builder
	
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
	
	// Build the histogram  
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
		
		// Create visual bar with proper scaling
		barLength := 0
		if maxActivity > 0 {
			// Scale to max 25 characters for better fit
			barLength = (count * 25) / maxActivity
		}
		
		bar := ""
		if count > 0 {
			if barLength == 0 {
				bar = "·" // Minimal activity indicator
			} else {
				// Use simple ASCII for better alignment
				bar = strings.Repeat("█", barLength)
			}
		}
		
		// Determine what type of hour this is
		emoji := ""
		
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
				emoji = " 💤"
				break
			}
		}
		
		// Check for peak time first (highest priority)
		if emoji == "" && result.PeakProductivity.Count > 0 {
			peakStart := result.PeakProductivity.Start
			peakEnd := result.PeakProductivity.End
			localHourFloat := float64(localHour)
			
			// Check if the current hour overlaps with peak time
			hourStart := localHourFloat
			hourEnd := localHourFloat + 1.0
			
			// Check for overlap between [hourStart, hourEnd) and [peakStart, peakEnd)
			if hourEnd > peakStart && hourStart < peakEnd {
				emoji = " 🔥"
			}
		}
		
		// Check if it's within work hours (but not lunch or peak)
		if emoji == "" && (result.ActiveHoursLocal.Start != 0 || result.ActiveHoursLocal.End != 0) {
			localHourFloat := float64(localHour)
			
			// Check for lunch hour with proper half-hour handling
			if result.LunchHoursLocal.Start != 0 || result.LunchHoursLocal.End != 0 {
				lunchStartHour := result.LunchHoursLocal.Start
				lunchEndHour := result.LunchHoursLocal.End
				
				// Check if the current hour block overlaps with lunch time
				hourStart := localHourFloat
				hourEnd := localHourFloat + 1.0
				
				// Check for overlap between [hourStart, hourEnd) and [lunchStart, lunchEnd)
				if hourEnd > lunchStartHour && hourStart < lunchEndHour {
					emoji = " 🍽️"
				}
			}
			
			// If not lunch, check if it's work time
			if emoji == "" {
				workStart := result.ActiveHoursLocal.Start
				workEnd := result.ActiveHoursLocal.End
				
				if workStart <= workEnd {
					if localHourFloat >= workStart && localHourFloat < workEnd {
						// Don't add emoji for work hours - it's clear from the graph
					}
				} else {
					// Wrap around midnight
					if localHourFloat >= workStart || localHourFloat < workEnd {
						// Don't add emoji for work hours - it's clear from the graph
					}
				}
			}
		}
		
		// Format with consistent spacing - emoji goes at the end
		output.WriteString(fmt.Sprintf("%02d:00  ", localHour))
		output.WriteString(fmt.Sprintf("%-28s", bar))
		if count > 0 {
			output.WriteString(fmt.Sprintf(" %3d", count))
		} else {
			output.WriteString("    ")
		}
		output.WriteString(emoji)
		output.WriteString("\n")
	}
	
	return output.String()
}

func formatHour(decimalHour float64) string {
	hour := int(decimalHour)
	minutes := int((decimalHour - float64(hour)) * 60)
	return fmt.Sprintf("%02d:%02d", hour, minutes)
}