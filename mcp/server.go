package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/morphism/ace/core"
)

const protocolVersion = "2025-06-18"

type Server struct {
	space   *core.Space
	version string
}

func NewServer(space *core.Space, version string) *Server {
	if version == "" {
		version = "unknown"
	}
	return &Server{space: space, version: version}
}

func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	enc := json.NewEncoder(out)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		resp, ok := s.handleJSONRPC(ctx, line)
		if !ok {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "SSE stream is not available", http.StatusMethodNotAllowed)
		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "GET or POST required", http.StatusMethodNotAllowed)
		return
	}

	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && !supportsProtocol(version) {
		http.Error(w, "unsupported MCP protocol version", http.StatusBadRequest)
		return
	}
	if accept := r.Header.Get("Accept"); accept != "" && !acceptsMCPResponse(accept) {
		http.Error(w, "unsupported Accept header", http.StatusNotAcceptable)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp, ok := s.handleJSONRPC(r.Context(), bytes.TrimSpace(data))
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if isProtocolError(resp, parseError, invalidRequest) {
		w.WriteHeader(http.StatusBadRequest)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}

func acceptsMCPResponse(accept string) bool {
	for _, part := range bytes.Split([]byte(accept), []byte(",")) {
		mediaType := string(bytes.TrimSpace(bytes.Split(part, []byte(";"))[0]))
		switch mediaType {
		case "*/*", "application/*", "application/json", "text/event-stream":
			return true
		}
	}
	return false
}

func isProtocolError(resp response, codes ...int) bool {
	if resp.Error == nil {
		return false
	}
	for _, code := range codes {
		if resp.Error.Code == code {
			return true
		}
	}
	return false
}

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	parseError     = -32700
	invalidRequest = -32600
	methodNotFound = -32601
	invalidParams  = -32602
	internalError  = -32603
)

func (s *Server) handle(ctx context.Context, req request) (response, bool) {
	if req.JSONRPC != "2.0" || req.Method == "" {
		return errorResponse(idOrNull(req.ID), invalidRequest, "invalid request", nil), true
	}
	if req.ID == nil {
		return response{}, false
	}
	if bytes.Equal(bytes.TrimSpace(*req.ID), []byte("null")) {
		return errorResponse(nullID(), invalidRequest, "request id must not be null", nil), true
	}

	switch req.Method {
	case "initialize":
		return resultResponse(*req.ID, s.initialize(req.Params)), true
	case "tools/list":
		return resultResponse(*req.ID, listToolsResult{Tools: tools()}), true
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			return errorResponse(*req.ID, invalidParams, err.Error(), nil), true
		}
		return resultResponse(*req.ID, result), true
	case "ping":
		return resultResponse(*req.ID, map[string]any{}), true
	default:
		return errorResponse(*req.ID, methodNotFound, "method not found", nil), true
	}
}

func (s *Server) handleJSONRPC(ctx context.Context, raw []byte) (response, bool) {
	if len(raw) == 0 {
		return errorResponse(nullID(), parseError, "parse error", nil), true
	}

	var msg message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return errorResponse(nullID(), parseError, "parse error", nil), true
	}
	if msg.Method == "" && (len(msg.Result) > 0 || msg.Error != nil) {
		return response{}, false
	}

	req := request{
		JSONRPC: msg.JSONRPC,
		ID:      msg.ID,
		Method:  msg.Method,
		Params:  extractParams(raw),
	}
	return s.handle(ctx, req)
}

func extractParams(raw []byte) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields["params"]
}

func (s *Server) initialize(raw json.RawMessage) initializeResult {
	var params initializeParams
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}
	version := protocolVersion
	if params.ProtocolVersion != "" && supportsProtocol(params.ProtocolVersion) {
		version = params.ProtocolVersion
	}
	return initializeResult{
		ProtocolVersion: version,
		Capabilities: capabilities{
			Tools: toolsCapability{ListChanged: false},
		},
		ServerInfo: serverInfo{
			Name:    "ace",
			Version: s.version,
		},
	}
}

