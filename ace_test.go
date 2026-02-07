package ace

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
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

func newTestSpaceDeletes(t *testing.T, visTimeout time.Duration) *Space {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.Deletes = true
	cfg.VisibilityTimeout = visTimeout
	return newTestSpaceWithConfig(t, cfg)
}

func TestExplicitDeleteBasic(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
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
		t.Fatal("expected result")
	}
	if r.DeleteID == "" {
		t.Fatal("expected delete_id")
	}

	// Object should be invisible to rd.
	r2, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 != nil {
		t.Fatal("expected nil: object should be invisible")
	}

	// Del should succeed.
	deleted, err := s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected deleted=true")
	}

	// Object should be permanently gone.
	r3, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r3 != nil {
		t.Fatal("expected nil: object should be deleted")
	}
}

func TestExplicitDeleteTimeout(t *testing.T) {
	s := newTestSpaceDeletes(t, 10*time.Millisecond)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.DeleteID == "" {
		t.Fatal("expected result with delete_id")
	}

	time.Sleep(20 * time.Millisecond)

	// Object should reappear.
	r2, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 == nil {
		t.Fatal("expected object to reappear after visibility timeout")
	}

	// Del with expired delete_id should fail.
	deleted, err := s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("expected deleted=false after timeout")
	}
}

func TestExplicitDeleteDisabled(t *testing.T) {
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
		t.Fatal("expected result")
	}
	if r.DeleteID != "" {
		t.Fatalf("expected no delete_id when deletes disabled, got %q", r.DeleteID)
	}

	// Object should be gone (immediate delete).
	r2, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 != nil {
		t.Fatal("expected nil: object should be deleted immediately")
	}
}

func TestExplicitDeleteReissue(t *testing.T) {
	s := newTestSpaceDeletes(t, 10*time.Millisecond)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	// First in: get delete_id.
	r1, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	did1 := r1.DeleteID

	// Wait for timeout.
	time.Sleep(20 * time.Millisecond)

	// Second in: get new delete_id.
	r2, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 == nil {
		t.Fatal("expected object to reappear")
	}
	did2 := r2.DeleteID
	if did1 == did2 {
		t.Fatal("expected different delete_ids")
	}

	// Old delete_id should not work.
	deleted, err := s.Del(did1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("old delete_id should not work")
	}

	// New delete_id should work.
	deleted, err = s.Del(did2)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("new delete_id should work")
	}
}

func TestDelCascadesAccessAndBranches(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
	ctx := context.Background()

	acc := &Access{In: []string{"w1"}, Rd: []string{"r1"}}
	_, err := s.Out(json.RawMessage(`{"a":1,"b":2}`), acc, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "w1", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected result")
	}

	deleted, err := s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected deleted=true")
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Objects != 0 {
		t.Fatalf("expected 0 objects, got %d", st.Objects)
	}
	if st.Branches != 0 {
		t.Fatalf("expected 0 branches after cascade, got %d", st.Branches)
	}
	if st.AccessRecords != 0 {
		t.Fatalf("expected 0 access records after cascade, got %d", st.AccessRecords)
	}
}

