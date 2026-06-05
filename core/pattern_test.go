package core

import (
	"sort"
	"strings"
	"testing"
	"time"
)

func TestExtractPatternBranchesAtomic(t *testing.T) {
	pbs, err := ExtractPatternBranches([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pbs) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(pbs))
	}
	if len(pbs[0].Alternatives) != 1 || pbs[0].Alternatives[0] != "a=1" {
		t.Fatalf("got %v", pbs[0].Alternatives)
	}
}

func TestExtractPatternBranchesArray(t *testing.T) {
	pbs, err := ExtractPatternBranches([]byte(`{"a":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pbs) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(pbs))
	}
	alts := pbs[0].Alternatives
	sort.Strings(alts)
	if len(alts) != 2 || alts[0] != "a=1" || alts[1] != "a=2" {
		t.Fatalf("got %v", alts)
	}
}

func TestExtractPatternBranchesNested(t *testing.T) {
	pbs, err := ExtractPatternBranches([]byte(`{"a":{"b":1,"c":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pbs) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(pbs))
	}
	all := make([]string, 0)
	for _, pb := range pbs {
		all = append(all, pb.Alternatives...)
	}
	sort.Strings(all)
	if all[0] != "a.b=1" || all[1] != "a.c=2" {
		t.Fatalf("got %v", all)
	}
}

func TestExtractPatternBranchesEmpty(t *testing.T) {
	pbs, err := ExtractPatternBranches([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pbs) != 0 {
		t.Fatalf("expected 0 branches, got %d", len(pbs))
	}
}

func TestBuildMatchQuerySimple(t *testing.T) {
	pbs := []PatternBranch{{Alternatives: []string{"a=1"}}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	query, args := BuildMatchQuery(pbs, "in", "agent-1", "", now)

	if !strings.Contains(query, "br.b = ?") {
		t.Fatalf("expected single-value match, got: %s", query)
	}
	if !strings.Contains(query, "NOT EXISTS") {
		t.Fatalf("expected access check, got: %s", query)
	}
	// 3 base (expires, invisible_until, since) + 1 branch + 3 access (type, type, callerID) = 7
	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}
}

func TestBuildMatchQueryArrayLeaf(t *testing.T) {
	pbs := []PatternBranch{{Alternatives: []string{"a=1", "a=2"}}}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	query, args := BuildMatchQuery(pbs, "rd", "agent-1", "", now)

	if !strings.Contains(query, "br.b IN (?, ?)") {
		t.Fatalf("expected IN clause, got: %s", query)
	}
	// 3 base + 2 branch alts + 3 access = 8
	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d: %v", len(args), args)
	}
}

func TestBuildMatchQueryMultipleBranches(t *testing.T) {
	pbs := []PatternBranch{
		{Alternatives: []string{`type="task"`}},
		{Alternatives: []string{"priority=1", "priority=2"}},
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	query, args := BuildMatchQuery(pbs, "in", "agent-7", "", now)

	existsCount := strings.Count(query, "EXISTS (SELECT 1 FROM branches")
	if existsCount != 2 {
		t.Fatalf("expected 2 branch EXISTS clauses, got %d in: %s", existsCount, query)
	}
	// 3 base + 1 + 2 branch alts + 3 access = 9
	if len(args) != 9 {
		t.Fatalf("expected 9 args, got %d: %v", len(args), args)
	}
}
