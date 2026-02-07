package ace

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type Server struct {
	space   *Space
	mux     *http.ServeMux
	waitSem chan struct{}
}

func NewServer(space *Space, maxWaiters int) *Server {
	srv := &Server{space: space}
	if maxWaiters > 0 {
		srv.waitSem = make(chan struct{}, maxWaiters)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/out", srv.handleOut)
	mux.HandleFunc("/in", srv.handleIn)
	mux.HandleFunc("/rd", srv.handleRd)
	mux.HandleFunc("/limits", srv.handleLimits)
	mux.HandleFunc("/stats", srv.handleStats)
	srv.mux = mux
	return srv
}

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
	Wait    float64         `json:"wait,omitempty"`
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
		writeError(w, http.StatusBadRequest, err.Error())
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
	if callerID == "" {
		writeError(w, http.StatusBadRequest, "X-ACE-ID header is required")
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

	wait := time.Duration(req.Wait * float64(time.Second))

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
	var err error
	if remove {
		result, err = srv.space.In(r.Context(), callerID, req.Pattern, wait, req.Since)
	} else {
		result, err = srv.space.Rd(r.Context(), callerID, req.Pattern, wait, req.Since)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if result == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
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

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorResponse{Error: msg})
}
