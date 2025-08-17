package ghutz

// detectPeakProductivityWithHalfHours identifies the single 30-minute bucket with highest activity.
func detectPeakProductivityWithHalfHours(halfHourCounts map[float64]int, utcOffset int) (start, end float64, count int) {
	if len(halfHourCounts) == 0 {
		return -1, -1, 0
	}

	// Find the bucket with maximum activity
	var maxBucket float64
	maxCount := 0
	
	for bucket, activityCount := range halfHourCounts {
		if activityCount > maxCount {
			maxCount = activityCount
			maxBucket = bucket
		}
	}

	if maxCount == 0 {
		return -1, -1, 0
	}

	// Return the single peak bucket
	// Start is the bucket time, end is 30 minutes later
	return maxBucket, maxBucket + 0.5, maxCount
}