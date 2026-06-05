package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MarshalJSON encodes Limits to JSON with TTLMax as an ISO 8601 duration string.
func (l Limits) MarshalJSON() ([]byte, error) {
	type alias Limits
	return json.Marshal(struct {
		alias
		TTLMax string `json:"ttl_max"`
	}{
		alias:  alias(l),
		TTLMax: FormatISO8601Duration(l.TTLMax),
	})
}

// UnmarshalJSON decodes Limits from JSON, accepting TTLMax as either an
// ISO 8601 duration string (e.g., "P7D") or a number of nanoseconds.
func (l *Limits) UnmarshalJSON(data []byte) error {
	type alias Limits
	wire := struct {
		alias
		TTLMax json.RawMessage `json:"ttl_max"`
	}{
		alias: alias(*l),
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*l = Limits(wire.alias)

	if len(wire.TTLMax) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(wire.TTLMax, &s) == nil {
		d, err := ParseISO8601Duration(s)
		if err != nil {
			return fmt.Errorf("invalid ttl_max %q: %w", s, err)
		}
		l.TTLMax = d
		return nil
	}
	var n int64
	if err := json.Unmarshal(wire.TTLMax, &n); err != nil {
		return fmt.Errorf("ttl_max must be a duration string or nanoseconds")
	}
	l.TTLMax = time.Duration(n)
	return nil
}

// ValidationError indicates a client-supplied value violates a constraint.
type ValidationError struct {
	Err error
}

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

func ValidationErr(err error) error {
	if err == nil {
		return nil
	}
	return &ValidationError{Err: err}
}

func validationErr(err error) error {
	return ValidationErr(err)
}

// Limits constrains object, pattern, access, and TTL parameters.
type Limits struct {
	ObjectSize                 int           `json:"object_size"`
	PropertySize               int           `json:"property_size"`
	ObjectValueSize            int           `json:"object_value_size"`
	ObjectUnmatchableValueSize int           `json:"object_unmatchable_value_size"`
	ObjectLeaves               int           `json:"object_leaves"`
	ObjectArrayLength          int           `json:"object_array_length"`
	PatternSize                int           `json:"pattern_size"`
	PatternLeaves              int           `json:"pattern_leaves"`
	PatternArrayLength         int           `json:"pattern_array_length"`
	PatternAtomicLength        int           `json:"pattern_atomic_length"`
	AccessSize                 int           `json:"access_size"`
	AccessLength               int           `json:"access_length"`
	TTLMax                     time.Duration `json:"ttl_max"`
	IDSize                     int           `json:"id_size"`
}

// DefaultLimits returns the default limits.
func DefaultLimits() Limits {
	return Limits{
		ObjectSize:                 2048,
		PropertySize:               64,
		ObjectValueSize:            128,
		ObjectUnmatchableValueSize: 256,
		ObjectLeaves:               8,
		ObjectArrayLength:          4,
		PatternSize:                2048,
		PatternLeaves:              4,
		PatternArrayLength:         4,
		PatternAtomicLength:        128,
		AccessSize:                 1024,
		AccessLength:               16,
		TTLMax:                     7 * 24 * time.Hour,
		IDSize:                     128,
	}
}

// ValidateObject checks an object against size, leaf, and array limits.
func (l Limits) ValidateObject(raw []byte) error {
	if len(raw) > l.ObjectSize {
		return fmt.Errorf("object size is %d > %d bytes", len(raw), l.ObjectSize)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("object is not valid JSON: %w", err)
	}
	leaves, err := l.walkObject(obj, "")
	if err != nil {
		return err
	}
	if leaves > l.ObjectLeaves {
		return fmt.Errorf("object has %d > %d leaves", leaves, l.ObjectLeaves)
	}
	return nil
}

func (l Limits) walkObject(obj map[string]interface{}, path string) (int, error) {
	leaves := 0
	for k, v := range obj {
		fullPath := k
		if path != "" {
			fullPath = path + "." + k
		}
		if strings.HasPrefix(k, "#") {
			if err := l.walkMeta(v, fullPath); err != nil {
				return 0, err
			}
			continue
		}
		if len(k) > l.PropertySize {
			return 0, fmt.Errorf("property name %q is %d > %d bytes", k, len(k), l.PropertySize)
		}
		switch val := v.(type) {
		case map[string]interface{}:
			n, err := l.walkObject(val, fullPath)
			if err != nil {
				return 0, err
			}
			leaves += n
		case []interface{}:
			if len(val) > l.ObjectArrayLength {
				return 0, fmt.Errorf("object array at %q has %d > %d elements", fullPath, len(val), l.ObjectArrayLength)
			}
			for i, elem := range val {
				if _, ok := elem.(map[string]interface{}); ok {
					return 0, fmt.Errorf("object array at %q[%d] contains an object", fullPath, i)
				}
				if _, ok := elem.([]interface{}); ok {
					return 0, fmt.Errorf("object array at %q[%d] contains an array", fullPath, i)
				}
				ser, err := json.Marshal(elem)
				if err != nil {
					return 0, fmt.Errorf("cannot serialize value at %q[%d]: %w", fullPath, i, err)
				}
				if len(ser) > l.ObjectValueSize {
					return 0, fmt.Errorf("value at %q[%d] is %d > %d bytes", fullPath, i, len(ser), l.ObjectValueSize)
				}
			}
			leaves += len(val)
		default:
			ser, err := json.Marshal(v)
			if err != nil {
				return 0, fmt.Errorf("cannot serialize value at %q: %w", fullPath, err)
			}
			if len(ser) > l.ObjectValueSize {
				return 0, fmt.Errorf("value at %q is %d > %d bytes", fullPath, len(ser), l.ObjectValueSize)
			}
			leaves++
		}
	}
	return leaves, nil
}

func (l Limits) walkMeta(v interface{}, path string) error {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if err := l.walkMeta(child, path+"."+k); err != nil {
				return err
			}
		}
	case []interface{}:
		for i, elem := range val {
			if err := l.walkMeta(elem, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		ser, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("cannot serialize value at %q: %w", path, err)
		}
		if len(ser) > l.ObjectUnmatchableValueSize {
			return fmt.Errorf("value at %q is %d > %d bytes", path, len(ser), l.ObjectUnmatchableValueSize)
		}
	}
	return nil
}

