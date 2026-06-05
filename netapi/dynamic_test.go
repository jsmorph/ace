package netapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerRdEmbeddingsRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(`{"object":{"type":"task","context":"tacos and queso"}}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("out: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/rd", bytes.NewBufferString(`{"pattern":{"type":"task","context~":"TexMex food"}}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "embeddings filtering isn't available") {
		t.Fatalf("unexpected response body %q", w.Body.String())
	}
}

func TestServerRdQuestionRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(`{"object":{"type":"task","comment":"tacos and queso"}}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("out: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/rd", bytes.NewBufferString(`{"pattern":{"type":"task","comment?":"TexMex food"}}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "LLM filtering isn't available") {
		t.Fatalf("unexpected response body %q", w.Body.String())
	}
}
