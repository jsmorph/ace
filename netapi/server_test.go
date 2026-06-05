package netapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morphism/ace/core"
)

func newTestSpace(t *testing.T) *core.Space {
	t.Helper()
	cfg := core.DefaultConfig()
	cfg.Blocking = core.BlockingPoll
	cfg.InsecureIDs = true
	return newTestSpaceWithConfig(t, cfg)
}

func newTestSpaceWithConfig(t *testing.T, cfg core.Config) *core.Space {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := core.NewSpace(dbPath, cfg)
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

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestSpace(t)
	return NewServer(s, 0)
}

func TestServerOut(t *testing.T) {
	srv := newTestServer(t)

	body := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp outResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestServerRd(t *testing.T) {
	srv := newTestServer(t)

	// put an object
	outBody := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(outBody))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("out: expected 200, got %d", w.Code)
	}

	// read it
	rdBody := `{"pattern":{"a":1}}`
	req = httptest.NewRequest("POST", "/rd", bytes.NewBufferString(rdBody))
	req.Header.Set("X-ACE-ID", "test")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("rd: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp core.Result
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestServerIn(t *testing.T) {
	srv := newTestServer(t)

	// put
	outBody := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(outBody))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("out: expected 200, got %d", w.Code)
	}

	// in (consume)
	inBody := `{"pattern":{"a":1}}`
	req = httptest.NewRequest("POST", "/in", bytes.NewBufferString(inBody))
	req.Header.Set("X-ACE-ID", "test")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("in: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp core.Result
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" {
		t.Fatal("expected result from in")
	}

	// in again: should be gone
	req = httptest.NewRequest("POST", "/in", bytes.NewBufferString(inBody))
	req.Header.Set("X-ACE-ID", "test")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("in2: expected 200, got %d", w.Code)
	}
	if w.Body.String() != "null\n" {
		t.Fatalf("expected null, got %s", w.Body.String())
	}
}

func TestServerNoIDAllowed(t *testing.T) {
	srv := newTestServer(t)

	// Write an unrestricted object.
	outBody := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(outBody))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("out: expected 200, got %d", w.Code)
	}

	// Read without X-ACE-ID header.
	rdBody := `{"pattern":{"a":1}}`
	req = httptest.NewRequest("POST", "/rd", bytes.NewBufferString(rdBody))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("rd: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp core.Result
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" {
		t.Fatal("expected result without X-ACE-ID for unrestricted object")
	}
}

func TestServerLimits(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/limits", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var lim core.Limits
	if err := json.NewDecoder(w.Body).Decode(&lim); err != nil {
		t.Fatal(err)
	}
	if lim.ObjectSize != 2048 {
		t.Fatalf("expected object_size 2048, got %d", lim.ObjectSize)
	}
}

func TestServerMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/out", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestServerOutWithTTL(t *testing.T) {
	srv := newTestServer(t)

	body := `{"object":{"a":1},"ttl":"P1D"}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerOutWithAccess(t *testing.T) {
	srv := newTestServer(t)

	body := `{"object":{"a":1},"access":{"in":["agent-1"],"rd":["agent-2"]}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// agent-2 should not be able to in
	inBody := `{"pattern":{"a":1}}`
	req = httptest.NewRequest("POST", "/in", bytes.NewBufferString(inBody))
	req.Header.Set("X-ACE-ID", "agent-2")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Body.String() != "null\n" {
		t.Fatalf("agent-2 should not be able to in, got %s", w.Body.String())
	}

	// agent-1 should be able to in
	req = httptest.NewRequest("POST", "/in", bytes.NewBufferString(inBody))
	req.Header.Set("X-ACE-ID", "agent-1")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var resp core.Result
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" {
		t.Fatal("agent-1 should be able to in")
	}
}

