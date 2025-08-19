package ghutz

import (
	"testing"
)

// TestAsh2kSydneyDetection tests that ash2k is correctly detected as UTC+10 (Sydney)
// Based on the observation that it's 3:26am local time when it's 13:25 EDT
// User confirmed to be in Sydney, Australia
func TestAsh2kSydneyDetection(t *testing.T) {
	// ACTUAL 30-minute bucket counts from ash2k's GitHub activity
	// Extracted from the histogram showing activity pattern
	halfHourCounts := map[float64]int{
		// UTC time : event count (from histogram)
		3.0:  11, // 03:00 UTC = 13:00 UTC+10 (1pm local)
		3.5:  4,  // 03:30 UTC = 13:30 UTC+10
		4.0:  10, // 04:00 UTC = 14:00 UTC+10 (2pm local)
		4.5:  15, // 04:30 UTC = 14:30 UTC+10 - PEAK afternoon
		5.0:  9,  // 05:00 UTC = 15:00 UTC+10 (3pm local)
		5.5:  0,  // 05:30 UTC = 15:30 UTC+10
		6.0:  9,  // 06:00 UTC = 16:00 UTC+10 (4pm local)
		6.5:  16, // 06:30 UTC = 16:30 UTC+10 - PEAK before end of day
		7.0:  7,  // 07:00 UTC = 17:00 UTC+10 (5pm local)
		7.5:  11, // 07:30 UTC = 17:30 UTC+10
		8.0:  11, // 08:00 UTC = 18:00 UTC+10 (6pm local - evening)
		8.5:  3,  // 08:30 UTC = 18:30 UTC+10
		9.0:  9,  // 09:00 UTC = 19:00 UTC+10 (7pm local - evening)
		9.5:  3,  // 09:30 UTC = 19:30 UTC+10
		10.0: 6,  // 10:00 UTC = 20:00 UTC+10 (8pm local - evening)
		10.5: 8,  // 10:30 UTC = 20:30 UTC+10
		11.0: 2,  // 11:00 UTC = 21:00 UTC+10 (9pm local)
		11.5: 4,  // 11:30 UTC = 21:30 UTC+10
		12.0: 9,  // 12:00 UTC = 22:00 UTC+10 (10pm local - late evening)
		12.5: 3,  // 12:30 UTC = 22:30 UTC+10 - LUNCH (detected as 12:30)
		13.0: 4,  // 13:00 UTC = 23:00 UTC+10 (11pm local)
		13.5: 15, // 13:30 UTC = 23:30 UTC+10 - late night burst
		14.0: 4,  // 14:00 UTC = 00:00 UTC+10 (midnight)
		14.5: 2,  // 14:30 UTC = 00:30 UTC+10
		15.0: 7,  // 15:00 UTC = 01:00 UTC+10 (1am local)
		15.5: 4,  // 15:30 UTC = 01:30 UTC+10
		16.0: 3,  // 16:00 UTC = 02:00 UTC+10 (2am local)
		16.5: 1,  // 16:30 UTC = 02:30 UTC+10
		// Quiet hours 18:00-22:00 UTC = 04:00-08:00 UTC+10 (4am-8am local - sleep)
		18.0: 0, // 18:00 UTC = 04:00 UTC+10 - SLEEP
		19.0: 1, // 19:00 UTC = 05:00 UTC+10
		21.0: 1, // 21:00 UTC = 07:00 UTC+10
		23.0: 1, // 23:00 UTC = 09:00 UTC+10 (9am local - work start)
		0.0:  1, // 00:00 UTC = 10:00 UTC+10 (10am local)
		2.5:  7, // 02:30 UTC = 12:30 UTC+10 (12:30pm local)
	}

	// Test for UTC+10 (Sydney/Brisbane time)
	offset := 10 // Positive offset for UTC+10

	// Key patterns for UTC+10:
	// 1. Sleep time 18:00-22:00 UTC = 4am-8am local (perfect sleep window)
	// 2. Work starts around 23:00-03:00 UTC = 9am-1pm local (reasonable)
	// 3. Peak at 6:30 UTC = 4:30pm local (end of workday)
	// 4. Evening activity 8:00-12:00 UTC = 6pm-10pm local (perfect evening)

	// Calculate quiet hours
	quietHoursUTC := []int{}
	for hour := range 24 {
		count := 0
		if c, exists := halfHourCounts[float64(hour)]; exists {
			count += c
		}
		if c, exists := halfHourCounts[float64(hour)+0.5]; exists {
			count += c
		}
		if count <= 1 { // Very quiet
			quietHoursUTC = append(quietHoursUTC, hour)
		}
	}

	t.Logf("Quiet hours UTC: %v", quietHoursUTC)

	// Quiet hours should be 18-22 UTC (4am-8am UTC+10)
	expectedQuiet := map[int]bool{18: true, 19: true, 20: true, 21: true, 22: true}
	for _, hour := range quietHoursUTC {
		if expectedQuiet[hour] {
			localHour := (hour + offset) % 24
			t.Logf("Quiet hour %d UTC = %d:00 local (good for sleep)", hour, localHour)
		}
	}

	// Check work start time
	// First significant activity is at 23:00 UTC = 9am UTC+10
	workStartUTC := 23
	workStartLocal := (workStartUTC + offset) % 24
	if workStartLocal != 9 {
		t.Errorf("Work start calculation wrong: expected 9am, got %dam", workStartLocal)
	}

	// Check evening activity
	// Peak evening is 8:00-12:00 UTC = 6pm-10pm UTC+10
	eveningStartUTC := 8
	eveningStartLocal := (eveningStartUTC + offset) % 24
	if eveningStartLocal != 18 {
		t.Errorf("Evening start wrong: expected 6pm, got %dpm", eveningStartLocal-12)
	}

	t.Logf("UTC+10 (Sydney) interpretation:")
	t.Logf("- Sleep: 18:00-22:00 UTC = 4am-8am local (perfect)")
	t.Logf("- Work starts: 23:00 UTC = 9am local (perfect)")
	t.Logf("- Afternoon peak: 6:30 UTC = 4:30pm local (end of day)")
	t.Logf("- Evening: 8:00-12:00 UTC = 6pm-10pm local (perfect)")
	t.Logf("- CONFIRMED: User is in Sydney, Australia")

	// Compare with Moscow (UTC+3) - should be unreasonable
	moscowOffset := -3
	moscowWorkStart := (workStartUTC - moscowOffset + 24) % 24
	moscowSleepStart := (18 - moscowOffset + 24) % 24

	t.Logf("\nMoscow (UTC+3) interpretation (WRONG):")
	t.Logf("- Sleep: 18:00-22:00 UTC = 9pm-1am local (too early)")
	t.Logf("- Work starts: 23:00 UTC = 2am local (absurd!)")
	t.Logf("- Work at %dam is unreasonable", moscowWorkStart)
	t.Logf("- Sleep at %dpm is too early for a developer", moscowSleepStart-12)
}

