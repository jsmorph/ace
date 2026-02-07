package ace

import (
	"encoding/json"
	"fmt"
	"time"
)

type Limits struct {
	ObjectSize          int           `json:"object_size"`
	PropertySize        int           `json:"property_size"`
	ObjectValueSize     int           `json:"object_value_size"`
	ObjectLeaves        int           `json:"object_leaves"`
	PatternSize         int           `json:"pattern_size"`
	PatternLeaves       int           `json:"pattern_leaves"`
	PatternArrayLength  int           `json:"pattern_array_length"`
	PatternAtomicLength int           `json:"pattern_atomic_length"`
	AccessSize          int           `json:"access_size"`
	AccessLength        int           `json:"access_length"`
	TTLMax              time.Duration `json:"ttl_max"`
	IDSize              int           `json:"id_size"`
}

func DefaultLimits() Limits {
	return Limits{
		ObjectSize:          2048,
		PropertySize:        64,
		ObjectValueSize:     128,
		ObjectLeaves:        8,
		PatternSize:         2048,
		PatternLeaves:       4,
		PatternArrayLength:  4,
		PatternAtomicLength: 128,
		AccessSize:          1024,
		AccessLength:        16,
		TTLMax:              7 * 24 * time.Hour,
		IDSize:              128,
	}
}

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
		if len(k) > l.PropertySize {
			return 0, fmt.Errorf("property name %q is %d > %d bytes", k, len(k), l.PropertySize)
		}
		fullPath := k
		if path != "" {
			fullPath = path + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			n, err := l.walkObject(val, fullPath)
			if err != nil {
				return 0, err
			}
			leaves += n
		case []interface{}:
			return 0, fmt.Errorf("arrays are not permitted in object values (at %q)", fullPath)
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

func (l Limits) ValidateAccess(raw []byte) error {
	if len(raw) > l.AccessSize {
		return fmt.Errorf("access size is %d > %d bytes", len(raw), l.AccessSize)
	}
	var acc Access
	if err := json.Unmarshal(raw, &acc); err != nil {
		return fmt.Errorf("access is not valid JSON: %w", err)
	}
	total := len(acc.In) + len(acc.Rd)
	if total > l.AccessLength {
		return fmt.Errorf("access has %d > %d identifiers", total, l.AccessLength)
	}
	for _, id := range acc.In {
		if len(id) > l.IDSize {
			return fmt.Errorf("access in ID is %d > %d bytes", len(id), l.IDSize)
		}
	}
	for _, id := range acc.Rd {
		if len(id) > l.IDSize {
			return fmt.Errorf("access rd ID is %d > %d bytes", len(id), l.IDSize)
		}
	}
	return nil
}

func (l Limits) ValidateTTL(d time.Duration) error {
	if d > l.TTLMax {
		return fmt.Errorf("ttl %v exceeds maximum %v", d, l.TTLMax)
	}
	if d <= 0 {
		return fmt.Errorf("ttl must be positive")
	}
	return nil
}

func (l Limits) ValidateCallerID(id string) error {
	if len(id) > l.IDSize {
		return fmt.Errorf("caller ID is %d > %d bytes", len(id), l.IDSize)
	}
	return nil
}

type Access struct {
	In []string `json:"in,omitempty"`
	Rd []string `json:"rd,omitempty"`
}