func TestServerValidationError(t *testing.T) {
	srv := newTestServer(t)

	big := `{"object":{"x":"` + strings.Repeat("a", 3000) + `"}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(big))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for oversized object, got %d", w.Code)
	}
}

func TestServerNoMatchReturnsNull(t *testing.T) {
	srv := newTestServer(t)

	body := `{"pattern":{"nonexistent":1}}`
	req := httptest.NewRequest("POST", "/rd", bytes.NewBufferString(body))
	req.Header.Set("X-ACE-ID", "test")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "null\n" {
		t.Fatalf("expected null, got %s", w.Body.String())
	}
}

func newTestServerHTTP(t *testing.T, maxWaiters int) (*httptest.Server, *core.Space) {
	t.Helper()
	cfg := core.DefaultConfig()
	cfg.Blocking = core.BlockingNotify
	cfg.InsecureIDs = true
	return newTestServerHTTPWithConfig(t, cfg, maxWaiters)
}

func newTestServerHTTPWithConfig(t *testing.T, cfg core.Config, maxWaiters int) (*httptest.Server, *core.Space) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	space, err := core.NewSpace(dbPath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := space.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	srv := NewServer(space, maxWaiters)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, space
}

func TestServerMaxWaitersRejectsExcess(t *testing.T) {
	ts, _ := newTestServerHTTP(t, 1)

	// First blocking request fills the semaphore.
	blocked := make(chan int, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		body := `{"pattern":{"z":1},"wait":1}`
		req, err := http.NewRequest("POST", ts.URL+"/in", strings.NewReader(body))
		if err != nil {
			t.Errorf("new request: %v", err)
			return
		}
		req.Header.Set("X-ACE-ID", "blocker")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Errorf("blocker request: %v", err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		blocked <- resp.StatusCode
	}()

	// Give the blocker time to enter the wait.
	time.Sleep(200 * time.Millisecond)

	// Second blocking request should get 503.
	body := `{"pattern":{},"wait":1}`
	req, err := http.NewRequest("POST", ts.URL+"/in", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACE-ID", "rejected")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	wg.Wait()
	code := <-blocked
	if code != 200 {
		t.Fatalf("blocker expected 200, got %d", code)
	}
}

func TestServerMaxWaitersAllowsNonBlocking(t *testing.T) {
	ts, space := newTestServerHTTP(t, 1)

	// Write an object so the non-blocking request has something to find.
	_, err := space.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Fill the semaphore with a blocking request.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		body := `{"pattern":{"nope":1},"wait":5}`
		req, err := http.NewRequest("POST", ts.URL+"/rd", strings.NewReader(body))
		if err != nil {
			t.Errorf("new request: %v", err)
			return
		}
		req.Header.Set("X-ACE-ID", "blocker")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Errorf("blocker: %v", err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	time.Sleep(200 * time.Millisecond)

	// Non-blocking request (no wait) should pass through.
	body := `{"pattern":{"a":1}}`
	req, err := http.NewRequest("POST", ts.URL+"/rd", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ACE-ID", "nonblocking")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("non-blocking request expected 200, got %d", resp.StatusCode)
	}

	// Unblock the blocker by writing a matching object.
	_, err = space.Out(json.RawMessage(`{"nope":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func newTestServerDeletes(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := core.DefaultConfig()
	cfg.Blocking = core.BlockingNotify
	cfg.Deletes = true
	cfg.InsecureIDs = true
	cfg.VisibilityTimeout = 5 * time.Second
	ts, _ := newTestServerHTTPWithConfig(t, cfg, 0)
	return ts
}

func serverOut(t *testing.T, ts *httptest.Server, object string) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/out", "application/json", strings.NewReader(`{"object":`+object+`}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("out: expected 200, got %d", resp.StatusCode)
	}
}

