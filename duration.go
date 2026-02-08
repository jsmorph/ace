package ace

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// FormatISO8601Duration formats a time.Duration as an ISO 8601 duration string
// (e.g., "P7D", "PT1H30M"). Sub-second precision is discarded.
func FormatISO8601Duration(d time.Duration) string {
	if d == 0 {
		return "PT0S"
	}

	var b strings.Builder
	b.WriteByte('P')

	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	if days > 0 {
		fmt.Fprintf(&b, "%dD", days)
	}

	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	if hours > 0 || minutes > 0 || seconds > 0 {
		b.WriteByte('T')
		if hours > 0 {
			fmt.Fprintf(&b, "%dH", hours)
		}
		if minutes > 0 {
			fmt.Fprintf(&b, "%dM", minutes)
		}
		if seconds > 0 {
			fmt.Fprintf(&b, "%dS", seconds)
		}
	}

	return b.String()
}

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

// ParseWait parses a wait duration from a string. It accepts three formats:
// ISO 8601 durations (e.g. "PT10S"), Go durations (e.g. "10s"), and bare
// integers interpreted as seconds (e.g. "10").
func ParseWait(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty wait value")
	}
	if d, err := ParseISO8601Duration(s); err == nil {
		return d, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	n, rest, err := parseLeadingInt(s)
	if err == nil && rest == "" {
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid wait %q: expected ISO 8601 duration, Go duration, or integer seconds", s)
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
		if n > (math.MaxInt64-9)/10 {
			return 0, s, fmt.Errorf("number too large")
		}
		n = n*10 + int64(c-'0')
	}
	return n, s[i:], nil
}
