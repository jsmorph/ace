package ace

import (
	"strings"
	"testing"
)

func TestValidateObjectOK(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidateObject([]byte(`{"a":1,"b":"hello","c":true}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateObjectNested(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidateObject([]byte(`{"a":{"b":1,"c":2}}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateObjectTooLarge(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectSize = 10
	err := lim.ValidateObject([]byte(`{"a":1,"b":2}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "object size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateObjectTooManyLeaves(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectLeaves = 2
	err := lim.ValidateObject([]byte(`{"a":1,"b":2,"c":3}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "3 > 2 leaves") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateObjectArray(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidateObject([]byte(`{"a":[1,2]}`))
	if err == nil {
		t.Fatal("expected error for array in object value")
	}
}

func TestValidateObjectPropertyNameTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.PropertySize = 2
	err := lim.ValidateObject([]byte(`{"abc":1}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidatePatternOK(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidatePattern([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidatePatternArray(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidatePattern([]byte(`{"a":[1,2,3]}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidatePatternTooManyLeaves(t *testing.T) {
	lim := DefaultLimits()
	lim.PatternLeaves = 2
	err := lim.ValidatePattern([]byte(`{"a":1,"b":2,"c":3}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidatePatternArrayTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.PatternArrayLength = 2
	err := lim.ValidatePattern([]byte(`{"a":[1,2,3]}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateAccessOK(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidateAccess([]byte(`{"in":["a","b"],"rd":["c"]}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateAccessTooMany(t *testing.T) {
	lim := DefaultLimits()
	lim.AccessLength = 2
	err := lim.ValidateAccess([]byte(`{"in":["a","b","c"]}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateTTL(t *testing.T) {
	lim := DefaultLimits()
	if err := lim.ValidateTTL(24 * 3600e9); err != nil { // 1 day in nanoseconds = time.Duration
		t.Fatal(err)
	}
	if err := lim.ValidateTTL(lim.TTLMax + 1); err == nil {
		t.Fatal("expected error for TTL exceeding max")
	}
	if err := lim.ValidateTTL(0); err == nil {
		t.Fatal("expected error for zero TTL")
	}
}
