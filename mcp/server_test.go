package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morphism/ace/core"
)

func newTestMCPServer(t *testing.T) *Server {
	t.Helper()
	cfg := core.DefaultConfig()
	cfg.Blocking = core.BlockingPoll
	cfg.InsecureIDs = true
	space, err := core.NewSpace(filepath.Join(t.TempDir(), "test.db"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := space.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return NewServer(space, "test")
}

type rawResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func runMessages(t *testing.T, srv *Server, lines ...string) []rawResponse {
	t.Helper()
	var out bytes.Buffer
	input := strings.Join(lines, "\n") + "\n"
	if err := srv.ServeStdio(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	var responses []rawResponse
	scanner := strings.Split(strings.TrimSpace(out.String()), "\n")
	for _, line := range scanner {
		if line == "" {
			continue
		}
		var resp rawResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestInitializeAndToolsList(t *testing.T) {
	srv := newTestMCPServer(t)

	responses := runMessages(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("initialize error: %+v", responses[0].Error)
	}
	var init initializeResult
	if err := json.Unmarshal(responses[0].Result, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != protocolVersion {
		t.Fatalf("expected protocol %s, got %s", protocolVersion, init.ProtocolVersion)
	}
	if !strings.EqualFold(init.ServerInfo.Name, "ace") {
		t.Fatalf("unexpected server name %q", init.ServerInfo.Name)
	}

	var listed listToolsResult
	if err := json.Unmarshal(responses[1].Result, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 8 {
		t.Fatalf("expected 8 tools, got %d", len(listed.Tools))
	}
	if listed.Tools[0].Name != "ace_out" {
		t.Fatalf("unexpected first tool %q", listed.Tools[0].Name)
	}
}

func TestToolOutAndRd(t *testing.T) {
	srv := newTestMCPServer(t)

	responses := runMessages(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ace_out","arguments":{"object":{"a":1}}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ace_rd","arguments":{"pattern":{"a":1}}}}`,
	)
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}

	var out callToolResult
	if err := json.Unmarshal(responses[0].Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.IsError {
		t.Fatalf("out returned tool error: %s", out.Content[0].Text)
	}
	if out.StructuredOutput["id"] == "" {
		t.Fatalf("missing id in output: %+v", out.StructuredOutput)
	}

	var rd callToolResult
	if err := json.Unmarshal(responses[1].Result, &rd); err != nil {
		t.Fatal(err)
	}
	if rd.IsError {
		t.Fatalf("rd returned tool error: %s", rd.Content[0].Text)
	}
	result, ok := rd.StructuredOutput["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result object: %+v", rd.StructuredOutput)
	}
	object, ok := result["object"].(map[string]any)
	if !ok {
		t.Fatalf("missing result object payload: %+v", result)
	}
	if object["a"].(float64) != 1 {
		t.Fatalf("unexpected object: %+v", object)
	}
}

func TestToolExecutionErrorUsesToolResult(t *testing.T) {
	srv := newTestMCPServer(t)

	responses := runMessages(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ace_rd","arguments":{}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected protocol error: %+v", responses[0].Error)
	}
	var result callToolResult
	if err := json.Unmarshal(responses[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error result")
	}
	if !strings.Contains(result.Content[0].Text, "pattern is required") {
		t.Fatalf("unexpected tool error: %+v", result.Content)
	}
}
