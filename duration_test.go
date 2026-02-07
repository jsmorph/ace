package ace

import (
	"testing"
	"time"
)

func TestParseISO8601Duration(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"P3D", 3 * 24 * time.Hour},
		{"P1D", 24 * time.Hour},
		{"P7D", 7 * 24 * time.Hour},
		{"PT1H", time.Hour},
		{"PT30M", 30 * time.Minute},
		{"PT30S", 30 * time.Second},
		{"P1DT12H", 36 * time.Hour},
		{"PT1H30M", 90 * time.Minute},
		{"P2DT3H4M5S", 2*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second},
		{"PT0S", 0},
	}

	for _, c := range cases {
		got, err := ParseISO8601Duration(c.input)
		if err != nil {
			t.Errorf("ParseISO8601Duration(%q) error: %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseISO8601Duration(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestParseISO8601DurationErrors(t *testing.T) {
	cases := []string{
		"",
		"3D",
		"P",
		"PT",
		"PD",
		"P1Y",
		"P1M",
		"PH",
	}

	for _, c := range cases {
		_, err := ParseISO8601Duration(c)
		if err == nil {
			t.Errorf("ParseISO8601Duration(%q) expected error, got nil", c)
		}
	}
}
