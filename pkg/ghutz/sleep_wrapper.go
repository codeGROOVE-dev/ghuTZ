package ghutz

import (
	"github.com/codeGROOVE-dev/ghuTZ/pkg/sleep"
)

// detectSleepPeriodsWithHalfHours identifies sleep periods using 30-minute resolution data.
// It requires at least 30 minutes of buffer between activity and sleep.
func detectSleepPeriodsWithHalfHours(halfHourCounts map[float64]int) []float64 {
	return sleep.DetectSleepPeriodsWithHalfHours(halfHourCounts)
}

// findSleepHours identifies sleep hours from hourly activity data
func findSleepHours(hourCounts map[int]int) []int {
	return sleep.FindSleepHours(hourCounts)
}

// findQuietHours identifies hours with minimal activity.
func findQuietHours(hourCounts map[int]int) []int {
	return sleep.FindQuietHours(hourCounts)
}