package timezone

// TimezoneCandidate represents a timezone detection result with evidence.
type TimezoneCandidate struct {
	Timezone         string  `json:"timezone"`
	Offset           float64 `json:"offset"` // UTC offset in hours (e.g., -5, 5.5, 5.75)
	Confidence       float64 `json:"confidence"`
	EveningActivity  int     `json:"evening_activity"`
	LunchReasonable  bool    `json:"lunch_reasonable"`
	WorkHoursNormal  bool    `json:"work_hours_normal"`
	LunchLocalTime   float64 `json:"lunch_local_time"`   // Local time of detected lunch (e.g., 12.5 = 12:30pm)
	WorkStartLocal   int     `json:"work_start_local"`   // Local hour when work starts
	SleepMidLocal    float64 `json:"sleep_mid_local"`     // Local time of mid-sleep point
	LunchDipStrength float64 `json:"lunch_dip_strength"`  // Percentage of activity drop during lunch
	LunchStartUTC    float64 `json:"lunch_start_utc"`    // UTC time of lunch start (for reuse)
	LunchEndUTC      float64 `json:"lunch_end_utc"`      // UTC time of lunch end (for reuse)
	LunchConfidence  float64 `json:"lunch_confidence"`   // Confidence of lunch detection (for reuse)
}