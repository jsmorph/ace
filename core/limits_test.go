package core

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
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

func TestValidateObjectArrayOK(t *testing.T) {
	lim := DefaultLimits()
	err := lim.ValidateObject([]byte(`{"a":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateObjectArrayTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectArrayLength = 2
	err := lim.ValidateObject([]byte(`{"a":[1,2,3]}`))
	if err == nil {
		t.Fatal("expected error for array exceeding ObjectArrayLength")
	}
	if !strings.Contains(err.Error(), "3 > 2 elements") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateObjectArrayLeafCount(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectLeaves = 2
	err := lim.ValidateObject([]byte(`{"a":[1,2,3]}`))
	if err == nil {
		t.Fatal("expected error: 3-element array should count as 3 leaves")
	}
	if !strings.Contains(err.Error(), "3 > 2 leaves") {
		t.Fatalf("unexpected error: %v", err)
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

func TestValidateObjectMetaValueOK(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectUnmatchableValueSize = 10
	err := lim.ValidateObject([]byte(`{"#id":"short","a":1}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateObjectMetaValueTooLarge(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectUnmatchableValueSize = 5
	err := lim.ValidateObject([]byte(`{"#id":"toolong","a":1}`))
	if err == nil {
		t.Fatal("expected error for oversized metadata value")
	}
	if !strings.Contains(err.Error(), "#id") {
		t.Fatalf("error should reference #id: %v", err)
	}
}

func TestValidateObjectMetaNoLeafCount(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectLeaves = 1
	err := lim.ValidateObject([]byte(`{"#id":"abc","a":1}`))
	if err != nil {
		t.Fatalf("# property should not count as a leaf: %v", err)
	}
}

func TestValidateObjectMetaNested(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectUnmatchableValueSize = 5
	err := lim.ValidateObject([]byte(`{"#meta":{"author":"toolong"}}`))
	if err == nil {
		t.Fatal("expected error for oversized nested metadata value")
	}
	if !strings.Contains(err.Error(), "#meta.author") {
		t.Fatalf("error should reference #meta.author: %v", err)
	}
}

func TestValidateObjectValueTooLarge(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectValueSize = 3
	err := lim.ValidateObject([]byte(`{"a":"toolong"}`))
	if err == nil {
		t.Fatal("expected error for oversized value")
	}
	if !strings.Contains(err.Error(), "\"a\"") {
		t.Fatalf("error should reference field: %v", err)
	}
}

func TestValidateObjectArrayValueTooLarge(t *testing.T) {
	lim := DefaultLimits()
	lim.ObjectValueSize = 3
	err := lim.ValidateObject([]byte(`{"a":["toolong"]}`))
	if err == nil {
		t.Fatal("expected error for oversized array element")
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

func TestValidatePatternTooLarge(t *testing.T) {
	lim := DefaultLimits()
	lim.PatternSize = 5
	err := lim.ValidatePattern([]byte(`{"a":1,"b":2}`))
	if err == nil {
		t.Fatal("expected error for oversized pattern")
	}
	if !strings.Contains(err.Error(), "pattern size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePatternAtomicTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.PatternAtomicLength = 3
	err := lim.ValidatePattern([]byte(`{"a":"toolong"}`))
	if err == nil {
		t.Fatal("expected error for oversized pattern value")
	}
}

func TestValidatePatternArrayAtomicTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.PatternAtomicLength = 3
	err := lim.ValidatePattern([]byte(`{"a":["toolong"]}`))
	if err == nil {
		t.Fatal("expected error for oversized pattern array element")
	}
}

func TestValidatePatternPropertyNameTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.PropertySize = 2
	err := lim.ValidatePattern([]byte(`{"abc":1}`))
	if err == nil {
		t.Fatal("expected error for oversized property name in pattern")
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

func TestLimitsMarshalJSON(t *testing.T) {
	data, err := json.Marshal(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	ttl, ok := raw["ttl_max"].(string)
	if !ok {
		t.Fatalf("ttl_max is %T, want string", raw["ttl_max"])
	}
	if ttl != "P7D" {
		t.Fatalf("ttl_max = %q, want %q", ttl, "P7D")
	}
}

func TestLimitsUnmarshalString(t *testing.T) {
	var l Limits
	if err := json.Unmarshal([]byte(`{"ttl_max":"P1D"}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.TTLMax != 24*time.Hour {
		t.Fatalf("TTLMax = %v, want 24h", l.TTLMax)
	}
}

func TestLimitsUnmarshalNanoseconds(t *testing.T) {
	var l Limits
	ns := int64(2 * 24 * time.Hour)
	data, _ := json.Marshal(map[string]int64{"ttl_max": ns})
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatal(err)
	}
	if l.TTLMax != 2*24*time.Hour {
		t.Fatalf("TTLMax = %v, want 48h", l.TTLMax)
	}
}

func TestLimitsRoundTrip(t *testing.T) {
	orig := DefaultLimits()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got Limits
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Fatalf("round-trip mismatch:\n  got:  %+v\n  want: %+v", got, orig)
	}
}

func TestLimitsOverlay(t *testing.T) {
	l := DefaultLimits()
	if err := json.Unmarshal([]byte(`{"object_size":512,"ttl_max":"P1D"}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.ObjectSize != 512 {
		t.Fatalf("ObjectSize = %d, want 512", l.ObjectSize)
	}
	if l.TTLMax != 24*time.Hour {
		t.Fatalf("TTLMax = %v, want 24h", l.TTLMax)
	}
	if l.PatternSize != 2048 {
		t.Fatalf("PatternSize = %d, want 2048 (default preserved)", l.PatternSize)
	}
}

func TestValidateAccessTooLarge(t *testing.T) {
	lim := DefaultLimits()
	lim.AccessSize = 10
	err := lim.ValidateAccess([]byte(`{"in":["alice","bob"]}`))
	if err == nil {
		t.Fatal("expected error for oversized access")
	}
	if !strings.Contains(err.Error(), "access size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAccessIDTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.IDSize = 3
	err := lim.ValidateAccess([]byte(`{"in":["toolong"]}`))
	if err == nil {
		t.Fatal("expected error for oversized access ID")
	}
	if !strings.Contains(err.Error(), "access in ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCallerIDTooLong(t *testing.T) {
	lim := DefaultLimits()
	lim.IDSize = 3
	err := lim.ValidateCallerID("toolong")
	if err == nil {
		t.Fatal("expected error for oversized caller ID")
	}
	if !strings.Contains(err.Error(), "caller ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLimitsJSON(t *testing.T) {
	data, err := os.ReadFile("../limits.json")
	if err != nil {
		t.Fatal(err)
	}
	var got Limits
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != DefaultLimits() {
		t.Fatalf("limits.json does not match DefaultLimits():\n  got:  %+v\n  want: %+v", got, DefaultLimits())
	}
}
