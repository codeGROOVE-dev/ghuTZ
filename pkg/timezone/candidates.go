// Package timezone provides timezone candidate evaluation and analysis.
package timezone

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/codeGROOVE-dev/guTZ/pkg/lunch"
)

// GlobalLunchPattern represents the best lunch pattern found globally in UTC.
type GlobalLunchPattern struct {
	StartUTC    float64
	EndUTC      float64
	Confidence  float64
	DropPercent float64
}

// calculateEasternLunchBonus calculates bonus points for Eastern timezone lunch patterns.
func calculateEasternLunchBonus(dropRatio, lunchLocalStart float64) (bonus float64, adjustment string) {
	// Strong early lunch signal
	if dropRatio > 0.7 && lunchLocalStart >= 11.0 && lunchLocalStart <= 12.0 {
		// 70%+ drop at 11:30am AND lunch detected in that range
		return 8.0, fmt.Sprintf("+8 (Eastern 11:30am lunch %.1f%% drop)", dropRatio*100)
	}
	if dropRatio > 0.5 && lunchLocalStart >= 11.0 && lunchLocalStart <= 12.5 {
		// 50%+ drop around 11:30am-12:30pm
		return 5.0, fmt.Sprintf("+5 (Eastern lunch %.1f%% drop)", dropRatio*100)
	}
	return 0, ""
}

