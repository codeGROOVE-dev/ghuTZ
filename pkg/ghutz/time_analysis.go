package ghutz

// calculateTypicalActiveHours determines typical work hours based on activity patterns.
func calculateTypicalActiveHours(hourCounts map[int]int, quietHours []int, utcOffset int) (start, end int) {
	// Find the first hour with significant activity after quiet hours
	maxQuiet := -1
	for _, h := range quietHours {
		if h > maxQuiet {
			maxQuiet = h
		}
	}

	// Look for work start after quiet hours end
	workStart := -1
	for h := maxQuiet + 1; h < maxQuiet+12; h++ {
		hourUTC := h % 24
		localHour := (hourUTC + utcOffset + 24) % 24
		count := hourCounts[hourUTC]

		// Work typically starts between 6 AM and 11 AM local time
		if localHour >= 6 && localHour <= 11 && count > 0 {
			// Check if this is sustained activity (not just a blip)
			nextHour := (hourUTC + 1) % 24
			nextNextHour := (hourUTC + 2) % 24
			if hourCounts[nextHour] > 0 || hourCounts[nextNextHour] > 0 {
				workStart = hourUTC
				break
			}
		}
	}

	// If no clear work start found, look for any morning activity
	if workStart == -1 {
		for h := range 24 {
			localHour := (h + utcOffset + 24) % 24
			if localHour >= 6 && localHour <= 11 && hourCounts[h] > 0 {
				workStart = h
				break
			}
		}
	}

	// Default to 9 AM local if still not found
	if workStart == -1 {
		workStart = (9 - utcOffset + 24) % 24
	}

	// Find work end by looking for activity drop in evening
	workEnd := -1
	for h := workStart + 6; h < workStart+14; h++ {
		hourUTC := h % 24
		localHour := (hourUTC + utcOffset + 24) % 24
		count := hourCounts[hourUTC]
		nextHour := (hourUTC + 1) % 24
		nextCount := hourCounts[nextHour]

		// Look for significant drop in activity in evening hours (4 PM - 8 PM local)
		if localHour >= 16 && localHour <= 20 {
			if count > 0 && nextCount < count/2 {
				workEnd = hourUTC
				break
			}
		}
	}

	// Default to 6 PM local if not found
	if workEnd == -1 {
		workEnd = (18 - utcOffset + 24) % 24
	}

	// Ensure work hours are reasonable (8-12 hours)
	duration := (workEnd - workStart + 24) % 24
	if duration < 8 {
		workEnd = (workStart + 9) % 24
	} else if duration > 12 {
		workEnd = (workStart + 10) % 24
	}

	return workStart, workEnd
}

// findSleepHours identifies likely sleep hours based on activity patterns.

// detectLunchBreak identifies potential lunch break patterns in work hours.