func TestInvisibleObjectSkippedInFIFO(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
	ctx := context.Background()

	idA, err := s.Out(json.RawMessage(`{"x":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := s.Out(json.RawMessage(`{"x":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	// in consumes A (marks invisible)
	r, err := s.In(ctx, "", json.RawMessage(`{"x":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != idA {
		t.Fatalf("expected A (%s), got %s", idA, r.ID)
	}

	// next in should skip invisible A and return B
	r2, err := s.In(ctx, "", json.RawMessage(`{"x":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 == nil {
		t.Fatal("expected B, got nil")
	}
	if r2.ID != idB {
		t.Fatalf("expected B (%s), got %s", idB, r2.ID)
	}

	// rd should also return nothing (both consumed)
	r3, err := s.Rd(ctx, "", json.RawMessage(`{"x":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r3 != nil {
		t.Fatal("expected nil from rd: A is invisible, B is invisible")
	}
}

func TestReappearancePreservesFIFO(t *testing.T) {
	s := newTestSpaceDeletes(t, 10*time.Millisecond)
	ctx := context.Background()

	idA, err := s.Out(json.RawMessage(`{"x":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Out(json.RawMessage(`{"x":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	// in consumes A
	r, err := s.In(ctx, "", json.RawMessage(`{"x":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != idA {
		t.Fatalf("expected A, got %s", r.ID)
	}

	// wait for A to reappear
	time.Sleep(20 * time.Millisecond)

	// rd should return A (earlier ID, reappeared)
	r2, err := s.Rd(ctx, "", json.RawMessage(`{"x":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 == nil {
		t.Fatal("expected A to reappear")
	}
	if r2.ID != idA {
		t.Fatalf("expected reappeared A (%s), got %s", idA, r2.ID)
	}
}

func TestRdNeverReturnsDeleteID(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Rd(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected result")
	}
	if r.DeleteID != "" {
		t.Fatalf("rd should never return delete_id, got %q", r.DeleteID)
	}

	r2, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 == nil || r2.DeleteID == "" {
		t.Fatal("in should return delete_id")
	}
}

func TestDelBogusID(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)

	deleted, err := s.Del("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if deleted {
		t.Fatal("expected deleted=false for bogus ID")
	}
}

func TestDelEmptyID(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)

	_, err := s.Del("")
	if err == nil {
		t.Fatal("expected error for empty delete_id")
	}
}

func TestDelIdempotent(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("first del should succeed")
	}

	deleted, err = s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("second del should return false")
	}
}

func TestDeleteExpiredRemovesInvisibleObject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.Deletes = true
	cfg.VisibilityTimeout = 5 * time.Second
	s := newTestSpaceWithConfig(t, cfg)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected result")
	}

	time.Sleep(20 * time.Millisecond)

	n, err := s.DeleteExpired()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted by expiration, got %d", n)
	}

	deleted, err := s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("del should return false: row was already removed by expiration")
	}
}

func TestTTLPreventsReappearance(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.Deletes = true
	cfg.VisibilityTimeout = 10 * time.Millisecond
	s := newTestSpaceWithConfig(t, cfg)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 15*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.In(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}

	// both visibility timeout and TTL expire
	time.Sleep(25 * time.Millisecond)

	r, err := s.Rd(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("expired object should not reappear even after visibility timeout")
	}
}

func TestInvisibleObjectAccessControlPreserved(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
	ctx := context.Background()

	acc := &Access{In: []string{"alpha"}}
	_, err := s.Out(json.RawMessage(`{"a":1}`), acc, 0)
	if err != nil {
		t.Fatal(err)
	}

	// beta cannot in
	r, err := s.In(ctx, "beta", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("beta should not have access")
	}

	// alpha can in
	r, err = s.In(ctx, "alpha", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.DeleteID == "" {
		t.Fatal("alpha should get result with delete_id")
	}

	// even alpha cannot rd an invisible object
	r2, err := s.Rd(ctx, "alpha", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r2 != nil {
		t.Fatal("invisible object should be invisible to everyone, including authorized callers")
	}

	deleted, err := s.Del(r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("del should succeed")
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.AccessRecords != 0 {
		t.Fatalf("expected 0 access records after del cascade, got %d", st.AccessRecords)
	}
}

func TestBlockingInReturnsDeleteID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.Deletes = true
	cfg.VisibilityTimeout = 5 * time.Second
	s := newTestSpaceWithConfig(t, cfg)
	ctx := context.Background()

	done := make(chan *Result, 1)
	go func() {
		r, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 2*time.Second, "")
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
		if r.DeleteID == "" {
			t.Fatal("blocking in should return delete_id")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestNotifyInReturnsDeleteID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingNotify
	cfg.Deletes = true
	cfg.VisibilityTimeout = 5 * time.Second
	s := newTestSpaceWithConfig(t, cfg)
	ctx := context.Background()

	done := make(chan *Result, 1)
	go func() {
		r, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 2*time.Second, "")
		if err != nil {
			t.Errorf("in: %v", err)
			return
		}
		done <- r
	}()

	time.Sleep(100 * time.Millisecond)
	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-done:
		if r == nil {
			t.Fatal("expected result from notify in")
		}
		if r.DeleteID == "" {
			t.Fatal("notify in should return delete_id")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestNotifyReappearanceWakesWaiter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingNotify
	cfg.Deletes = true
	cfg.VisibilityTimeout = 100 * time.Millisecond
	s := newTestSpaceWithConfig(t, cfg)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	// first in: marks object invisible
	r1, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r1 == nil {
		t.Fatal("expected result")
	}

	// second in: blocks waiting for the object to reappear
	done := make(chan *Result, 1)
	go func() {
		r, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 2*time.Second, "")
		if err != nil {
			t.Errorf("blocking in: %v", err)
			return
		}
		done <- r
	}()

	// the timer should fire after 100ms, waking the waiter
	select {
	case r := <-done:
		if r == nil {
			t.Fatal("expected reappeared object")
		}
		if r.DeleteID == "" {
			t.Fatal("expected delete_id on reappeared object")
		}
		if r.DeleteID == r1.DeleteID {
			t.Fatal("reappeared object should have a new delete_id")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out: waiter was not woken by reappearance (notify bug)")
	}
}

func TestInvisibleObjectStillCountedInStats(t *testing.T) {
	s := newTestSpaceDeletes(t, 5*time.Second)
	ctx := context.Background()

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.In(ctx, "", json.RawMessage(`{"a":1}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Objects != 1 {
		t.Fatalf("invisible object should still be counted: expected 1, got %d", st.Objects)
	}
	if st.Branches != 1 {
		t.Fatalf("expected 1 branch, got %d", st.Branches)
	}

	s.Del(r.DeleteID)

	st, err = s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Objects != 0 {
		t.Fatalf("after del: expected 0 objects, got %d", st.Objects)
	}
	if st.Branches != 0 {
		t.Fatalf("after del: expected 0 branches, got %d", st.Branches)
	}
}

func TestQueryPlans(t *testing.T) {
	s := newTestSpace(t)

	acc := &Access{In: []string{"w1"}, Rd: []string{"r1"}}
	_, err := s.Out(json.RawMessage(`{"a":1,"b":2}`), acc, 0)
	if err != nil {
		t.Fatal(err)
	}

	pbs, err := ExtractPatternBranches(json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	matchQuery, matchArgs := BuildMatchQuery(pbs, "in", "w1", "", time.Now())

	plans := []struct {
		name  string
		query string
		args  []interface{}
	}{
		{"match", matchQuery, matchArgs},
		{"del", "DELETE FROM objects WHERE delete_id = ? AND invisible_until > ?", []interface{}{"x", "2099-01-01T00:00:00.000000000"}},
		{"delete_expired", "DELETE FROM objects WHERE expires <= ?", []interface{}{"2099-01-01T00:00:00.000000000"}},
	}

	for _, p := range plans {
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+p.query, p.args...)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		var lines []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, detail)
		}
		rows.Close()
		plan := strings.Join(lines, "\n")
		t.Logf("=== %s ===\n%s", p.name, plan)
	}

	// Verify the match query uses indexes for branch and access subqueries.
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+matchQuery, matchArgs...)
	if err != nil {
		t.Fatal(err)
	}
	var allDetails []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		allDetails = append(allDetails, detail)
	}
	rows.Close()
	fullPlan := strings.Join(allDetails, "\n")

	if !strings.Contains(fullPlan, "idx_branches") {
		t.Errorf("expected match query to use idx_branches, got:\n%s", fullPlan)
	}
	if !strings.Contains(fullPlan, "idx_access") {
		t.Errorf("expected match query to use idx_access, got:\n%s", fullPlan)
	}
}

func TestDBOperationTimeMonitorLimit(t *testing.T) {
	var buf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	})

	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.DBOperationTimeMonitorLimit = time.Nanosecond
	s := newTestSpaceWithConfig(t, cfg)

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "WARN high latency") {
		t.Fatalf("expected WARN high latency in log output, got: %q", output)
	}
	if !strings.Contains(output, "for out") {
		t.Fatalf("expected 'for out' in log output, got: %q", output)
	}
}

func TestDBOperationTimeMonitorDisabled(t *testing.T) {
	var buf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	})

	cfg := DefaultConfig()
	cfg.Blocking = BlockingPoll
	cfg.DBOperationTimeMonitorLimit = 0
	s := newTestSpaceWithConfig(t, cfg)

	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if buf.Len() > 0 {
		t.Fatalf("expected no log output when monitoring disabled, got: %q", buf.String())
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
