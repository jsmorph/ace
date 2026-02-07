package ace

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestSpace(t)
	return NewServer(s)
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

func TestServerMissingID(t *testing.T) {
	srv := newTestServer(t)

	body := `{"pattern":{"a":1}}`
	req := httptest.NewRequest("POST", "/in", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
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
