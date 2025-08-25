// Package constants defines shared constants for the gutz application.
package constants

// TargetDataPoints is the desired number of unique data points for timezone analysis.
// This ensures we have enough activity data for reliable timezone detection.
// Data points are deduplicated by timestamp to avoid counting the same event multiple times.
const TargetDataPoints = 160
