package tzconvert

import (
	"math"
	"testing"
)

func TestUTCToLocal(t *testing.T) {
	tests := []struct {
		name      string
		utcHour   float64
		utcOffset int
		want      float64
	}{
		// Eastern Time (UTC-4)
		{"EDT noon UTC to 8am local", 12.0, -4, 8.0},
		{"EDT 15:30 UTC to 11:30 local", 15.5, -4, 11.5},
		{"EDT 22:30 UTC to 18:30 local", 22.5, -4, 18.5},
		{"EDT midnight wrap", 2.0, -4, 22.0}, // 2am UTC = 10pm previous day
		
		// Pacific Time (UTC-7)
		{"PDT noon UTC to 5am local", 12.0, -7, 5.0},
		{"PDT wrap around midnight", 4.0, -7, 21.0},
		
		// China Time (UTC+8)
		{"CST 2am UTC to 10am local", 2.0, 8, 10.0},
		{"CST wrap past midnight", 20.0, 8, 4.0}, // 8pm UTC = 4am next day
		
		// GMT (UTC+0)
		{"GMT no change", 12.0, 0, 12.0},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UTCToLocal(tt.utcHour, tt.utcOffset)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("UTCToLocal(%v, %v) = %v, want %v", 
					tt.utcHour, tt.utcOffset, got, tt.want)
			}
		})
	}
}

func TestLocalToUTC(t *testing.T) {
	tests := []struct {
		name       string
		localHour  float64
		utcOffset  int
		want       float64
	}{
		// Eastern Time (UTC-4)
		{"EDT 8am local to noon UTC", 8.0, -4, 12.0},
		{"EDT 11:30 local to 15:30 UTC", 11.5, -4, 15.5},
		{"EDT 18:30 local to 22:30 UTC", 18.5, -4, 22.5},
		{"EDT 22:00 local to 2:00 UTC next day", 22.0, -4, 2.0},
		
		// Pacific Time (UTC-7)
		{"PDT 5am local to noon UTC", 5.0, -7, 12.0},
		{"PDT 21:00 local to 4:00 UTC", 21.0, -7, 4.0},
		
		// China Time (UTC+8)
		{"CST 10am local to 2am UTC", 10.0, 8, 2.0},
		{"CST 4am local to 8pm UTC previous day", 4.0, 8, 20.0},
		
		// GMT (UTC+0)
		{"GMT no change", 12.0, 0, 12.0},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LocalToUTC(tt.localHour, tt.utcOffset)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("LocalToUTC(%v, %v) = %v, want %v",
					tt.localHour, tt.utcOffset, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that converting UTC->Local->UTC gives back the original
	hours := []float64{0, 6, 12, 18, 23.5}
	offsets := []int{-11, -7, -4, 0, 3, 8, 12}
	
	for _, hour := range hours {
		for _, offset := range offsets {
			local := UTCToLocal(hour, offset)
			backToUTC := LocalToUTC(local, offset)
			if math.Abs(backToUTC-hour) > 0.01 {
				t.Errorf("Round trip failed: UTC %v -> Local %v -> UTC %v (offset %v)",
					hour, local, backToUTC, offset)
			}
		}
	}
}

func TestParseTimezoneOffset(t *testing.T) {
	tests := []struct {
		timezone string
		want     int
	}{
		{"UTC-4", -4},
		{"UTC-7", -7},
		{"UTC+8", 8},
		{"UTC+0", 0},
		{"UTC", 0},
		{"UTC-10", -10},
		{"UTC+12", 12},
		{"America/New_York", -4}, // IANA timezone (currently EDT)
		{"", 0},                 // Empty string
	}
	
	for _, tt := range tests {
		t.Run(tt.timezone, func(t *testing.T) {
			got := ParseTimezoneOffset(tt.timezone)
			if got != tt.want {
				t.Errorf("ParseTimezoneOffset(%q) = %v, want %v",
					tt.timezone, got, tt.want)
			}
		})
	}
}