package core

import (
	"encoding/json"
	"testing"
)

func TestMatchSimple(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}
}

func TestMatchNoMatch(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no match")
	}
}

func TestMatchExtraFields(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match: extra fields should not prevent matching")
	}
}

func TestMatchArrayPattern(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match: array pattern should match any alternative")
	}
}

func TestMatchEmptyPattern(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":1}`), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match: empty pattern matches everything")
	}
}

func TestMatchNested(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":{"b":1}}`), json.RawMessage(`{"a":{"b":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}
}

func TestMatchHashPropertyIgnored(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"#tag":"x","a":1}`), json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match: # properties should be ignored in object")
	}
}

func TestMatchHashInPatternError(t *testing.T) {
	_, err := Match(json.RawMessage(`{"a":1}`), json.RawMessage(`{"#tag":"x"}`))
	if err == nil {
		t.Fatal("expected error for # property in pattern")
	}
}

func TestMatchObjectArray(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":[1,2,3]}`), json.RawMessage(`{"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match: object array element should match atomic pattern")
	}
}

func TestMatchObjectArrayPatternArray(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":[1,2,3]}`), json.RawMessage(`{"a":[2,4]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match: object array and pattern array should intersect")
	}
}

func TestMatchObjectArrayNoMatch(t *testing.T) {
	ok, err := Match(json.RawMessage(`{"a":[1,2,3]}`), json.RawMessage(`{"a":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no match")
	}
}

// Quamina supports operators like prefix, suffix, anything-but, numeric, and
// exists. ACE does not expose these. Nested objects in patterns become
// structural (path-based) matches, and objects inside pattern arrays are
// rejected. These tests verify that Quamina operators have no special meaning
// in ACE patterns.
func TestMatchQuaminaOperatorsNotSupported(t *testing.T) {
	type tc struct {
		name    string
		object  string
		pattern string
		match   bool
		wantErr bool
	}
	cases := []tc{
		{
			name:    "prefix-nested",
			object:  `{"field":"food"}`,
			pattern: `{"field":{"prefix":"fo"}}`,
			match:   false,
		},
		{
			name:    "prefix-nested-structural",
			object:  `{"field":{"prefix":"fo"}}`,
			pattern: `{"field":{"prefix":"fo"}}`,
			match:   true,
		},
		{
			name:    "prefix-in-array",
			object:  `{"field":"food"}`,
			pattern: `{"field":[{"prefix":"fo"}]}`,
			wantErr: true,
		},
		{
			name:    "suffix-nested",
			object:  `{"field":"running"}`,
			pattern: `{"field":{"suffix":"ing"}}`,
			match:   false,
		},
		{
			name:    "anything-but-nested",
			object:  `{"field":"y"}`,
			pattern: `{"field":{"anything-but":"x"}}`,
			match:   false,
		},
		{
			name:    "numeric-nested",
			object:  `{"field":50}`,
			pattern: `{"field":{"numeric":[">",0,"<=",100]}}`,
			match:   false,
		},
		{
			name:    "exists-nested",
			object:  `{"field":"hello"}`,
			pattern: `{"field":{"exists":true}}`,
			match:   false,
		},
		{
			name:    "exists-nested-structural",
			object:  `{"field":{"exists":true}}`,
			pattern: `{"field":{"exists":true}}`,
			match:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, err := Match(json.RawMessage(c.object), json.RawMessage(c.pattern))
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok != c.match {
				t.Fatalf("expected match=%v, got %v", c.match, ok)
			}
		})
	}
}
