// Package timezone provides timezone candidate evaluation and analysis.
package timezone

import (
	"fmt"
	"math"

	"github.com/codeGROOVE-dev/ghuTZ/pkg/lunch"
)

// GlobalLunchPattern represents the best lunch pattern found globally in UTC.
type GlobalLunchPattern struct {
	StartUTC    float64
	EndUTC      float64
	Confidence  float64
	DropPercent float64
}

// EvaluateCandidates evaluates multiple timezone offsets to find the best candidates.
func EvaluateCandidates(username string, hourCounts map[int]int, halfHourCounts map[float64]int, totalActivity int, quietHours []int, midQuiet float64, activeStart int, bestGlobalLunch GlobalLunchPattern) []Candidate {
	var candidates []Candidate // Store timezone candidates for Gemini

	// Evaluate multiple timezone offsets to find the best candidates
	// CRITICAL: Always test both American AND European timezones
	// Always test ALL possible UTC offsets from -12 to +14
	// This ensures --force-offset works for any valid timezone
	minOffset := -12
	maxOffset := 14

	for testOffset := minOffset; testOffset <= maxOffset; testOffset++ {
		// Calculate metrics for this offset
		// 1. Lunch timing analysis
		testLunchStart, testLunchEnd, testLunchConf := lunch.DetectLunchBreakNoonCentered(halfHourCounts, testOffset)

		lunchLocalStart := math.Mod(testLunchStart+float64(testOffset)+24, 24)

		// Work start time (needed to validate lunch)
		testWorkStart := (activeStart + testOffset + 24) % 24

		// Find first SIGNIFICANT activity in this timezone (more accurate than activeStart which uses initial offset)
		// Look for sustained activity (>5 events) not just any blip
		firstActivityLocal := 24.0
		for utcHour := range 24 {
			if hourCounts[utcHour] > 5 { // Changed from > 0 to > 5 for significant activity
				localHour := float64((utcHour + testOffset + 24) % 24)
				if localHour < firstActivityLocal {
					firstActivityLocal = localHour
				}
			}
		}

		// Lunch is only reasonable if:
		// 1. Detected in the 10am-2:30pm window
		// 2. At least 1 hour after first activity (can't have lunch right after arriving)
		lunchReasonable := testLunchStart >= 0 &&
			lunchLocalStart >= 10.0 &&
			lunchLocalStart <= 14.5 &&
			testLunchConf >= 0.3 &&
			lunchLocalStart >= firstActivityLocal+1.0 // At least 1 hour after first activity

		// Calculate lunch dip strength
		lunchDipStrength := 0.0
		if testLunchConf > 0 && testLunchStart >= 0 {
			// Get activity before and during lunch to calculate dip percentage
			beforeLunchBucket := testLunchStart - 1.0
			if beforeLunchBucket < 0 {
				beforeLunchBucket += 24
			}
			beforeActivity := float64(halfHourCounts[beforeLunchBucket])
			lunchActivity := float64(halfHourCounts[testLunchStart])
			if beforeActivity > 0 {
				lunchDipStrength = (beforeActivity - lunchActivity) / beforeActivity
			}
		}

		// 2. Sleep timing analysis
		// Calculate what local sleep time would be for this offset
		sleepLocalMid := math.Mod(midQuiet+float64(testOffset)+24, 24)
		// Sleep is reasonable if mid-sleep is between 10pm and 5am
		sleepReasonable := (sleepLocalMid >= 0 && sleepLocalMid <= 5) || sleepLocalMid >= 22

		// 3. Work hours analysis
		// testWorkStart already calculated above for lunch validation
		// Check if work hours are reasonable based on ACTUAL first activity, not initial guess
		workReasonable := firstActivityLocal >= 6 && firstActivityLocal <= 10 // Allow 6am starts for early risers

		// 4. Evening activity (7-11pm local)
		// To convert local hour to UTC: UTC = local - offset
		// For UTC-5: 7pm local = 19:00 local = 19 - (-5) = 24 = 0 UTC
		eveningActivity := 0
		for localHour := 19; localHour <= 23; localHour++ {
			utcHour := (localHour - testOffset + 24) % 24
			eveningActivity += hourCounts[utcHour]
		}

		// 5. European timezone validation - Europeans should have morning activity by 10am latest
		europeanMorningActivityCheck := true
		if testOffset >= 0 && testOffset <= 3 { // European timezones (UTC+0 to UTC+3)
			// Check for activity between 8am-10am local time
			morningActivity := 0
			for localHour := 8; localHour <= 10; localHour++ {
				utcHour := (localHour - testOffset + 24) % 24
				morningActivity += hourCounts[utcHour]
			}
			// If no morning activity, this is likely not a real European timezone
			if morningActivity == 0 {
				europeanMorningActivityCheck = false
			}
		}

		// Calculate overall confidence score on 0-100 scale
		// Prioritize lunch and sleep times over evening activity
		testConfidence := 0.0
		adjustments := []string{} // Track all adjustments for debugging

		// Sleep timing (most reliable) - 15 points max (increased weight)
		// Early sleep (9-10pm) is a strong Pacific Time indicator
		if sleepReasonable {
			// Check for early sleep pattern (strong Pacific indicator)
			// Find the first quiet hour to determine sleep start
			sleepStartUTC := -1
			if len(quietHours) > 0 {
				sleepStartUTC = quietHours[0]
			}

			switch {
			case sleepLocalMid >= 1 && sleepLocalMid <= 4:
				testConfidence += 12 // Perfect sleep timing (1-4am)
				adjustments = append(adjustments, fmt.Sprintf("+12 (perfect sleep 1-4am, mid=%.1f)", sleepLocalMid))
				// Bonus for early sleep (9-11pm start) - Pacific pattern
				if sleepStartUTC >= 0 {
					sleepStartLocal := float64((sleepStartUTC + testOffset + 24) % 24)
					if sleepStartLocal >= 21 && sleepStartLocal <= 23 {
						testConfidence += 3 // Early sleep bonus (Pacific indicator)
						adjustments = append(adjustments, fmt.Sprintf("+3 (early sleep bonus, start=%.0fpm)", sleepStartLocal-12))
					}
				}
			case sleepLocalMid >= 0 && sleepLocalMid <= 5:
				testConfidence += 8 // Good sleep timing
				adjustments = append(adjustments, fmt.Sprintf("+8 (good sleep, mid=%.1f)", sleepLocalMid))
				// Still give bonus for early sleep
				if sleepStartUTC >= 0 {
					sleepStartLocal := float64((sleepStartUTC + testOffset + 24) % 24)
					if sleepStartLocal >= 21 && sleepStartLocal <= 23 {
						testConfidence += 2 // Early sleep bonus
						adjustments = append(adjustments, fmt.Sprintf("+2 (early sleep, start=%.0fpm)", sleepStartLocal-12))
					}
				}
			case sleepLocalMid >= 22 || sleepLocalMid <= 6:
				testConfidence += 4 // Sleep detected but unusual timing
				adjustments = append(adjustments, fmt.Sprintf("+4 (unusual sleep, mid=%.1f)", sleepLocalMid))
			default:
				// Sleep pattern exists but timing is very unusual
				testConfidence++
				adjustments = append(adjustments, fmt.Sprintf("+1 (unusual sleep timing, mid=%.1f)", sleepLocalMid))
			}
		} else {
			adjustments = append(adjustments, "0 (no reasonable sleep pattern)")
		}

		// STEP 2: Distance-from-noon lunch bonus system
		var noonDistanceBonus float64
		if bestGlobalLunch.Confidence > 0 {
			// Calculate what local time the global lunch would be for this timezone
			globalLunchLocalTime := math.Mod(bestGlobalLunch.StartUTC+float64(testOffset)+24, 24)

			// Calculate distance from noon (12:00) in 30-minute buckets
			distanceFromNoon := math.Abs(globalLunchLocalTime - 12.0)

			// Convert distance to number of 30-minute buckets
			bucketsFromNoon := distanceFromNoon / 0.5

			// Moderate bonus based on proximity to noon
			// Perfect noon (0 buckets away) = 10 points
			// 1 bucket away (30 min) = 6 points
			// 2 buckets away (1 hour) = 3 points
			// 3 buckets away (1.5 hours) = 1 point
			// 4+ buckets away (2+ hours) = minimal points
			switch {
			case bucketsFromNoon == 0:
				noonDistanceBonus = 10 * bestGlobalLunch.Confidence // Perfect noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (perfect noon lunch)", noonDistanceBonus))
			case bucketsFromNoon <= 1:
				noonDistanceBonus = 6 * bestGlobalLunch.Confidence // Within 30 min of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 30min of noon)", noonDistanceBonus))
			case bucketsFromNoon <= 2:
				noonDistanceBonus = 3 * bestGlobalLunch.Confidence // Within 1 hour of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 1hr of noon)", noonDistanceBonus))
			case bucketsFromNoon <= 3:
				noonDistanceBonus = 1 * bestGlobalLunch.Confidence // Within 1.5 hours of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 1.5hr of noon)", noonDistanceBonus))
			case bucketsFromNoon <= 4:
				noonDistanceBonus = 0.5 * bestGlobalLunch.Confidence // Within 2 hours of noon
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch within 2hr of noon)", noonDistanceBonus))
			default:
				noonDistanceBonus = 0.2 * bestGlobalLunch.Confidence // Too far from reasonable lunch time
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch >2hr from noon)", noonDistanceBonus))
			}

			testConfidence += noonDistanceBonus
		}

		// Lunch timing - 15 points max (strong signal when clear)
		if lunchReasonable {
			var lunchScore float64
			// CRITICAL: Noon (12:00) is the most common lunch time globally
			switch {
			case lunchLocalStart >= 11.75 && lunchLocalStart <= 12.25:
				// Within 15 minutes of noon - STRONGEST signal
				lunchScore = 15 // Perfect noon timing gets maximum bonus
				adjustments = append(adjustments, fmt.Sprintf("+15 (perfect noon lunch, at %.1f)", lunchLocalStart))
			case lunchLocalStart >= 11.5 && lunchLocalStart <= 13.5:
				// 11:30am to 1:30pm are good lunch times
				lunchScore = 10 // Good lunch timing
				adjustments = append(adjustments, fmt.Sprintf("+10 (good lunch 11:30am-1:30pm, at %.1f)", lunchLocalStart))
			case lunchLocalStart >= 11.0 && lunchLocalStart <= 14.0:
				lunchScore = 8 // Acceptable lunch timing (11am-2pm)
				adjustments = append(adjustments, fmt.Sprintf("+8 (acceptable lunch 11am-2pm, at %.1f)", lunchLocalStart))
			case lunchLocalStart >= 10.5 && lunchLocalStart <= 14.5:
				lunchScore = 6 // Acceptable lunch timing
				adjustments = append(adjustments, fmt.Sprintf("+6 (acceptable lunch 10:30am-2:30pm, at %.1f)", lunchLocalStart))
			default:
				lunchScore = 2 // Lunch detected but unusual timing
				adjustments = append(adjustments, fmt.Sprintf("+2 (unusual lunch timing at %.1f)", lunchLocalStart))
			}
			// Boost score based on dip strength (up to 5 bonus points for strong drops)
			// An 85%+ drop like Kevin's noon lunch is an EXTREMELY strong signal
			dipBonus := 0.0
			switch {
			case lunchDipStrength >= 0.8:
				dipBonus = 5.0 // Massive bonus for 80%+ drops
			case lunchDipStrength >= 0.6:
				dipBonus = 3.0 // Good bonus for 60%+ drops
			case lunchDipStrength >= 0.4:
				dipBonus = 1.5 // Small bonus for 40%+ drops
			default:
				// No bonus for dip strength below 40%
			}
			if dipBonus > 0 {
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (lunch dip strength %.1f%%)", dipBonus, lunchDipStrength*100))
			}
			lunchScore += dipBonus

			// PENALTY for weak lunch signals after 1:30pm
			// A 25% drop at 2pm is likely not a real lunch
			if lunchLocalStart > 13.5 && lunchDipStrength < 0.4 {
				oldScore := lunchScore
				lunchScore *= 0.3 // Severely reduce score for weak late lunches
				adjustments = append(adjustments, fmt.Sprintf("-%.1f (weak late lunch penalty)", oldScore-lunchScore))
			}

			finalLunchScore := math.Min(15, lunchScore)
			testConfidence += finalLunchScore
		} else if testLunchStart < 0 {
			// No detectable lunch break - apply penalty
			// This might indicate invalid timezone or unusual work pattern
			testConfidence -= 5
			adjustments = append(adjustments, "-5 (no lunch detected)")
		}

		// Work hours - 8 points max, with penalties for too-early starts
		// CRITICAL: Use firstActivityLocal which is calculated per-timezone, not testWorkStart
		if workReasonable && firstActivityLocal < 24 {
			actualWorkStart := int(firstActivityLocal)
			switch {
			case actualWorkStart >= 7 && actualWorkStart <= 9:
				testConfidence += 8 // Good work start (7-9am)
				adjustments = append(adjustments, fmt.Sprintf("+8 (good work start %dam)", actualWorkStart))
			case actualWorkStart == 6:
				testConfidence += 4 // 6am is early but some people do it
				adjustments = append(adjustments, "+4 (early 6am work start)")
			case actualWorkStart >= 5 && actualWorkStart <= 10:
				testConfidence += 2 // Acceptable but unusual
				adjustments = append(adjustments, fmt.Sprintf("+2 (unusual work start %dam)", actualWorkStart))
			default:
				testConfidence++ // Work hours detected but very unusual
				adjustments = append(adjustments, fmt.Sprintf("+1 (very unusual work start %dam)", actualWorkStart))
			}
		} else if firstActivityLocal < 24 {
			// Apply STRONG penalty for unreasonable work hours
			actualWorkStart := int(firstActivityLocal)
			if actualWorkStart >= 0 && actualWorkStart < 5 {
				// Starting work before 5am is absurd - massive penalty
				testConfidence -= 30 // HUGE penalty for pre-5am starts
				adjustments = append(adjustments, fmt.Sprintf("-30 (absurd pre-5am start %dam)", actualWorkStart))
				if actualWorkStart < 2 {
					// Midnight to 2am? Even more absurd
					testConfidence -= 20 // Additional massive penalty
					adjustments = append(adjustments, fmt.Sprintf("-20 (extra penalty for midnight-%dam start)", actualWorkStart))
				}
			} else if actualWorkStart == 5 {
				// 5am is very early but some people do it - moderate penalty
				testConfidence -= 8
				adjustments = append(adjustments, "-8 (very early 5am start)")
			}
		}

		// Evening activity - up to 1 point (very weak signal, many users only use GitHub for work)
		// NOT ALL GITHUB USERS CODE IN THE EVENING
		// What looks like "evening" in one timezone might be afternoon work in another
		// Example: 19:00-23:00 UTC is 7-11pm for UTC+1 but 3-7pm for UTC-4 (normal work hours)
		if eveningActivity > 0 {
			eveningRatio := float64(eveningActivity) / float64(totalActivity)

			// Only give points if evening activity is VERY SUBSTANTIAL (>30% of total)
			// This helps avoid misinterpreting afternoon work as evening coding
			if eveningRatio > 0.3 {
				// Ultra conservative scoring - max 1 point, only if >30% evening
				eveningPoints := 1 * math.Min(1.0, (eveningRatio-0.3)*3.33)
				testConfidence += eveningPoints
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (evening activity %.1f%%)", eveningPoints, eveningRatio*100))
			}
			// No points for <30% evening activity - likely just late afternoon work

			// PENALTY if low evening activity but interpreted as high due to timezone
			// If <10% evening activity, this timezone might be wrong
			if eveningRatio < 0.1 && testOffset >= -5 && testOffset <= -3 {
				// Eastern/Atlantic timezones with low evening = probably wrong
				testConfidence -= 2
				adjustments = append(adjustments, fmt.Sprintf("-2 (low evening %.1f%% for Eastern)", eveningRatio*100))
			}
		}

		// Work hours activity bonus - 2 points max
		// Check activity during expected work hours (9am-5pm local) for this timezone
		workHoursActivity := 0
		for localHour := 9; localHour <= 17; localHour++ {
			utcHour := (localHour - testOffset + 24) % 24
			workHoursActivity += hourCounts[utcHour]
		}
		if workHoursActivity > 0 && totalActivity > 0 {
			workRatio := float64(workHoursActivity) / float64(totalActivity)
			// Bonus for having high work hours activity (up to 2 points)
			workHoursBonus := 2 * math.Min(1.0, workRatio*1.5)
			testConfidence += workHoursBonus
			if workHoursBonus > 0 {
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (work hours activity %.1f%%)", workHoursBonus, workRatio*100))
			}
		}

		// Peak activity timing bonus - 5 points max
		// Find the hour with maximum activity and check if it occurs during ideal work hours (10am-4pm local)
		maxActivity := 0
		maxActivityHour := -1
		for hour, count := range hourCounts {
			if count > maxActivity {
				maxActivity = count
				maxActivityHour = hour
			}
		}
		if maxActivityHour >= 0 {
			// Convert peak activity UTC hour to local time for this timezone
			peakLocalHour := (maxActivityHour + testOffset + 24) % 24
			// Bonus for peak activity during ideal work hours (10am-4pm = 10-16 local)
			if peakLocalHour >= 10 && peakLocalHour <= 16 {
				// Perfect timing: 5 points for peak during 1-3pm, 3 points for 12-2pm or 3-4pm
				var peakBonus float64
				switch {
				case peakLocalHour >= 13 && peakLocalHour <= 15:
					// 1-3pm is ideal afternoon work time
					peakBonus = 5.0
					adjustments = append(adjustments, fmt.Sprintf("+5 (peak at %dpm ideal)", peakLocalHour-12))
				case peakLocalHour >= 12 && peakLocalHour <= 16:
					// 12-4pm is good work time
					peakBonus = 3.0
					adjustments = append(adjustments, fmt.Sprintf("+3 (peak at %d good work time)", peakLocalHour))
				default:
					// 10-12pm is morning work time
					peakBonus = 2.0
					adjustments = append(adjustments, fmt.Sprintf("+2 (peak at %dam morning)", peakLocalHour))
				}
				testConfidence += peakBonus
			}

			// REDUCED PENALTY for late peak activity (5-7pm)
			// Some people do their best work in the evening, especially remote workers
			// Only penalize peaks after 7pm as those are truly unusual
			if peakLocalHour >= 19 {
				testConfidence -= 10 // Strong penalty for very late peak (after 7pm)
				adjustments = append(adjustments, fmt.Sprintf("-10 (very late peak at %dpm)", peakLocalHour-12))
			} else if peakLocalHour >= 17 {
				testConfidence -= 3 // Small penalty for 5-7pm peak (could be valid)
				adjustments = append(adjustments, fmt.Sprintf("-3 (evening peak at %dpm)", peakLocalHour-12))
			}

			// PENALTY for very late work start (after 2pm)
			// Nobody regularly starts work at 2:30pm regardless of timezone
			if firstActivityLocal >= 14 {
				testConfidence -= 20 // Massive penalty for starting after 2pm
				adjustments = append(adjustments, fmt.Sprintf("-20 (work starts after 2pm at %.1f)", firstActivityLocal))
			} else if firstActivityLocal >= 12 {
				testConfidence -= 10 // Moderate penalty for starting after noon
				adjustments = append(adjustments, fmt.Sprintf("-10 (work starts after noon at %.1f)", firstActivityLocal))
			}
		}

		// Pacific timezone pattern recognition - 25 points max (increased)
		// Check for classic Pacific timezone indicators
		if testOffset == -8 || testOffset == -7 { // PST or PDT
			// Strong Pacific indicators
			pacificBonus := 0.0

			// astrojerms pattern: Peak at 18-19 UTC (10-11am Pacific)
			morning18 := hourCounts[18] // 10am Pacific
			morning19 := hourCounts[19] // 11am Pacific
			if morning18 > 30 || morning19 > 15 {
				pacificBonus += 10.0 // Strong morning peak (astrojerms has 43 at 18:00)
				adjustments = append(adjustments, fmt.Sprintf("+10 (Pacific strong morning peak %d/%d)", morning18, morning19))
			} else if morning18 > 20 || morning19 > 10 {
				pacificBonus += 3.0 // Reduced: Moderate morning activity (was 6)
				adjustments = append(adjustments, fmt.Sprintf("+3 (Pacific moderate morning %d/%d)", morning18, morning19))
			}

			// Lunch dip at 20 UTC (noon Pacific) - astrojerms pattern
			lunch20 := hourCounts[20]     // 12pm Pacific
			beforeLunch := hourCounts[19] // 11am Pacific
			if beforeLunch > 0 && lunch20 > 0 && float64(lunch20) < float64(beforeLunch)*0.8 {
				pacificBonus += 5.0 // Clear lunch dip at noon
				adjustments = append(adjustments, fmt.Sprintf("+5 (Pacific noon lunch dip %d->%d)", beforeLunch, lunch20))
			}

			// Work starts early: 14-16 UTC (6-8am Pacific)
			// BUT ONLY if actual work start is reasonable (not midnight!)
			early14 := hourCounts[14] // 6am Pacific
			early15 := hourCounts[15] // 7am Pacific
			early16 := hourCounts[16] // 8am Pacific
			if (early14 > 0 || early15 > 5 || early16 > 10) && firstActivityLocal >= 5 && firstActivityLocal <= 8 {
				pacificBonus += 2.0 // Reduced: Early morning start (was 5)
				adjustments = append(adjustments, fmt.Sprintf("+2 (Pacific early start %d/%d/%d)", early14, early15, early16))
			}

			// Low late evening activity: 0-4 UTC (4-8pm Pacific)
			late0 := hourCounts[0] // 4pm Pacific
			late1 := hourCounts[1] // 5pm Pacific
			late2 := hourCounts[2] // 6pm Pacific
			late3 := hourCounts[3] // 7pm Pacific
			late4 := hourCounts[4] // 8pm Pacific
			lateTotal := late0 + late1 + late2 + late3 + late4
			if lateTotal > 0 && lateTotal < 50 {
				pacificBonus += 3.0 // Moderate late afternoon/evening activity
				adjustments = append(adjustments, fmt.Sprintf("+3 (Pacific moderate evening %d total)", lateTotal))
			}

			// Early sleep pattern: quiet at 5-9 UTC (9pm-1am Pacific)
			if hourCounts[5] == 0 && hourCounts[6] == 0 && hourCounts[7] == 0 {
				pacificBonus += 2.0 // Clear early sleep pattern
				adjustments = append(adjustments, "+2 (Pacific early sleep 9pm-1am)")
			}

			if pacificBonus > 0 {
				testConfidence += pacificBonus
			}
		}

		// Mountain timezone pattern recognition - 6 points max
		// Check for classic Mountain timezone indicators (UTC-6/UTC-7)
		if testOffset == -6 || testOffset == -7 {
			mountainBonus := 0.0

			// Strong 1:30pm lunch pattern like IdlePhysicist
			// For UTC-6: 13:30 local = 19:30 UTC
			// For UTC-7: 13:30 local = 20:30 UTC
			lunchBucket := 19.5
			if testOffset == -7 {
				lunchBucket = 20.5
			}
			beforeLunchBucket := lunchBucket - 0.5

			// Check for significant drop at 1:30pm (like IdlePhysicist's 58% drop)
			// But also check that it's actually the strongest lunch signal
			if beforeCount, exists := halfHourCounts[beforeLunchBucket]; exists && beforeCount > 10 {
				if lunchCount, exists := halfHourCounts[lunchBucket]; exists {
					dropRatio := float64(beforeCount-lunchCount) / float64(beforeCount)
					// Only apply bonus if this is a strong drop AND lunch was detected here
					if dropRatio > 0.5 && lunchLocalStart >= 13.0 && lunchLocalStart <= 14.0 {
						// 50%+ drop at 1:30pm AND lunch was actually detected in that range
						mountainBonus += 6.0 // Mountain lunch pattern (reduced from 8)
						adjustments = append(adjustments, fmt.Sprintf("+6 (Mountain 1:30pm lunch %.1f%% drop)", dropRatio*100))
					}
				}
			}

			// Check for caving/outdoor activity patterns (weekend mornings)
			// Mountain timezone folks often have outdoor hobbies
			// This would show as activity patterns different on weekends
			// but we don't have day-of-week data yet

			if mountainBonus > 0 {
				testConfidence += mountainBonus
			}
		}

		// Eastern timezone pattern recognition - 8 points max
		// Check for classic Eastern timezone indicators (UTC-4/UTC-5)
		if testOffset == -4 || testOffset == -5 {
			easternBonus := 0.0

			// Early lunch pattern (11:30am-12:00pm) common in East Coast
			// For UTC-4: 11:30 local = 15:30 UTC
			// For UTC-5: 11:30 local = 16:30 UTC
			earlyLunchBucket := 15.5
			if testOffset == -5 {
				earlyLunchBucket = 16.5
			}
			beforeEarlyLunchBucket := earlyLunchBucket - 0.5

			// Check for significant drop at 11:30am (tstromberg pattern)
			if beforeCount, exists := halfHourCounts[beforeEarlyLunchBucket]; exists && beforeCount > 20 {
				if lunchCount, exists := halfHourCounts[earlyLunchBucket]; exists {
					dropRatio := float64(beforeCount-lunchCount) / float64(beforeCount)
					// Strong early lunch signal
					if dropRatio > 0.7 && lunchLocalStart >= 11.0 && lunchLocalStart <= 12.0 {
						// 70%+ drop at 11:30am AND lunch detected in that range
						easternBonus += 8.0 // Strong Eastern early lunch pattern
						adjustments = append(adjustments, fmt.Sprintf("+8 (Eastern 11:30am lunch %.1f%% drop)", dropRatio*100))
					} else if dropRatio > 0.5 && lunchLocalStart >= 11.0 && lunchLocalStart <= 12.5 {
						// 50%+ drop around 11:30am-12:30pm
						easternBonus += 5.0 // Moderate Eastern lunch pattern
						adjustments = append(adjustments, fmt.Sprintf("+5 (Eastern lunch %.1f%% drop)", dropRatio*100))
					}
				}
			}

			// High morning productivity (9-11am) typical of East Coast
			morningActivity := 0
			for localHour := 9.0; localHour <= 11.0; localHour += 0.5 {
				utcHour := localHour - float64(testOffset)
				for utcHour < 0 {
					utcHour += 24
				}
				for utcHour >= 24 {
					utcHour -= 24
				}
				morningActivity += halfHourCounts[utcHour]
			}
			// If strong morning activity, add small bonus
			if morningActivity > 50 {
				easternBonus += 2.0
				adjustments = append(adjustments, fmt.Sprintf("+2 (Eastern high morning activity %d)", morningActivity))
			}

			// CRITICAL Eastern pattern: 5pm end-of-day peak (very common)
			// For UTC-4: 17:00 local = 21:00 UTC
			// For UTC-5: 17:00 local = 22:00 UTC
			endOfDayUTC := 21
			if testOffset == -5 {
				endOfDayUTC = 22
			}

			// 5pm activity is NOT a good signal - many people are commuting
			// Only give a small bonus if there's VERY high activity suggesting they work on the train/from home
			if hourCounts[endOfDayUTC] >= 30 {
				// Unusually high 5pm activity might indicate remote work or train commute coding
				easternBonus += 2.0
				adjustments = append(adjustments, fmt.Sprintf("+2 (High 5pm activity %d - possible remote work)", hourCounts[endOfDayUTC]))
			}

			// Check for 12pm lunch dip pattern (even if weak)
			// For UTC-4: 12:00 local = 16:00 UTC
			// For UTC-5: 12:00 local = 17:00 UTC
			noonUTC := 16
			if testOffset == -5 {
				noonUTC = 17
			}
			beforeNoon := noonUTC - 1
			afterNoon := noonUTC + 1

			// Even a small dip at noon is meaningful for Eastern
			if hourCounts[beforeNoon] > hourCounts[noonUTC] && hourCounts[afterNoon] > hourCounts[noonUTC] {
				dropPercent := float64(hourCounts[beforeNoon]-hourCounts[noonUTC]) / float64(hourCounts[beforeNoon]) * 100
				easternBonus += 3.0
				adjustments = append(adjustments, fmt.Sprintf("+3 (Eastern noon dip pattern %.1f%%)", dropPercent))
			}

			if easternBonus > 0 {
				testConfidence += easternBonus
			}
		}

		// South American timezone pattern recognition (UTC-3)
		// Brazil, Argentina, Uruguay, etc. have distinct patterns
		if testOffset == -3 {
			southAmericaBonus := 0.0

			// UTC-3 typically has lunch at 12:00-1:00pm (not early like US)
			// Check for noon lunch pattern
			if lunchLocalStart >= 11.5 && lunchLocalStart <= 13.0 && testLunchConf > 0.5 {
				southAmericaBonus += 6.0
				adjustments = append(adjustments, fmt.Sprintf("+6 (South America noon lunch at %.1f)", lunchLocalStart))
			}

			// CRITICAL: 6pm end-of-day peak is common in South America
			// For UTC-3: 18:00 local = 21:00 UTC
			endOfDayUTC := 21

			// Check if 6pm has significant activity
			if hourCounts[endOfDayUTC] >= 20 {
				// Strong 6pm activity is typical for Brazil/Argentina
				southAmericaBonus += 8.0
				adjustments = append(adjustments, fmt.Sprintf("+8 (South America 6pm peak with %d events)", hourCounts[endOfDayUTC]))

				// If 21:00 UTC is a peak hour, that's very strong for UTC-3
				isPeak := true
				for h := range 24 {
					if h != endOfDayUTC && hourCounts[h] > hourCounts[endOfDayUTC] {
						isPeak = false
						break
					}
				}
				if isPeak {
					southAmericaBonus += 4.0
					adjustments = append(adjustments, "+4 (6pm is peak hour - typical South America)")
				}
			}

			// Morning start pattern - South Americans often start 8-9am (not as early as US)
			morningStartUTC := 11 // 8am local in UTC-3
			lateStartUTC := 12    // 9am local in UTC-3

			if hourCounts[morningStartUTC] > 10 || hourCounts[lateStartUTC] > 10 {
				southAmericaBonus += 3.0
				adjustments = append(adjustments, "+3 (South America 8-9am work start)")
			}

			// Check for evening activity (7-10pm) - common in South America
			southAmericaEveningActivity := 0
			for h := 22; h <= 24; h++ { // 7-9pm local
				if h < 24 {
					southAmericaEveningActivity += hourCounts[h]
				}
			}
			southAmericaEveningActivity += hourCounts[0] + hourCounts[1] // 9-11pm local

			if southAmericaEveningActivity > 40 {
				southAmericaBonus += 2.0
				adjustments = append(adjustments, fmt.Sprintf("+2 (South America evening activity %d events)", southAmericaEveningActivity))
			}

			// Population center bonus - Brazil/Argentina are major tech hubs
			southAmericaBonus += 2.0
			adjustments = append(adjustments, "+2 (South America population centers)")

			if southAmericaBonus > 0 {
				testConfidence += southAmericaBonus
			}
		}

		// Apply penalties for unlikely scenarios (subtract points)

		// Small penalty if no lunch can be detected at all (suggests wrong timezone)
		if testLunchStart < 0 || testLunchConf < 0.2 {
			testConfidence -= 1.0 // Small penalty for no detectable lunch pattern
			adjustments = append(adjustments, "-1 (weak/no lunch pattern)")
		}

		// Penalize if lunch is at an unusual time despite having a dip
		if testLunchStart >= 0 && testLunchConf > 0.3 {
			if lunchLocalStart < 10.5 || lunchLocalStart > 14.5 {
				testConfidence -= 10 // Subtract points for very unusual lunch time
				adjustments = append(adjustments, fmt.Sprintf("-10 (unusual lunch time %.1f)", lunchLocalStart))
			}
			// Extra penalty for pre-11am lunch (too early for most regions)
			// BUT reduced penalty for UTC+10/+11 as these could be valid morning break patterns
			if lunchLocalStart < 11.0 {
				if testOffset >= 10 && testOffset <= 11 {
					testConfidence -= 2 // Reduced penalty for Pacific timezones
					adjustments = append(adjustments, fmt.Sprintf("-2 (early lunch at %.1f for UTC+%d)", lunchLocalStart, testOffset))
				} else {
					testConfidence -= 5 // Full penalty for other timezones
					adjustments = append(adjustments, fmt.Sprintf("-5 (lunch before 11am at %.1f)", lunchLocalStart))
				}
			}
			// EXTRA penalty for extremely late lunch (after 3pm)
			// This catches cases like UTC+1 for AmberArcadia where lunch would be at 5pm
			if lunchLocalStart > 15.0 {
				testConfidence -= 20 // Massive penalty for lunch after 3pm
				adjustments = append(adjustments, fmt.Sprintf("-20 (lunch after 3pm at %.1f)", lunchLocalStart))
			}
		}

		// Note: Work start penalty is now applied globally based on firstActivityLocal
		// in the peak activity timing section above

		// Apply statistics-based population adjustments for Americas tech workers
		// Based on Stack Overflow surveys, GitHub data, and tech employment statistics
		// Total tech workers by timezone (approximate):
		// Pacific (UTC-8/-7): ~950K workers (37% of US) - SF Bay, Seattle, LA, Portland
		// Eastern (UTC-5/-4): ~900K workers (35% of US) - NYC, Boston, DC, Atlanta, Toronto
		// Central (UTC-6): ~500K workers (20% of US) - Austin, Chicago, Dallas, Houston
		// Mountain (UTC-7/-6): ~200K workers (8% of US) - Denver, Phoenix, SLC
		// Brazil/Argentina (UTC-3): ~300K workers - São Paulo, Buenos Aires, Rio

		switch testOffset {
		case -8: // Pacific Standard Time
			testConfidence += 4.5 // Highest concentration: Silicon Valley effect
			adjustments = append(adjustments, "+4.5 (Pacific - 37% of US tech)")
		case -7: // Pacific Daylight / Mountain Standard
			// Could be Pacific (summer) or Mountain (winter)
			// Weight toward Pacific since it's 4.6x larger than Mountain
			testConfidence += 3.5 // Weighted average
			adjustments = append(adjustments, "+3.5 (PDT/MST - mixed Pacific/Mountain)")
		case -5: // Eastern Standard / Central Daylight
			// Could be Eastern (winter) or Central (summer)
			// Eastern is 1.8x larger than Central
			testConfidence += 3.5 // Weighted average
			adjustments = append(adjustments, "+3.5 (EST/CDT - mixed Eastern/Central)")
		case -4: // Eastern Daylight Time
			testConfidence += 4.0 // Second highest: NYC, Boston, DC
			adjustments = append(adjustments, "+4.0 (Eastern - 35% of US tech)")
		case -6: // Central Standard / Mountain Daylight
			// Could be Central (winter) or Mountain (summer)
			// Central is 2.5x larger than Mountain
			testConfidence += 2.0 // Weighted average
			adjustments = append(adjustments, "+2.0 (CST/MDT - mixed Central/Mountain)")
		case -3: // Brazil/Argentina Time
			testConfidence += 2.0 // Major South American tech hubs
			adjustments = append(adjustments, "+2.0 (Brazil/Argentina tech hubs)")
		case -2, -1: // Atlantic ocean timezones (Azores, Cape Verde, etc.)
			testConfidence -= 10 // Significant penalty - very few developers
			adjustments = append(adjustments, "-10 (Atlantic ocean low population)")
		case 0: // UTC/GMT (UK, Portugal, Iceland, West Africa)
			// No penalty - significant population
			// Check for European commute pattern (quiet hour at 17-18 UTC = 17-18 local)
			if hourCounts[17] < 5 && hourCounts[18] > 10 {
				testConfidence += 5.0 // Bonus for commute pattern
				adjustments = append(adjustments, "+5 (European commute pattern 17:00)")
			}
		case 1, 2: // European timezones
			testConfidence += 0.5 // Small boost for European developers
			adjustments = append(adjustments, "+0.5 (European population boost)")
			// Check for European commute pattern
			commuteHourUTC := 17 - testOffset // 17:00 local in UTC
			if commuteHourUTC >= 0 && commuteHourUTC < 24 {
				nextHourUTC := (commuteHourUTC + 1) % 24
				if hourCounts[commuteHourUTC] < 5 && hourCounts[nextHourUTC] > 10 {
					testConfidence += 5.0 // Bonus for commute pattern
					adjustments = append(adjustments, fmt.Sprintf("+5 (European commute at %d:00 UTC)", commuteHourUTC))
				}
			}
		case 8, 9: // China, Singapore, Australia
			testConfidence += 0.5 // Small boost for Asian developers
			adjustments = append(adjustments, "+0.5 (Asian population boost)")
		case 11: // Pacific ocean
			testConfidence -= 6 // Moderate penalty - mostly ocean
			adjustments = append(adjustments, "-6 (Pacific ocean low population)")
		case 12, 13: // Could be New Zealand
			testConfidence -= 2 // Small penalty - NZ is possible
			adjustments = append(adjustments, "-2 (NZ/Pacific low population)")
		case -11, -12: // Pacific ocean (almost no land)
			testConfidence -= 12 // Large penalty - almost no population
			adjustments = append(adjustments, "-12 (Pacific ocean almost no land)")
		default:
			// No specific adjustment for other timezones
		}

		// Special bonus for UTC+10 when there are clear evening activity patterns
		// This helps identify Sydney/Brisbane over Japan/Korea time
		if testOffset == 10 && eveningActivity > 50 {
			australiaBonus := 8.0 // Boost to compete with UTC+9
			testConfidence += australiaBonus
			adjustments = append(adjustments, fmt.Sprintf("+%.1f (strong evening activity suggests Australia UTC+10)", australiaBonus))
		}

		// Apply European morning activity penalty
		if !europeanMorningActivityCheck {
			testConfidence -= 15 // Heavy penalty for European timezones with no morning activity
			adjustments = append(adjustments, "-15 (European timezone but no morning activity)")
		}

		// Ensure confidence stays above 0 (no upper cap - let real scores determine ranking)
		testConfidence = math.Max(0, testConfidence)

		// Add to candidates if confidence is reasonable (at least 10%)
		if testConfidence >= 10 {
			candidate := Candidate{
				Timezone:         fmt.Sprintf("UTC%+d", testOffset),
				Offset:           float64(testOffset),
				Confidence:       testConfidence,
				EveningActivity:  eveningActivity,
				LunchReasonable:  lunchReasonable,
				WorkHoursNormal:  workReasonable,
				LunchLocalTime:   lunchLocalStart,
				WorkStartLocal:   testWorkStart,
				SleepMidLocal:    sleepLocalMid,
				LunchDipStrength: lunchDipStrength,
				LunchStartUTC:    testLunchStart, // Store for reuse
				LunchEndUTC:      testLunchEnd,   // Store for reuse
				LunchConfidence:  testLunchConf,  // Store for reuse
			}
			_ = adjustments // Suppress unused warning for debug variable
			candidates = append(candidates, candidate)
		}
	}

	// Sort candidates by confidence score
	for i := range len(candidates) - 1 {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Confidence > candidates[i].Confidence {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	return candidates
}