// ValidatePattern checks a pattern against size, leaf, and array limits.
func (l Limits) ValidatePattern(raw []byte) error {
	if len(raw) > l.PatternSize {
		return fmt.Errorf("pattern size is %d > %d bytes", len(raw), l.PatternSize)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("pattern is not valid JSON: %w", err)
	}
	leaves, err := l.walkPattern(obj, "")
	if err != nil {
		return err
	}
	if leaves > l.PatternLeaves {
		return fmt.Errorf("pattern has %d > %d leaves", leaves, l.PatternLeaves)
	}
	return nil
}

func (l Limits) walkPattern(obj map[string]interface{}, path string) (int, error) {
	leaves := 0
	for k, v := range obj {
		if strings.HasPrefix(k, "#") {
			return 0, fmt.Errorf("pattern property %q starts with #; metadata properties are not matchable", k)
		}
		if len(k) > l.PropertySize {
			return 0, fmt.Errorf("property name %q is %d > %d bytes", k, len(k), l.PropertySize)
		}
		fullPath := k
		if path != "" {
			fullPath = path + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			n, err := l.walkPattern(val, fullPath)
			if err != nil {
				return 0, err
			}
			leaves += n
		case []interface{}:
			if len(val) > l.PatternArrayLength {
				return 0, fmt.Errorf("pattern array at %q has %d > %d elements", fullPath, len(val), l.PatternArrayLength)
			}
			for i, elem := range val {
				if _, ok := elem.(map[string]interface{}); ok {
					return 0, fmt.Errorf("pattern array at %q[%d] contains an object", fullPath, i)
				}
				if _, ok := elem.([]interface{}); ok {
					return 0, fmt.Errorf("pattern array at %q[%d] contains an array", fullPath, i)
				}
				ser, err := json.Marshal(elem)
				if err != nil {
					return 0, fmt.Errorf("cannot serialize pattern value at %q[%d]: %w", fullPath, i, err)
				}
				if len(ser) > l.PatternAtomicLength {
					return 0, fmt.Errorf("pattern value at %q[%d] is %d > %d bytes", fullPath, i, len(ser), l.PatternAtomicLength)
				}
			}
			leaves++
		default:
			ser, err := json.Marshal(v)
			if err != nil {
				return 0, fmt.Errorf("cannot serialize pattern value at %q: %w", fullPath, err)
			}
			if len(ser) > l.PatternAtomicLength {
				return 0, fmt.Errorf("pattern value at %q is %d > %d bytes", fullPath, len(ser), l.PatternAtomicLength)
			}
			leaves++
		}
	}
	return leaves, nil
}

// ValidateAccess checks an access value against size, length, and semantic limits.
func (l Limits) ValidateAccess(raw []byte) error {
	if len(raw) > l.AccessSize {
		return fmt.Errorf("access size is %d > %d bytes", len(raw), l.AccessSize)
	}
	var acc Access
	if err := json.Unmarshal(raw, &acc); err != nil {
		return fmt.Errorf("access is not valid JSON: %w", err)
	}
	if acc.In != nil && len(acc.In) == 0 && acc.Rd != nil && len(acc.Rd) == 0 {
		return fmt.Errorf("access denies both in and rd; object would be inaccessible")
	}
	total := len(acc.In) + len(acc.Rd)
	if total > l.AccessLength {
		return fmt.Errorf("access has %d > %d identifiers", total, l.AccessLength)
	}
	for _, id := range acc.In {
		if id == "" {
			return fmt.Errorf("access identity must not be empty")
		}
		if id == "!" {
			return fmt.Errorf("access in ID %q is reserved", id)
		}
		if len(id) > l.IDSize {
			return fmt.Errorf("access in ID is %d > %d bytes", len(id), l.IDSize)
		}
	}
	for _, id := range acc.Rd {
		if id == "" {
			return fmt.Errorf("access identity must not be empty")
		}
		if id == "!" {
			return fmt.Errorf("access rd ID %q is reserved", id)
		}
		if len(id) > l.IDSize {
			return fmt.Errorf("access rd ID is %d > %d bytes", len(id), l.IDSize)
		}
	}
	return nil
}

// ValidateTTL checks that a TTL is positive and within the maximum.
func (l Limits) ValidateTTL(d time.Duration) error {
	if d > l.TTLMax {
		return fmt.Errorf("ttl %v exceeds maximum %v", d, l.TTLMax)
	}
	if d <= 0 {
		return fmt.Errorf("ttl must be positive")
	}
	return nil
}

// ValidateCallerID checks that a caller ID is within the size limit and not reserved.
func (l Limits) ValidateCallerID(id string) error {
	if id == "!" {
		return fmt.Errorf("caller ID %q is reserved", id)
	}
	if len(id) > l.IDSize {
		return fmt.Errorf("caller ID is %d > %d bytes", len(id), l.IDSize)
	}
	return nil
}

// Access restricts which callers may retrieve an object. A nil slice means
// unrestricted; an empty non-nil slice means no caller is permitted.
type Access struct {
	In []string `json:"in"`
	Rd []string `json:"rd"`
}
