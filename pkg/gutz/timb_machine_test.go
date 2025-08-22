package gutz

import (
	"testing"
)

// TestTimbMachineSleepPeriod tests that timb-machine's sleep period is correctly detected.
// The sleep should start around 23:00 London time when activity ends, not at 01:30.
func TestTimbMachineSleepPeriod(t *testing.T) {
	// These are the actual sleep buckets for timb-machine in UTC
	// Represents a wraparound sleep period from 23:00-09:30 London time
	sleepBucketsUTC := []float64{
		22, 22.5, 23, 23.5, // Late evening (23:00-00:30 London)
		0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5, // Night period (01:00-06:00 London)
	}

	// Convert to London time (UTC+1)
	timezone := "Europe/London"

	// Debug: show local conversion
	localSleepBuckets := make([]float64, len(sleepBucketsUTC))
	for i, utcBucket := range sleepBucketsUTC {
		localSleepBuckets[i] = convertUTCToLocalFloat(utcBucket, timezone)
	}
	t.Logf("UTC sleep buckets: %v", sleepBucketsUTC)
	t.Logf("Local sleep buckets: %v", localSleepBuckets)

	sleepRanges := CalculateSleepRangesFromBuckets(sleepBucketsUTC, timezone)

	// Should detect ONE sleep period that wraps around midnight
	if len(sleepRanges) != 1 {
		t.Errorf("Expected 1 sleep period for timb-machine, got %d", len(sleepRanges))
		t.Logf("Sleep ranges detected: %+v", sleepRanges)

		// Debug: show what the ranges would be
		for i, r := range sleepRanges {
			t.Logf("Range %d: %.1f - %.1f (duration: %.1fh)", i+1, r.Start, r.End, r.Duration)
		}
		return
	}

	// Verify the sleep period starts around 23:00 London time
	r := sleepRanges[0]
	if r.Start < 22.0 || r.Start > 23.5 {
		t.Errorf("Expected sleep to start around 23:00 London time, got %.1f", r.Start)
	}

	// Verify it ends in the morning (around 6:00-10:00)
	if r.End < 5.0 || r.End > 10.0 {
		t.Errorf("Expected sleep to end in morning (5:00-10:00), got %.1f", r.End)
	}

	t.Logf("Found sleep period: %.1f - %.1f", r.Start, r.End)

	// Debug output
	t.Logf("Total rest ranges found: %d", len(sleepRanges))
	for i, r := range sleepRanges {
		startHour := int(r.Start)
		startMin := int((r.Start - float64(startHour)) * 60)
		endHour := int(r.End)
		endMin := int((r.End - float64(endHour)) * 60)
		t.Logf("Rest period %d: %02d:%02d - %02d:%02d (%.1f hours)",
			i+1, startHour, startMin, endHour, endMin, r.Duration)
	}
}