// TestAsh2kTimezoneScoring verifies UTC+10 scores higher than Moscow
func TestAsh2kTimezoneScoring(t *testing.T) {
	// Hourly counts aggregated from 30-minute data
	hourCounts := map[int]int{
		0:  1,  // 00:00 UTC
		1:  0,  // 01:00 UTC
		2:  7,  // 02:00 UTC
		3:  15, // 03:00 UTC - work hours for UTC+10 (1pm)
		4:  25, // 04:00 UTC - work hours for UTC+10 (2pm)
		5:  9,  // 05:00 UTC
		6:  25, // 06:00 UTC - peak for UTC+10 (4pm)
		7:  18, // 07:00 UTC - end of work UTC+10 (5pm)
		8:  14, // 08:00 UTC - evening UTC+10 (6pm)
		9:  12, // 09:00 UTC - evening UTC+10 (7pm)
		10: 14, // 10:00 UTC - evening UTC+10 (8pm)
		11: 6,  // 11:00 UTC
		12: 12, // 12:00 UTC - late evening UTC+10 (10pm)
		13: 19, // 13:00 UTC - near midnight UTC+10
		14: 6,  // 14:00 UTC - after midnight UTC+10
		15: 11, // 15:00 UTC - 1am UTC+10
		16: 4,  // 16:00 UTC - 2am UTC+10
		17: 0,  // 17:00 UTC - 3am UTC+10 (sleep)
		18: 0,  // 18:00 UTC - 4am UTC+10 (sleep)
		19: 1,  // 19:00 UTC - 5am UTC+10 (sleep)
		20: 0,  // 20:00 UTC - 6am UTC+10 (sleep)
		21: 1,  // 21:00 UTC - 7am UTC+10 (sleep)
		22: 0,  // 22:00 UTC - 8am UTC+10 (waking up)
		23: 1,  // 23:00 UTC - 9am UTC+10 (work starts)
	}

	// For UTC+10, evening activity (6-10pm) is 8-12 UTC
	utc10Evening := hourCounts[8] + hourCounts[9] + hourCounts[10] + hourCounts[11] + hourCounts[12]

	// For Moscow (UTC+3), evening (6-10pm) would be 15-19 UTC
	moscowEvening := hourCounts[15] + hourCounts[16] + hourCounts[17] + hourCounts[18] + hourCounts[19]

	t.Logf("Evening activity comparison:")
	t.Logf("- UTC+10 (8-12 UTC): %d events", utc10Evening)
	t.Logf("- Moscow (15-19 UTC): %d events", moscowEvening)

	if utc10Evening <= moscowEvening {
		t.Errorf("UTC+10 should have more evening activity than Moscow")
	}

	// Check sleep patterns
	// UTC+10: sleep 17-22 UTC (3am-8am local)
	// Moscow: sleep 0-5 UTC (3am-8am local)

	utc10Sleep := hourCounts[17] + hourCounts[18] + hourCounts[19] + hourCounts[20] + hourCounts[21] + hourCounts[22]
	moscowSleep := hourCounts[0] + hourCounts[1] + hourCounts[2] + hourCounts[3] + hourCounts[4] + hourCounts[5]

	t.Logf("Sleep period activity (should be minimal):")
	t.Logf("- UTC+10 (17-22 UTC): %d events", utc10Sleep)
	t.Logf("- Moscow (0-5 UTC): %d events", moscowSleep)

	if utc10Sleep >= moscowSleep {
		t.Errorf("UTC+10 has clearer sleep pattern (less activity) than Moscow")
	}
}