// EvaluateCandidates evaluates multiple timezone offsets to find the best candidates.
//
//nolint:gocognit,nestif,revive,maintidx // Timezone evaluation requires comprehensive multi-factor analysis
func EvaluateCandidates(username string, hourCounts map[int]int, halfHourCounts map[float64]int, totalActivity int, quietHours []int, midQuiet float64, activeStart float64, bestGlobalLunch GlobalLunchPattern, profileTimezone string, newestActivity time.Time) []Candidate {
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

		// Work start time - use the activeStart which is calculated based on sustained activity
		// activeStart is in UTC, convert to local time for this offset
		testWorkStart := math.Mod(activeStart+float64(testOffset)+24, 24)
		firstActivityLocal := testWorkStart

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
		// Sleep is reasonable if mid-sleep is between 10pm and 10am (allowing for various sleep schedules)
		// Note: midQuiet might include all quiet hours, not just nighttime sleep
		sleepReasonable := (sleepLocalMid >= 0 && sleepLocalMid <= 10) || sleepLocalMid >= 22

		// 3. Work hours analysis
		// testWorkStart already calculated above for lunch validation
		// Check if work hours are reasonable based on ACTUAL first activity, not initial guess
		workReasonable := firstActivityLocal >= 6 && firstActivityLocal <= 10 // Allow 6am starts for early risers

		// 4. Evening activity (7-11pm local)
		// STRICT: Only count 7pm-11pm as evening, NOT 5-6pm which is dinner/transition
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
				testConfidence += 10 // Good sleep timing
				adjustments = append(adjustments, fmt.Sprintf("+10 (good sleep, mid=%.1f)", sleepLocalMid))
				// Still give bonus for early sleep
				if sleepStartUTC >= 0 {
					sleepStartLocal := float64((sleepStartUTC + testOffset + 24) % 24)
					if sleepStartLocal >= 21 && sleepStartLocal <= 23 {
						testConfidence += 2 // Early sleep bonus
						adjustments = append(adjustments, fmt.Sprintf("+2 (early sleep, start=%.0fpm)", sleepStartLocal-12))
					}
				}
			case sleepLocalMid >= 22 || sleepLocalMid <= 10:
				// Sleep at late or early times - reasonable for many people
				// Note: sleepLocalMid of 6-10 can occur when quiet hours include both night and morning
				testConfidence += 5 // Normal sleep pattern (reduced slightly since it's broader)
				adjustments = append(adjustments, fmt.Sprintf("+5 (acceptable sleep, mid=%.1f)", sleepLocalMid))
			default:
				// Sleep pattern exists but timing is very unusual (mid-day sleep)
				testConfidence -= 5 // Increased penalty for truly unusual sleep
				adjustments = append(adjustments, fmt.Sprintf("-5 (daytime sleep, mid=%.1f)", sleepLocalMid))
			}
		} else {
			adjustments = append(adjustments, "0 (no reasonable sleep pattern)")
		}

		// Skip the global lunch distance bonus - will be handled in the main lunch timing section to avoid duplicates

		// Lunch timing - 15 points max (strong signal when clear)
		if lunchReasonable {
			var lunchScore float64
			// CRITICAL: Noon (12:00) is the most common lunch time globally
			switch {
			case lunchLocalStart >= 11.75 && lunchLocalStart <= 12.25:
				// Within 15 minutes of noon - STRONGEST signal (combine both bonuses here)
				lunchScore = 15 // Perfect noon timing gets maximum bonus
				// Add global lunch context bonus if applicable
				if bestGlobalLunch.Confidence > 0 {
					globalLunchLocalTime := math.Mod(bestGlobalLunch.StartUTC+float64(testOffset)+24, 24)
					if math.Abs(globalLunchLocalTime-12.0) < 0.5 { // Within 30 min of noon
						lunchScore += 5 // Additional bonus for matching global pattern
						adjustments = append(adjustments, fmt.Sprintf("+%.0f (perfect noon lunch matching global pattern)", lunchScore))
					} else {
						adjustments = append(adjustments, fmt.Sprintf("+%.0f (perfect noon lunch)", lunchScore))
					}
				} else {
					adjustments = append(adjustments, fmt.Sprintf("+%.0f (perfect noon lunch)", lunchScore))
				}
			case lunchLocalStart >= 11.5 && lunchLocalStart <= 13.5:
				// 11:30am to 1:30pm are good lunch times
				lunchScore = 10 // Good lunch timing
				adjustments = append(adjustments, "+10 (good lunch timing 11:30am-1:30pm)")
			case lunchLocalStart >= 11.0 && lunchLocalStart <= 14.0:
				lunchScore = 8 // Acceptable lunch timing (11am-2pm)
				adjustments = append(adjustments, "+8 (acceptable lunch timing 11am-2pm)")
			case lunchLocalStart >= 10.5 && lunchLocalStart <= 14.5:
				lunchScore = 6 // Acceptable lunch timing
				adjustments = append(adjustments, "+6 (acceptable lunch timing 10:30am-2:30pm)")
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

			// PENALTY: Lunch at the very end of work day makes no sense
			// Calculate last activity hour for this timezone
			activeEndLocal := 0.0
			for hour := 23; hour >= 0; hour-- {
				utcHour := (hour - testOffset + 24) % 24
				if hourCounts[utcHour] > 0 {
					activeEndLocal = float64(hour)
					break
				}
			}
			// Check if lunch is within last 1.5 hours of activity
			if activeEndLocal > 0 && lunchLocalStart >= activeEndLocal-1.5 {
				lunchScore -= 10
				adjustments = append(adjustments, fmt.Sprintf("-10 (lunch at end of work day %.1f vs work end %.0f)", lunchLocalStart, activeEndLocal))
			}

			finalLunchScore := math.Min(15, lunchScore)
			testConfidence += finalLunchScore
		} else if testLunchStart < 0 {
			// No detectable lunch break - apply penalty
			// This might indicate invalid timezone or unusual work pattern
			testConfidence -= 5
			adjustments = append(adjustments, "-5 (no lunch detected)")
		}

		// Work hours - 8 points max, with STRONG penalties for too-early starts
		// CRITICAL: Use firstActivityLocal which is calculated per-timezone, not testWorkStart
		if firstActivityLocal < 24 {
			// Use more precise work start time (with half-hour granularity)
			preciseWorkStart := firstActivityLocal

			// ALWAYS penalize very early starts, regardless of workReasonable flag
			switch {
			case preciseWorkStart <= 4.0:
				// 4am or earlier is completely absurd - MASSIVE penalty
				testConfidence -= 50
				adjustments = append(adjustments, fmt.Sprintf("-50 (impossible %.1fam work start)", preciseWorkStart))
				if preciseWorkStart < 2.0 {
					testConfidence -= 30 // Additional penalty for midnight starts
					adjustments = append(adjustments, fmt.Sprintf("-30 (extra penalty for midnight-%.1fam)", preciseWorkStart))
				}
			case preciseWorkStart >= 5.0 && preciseWorkStart < 5.5:
				// 5:00am is early but some people (especially East Coast) do start then
				penalty := -10.0
				// Check if this person has good afternoon productivity to offset early start
				afternoonProductivity := 0
				for localHour := 13; localHour <= 16; localHour++ {
					utcHour := (localHour - testOffset + 24) % 24
					afternoonProductivity += hourCounts[utcHour]
				}
				if afternoonProductivity > 40 {
					// If they have good afternoon productivity (40+ events 1-4pm), reduce penalty
					penalty = -5.0
					adjustments = append(adjustments, "-5 (early 5:00am start but good afternoon productivity)")
				} else {
					adjustments = append(adjustments, "-10 (early 5:00am start)")
				}
				testConfidence += penalty
			case preciseWorkStart >= 5.5 && preciseWorkStart < 6.0:
				// 5:30am is more reasonable for early risers
				penalty := -5.0
				// Check afternoon productivity for additional leniency
				afternoonProductivity := 0
				for localHour := 13; localHour <= 16; localHour++ {
					utcHour := (localHour - testOffset + 24) % 24
					afternoonProductivity += hourCounts[utcHour]
				}
				if afternoonProductivity > 40 {
					// Very lenient for good afternoon productivity
					penalty = -2.0
					adjustments = append(adjustments, "-2 (5:30am start with good afternoon productivity)")
				} else {
					adjustments = append(adjustments, "-5 (5:30am start)")
				}
				testConfidence += penalty
			case workReasonable:
				// Only give bonuses for reasonable starts (6am+)
				actualWorkStart := int(preciseWorkStart) // Convert back to int for existing logic
				switch {
				case actualWorkStart >= 7 && actualWorkStart <= 9:
					testConfidence += 12 // Good work start (7-9am) - increased from 8 to 12
					adjustments = append(adjustments, fmt.Sprintf("+12 (good work start %dam)", actualWorkStart))
				case actualWorkStart == 6:
					testConfidence += 4 // 6am is early but some people do it
					adjustments = append(adjustments, "+4 (early 6am work start)")
				case actualWorkStart >= 6 && actualWorkStart <= 10:
					testConfidence += 2 // Acceptable but unusual
					adjustments = append(adjustments, fmt.Sprintf("+2 (unusual work start %dam)", actualWorkStart))
				default:
					testConfidence++ // Work hours detected but very unusual
					adjustments = append(adjustments, fmt.Sprintf("+1 (very unusual work start %dam)", actualWorkStart))
				}
			default:
				// Work hours detected but not reasonable - apply penalties
				actualWorkStart := int(preciseWorkStart) // Convert back to int for existing logic
				testConfidence -= 5
				adjustments = append(adjustments, fmt.Sprintf("-5 (unreasonable work hours %dam)", actualWorkStart))
			}
		}

		// Evening activity - VERY conservative scoring (max 0.5 points)
		// NOT ALL GITHUB USERS CODE IN THE EVENING
		// What looks like "evening" in one timezone might be afternoon work in another
		// Also check for misclassified late afternoon activity (5-6pm)
		if eveningActivity > 0 {
			eveningRatio := float64(eveningActivity) / float64(totalActivity)

			// Also check 5-6pm activity to detect misclassification
			lateAfternoonActivity := 0
			for localHour := 17; localHour <= 18; localHour++ {
				utcHour := (localHour - testOffset + 24) % 24
				lateAfternoonActivity += hourCounts[utcHour]
			}
			lateAfternoonRatio := float64(lateAfternoonActivity) / float64(totalActivity)

			// Only give points if evening activity is VERY SUBSTANTIAL (>30% of total)
			// AND late afternoon (5-6pm) isn't also high (which would indicate wrong timezone)
			if eveningRatio > 0.3 && lateAfternoonRatio < 0.2 {
				// Ultra conservative scoring - max 0.5 points, only if >30% evening
				eveningPoints := 0.5 * math.Min(1.0, (eveningRatio-0.3)*3.33)
				testConfidence += eveningPoints
				adjustments = append(adjustments, fmt.Sprintf("+%.1f (evening 7-11pm: %.1f%%)", eveningPoints, eveningRatio*100))
			} else if lateAfternoonRatio > 0.15 && eveningRatio < 0.2 {
				// PENALTY: High 5-6pm but low 7-11pm suggests wrong timezone
				// This is likely afternoon work being misinterpreted
				testConfidence -= 3
				adjustments = append(adjustments, fmt.Sprintf("-3 (high 5-6pm %.1f%% but low evening %.1f%%)", lateAfternoonRatio*100, eveningRatio*100))
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

		// Track if peak time is reasonable
		peakReasonable := false
		if maxActivityHour >= 0 {
			// Convert peak activity UTC hour to local time for this timezone
			peakLocalHour := (maxActivityHour + testOffset + 24) % 24

			// Peak is reasonable if it's between 9am-4pm (work) or 6-9pm (OSS hobbyist)
			// Morning productivity (9am-12pm) is very common and healthy
			peakReasonable = (peakLocalHour >= 9 && peakLocalHour <= 16) || (peakLocalHour >= 18 && peakLocalHour <= 21)
			// Bonus for peak activity during ideal work hours (10am-3pm = 10-15 local)
			// PENALTY for peak at 5-6pm (transitioning to dinner/hobbies)
			var peakBonus float64
			switch {
			case peakLocalHour >= 17 && peakLocalHour <= 18:
				// 5-6pm peak is suspicious - transition/dinner time (increased penalty)
				peakBonus = -8.0 // Increased from -3 to -8
				adjustments = append(adjustments, fmt.Sprintf("-8 (suspicious peak at %d:00, dinner/transition time)", peakLocalHour))
			case peakLocalHour >= 19 && peakLocalHour <= 21:
				// 7-9pm peak is common for OSS hobbyists working from home
				peakBonus = 2.0
				adjustments = append(adjustments, fmt.Sprintf("+2 (evening OSS peak at %dpm)", peakLocalHour-12))
			case peakLocalHour >= 22 || peakLocalHour <= 6:
				// After 10pm or before 7am peak is very unusual - likely wrong timezone
				peakBonus = -20.0
				if peakLocalHour <= 6 {
					adjustments = append(adjustments, fmt.Sprintf("-20 (peak at %dam - night owl unlikely)", peakLocalHour))
				} else {
					adjustments = append(adjustments, fmt.Sprintf("-20 (very late peak at %dpm)", peakLocalHour-12))
				}
			case peakLocalHour >= 13 && peakLocalHour <= 15:
				// 1-3pm is ideal afternoon work time
				peakBonus = 5.0
				adjustments = append(adjustments, fmt.Sprintf("+5 (peak at %dpm ideal)", peakLocalHour-12))
			case peakLocalHour >= 9 && peakLocalHour <= 12:
				// 9am-noon is good morning work time (many people are most productive in the morning)
				peakBonus = 3.0
				adjustments = append(adjustments, fmt.Sprintf("+3 (peak at %dam good morning)", peakLocalHour))
			case peakLocalHour == 16:
				// 4pm is still good work time
				peakBonus = 3.0
				adjustments = append(adjustments, "+3 (peak at 4pm good work time)")
			default:
				// Other times are neutral or negative
				peakBonus = 0.0
				adjustments = append(adjustments, fmt.Sprintf("0 (unusual peak time %d:00)", peakLocalHour))
			}
			testConfidence += peakBonus
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

		// PENALTY for midnight-8am having higher average activity than rest of day
		// This is a strong indicator of wrong timezone
		nightActivity := 0 // midnight to 8am local
		nightHours := 0
		dayActivity := 0 // 8am to midnight local
		dayHours := 0

		for hour, count := range hourCounts {
			localHour := (hour + testOffset + 24) % 24
			if localHour >= 0 && localHour < 8 {
				nightActivity += count
				if count > 0 {
					nightHours++
				}
			} else {
				dayActivity += count
				if count > 0 {
					dayHours++
				}
			}
		}

		// Calculate averages (avoid division by zero)
		nightAvg := 0.0
		if nightHours > 0 {
			nightAvg = float64(nightActivity) / 8.0 // Average per hour for night period
		}
		dayAvg := 0.0
		if dayHours > 0 {
			dayAvg = float64(dayActivity) / 16.0 // Average per hour for day period
		}

		// Strong penalty if night activity exceeds day activity
		if nightAvg > dayAvg && nightActivity > 10 { // Only apply if there's meaningful activity
			penalty := -25.0
			testConfidence += penalty
			adjustments = append(adjustments, fmt.Sprintf("-25 (night activity %.1f > day %.1f avg/hr)", nightAvg, dayAvg))
		}

		// PENALTY for unreasonable overnight vs afternoon productivity (1:00-2:30am vs 2:00-3:30pm local)
		// This is a strong signal that the timezone is wrong - very few people are more productive at 1-2:30am than 2-3:30pm

		// Helper function to get activity count for a local time range
		getActivityInRange := func(startLocalHour, endLocalHour int, includeHalfHour bool) int {
			activity := 0
			for localHour := startLocalHour; localHour <= endLocalHour; localHour++ {
				utcHour := (localHour - testOffset + 24) % 24
				activity += hourCounts[utcHour]

				// Include half-hour bucket if requested and this is the end hour
				if includeHalfHour && localHour == endLocalHour {
					utcHalfHour := float64(localHour-1) + 0.5 - float64(testOffset)
					for utcHalfHour >= 24 {
						utcHalfHour -= 24
					}
					for utcHalfHour < 0 {
						utcHalfHour += 24
					}
					activity += halfHourCounts[utcHalfHour]
				}
			}
			return activity
		}

		overnightActivity := getActivityInRange(1, 2, true)   // 1:00-2:30am local
		afternoonActivity := getActivityInRange(14, 15, true) // 2:00-3:30pm local

		// Only penalize if overnight productivity exceeds afternoon productivity AND there's significant overnight activity
		if overnightActivity > 10 && overnightActivity > afternoonActivity {
			productivityRatio := float64(overnightActivity) / math.Max(float64(afternoonActivity), 1.0)
			var penalty float64
			if productivityRatio > 2.0 {
				// Overnight is more than 2x afternoon productivity - very suspicious
				penalty = -30.0
				adjustments = append(adjustments, fmt.Sprintf("-30 (overnight %d >> afternoon %d events - %.1fx more productive)", overnightActivity, afternoonActivity, productivityRatio))
			} else if productivityRatio > 1.5 {
				// Overnight is 1.5x+ afternoon productivity - suspicious
				penalty = -15.0
				adjustments = append(adjustments, fmt.Sprintf("-15 (overnight %d > afternoon %d events - %.1fx more productive)", overnightActivity, afternoonActivity, productivityRatio))
			} else {
				// Overnight is moderately higher than afternoon
				penalty = -5.0
				adjustments = append(adjustments, fmt.Sprintf("-5 (overnight %d > afternoon %d events - %.1fx more productive)", overnightActivity, afternoonActivity, productivityRatio))
			}
			testConfidence += penalty
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
					easternLunchBonus, adjustment := calculateEasternLunchBonus(dropRatio, lunchLocalStart)
					easternBonus += easternLunchBonus
					if adjustment != "" {
						adjustments = append(adjustments, adjustment)
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
			if username == "egibs" {
				fmt.Printf("SOUTH AMERICA BONUS: Evaluating UTC-3 for egibs\n")
			}
			southAmericaBonus := 0.0

			// UTC-3 typically has lunch at 12:00-1:00pm (not early like US)
			// Check for noon lunch pattern
			if lunchLocalStart >= 11.5 && lunchLocalStart <= 13.0 && testLunchConf > 0.5 {
				southAmericaBonus += 6.0
				adjustments = append(adjustments, fmt.Sprintf("+6 (South America noon lunch at %.1f)", lunchLocalStart))
			}

			// CRITICAL: 6pm end-of-day peak is SUSPICIOUS in South America
			// 5-6pm is dinner/transition time, not productive work time
			// For UTC-3: 17:00-18:00 local = 20:00-21:00 UTC
			lateAfternoonUTC20 := hourCounts[20] // 5pm local
			endOfDayUTC21 := hourCounts[21]      // 6pm local

			// PENALTY for high 5-6pm activity - this is dinner time!
			if lateAfternoonUTC20 >= 20 || endOfDayUTC21 >= 20 {
				// High 5-6pm activity is SUSPICIOUS - people are usually transitioning
				southAmericaBonus -= 25.0 // Increased from 15 to 25
				adjustments = append(adjustments, fmt.Sprintf("-25 (suspicious 5-6pm activity %d/%d - dinner time)", lateAfternoonUTC20, endOfDayUTC21))

				// Check if this is actually the peak - if so, it's very wrong
				isPeak := true
				for h := range 24 {
					if h != 20 && h != 21 && hourCounts[h] > max(lateAfternoonUTC20, endOfDayUTC21) {
						isPeak = false
						break
					}
				}
				if isPeak {
					southAmericaBonus -= 30.0 // Increased from 20 to 30
					adjustments = append(adjustments, "-30 (peak at 5-6pm is wrong - dinner/transition time)")
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
			adjustments = append(adjustments, "+4.5 (UTC-8 common for US tech)")
		case -7: // Pacific Daylight / Mountain Standard
			// Could be Pacific (summer) or Mountain (winter)
			// Weight toward Pacific since it's 4.6x larger than Mountain
			testConfidence += 3.5 // Weighted average
			adjustments = append(adjustments, "+3.5 (UTC-7 common for US tech)")
		case -5: // Eastern Standard / Central Daylight
			// Could be Eastern (winter) or Central (summer)
			// Eastern is 1.8x larger than Central
			testConfidence += 3.5 // Weighted average
			adjustments = append(adjustments, "+3.5 (UTC-5 common for US tech)")
		case -4: // Eastern Daylight Time
			testConfidence += 4.0 // Second highest: NYC, Boston, DC
			adjustments = append(adjustments, "+4.0 (UTC-4 common for US tech)")
		case -6: // Central Standard / Mountain Daylight
			// Could be Central (winter) or Mountain (summer)
			// Central is 2.5x larger than Mountain
			testConfidence += 2.0 // Weighted average
			adjustments = append(adjustments, "+2.0 (UTC-6 US tech hub)")
		case -3: // Brazil/Argentina Time
			testConfidence += 2.0 // Major South American tech hubs
			adjustments = append(adjustments, "+2.0 (Brazil/Argentina tech hubs)")
		case -2, -1: // Atlantic ocean timezones (Azores, Cape Verde, etc.)
			testConfidence -= 10 // Significant penalty - very few developers
			adjustments = append(adjustments, "-10 (Atlantic ocean low population)")
		case 0, 1: // UTC+0/UTC+1 (UK/Ireland/Portugal in GMT/BST, Western/Central Europe)
			// Moderate boost for UTC+0/+1 - major developer population but not overwhelming
			// UK uses UTC+0 in winter (GMT) and UTC+1 in summer (BST)
			// Central Europe uses UTC+1 in winter and UTC+2 in summer
			testConfidence += 8.0 // Reduced from 12 to 8 - still significant but more balanced
			if testOffset == 0 {
				adjustments = append(adjustments, "+8 (UK/Western Europe GMT/winter time)")
			} else {
				adjustments = append(adjustments, "+8 (UK BST/Central Europe winter time)")
			}

			// Check for European commute pattern (quiet hour at 17-18 local)
			commuteHourUTC := 17 - testOffset // 17:00 local in UTC
			if commuteHourUTC >= 0 && commuteHourUTC < 24 {
				nextHourUTC := (commuteHourUTC + 1) % 24
				if hourCounts[commuteHourUTC] < 5 && hourCounts[nextHourUTC] > 10 {
					testConfidence += 5.0 // Bonus for commute pattern
					adjustments = append(adjustments, fmt.Sprintf("+5 (European commute at %d:00 UTC)", commuteHourUTC))
				}
			}

			// Check for UK tea time pattern (15:00-16:00 local)
			teaTimeUTC := 15 - testOffset
			if teaTimeUTC >= 0 && teaTimeUTC < 24 {
				beforeTeaUTC := (teaTimeUTC - 1 + 24) % 24
				afterTeaUTC := (teaTimeUTC + 1) % 24
				if hourCounts[teaTimeUTC] < hourCounts[beforeTeaUTC] && hourCounts[teaTimeUTC] < hourCounts[afterTeaUTC] {
					testConfidence += 3.0 // Tea time pattern
					adjustments = append(adjustments, "+3 (UK tea time pattern)")
				}
			}
		case 2: // Central/Eastern European timezone
			testConfidence += 2.0 // Small boost for Central/Eastern European developers
			adjustments = append(adjustments, "+2 (Central/Eastern Europe population boost)")
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

		// Check if this is the user's profile timezone for bonus points
		isProfileTimezone := false
		if profileTimezone != "" {
			// Parse the profile timezone UTC offset string (e.g., "UTC-7", "UTC+5.5")
			// Since this comes directly from GitHub's data-hours-ahead-of-utc, it's already the current offset
			profileOffset := parseUTCOffsetString(profileTimezone)
			if profileOffset == testOffset {
				isProfileTimezone = true
				// Add a moderate boost for the profile timezone (before scaling)
				testConfidence += 10.0
				adjustments = append(adjustments, "+10 (user's profile timezone)")
			}
		}

		// Ensure confidence stays above 0 and scale up for better dynamic range
		testConfidence = math.Max(0, testConfidence)
		// Scale confidence by 1.5x for better dynamic range (50% -> 75%)
		testConfidence *= 1.5

		// Debug: Log scoring details for all candidates
		// For egibs debugging specifically
		if username == "egibs" && (testOffset == -3 || testOffset == -5 || testOffset == -6) {
			fmt.Printf("DEBUG [%s] UTC%+d: confidence=%.1f adjustments=%v\n",
				username, testOffset, testConfidence, adjustments)
		}

		// Debug for stevebeattie - log UTC-7, UTC-8, UTC-9
		if username == "stevebeattie" && (testOffset == -7 || testOffset == -8 || testOffset == -9) {
			fmt.Printf("DEBUG [%s] UTC%+d: confidence=%.1f adjustments=%v\n",
				username, testOffset, testConfidence, adjustments)
		}

		// Debug for tstromberg - log UTC+0, UTC-4, UTC-5 to see overnight penalty
		if username == "tstromberg" && (testOffset == 0 || testOffset == -4 || testOffset == -5) {
			fmt.Printf("DEBUG [%s] UTC%+d: confidence=%.1f adjustments=%v\n",
				username, testOffset, testConfidence, adjustments)
		}

		// Debug for mattmoor - show all timezones to see if profile timezone is included
		if username == "mattmoor" {
			fmt.Printf("DEBUG [%s] UTC%+d: confidence=%.1f isProfile=%v adjustments=%v\n",
				username, testOffset, testConfidence, isProfileTimezone, adjustments)
		}

		// Add ALL candidates regardless of confidence to ensure complete coverage
		// This ensures --force-offset works and profile locations are always considered
		// We'll let Gemini and other factors determine the final choice
		if true { // Always add candidate
			candidate := Candidate{
				Timezone:            fmt.Sprintf("UTC%+d", testOffset),
				Offset:              float64(testOffset),
				Confidence:          testConfidence,
				EveningActivity:     eveningActivity,
				LunchReasonable:     lunchReasonable,
				WorkHoursReasonable: workReasonable,
				SleepReasonable:     sleepReasonable,
				PeakTimeReasonable:  peakReasonable,
				LunchLocalTime:      lunchLocalStart,
				WorkStartLocal:      testWorkStart,
				SleepMidLocal:       sleepLocalMid,
				LunchDipStrength:    lunchDipStrength,
				LunchStartUTC:       testLunchStart,    // Store for reuse
				LunchEndUTC:         testLunchEnd,      // Store for reuse
				LunchConfidence:     testLunchConf,     // Store for reuse
				ScoringDetails:      adjustments,       // Include scoring details for Gemini
				IsProfile:           isProfileTimezone, // Mark if this is the profile timezone
			}
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

// parseUTCOffsetString converts a UTC offset string to its integer offset.
// Handles formats like "UTC", "UTC-5", "UTC+8", "UTC+5.5", "UTC-2.5", etc.
// Returns the offset rounded to the nearest integer hour for comparison.
func parseUTCOffsetString(tz string) int {
	// Handle plain UTC
	if tz == "UTC" {
		return 0
	}

	// Handle UTC+/- format with possible decimals
	if strings.HasPrefix(tz, "UTC+") {
		offsetStr := strings.TrimPrefix(tz, "UTC+")
		if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
			return int(math.Round(offset))
		}
	}
	if strings.HasPrefix(tz, "UTC-") {
		offsetStr := strings.TrimPrefix(tz, "UTC-")
		if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
			return -int(math.Round(offset))
		}
	}
	if strings.HasPrefix(tz, "UTC") && len(tz) > 3 {
		// Handle formats like UTC-5.5 or UTC5 (without explicit + or -)
		offsetStr := strings.TrimPrefix(tz, "UTC")
		if offset, err := strconv.ParseFloat(offsetStr, 64); err == nil {
			return int(math.Round(offset))
		}
	}

	return 0 // Default to UTC if parsing fails
}
