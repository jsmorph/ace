package ace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestSpace(t *testing.T) *Space {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	return newTestSpaceWithConfig(t, cfg)
}

func newTestSpaceWithConfig(t *testing.T, cfg Config) *Space {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := NewSpace(dbPath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return s
}

func TestOutAndRd(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	id, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected a result")
	}
	if r.ID != id {
		t.Fatalf("expected id %q, got %q", id, r.ID)
	}

	// rd does not remove
	r2, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 == nil {
		t.Fatal("rd should not remove; expected result on second call")
	}
}

func TestOutAndIn(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected a result")
	}

	// in removes: second call returns nil
	r2, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 != nil {
		t.Fatal("expected nil after in removed the object")
	}
}

func TestNoMatch(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":2}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("expected no match")
	}
}

func TestPatternMatchingSpecExamples(t *testing.T) {
	cases := []struct {
		pattern string
		object  string
		matches bool
	}{
		{`{"a":1}`, `{"a":1}`, true},
		{`{"a":[1,2]}`, `{"a":1}`, true},
		{`{"a":[1,2]}`, `{"a":2}`, true},
		{`{"a":[1,2]}`, `{"a":3}`, false},
		{`{"a":[1,2]}`, `{"a":1,"b":0}`, true},
		{`{"a":[1,2]}`, `{"a":2,"b":0}`, true},
		{`{"a":[1,2]}`, `{"a":3,"b":0}`, false},
		{`{"b":[1,2]}`, `{"a":1}`, false},
		{`{"b":[1,2]}`, `{"a":2}`, false},
		{`{"b":[1,2]}`, `{"a":3,"b":3}`, false},
		{`{"b":[1,2]}`, `{"a":3,"b":1}`, true},
		{`{"b":[1,2]}`, `{"b":1}`, true},
		{`{"a":{"b":1,"c":2}}`, `{"a":{"b":1,"c":2}}`, true},
		{`{"a":{"b":1,"c":2}}`, `{"a":{"b":1,"c":2,"d":3}}`, true},
		{`{"a":{"b":1,"c":2},"d":3}`, `{"a":{"b":1,"c":2,"d":3}}`, false},
	}

	for i, c := range cases {
		s := newTestSpace(t)
		ctx := context.Background()

		_, err := s.Out(json.RawMessage(c.object), nil, 0)
		if err != nil {
			t.Fatalf("case %d: out: %v", i, err)
		}

		r, err := s.Rd(ctx, "anyone", json.RawMessage(c.pattern), 0, "")
		if err != nil {
			t.Fatalf("case %d: rd: %v", i, err)
		}

		got := r != nil
		if got != c.matches {
			t.Errorf("case %d: pattern=%s object=%s: got match=%v, want %v", i, c.pattern, c.object, got, c.matches)
		}
	}
}

func TestAccessControlIn(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	acc := &Access{In: []string{"agent-1"}}
	_, err := s.Out(json.RawMessage(`{"a":1}`), acc, 0)
	if err != nil {
		t.Fatal(err)
	}

	// unauthorized caller
	r, err := s.In(ctx, "agent-2", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("agent-2 should not have access")
	}

	// authorized caller
	r, err = s.In(ctx, "agent-1", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("agent-1 should have access")
	}
}

func TestAccessControlRd(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	acc := &Access{Rd: []string{"agent-1"}}
	_, err := s.Out(json.RawMessage(`{"a":1}`), acc, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "agent-2", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("agent-2 should not have rd access")
	}

	r, err = s.Rd(ctx, "agent-1", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("agent-1 should have rd access")
	}
}

func TestNoAccessRestriction(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("no access restriction means anyone can in")
	}
}

func TestSinceFiltering(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	id1, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	id2, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	// since=id1 should skip the first object
	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, id1)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected result")
	}
	if r.ID != id2 {
		t.Fatalf("expected id %q (second object), got %q", id2, r.ID)
	}
}

func TestOrdering(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	id1, err := s.Out(json.RawMessage(`{"x":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Out(json.RawMessage(`{"x":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"x":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != id1 {
		t.Fatalf("expected earliest id %q, got %q", id1, r.ID)
	}
	_ = id2
}

func TestTTLExpiration(t *testing.T) {
	s := newTestSpace(t)

	ctx := context.Background()

	// Use a very short TTL (1 millisecond)
	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("expected expired object to be invisible")
	}
}

func TestEmptyPattern(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	id, err := s.Out(json.RawMessage(`{"a":1,"b":2}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.ID != id {
		t.Fatal("empty pattern should match any object")
	}
}

func TestWaitBlocking(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	done := make(chan *Result, 1)
	go func() {
		r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 2*time.Second, "")
		if err != nil {
			t.Errorf("in: %v", err)
			return
		}
		done <- r
	}()

	time.Sleep(200 * time.Millisecond)
	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-done:
		if r == nil {
			t.Fatal("expected result from blocking in")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for blocking in")
	}
}

func TestWaitTimeout(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 100*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("expected nil after wait timeout with no matching object")
	}
}

func TestLimitEnforcement(t *testing.T) {
	s := newTestSpace(t)

	big := make([]byte, 0, 3000)
	big = append(big, `{"x":"`...)
	for len(big) < 2500 {
		big = append(big, 'a')
	}
	big = append(big, `"}`...)

	_, err := s.Out(json.RawMessage(big), nil, 0)
	if err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestCanonicalization(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{ "b" : 1 , "a" : 2 }`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected result")
	}
	if string(r.Object) != `{"a":2,"b":1}` {
		t.Fatalf("expected canonical JSON, got %s", r.Object)
	}
}

func TestCanonicalizationPreservesHTMLChars(t *testing.T) {
	s := newTestSpace(t)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"msg":"<b>hi</b> & bye"}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "anyone", json.RawMessage(`{}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected result")
	}
	if string(r.Object) != `{"msg":"<b>hi</b> & bye"}` {
		t.Fatalf("HTML chars should not be escaped, got %s", r.Object)
	}
}

func TestCascadeDeleteOnExpire(t *testing.T) {
	s := newTestSpace(t)

	acc := &Access{In: []string{"w1"}, Rd: []string{"r1"}}
	_, err := s.Out(json.RawMessage(`{"a":1,"b":2}`), acc, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Out(json.RawMessage(`{"c":3}`), nil, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	// This one survives.
	_, err = s.Out(json.RawMessage(`{"d":4}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)

	deleted, err := s.DeleteExpired()
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Objects != 1 {
		t.Fatalf("expected 1 object, got %d", st.Objects)
	}
	if st.Branches != 1 {
		t.Fatalf("expected 1 branch, got %d", st.Branches)
	}
	if st.AccessRecords != 0 {
		t.Fatalf("expected 0 access records, got %d", st.AccessRecords)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