func serverIn(t *testing.T, ts *httptest.Server, pattern string) *core.Result {
	t.Helper()
	resp, err := http.Post(ts.URL+"/in", "application/json", strings.NewReader(`{"pattern":`+pattern+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("in: expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) == "null" {
		return nil
	}
	var r core.Result
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestServerDelPost(t *testing.T) {
	ts := newTestServerDeletes(t)

	serverOut(t, ts, `{"a":1}`)
	r := serverIn(t, ts, `{"a":1}`)
	if r == nil || r.DeleteID == "" {
		t.Fatal("expected result with delete_id")
	}

	// POST /del with the delete_id
	resp, err := http.Post(ts.URL+"/del", "application/json",
		strings.NewReader(`{"delete_id":"`+r.DeleteID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("del: expected 200, got %d", resp.StatusCode)
	}
	var dr delResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		t.Fatal(err)
	}
	if !dr.Deleted {
		t.Fatal("expected deleted=true")
	}

	// Second del: should return false
	resp2, err := http.Post(ts.URL+"/del", "application/json",
		strings.NewReader(`{"delete_id":"`+r.DeleteID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var dr2 delResponse
	if err := json.NewDecoder(resp2.Body).Decode(&dr2); err != nil {
		t.Fatal(err)
	}
	if dr2.Deleted {
		t.Fatal("expected deleted=false on second attempt")
	}
}

func TestServerDelGet(t *testing.T) {
	ts := newTestServerDeletes(t)

	serverOut(t, ts, `{"a":1}`)
	r := serverIn(t, ts, `{"a":1}`)
	if r == nil || r.DeleteID == "" {
		t.Fatal("expected result with delete_id")
	}

	// GET /del?delete_id=...
	resp, err := http.Get(ts.URL + "/del?delete_id=" + r.DeleteID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("del GET: expected 200, got %d", resp.StatusCode)
	}
	var dr delResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		t.Fatal(err)
	}
	if !dr.Deleted {
		t.Fatal("expected deleted=true via GET")
	}
}

func TestServerDelMethodNotAllowed(t *testing.T) {
	ts := newTestServerDeletes(t)

	req, err := http.NewRequest("PUT", ts.URL+"/del", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestServerMatchTest(t *testing.T) {
	srv := newTestServer(t)

	body := `{"object":{"a":1},"pattern":{"a":1}}`
	req := httptest.NewRequest("POST", "/match", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp matchTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Match {
		t.Fatal("expected match=true")
	}
}

func TestServerMatchTestNoMatch(t *testing.T) {
	srv := newTestServer(t)

	body := `{"object":{"a":1},"pattern":{"a":2}}`
	req := httptest.NewRequest("POST", "/match", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp matchTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Match {
		t.Fatal("expected match=false")
	}
}

func TestServerMatchTestGet(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/match?object=%7B%22a%22%3A1%7D&pattern=%7B%22a%22%3A1%7D", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp matchTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Match {
		t.Fatal("expected match=true via GET")
	}
}

func TestServerMatchTestMissingFields(t *testing.T) {
	srv := newTestServer(t)

	// POST: missing pattern
	body := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/match", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing pattern, got %d", w.Code)
	}

	// POST: missing object
	body = `{"pattern":{"a":1}}`
	req = httptest.NewRequest("POST", "/match", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing object, got %d", w.Code)
	}

	// GET: missing both
	req = httptest.NewRequest("GET", "/match", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400 for GET with no params, got %d", w.Code)
	}
}

func TestServerMatchTestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("PUT", "/match", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestServerDelMissingID(t *testing.T) {
	ts := newTestServerDeletes(t)

	resp, err := http.Post(ts.URL+"/del", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for missing delete_id, got %d", resp.StatusCode)
	}
}

func TestServerStats(t *testing.T) {
	srv := newTestServer(t)

	outBody := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(outBody))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("out: expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/stats", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("stats: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats core.Stats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Objects != 1 {
		t.Fatalf("expected 1 object, got %d", stats.Objects)
	}
	if stats.Branches != 1 {
		t.Fatalf("expected 1 branch, got %d", stats.Branches)
	}
}

func TestServerWaitStringFormats(t *testing.T) {
	srv := newTestServer(t)

	// Write an object so rd returns immediately.
	outBody := `{"object":{"a":1}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(outBody))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("out: expected 200, got %d", w.Code)
	}

	formats := []string{
		`{"pattern":{"a":1},"wait":10}`,
		`{"pattern":{"a":1},"wait":"10"}`,
		`{"pattern":{"a":1},"wait":"10s"}`,
		`{"pattern":{"a":1},"wait":"PT10S"}`,
	}
	for _, body := range formats {
		req = httptest.NewRequest("POST", "/rd", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("rd with %s: expected 200, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func TestServerWaitInvalidString(t *testing.T) {
	srv := newTestServer(t)

	body := `{"pattern":{"a":1},"wait":"bogus"}`
	req := httptest.NewRequest("POST", "/rd", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerNegativeWaitRejected(t *testing.T) {
	srv := newTestServer(t)

	cases := []string{
		`{"pattern":{},"wait":-5}`,
		`{"pattern":{},"wait":"-5s"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest("POST", "/rd", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("body %s: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
}

func TestServerZeroTTLRejected(t *testing.T) {
	srv := newTestServer(t)

	body := `{"object":{"a":1},"ttl":"PT0S"}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func newTestServerSecure(t *testing.T) (*Server, *core.Space) {
	t.Helper()
	cfg := core.DefaultConfig()
	cfg.Blocking = core.BlockingPoll
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := core.NewSpace(dbPath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return NewServer(s, 0), s
}

func TestServerReg(t *testing.T) {
	srv, _ := newTestServerSecure(t)

	req := httptest.NewRequest("POST", "/reg", bytes.NewBufferString(`{"name":"worker"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp regResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != "acen:worker" {
		t.Fatalf("expected name acen:worker, got %q", resp.Name)
	}
	if len(resp.Key) != 64 {
		t.Fatalf("expected 64-char key, got %d", len(resp.Key))
	}
}

func TestServerRegNoBody(t *testing.T) {
	srv, _ := newTestServerSecure(t)

	req := httptest.NewRequest("POST", "/reg", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerRegDuplicateName(t *testing.T) {
	srv, _ := newTestServerSecure(t)

	body := `{"name":"dup"}`
	req := httptest.NewRequest("POST", "/reg", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("first reg expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/reg", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("second reg expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerKeyAuth(t *testing.T) {
	srv, space := newTestServerSecure(t)

	ident, err := space.Register("reader")
	if err != nil {
		t.Fatal(err)
	}

	acc := &core.Access{In: []string{ident.ID}}
	if _, err := space.Out(json.RawMessage(`{"x":1}`), acc, 0); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/in",
		bytes.NewBufferString(`{"pattern":{"x":1}}`))
	req.Header.Set("X-ACE-Client-Key", ident.Key)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := strings.TrimSpace(w.Body.String())
	if body == "null" {
		t.Fatal("expected result, got null")
	}
}

func TestServerInsecureIDRejected(t *testing.T) {
	srv, _ := newTestServerSecure(t)

	req := httptest.NewRequest("POST", "/rd",
		bytes.NewBufferString(`{"pattern":{}}`))
	req.Header.Set("X-ACE-ID", "someone")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerInsecureIDAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/rd",
		bytes.NewBufferString(`{"pattern":{}}`))
	req.Header.Set("X-ACE-ID", "someone")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerRegCheckByKey(t *testing.T) {
	srv, space := newTestServerSecure(t)

	ident, err := space.Register("checker")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/regcheck", nil)
	req.Header.Set("X-ACE-Client-Key", ident.Key)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp regCheckResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != ident.ID {
		t.Fatalf("expected id %q, got %q", ident.ID, resp.ID)
	}
	if resp.Name != ident.Name {
		t.Fatalf("expected name %q, got %q", ident.Name, resp.Name)
	}
}

func TestServerRegCheckByName(t *testing.T) {
	srv, space := newTestServerSecure(t)

	ident, err := space.Register("namedcheck")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/regcheck?name=acen:namedcheck", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp regCheckResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != ident.ID {
		t.Fatalf("expected id %q, got %q", ident.ID, resp.ID)
	}
	if resp.Name != "" {
		t.Fatalf("expected no name field, got %q", resp.Name)
	}
}

func TestServerRegCheckByID(t *testing.T) {
	srv, space := newTestServerSecure(t)

	ident, err := space.Register("idcheck")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/regcheck?id="+ident.ID, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp regCheckResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != ident.Name {
		t.Fatalf("expected name %q, got %q", ident.Name, resp.Name)
	}
	if resp.ID != "" {
		t.Fatalf("expected no id field, got %q", resp.ID)
	}
}

func TestServerRegCheckNotFound(t *testing.T) {
	srv, _ := newTestServerSecure(t)

	req := httptest.NewRequest("GET", "/regcheck?key=nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServerOutRejectsUnprefixedAccess(t *testing.T) {
	srv, _ := newTestServerSecure(t)

	body := `{"object":{"a":1},"access":{"in":["bare-string"]}}`
	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
