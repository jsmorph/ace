package ace

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
)

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

	var resp Result
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

	var resp Result
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

	var resp Result
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

	var lim Limits
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
	var resp Result
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

func newTestServerHTTP(t *testing.T, maxWaiters int) (*httptest.Server, *Space) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Blocking = BlockingNotify
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	space, err := NewSpace(dbPath, cfg)
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
