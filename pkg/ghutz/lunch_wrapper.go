package ghutz

import (
	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// GlobalLunchPattern represents a detected lunch pattern in UTC time
type GlobalLunchPattern struct {
	startUTC    float64
	endUTC      float64
	confidence  float64
	dropPercent float64
}

// DetectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity  
func DetectLunchBreakNoonCentered(halfHourCounts map[float64]int, utcOffset int) (lunchStart, lunchEnd, confidence float64) {
	return lunch.DetectLunchBreakNoonCentered(halfHourCounts, utcOffset)
}

// detectLunchBreakNoonCentered looks for lunch breaks in the 10am-2:30pm window
// SIMPLIFIED VERSION - just find ANY drop in activity
func detectLunchBreakNoonCentered(halfHourCounts map[float64]int, utcOffset int) (lunchStart, lunchEnd, confidence float64) {
	return lunch.DetectLunchBreakNoonCentered(halfHourCounts, utcOffset)
}

// findBestGlobalLunchPattern finds the best lunch pattern globally in UTC time
// This is timezone-independent and looks for the strongest activity drop + recovery pattern
func findBestGlobalLunchPattern(halfHourCounts map[float64]int) GlobalLunchPattern {
	pattern := lunch.FindBestGlobalLunchPattern(halfHourCounts)
	return GlobalLunchPattern{
		startUTC:    pattern.StartUTC,
		endUTC:      pattern.EndUTC,
		confidence:  pattern.Confidence,
		dropPercent: pattern.DropPercent,
	}
}