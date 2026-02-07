package ace

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Server exposes a Space over HTTP.
type Server struct {
	space   *Space
	mux     *http.ServeMux
	waitSem chan struct{}
}

// NewServer returns an HTTP handler for the given space. If maxWaiters is
// positive, it limits concurrent blocking clients.
func NewServer(space *Space, maxWaiters int) *Server {
	srv := &Server{space: space}
	if maxWaiters > 0 {
		srv.waitSem = make(chan struct{}, maxWaiters)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/out", srv.handleOut)
	mux.HandleFunc("/in", srv.handleIn)
	mux.HandleFunc("/rd", srv.handleRd)
	mux.HandleFunc("/del", srv.handleDel)
	mux.HandleFunc("/match", srv.handleMatchTest)
	mux.HandleFunc("/limits", srv.handleLimits)
	mux.HandleFunc("/stats", srv.handleStats)
	mux.HandleFunc("/doc", handleDocIndex)
	mux.HandleFunc("/doc/", handleDocFile)
	srv.mux = mux
	return srv
}

// ServeHTTP implements http.Handler.
func (srv *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv.mux.ServeHTTP(w, r)
}

type outRequest struct {
	Object json.RawMessage `json:"object"`
	Access *Access         `json:"access,omitempty"`
	TTL    string          `json:"ttl,omitempty"`
}

type outResponse struct {
	ID string `json:"id"`
}

type matchRequest struct {
	Pattern json.RawMessage `json:"pattern"`
	Wait    json.RawMessage `json:"wait,omitempty"`
	Since   string          `json:"since,omitempty"`
}

func (srv *Server) handleOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req outRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Object == nil {
		writeError(w, http.StatusBadRequest, "object is required")
		return
	}

	var ttl time.Duration
	if req.TTL != "" {
		var err error
		ttl, err = ParseISO8601Duration(req.TTL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid ttl: "+err.Error())
			return
		}
	}

	id, err := srv.space.Out(req.Object, req.Access, ttl)
	if err != nil {
		writeError(w, httpStatusForError(err), err.Error())
		return
	}

	writeJSON(w, http.StatusOK, outResponse{ID: id})
}

func (srv *Server) handleIn(w http.ResponseWriter, r *http.Request) {
	srv.handleMatch(w, r, true)
}

func (srv *Server) handleRd(w http.ResponseWriter, r *http.Request) {
	srv.handleMatch(w, r, false)
}

func (srv *Server) handleMatch(w http.ResponseWriter, r *http.Request, remove bool) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	callerID := r.Header.Get("X-ACE-ID")

	var req matchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Pattern == nil {
		writeError(w, http.StatusBadRequest, "pattern is required")
		return
	}

	wait, err := parseWaitField(req.Wait)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid wait: "+err.Error())
		return
	}

	if wait > 0 && srv.waitSem != nil {
		select {
		case srv.waitSem <- struct{}{}:
			defer func() { <-srv.waitSem }()
		default:
			writeError(w, http.StatusServiceUnavailable, "too many waiting clients")
			return
		}
	}

	var result *Result
	if remove {
		result, err = srv.space.In(r.Context(), callerID, req.Pattern, wait, req.Since)
	} else {
		result, err = srv.space.Rd(r.Context(), callerID, req.Pattern, wait, req.Since)
	}
	if err != nil {
		writeError(w, httpStatusForError(err), err.Error())
		return
	}

	if result == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type delRequest struct {
	DeleteID string `json:"delete_id"`
}

type delResponse struct {
	Deleted bool `json:"deleted"`
}

func (srv *Server) handleDel(w http.ResponseWriter, r *http.Request) {
	var deleteID string

	switch r.Method {
	case http.MethodGet:
		deleteID = r.URL.Query().Get("delete_id")
	case http.MethodPost:
		var req delRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		deleteID = req.DeleteID
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
		return
	}

	if deleteID == "" {
		writeError(w, http.StatusBadRequest, "delete_id is required")
		return
	}

	deleted, err := srv.space.Del(deleteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, delResponse{Deleted: deleted})
}

func (srv *Server) handleLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	writeJSON(w, http.StatusOK, srv.space.Limits())
}

func (srv *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	stats, err := srv.space.Stats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type matchTestRequest struct {
	Object  json.RawMessage `json:"object"`
	Pattern json.RawMessage `json:"pattern"`
}

type matchTestResponse struct {
	Match bool `json:"match"`
}

func (srv *Server) handleMatchTest(w http.ResponseWriter, r *http.Request) {
	var req matchTestRequest

	switch r.Method {
	case http.MethodGet:
		obj := r.URL.Query().Get("object")
		pat := r.URL.Query().Get("pattern")
		if obj != "" {
			req.Object = json.RawMessage(obj)
		}
		if pat != "" {
			req.Pattern = json.RawMessage(pat)
		}
	case http.MethodPost:
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
		return
	}

	if req.Object == nil {
		writeError(w, http.StatusBadRequest, "object is required")
		return
	}
	if req.Pattern == nil {
		writeError(w, http.StatusBadRequest, "pattern is required")
		return
	}

	ok, err := Match(req.Object, req.Pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, matchTestResponse{Match: ok})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	return json.NewEncoder(w).Encode(v)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	if err := writeJSON(w, code, errorResponse{Error: msg}); err != nil {
		log.Printf("writeError: %v", err)
	}
}

// parseWaitField parses the "wait" field from a JSON request. It accepts a
// JSON number (seconds), a JSON string (ISO 8601, Go duration, or bare integer
// seconds), or null/absent (zero).
func parseWaitField(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	// Try as number (seconds).
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return time.Duration(n * float64(time.Second)), nil
	}
	// Try as string.
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("expected number or duration string")
	}
	return ParseWait(s)
}

func httpStatusForError(err error) int {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

const docSummary = "ACE is a coordination service for software agents based on the tuple-space model."

func handleDocIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, docSummary)
	fmt.Fprintln(w)
	for _, name := range DocFiles {
		fmt.Fprintf(w, "  /doc/%s\n", name)
	}
}

func handleDocFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/doc/")
	data, err := Docs.ReadFile(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown document")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}
