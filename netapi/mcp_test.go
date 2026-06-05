package netapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPToolsList(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) != 8 {
		t.Fatalf("expected 8 tools, got %d", len(resp.Result.Tools))
	}
	if resp.Result.Tools[0].Name != "ace_out" {
		t.Fatalf("unexpected first tool %q", resp.Result.Tools[0].Name)
	}
}

func TestMCPToolCallUsesSpace(t *testing.T) {
	srv := newTestServer(t)

	out := httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ace_out","arguments":{"object":{"a":1}}}}`,
	))
	out.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, out)
	if w.Code != http.StatusOK {
		t.Fatalf("out expected 200, got %d: %s", w.Code, w.Body.String())
	}

	rd := httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ace_rd","arguments":{"pattern":{"a":1}}}}`,
	))
	rd.Header.Set("Accept", "application/json, text/event-stream")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, rd)
	if w.Code != http.StatusOK {
		t.Fatalf("rd expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"structuredContent"`) {
		t.Fatalf("expected structuredContent, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"object":{"a":1}`) {
		t.Fatalf("expected rd result, got %s", w.Body.String())
	}
}

func TestMCPNotificationReturnsAccepted(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", w.Body.String())
	}
}

func TestMCPGetReturnsMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestMCPBearerToken(t *testing.T) {
	space := newTestSpace(t)
	srv := NewServerWithOptions(space, 0, Options{MCPBearerToken: "secret"})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMCPOriginRestriction(t *testing.T) {
	space := newTestSpace(t)
	srv := NewServerWithOptions(space, 0, Options{MCPAllowedOrigins: []string{"https://client.example"}})

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Origin", "https://client.example")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
