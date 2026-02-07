package ace

import (
	"testing"
)

func TestIDGenFormat(t *testing.T) {
	g := NewIDGen()
	id := g.Next()
	if len(id) != 29 {
		t.Fatalf("expected 29 characters, got %d: %q", len(id), id)
	}
	if id[4] != '-' || id[7] != '-' || id[10] != 'T' || id[13] != ':' || id[16] != ':' || id[19] != '.' {
		t.Fatalf("unexpected format: %q", id)
	}
}

func TestIDGenMonotonic(t *testing.T) {
	g := NewIDGen()
	prev := g.Next()
	for i := 0; i < 10000; i++ {
		next := g.Next()
		if next <= prev {
			t.Fatalf("id %d not greater than predecessor: %q <= %q", i, next, prev)
		}
		prev = next
	}
}

func TestIDGenUnique(t *testing.T) {
	g := NewIDGen()
	seen := make(map[string]bool)
	for i := 0; i < 10000; i++ {
		id := g.Next()
		if seen[id] {
			t.Fatalf("duplicate id at %d: %q", i, id)
		}
		seen[id] = true
	}
}
