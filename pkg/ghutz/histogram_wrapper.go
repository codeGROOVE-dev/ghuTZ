package ghutz

import (
	"github.com/codeGROOVE-dev/ghuTZ/pkg/histogram"
)

// GenerateHistogram creates a visual representation of user activity.
func GenerateHistogram(result *Result, hourCounts map[int]int, timezone string) string {
	// Convert ghutz.Result to histogram.Result
	histResult := &histogram.Result{
		HalfHourlyActivityUTC:      result.HalfHourlyActivityUTC,
		HourlyOrganizationActivity: result.HourlyOrganizationActivity,
		TopOrganizations:           convertOrgActivities(result.TopOrganizations),
		QuietHoursUTC:              result.QuietHoursUTC,
		SleepBucketsUTC:            result.SleepBucketsUTC,
		PeakProductivity:           convertPeakProductivity(result.PeakProductivity),
		LunchHoursUTC:              convertLunchBreak(result.LunchHoursUTC),
	}
	
	return histogram.GenerateHistogram(histResult, hourCounts, timezone)
}

func convertOrgActivities(orgs []OrgActivity) []histogram.OrgActivity {
	result := make([]histogram.OrgActivity, len(orgs))
	for i, org := range orgs {
		result[i] = histogram.OrgActivity{
			Name:  org.Name,
			Count: org.Count,
		}
	}
	return result
}

func convertPeakProductivity(peak PeakTime) histogram.PeakProductivity {
	return histogram.PeakProductivity{
		Start: peak.Start,
		End:   peak.End,
		Count: peak.Count,
	}
}

func convertLunchBreak(lunch LunchBreak) histogram.LunchBreak {
	return histogram.LunchBreak{
		Start:      lunch.Start,
		End:        lunch.End,
		Confidence: lunch.Confidence,
	}
}