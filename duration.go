package ace

import (
	"fmt"
	"time"
)

// ParseISO8601Duration parses a subset of ISO 8601 durations (P1D, PT1H30M, etc.)
// into a time.Duration.
func ParseISO8601Duration(s string) (time.Duration, error) {
	if len(s) == 0 || s[0] != 'P' {
		return 0, fmt.Errorf("duration must start with P: %q", s)
	}
	s = s[1:]
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}

	var d time.Duration
	inTime := false

	for len(s) > 0 {
		if s[0] == 'T' {
			inTime = true
			s = s[1:]
			if len(s) == 0 {
				return 0, fmt.Errorf("T with no time components")
			}
			continue
		}

		n, rest, err := parseLeadingInt(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}

		if len(rest) == 0 {
			return 0, fmt.Errorf("missing unit designator after %d", n)
		}

		unit := rest[0]
		s = rest[1:]

		switch {
		case !inTime && unit == 'D':
			d += time.Duration(n) * 24 * time.Hour
		case inTime && unit == 'H':
			d += time.Duration(n) * time.Hour
		case inTime && unit == 'M':
			d += time.Duration(n) * time.Minute
		case inTime && unit == 'S':
			d += time.Duration(n) * time.Second
		default:
			return 0, fmt.Errorf("unexpected unit %c (inTime=%v)", unit, inTime)
		}
	}

	return d, nil
}

func parseLeadingInt(s string) (int64, string, error) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, s, fmt.Errorf("expected digit, got %q", s)
	}
	var n int64
	for _, c := range s[:i] {
		n = n*10 + int64(c-'0')
	}
	return n, s[i:], nil
}
