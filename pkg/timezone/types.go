package timezone

// Candidate represents a timezone detection result with evidence.
type Candidate struct {
	Timezone            string   `json:"timezone"`
	ScoringDetails      []string `json:"scoring_details"`
	LunchStartUTC       float64  `json:"lunch_start_utc"`
	Confidence          float64  `json:"confidence"`
	Offset              float64  `json:"offset"`
	LunchConfidence     float64  `json:"lunch_confidence"`
	LunchEndUTC         float64  `json:"lunch_end_utc"`
	EveningActivity     int      `json:"evening_activity"`
	LunchLocalTime      float64  `json:"lunch_local_time"`
	WorkStartLocal      float64  `json:"work_start_local"`
	SleepMidLocal       float64  `json:"sleep_mid_local"`
	LunchDipStrength    float64  `json:"lunch_dip_strength"`
	PeakTimeReasonable  bool     `json:"peak_time_reasonable"`
	SleepReasonable     bool     `json:"sleep_reasonable"`
	WorkHoursReasonable bool     `json:"work_hours_reasonable"`
	LunchReasonable     bool     `json:"lunch_reasonable"`
	IsProfile           bool     `json:"is_profile"` // True if this matches the user's profile timezone
}
