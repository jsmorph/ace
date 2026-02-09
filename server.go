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

// Commit is the git commit hash, set at build time via ldflags.
var Commit string

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
	mux.HandleFunc("/reg", srv.handleReg)
	mux.HandleFunc("/regcheck", srv.handleRegCheck)
	mux.HandleFunc("/ping", handlePing)
	mux.HandleFunc("/doc", handleDocIndex)
	mux.HandleFunc("/doc/", handleDocFile)
	srv.mux = mux
	return srv
}

const maxRequestBytes = 4096

// ServeHTTP implements http.Handler.
func (srv *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
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
		if ttl <= 0 {
			writeError(w, http.StatusBadRequest, "ttl must be positive")
			return
		}
	}

	id, err := srv.space.Out(req.Object, req.Access, ttl)
	if err != nil {
		writeError(w, httpStatusForError(err), err.Error())
		return
	}

	if err := writeJSON(w, http.StatusOK, outResponse{ID: id}); err != nil {
		log.Printf("handleOut: write response: %v", err)
	}
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

	callerID, err := srv.resolveCallerID(r)
	if err != nil {
		writeError(w, httpStatusForError(err), err.Error())
		return
	}

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
		if err := writeJSON(w, http.StatusOK, nil); err != nil {
			log.Printf("handleMatch: write null response: %v", err)
		}
		return
	}
	if err := writeJSON(w, http.StatusOK, result); err != nil {
		log.Printf("handleMatch: write response: %v", err)
	}
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

	if err := writeJSON(w, http.StatusOK, delResponse{Deleted: deleted}); err != nil {
		log.Printf("handleDel: write response: %v", err)
	}
}

func (srv *Server) handleLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	if err := writeJSON(w, http.StatusOK, srv.space.Limits()); err != nil {
		log.Printf("handleLimits: write response: %v", err)
	}
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
	if err := writeJSON(w, http.StatusOK, stats); err != nil {
		log.Printf("handleStats: write response: %v", err)
	}
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

	if err := writeJSON(w, http.StatusOK, matchTestResponse{Match: ok}); err != nil {
		log.Printf("handleMatchTest: write response: %v", err)
	}
}

func (srv *Server) resolveCallerID(r *http.Request) (string, error) {
	clientKey := r.Header.Get("X-ACE-Client-Key")
	if clientKey != "" {
		ident, err := srv.space.LookupKey(clientKey)
		if err != nil {
			return "", fmt.Errorf("key lookup: %w", err)
		}
		if ident == nil {
			return "", validationErr(fmt.Errorf("invalid client key"))
		}
		return ident.ID, nil
	}
	aceID := r.Header.Get("X-ACE-ID")
	if aceID != "" {
		if !srv.space.Config().InsecureIDs {
			return "", validationErr(fmt.Errorf(
				"X-ACE-ID requires --insecure-ids; use X-ACE-Client-Key"))
		}
		return aceID, nil
	}
	return "", nil
}

type regRequest struct {
	Name string `json:"name,omitempty"`
}

type regResponse struct {
	Key  string `json:"key"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (srv *Server) handleReg(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req regRequest
	json.NewDecoder(r.Body).Decode(&req)

	ident, err := srv.space.Register(req.Name)
	if err != nil {
		writeError(w, httpStatusForError(err), err.Error())
		return
	}

	if err := writeJSON(w, http.StatusOK, regResponse{
		Key:  ident.Key,
		ID:   ident.ID,
		Name: ident.Name,
	}); err != nil {
		log.Printf("handleReg: write response: %v", err)
	}
}

type regCheckResponse struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (srv *Server) handleRegCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	clientKey := r.Header.Get("X-ACE-Client-Key")
	if clientKey == "" {
		clientKey = r.URL.Query().Get("key")
	}
	queryID := r.URL.Query().Get("id")
	queryName := r.URL.Query().Get("name")

	var ident *Identity
	var err error
	var resp regCheckResponse

	switch {
	case clientKey != "":
		ident, err = srv.space.LookupKey(clientKey)
		if err == nil && ident != nil {
			resp = regCheckResponse{ID: ident.ID, Name: ident.Name}
		}
	case queryID != "":
		ident, err = srv.space.LookupID(queryID)
		if err == nil && ident != nil {
			resp = regCheckResponse{Name: ident.Name}
		}
	case queryName != "":
		ident, err = srv.space.LookupName(queryName)
		if err == nil && ident != nil {
			resp = regCheckResponse{ID: ident.ID}
		}
	default:
		writeError(w, http.StatusBadRequest, "provide key, id, or name")
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ident == nil {
		writeError(w, http.StatusNotFound, "identity not found")
		return
	}

	if err := writeJSON(w, http.StatusOK, resp); err != nil {
		log.Printf("handleRegCheck: write response: %v", err)
	}
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
		if n < 0 {
			return 0, fmt.Errorf("wait must not be negative")
		}
		return time.Duration(n * float64(time.Second)), nil
	}
	// Try as string.
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("expected number or duration string")
	}
	d, err := ParseWait(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("wait must not be negative")
	}
	return d, nil
}

func httpStatusForError(err error) int {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	if err := writeJSON(w, http.StatusOK, struct {
		Commit string `json:"commit"`
	}{Commit: Commit}); err != nil {
		log.Printf("handlePing: write response: %v", err)
	}
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
