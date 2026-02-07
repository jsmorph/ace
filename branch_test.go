package ace

import (
	"sort"
	"testing"
)

func TestExtractBranchesSimple(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != "a=1" {
		t.Fatalf("got %v", branches)
	}
}

func TestExtractBranchesMultiple(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":1,"b":"hello","c":true,"d":null}`))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(branches)
	want := []string{`a=1`, `b="hello"`, `c=true`, `d=null`}
	sort.Strings(want)
	if len(branches) != len(want) {
		t.Fatalf("got %v, want %v", branches, want)
	}
	for i := range branches {
		if branches[i] != want[i] {
			t.Fatalf("got %v, want %v", branches, want)
		}
	}
}

func TestExtractBranchesNested(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":{"b":1,"c":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(branches)
	want := []string{"a.b=1", "a.c=2"}
	sort.Strings(want)
	if len(branches) != len(want) {
		t.Fatalf("got %v, want %v", branches, want)
	}
	for i := range branches {
		if branches[i] != want[i] {
			t.Fatalf("got %v, want %v", branches, want)
		}
	}
}

func TestExtractBranchesDeepNest(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":{"b":{"c":3}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != "a.b.c=3" {
		t.Fatalf("got %v", branches)
	}
}

func TestExtractBranchesEscaping(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a.b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != `a\.b=1` {
		t.Fatalf("got %v", branches)
	}
}

func TestExtractBranchesStringEscaping(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":"he said \"hi\""}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != `a="he said \"hi\""` {
		t.Fatalf("got %v, want %v", branches, `a="he said \"hi\""`)
	}
}

func TestExtractBranchesArray(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":[1,2,3]}`))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(branches)
	want := []string{"a=1", "a=2", "a=3"}
	if len(branches) != len(want) {
		t.Fatalf("got %v, want %v", branches, want)
	}
	for i := range branches {
		if branches[i] != want[i] {
			t.Fatalf("got %v, want %v", branches, want)
		}
	}
}

func TestExtractBranchesArrayRejectsNestedObject(t *testing.T) {
	_, err := ExtractBranches([]byte(`{"a":[{"b":1}]}`))
	if err == nil {
		t.Fatal("expected error for nested object in array")
	}
}

func TestExtractBranchesArrayRejectsNestedArray(t *testing.T) {
	_, err := ExtractBranches([]byte(`{"a":[[1]]}`))
	if err == nil {
		t.Fatal("expected error for nested array in array")
	}
}

func TestExtractBranchesNumberNormalization(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":1.0}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != "a=1" {
		t.Fatalf("got %v, want [a=1]", branches)
	}
}

func TestExtractBranchesSkipsHashProperty(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"#meta":"x","a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != "a=1" {
		t.Fatalf("expected [a=1], got %v", branches)
	}
}

func TestExtractBranchesSkipsNestedHashProperty(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{"a":{"#inner":"x","b":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0] != "a.b=1" {
		t.Fatalf("expected [a.b=1], got %v", branches)
	}
}

func TestExtractBranchesEmpty(t *testing.T) {
	branches, err := ExtractBranches([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 0 {
		t.Fatalf("expected no branches, got %v", branches)
	}
}
