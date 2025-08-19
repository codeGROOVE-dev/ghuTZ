package timezone

// DetectPeakProductivityWithHalfHours identifies the single 30-minute bucket with highest activity.
func DetectPeakProductivityWithHalfHours(halfHourCounts map[float64]int, _ int) (start, end float64, count int) {
	if len(halfHourCounts) == 0 {
		return -1, -1, 0
	}

	// Find the bucket with maximum activity
	// Use deterministic ordering to handle ties consistently
	var maxBucket float64
	maxCount := 0
	hasMax := false

	// Sort buckets to ensure deterministic iteration
	buckets := make([]float64, 0, len(halfHourCounts))
	for bucket := range halfHourCounts {
		buckets = append(buckets, bucket)
	}
	
	// Sort buckets numerically for deterministic ordering
	for i := 0; i < len(buckets); i++ {
		for j := i + 1; j < len(buckets); j++ {
			if buckets[i] > buckets[j] {
				buckets[i], buckets[j] = buckets[j], buckets[i]
			}
		}
	}

	// Find max count with deterministic tie-breaking (earliest time wins)
	for _, bucket := range buckets {
		activityCount := halfHourCounts[bucket]
		if activityCount > maxCount {
			maxCount = activityCount
			maxBucket = bucket
			hasMax = true
		}
	}

	if !hasMax || maxCount == 0 {
		return -1, -1, 0
	}

	// Return the single peak bucket
	// Start is the bucket time, end is 30 minutes later
	return maxBucket, maxBucket + 0.5, maxCount
}