func supportsProtocol(version string) bool {
	switch version {
	case "", "2024-11-05", "2025-03-26", protocolVersion:
		return true
	default:
		return false
	}
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

type capabilities struct {
	Tools toolsCapability `json:"tools"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type listToolsResult struct {
	Tools []tool `json:"tools"`
}

type tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type callToolResult struct {
	Content          []textContent  `json:"content"`
	StructuredOutput map[string]any `json:"structuredContent,omitempty"`
	IsError          bool           `json:"isError,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (callToolResult, error) {
	var params callToolParams
	if err := decode(raw, &params); err != nil {
		return callToolResult{}, fmt.Errorf("invalid tool call params: %w", err)
	}
	if params.Name == "" {
		return callToolResult{}, errors.New("tool name is required")
	}

	var (
		v   map[string]any
		err error
	)
	switch params.Name {
	case "ace_out":
		v, err = s.toolOut(params.Arguments)
	case "ace_in":
		v, err = s.toolMatch(ctx, params.Arguments, true)
	case "ace_rd":
		v, err = s.toolMatch(ctx, params.Arguments, false)
	case "ace_del":
		v, err = s.toolDel(params.Arguments)
	case "ace_stats":
		v, err = s.toolStats(params.Arguments)
	case "ace_reg":
		v, err = s.toolReg(params.Arguments)
	case "ace_regcheck":
		v, err = s.toolRegCheck(params.Arguments)
	case "ace_match":
		v, err = s.toolMatchTest(ctx, params.Arguments)
	default:
		return callToolResult{}, fmt.Errorf("unknown tool %q", params.Name)
	}
	if err != nil {
		return toolError(err), nil
	}
	return toolResult(v), nil
}

type outArgs struct {
	Object json.RawMessage `json:"object"`
	Access *core.Access    `json:"access,omitempty"`
	TTL    string          `json:"ttl,omitempty"`
}

func (s *Server) toolOut(raw json.RawMessage) (map[string]any, error) {
	var args outArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if args.Object == nil {
		return nil, errors.New("object is required")
	}
	var ttl time.Duration
	if args.TTL != "" {
		d, err := core.ParseISO8601Duration(args.TTL)
		if err != nil {
			return nil, fmt.Errorf("invalid ttl: %w", err)
		}
		if d <= 0 {
			return nil, errors.New("ttl must be positive")
		}
		ttl = d
	}
	id, err := s.space.Out(args.Object, args.Access, ttl)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

type matchArgs struct {
	Pattern   json.RawMessage `json:"pattern"`
	Wait      json.RawMessage `json:"wait,omitempty"`
	Since     string          `json:"since,omitempty"`
	CallerID  string          `json:"caller_id,omitempty"`
	ClientKey string          `json:"client_key,omitempty"`
}

func (s *Server) toolMatch(ctx context.Context, raw json.RawMessage, remove bool) (map[string]any, error) {
	var args matchArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if args.Pattern == nil {
		return nil, errors.New("pattern is required")
	}
	wait, err := parseWait(args.Wait)
	if err != nil {
		return nil, fmt.Errorf("invalid wait: %w", err)
	}
	callerID, err := s.resolveCallerID(args.ClientKey, args.CallerID)
	if err != nil {
		return nil, err
	}

	var result *core.Result
	if remove {
		result, err = s.space.In(ctx, callerID, args.Pattern, wait, args.Since)
	} else {
		result, err = s.space.Rd(ctx, callerID, args.Pattern, wait, args.Since)
	}
	if err != nil {
		return nil, err
	}
	return resultMap("result", result), nil
}

type delArgs struct {
	DeleteID string `json:"delete_id"`
}

func (s *Server) toolDel(raw json.RawMessage) (map[string]any, error) {
	var args delArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if args.DeleteID == "" {
		return nil, errors.New("delete_id is required")
	}
	deleted, err := s.space.Del(args.DeleteID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": deleted}, nil
}

func (s *Server) toolStats(raw json.RawMessage) (map[string]any, error) {
	if err := decode(raw, &struct{}{}); err != nil {
		return nil, err
	}
	stats, err := s.space.Stats()
	if err != nil {
		return nil, err
	}
	return toObject(stats)
}

type regArgs struct {
	Name string `json:"name,omitempty"`
}

func (s *Server) toolReg(raw json.RawMessage) (map[string]any, error) {
	var args regArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ident, err := s.space.Register(args.Name)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"key":  ident.Key,
		"id":   ident.ID,
		"name": ident.Name,
	}, nil
}

type regCheckArgs struct {
	Key  string `json:"key,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (s *Server) toolRegCheck(raw json.RawMessage) (map[string]any, error) {
	var args regCheckArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	count := 0
	for _, value := range []string{args.Key, args.ID, args.Name} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return nil, errors.New("provide exactly one of key, id, or name")
	}

	var ident *core.Identity
	var err error
	switch {
	case args.Key != "":
		ident, err = s.space.LookupKey(args.Key)
	case args.ID != "":
		ident, err = s.space.LookupID(args.ID)
	case args.Name != "":
		ident, err = s.space.LookupName(args.Name)
	}
	if err != nil {
		return nil, err
	}
	if ident == nil {
		return nil, errors.New("identity not found")
	}

	switch {
	case args.Key != "":
		return map[string]any{"id": ident.ID, "name": ident.Name}, nil
	case args.ID != "":
		return map[string]any{"name": ident.Name}, nil
	default:
		return map[string]any{"id": ident.ID}, nil
	}
}

type matchTestArgs struct {
	Object  json.RawMessage `json:"object"`
	Pattern json.RawMessage `json:"pattern"`
}

func (s *Server) toolMatchTest(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var args matchTestArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if args.Object == nil {
		return nil, errors.New("object is required")
	}
	if args.Pattern == nil {
		return nil, errors.New("pattern is required")
	}
	ok, err := core.MatchWithConfigContext(ctx, args.Object, args.Pattern, s.space.Config())
	if err != nil {
		return nil, err
	}
	return map[string]any{"match": ok}, nil
}

func (s *Server) resolveCallerID(clientKey, callerID string) (string, error) {
	if clientKey == "" {
		return callerID, nil
	}
	ident, err := s.space.LookupKey(clientKey)
	if err != nil {
		return "", fmt.Errorf("key lookup: %w", err)
	}
	if ident == nil {
		return "", errors.New("invalid client key")
	}
	return ident.ID, nil
}

func parseWait(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		if n < 0 {
			return 0, errors.New("wait must not be negative")
		}
		return time.Duration(n * float64(time.Second)), nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, errors.New("expected number or duration string")
	}
	d, err := core.ParseWait(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, errors.New("wait must not be negative")
	}
	return d, nil
}

func decode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func toolResult(v map[string]any) callToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return toolError(err)
	}
	return callToolResult{
		Content: []textContent{{
			Type: "text",
			Text: string(data),
		}},
		StructuredOutput: v,
	}
}

func toolError(err error) callToolResult {
	return callToolResult{
		Content: []textContent{{
			Type: "text",
			Text: err.Error(),
		}},
		StructuredOutput: map[string]any{"error": err.Error()},
		IsError:          true,
	}
}

func resultMap(name string, v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{name: nil}
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return map[string]any{name: nil}
	}
	return map[string]any{name: decoded}
}

func toObject(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func resultResponse(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, msg string, data any) response {
	return response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	}
}

func idOrNull(id *json.RawMessage) json.RawMessage {
	if id == nil {
		return nullID()
	}
	return *id
}

func nullID() json.RawMessage {
	return json.RawMessage("null")
}

func tools() []tool {
	return []tool{
		{
			Name:        "ace_out",
			Title:       "ACE out",
			Description: "Write a JSON object into the ACE tuple space.",
			InputSchema: objectSchema(map[string]any{
				"object": map[string]any{"type": "object"},
				"access": map[string]any{"type": "object"},
				"ttl":    map[string]any{"type": "string"},
			}, []string{"object"}),
			OutputSchema: objectSchema(map[string]any{
				"id": map[string]any{"type": "string"},
			}, []string{"id"}),
		},
		{
			Name:         "ace_in",
			Title:        "ACE in",
			Description:  "Find and remove the earliest matching ACE object.",
			InputSchema:  matchInputSchema(),
			OutputSchema: resultOutputSchema(),
		},
		{
			Name:         "ace_rd",
			Title:        "ACE rd",
			Description:  "Find the earliest matching ACE object without removing it.",
			InputSchema:  matchInputSchema(),
			OutputSchema: resultOutputSchema(),
		},
		{
			Name:        "ace_del",
			Title:       "ACE del",
			Description: "Confirm deletion of an object returned by ace_in when explicit deletes are enabled.",
			InputSchema: objectSchema(map[string]any{
				"delete_id": map[string]any{"type": "string"},
			}, []string{"delete_id"}),
			OutputSchema: objectSchema(map[string]any{
				"deleted": map[string]any{"type": "boolean"},
			}, []string{"deleted"}),
		},
		{
			Name:         "ace_stats",
			Title:        "ACE stats",
			Description:  "Return ACE storage statistics.",
			InputSchema:  objectSchema(map[string]any{}, nil),
			OutputSchema: objectSchema(map[string]any{}, nil),
		},
		{
			Name:        "ace_reg",
			Title:       "ACE reg",
			Description: "Register a new ACE client identity.",
			InputSchema: objectSchema(map[string]any{
				"name": map[string]any{"type": "string"},
			}, nil),
			OutputSchema: objectSchema(map[string]any{
				"key":  map[string]any{"type": "string"},
				"id":   map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			}, []string{"key", "id", "name"}),
		},
		{
			Name:        "ace_regcheck",
			Title:       "ACE regcheck",
			Description: "Look up an ACE identity by key, id, or name. Provide exactly one lookup field.",
			InputSchema: objectSchema(map[string]any{
				"key":  map[string]any{"type": "string"},
				"id":   map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			}, nil),
			OutputSchema: objectSchema(map[string]any{}, nil),
		},
		{
			Name:        "ace_match",
			Title:       "ACE match",
			Description: "Test whether a JSON object matches an ACE pattern.",
			InputSchema: objectSchema(map[string]any{
				"object":  map[string]any{"type": "object"},
				"pattern": map[string]any{"type": "object"},
			}, []string{"object", "pattern"}),
			OutputSchema: objectSchema(map[string]any{
				"match": map[string]any{"type": "boolean"},
			}, []string{"match"}),
		},
	}
}

func matchInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"pattern":    map[string]any{"type": "object"},
		"wait":       map[string]any{"anyOf": []any{map[string]any{"type": "number"}, map[string]any{"type": "string"}}},
		"since":      map[string]any{"type": "string"},
		"caller_id":  map[string]any{"type": "string"},
		"client_key": map[string]any{"type": "string"},
	}, []string{"pattern"})
}

func resultOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"result": map[string]any{},
	}, []string{"result"})
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
