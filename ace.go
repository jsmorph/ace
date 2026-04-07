// Package ace implements a tuple-space coordination service for software agents.
package ace

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const defaultTTL = 72 * time.Hour

// Space is a tuple space backed by SQLite.
type Space struct {
	db       *sql.DB
	idgen    *IDGen
	cfg      Config
	embedder embeddingProvider
	notifier *Notifier
	stop     chan struct{}
	timerMu  sync.Mutex
	timers   []*time.Timer
}

// Result holds an object returned by In or Rd.
type Result struct {
	ID       string          `json:"id"`
	Object   json.RawMessage `json:"object"`
	DeleteID string          `json:"delete_id,omitempty"`
}

// NewSpace opens or creates a tuple space at the given database path.
func NewSpace(dbPath string, cfg Config) (*Space, error) {
	switch cfg.Blocking {
	case BlockingPoll, BlockingNotify:
	default:
		return nil, fmt.Errorf("unrecognized blocking mode: %q", cfg.Blocking)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := initSchema(db); err != nil {
		return nil, errors.Join(fmt.Errorf("init schema: %w", err), db.Close())
	}

	s := &Space{
		db:       db,
		idgen:    NewIDGen(),
		cfg:      cfg,
		embedder: newOpenAIEmbeddingProvider(cfg.EmbeddingsURL),
		stop:     make(chan struct{}),
	}

	if cfg.Blocking == BlockingNotify {
		n, err := NewNotifier()
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create notifier: %w", err), db.Close())
		}
		s.notifier = n
	}

	return s, nil
}

// Close shuts down the space and closes the database.
func (s *Space) Close() error {
	close(s.stop)
	s.cancelTimers()
	return s.db.Close()
}

func (s *Space) trackTimer(t *time.Timer) {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	s.timers = append(s.timers, t)
}

func (s *Space) cancelTimers() {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	for _, t := range s.timers {
		t.Stop()
	}
	s.timers = nil
}

// Limits returns the active limits.
func (s *Space) Limits() Limits {
	return s.cfg.Limits
}

// Config returns the active configuration.
func (s *Space) Config() Config {
	return s.cfg
}

func (s *Space) logSlowOp(desc string) func() {
	if s.cfg.DBOperationTimeMonitorLimit <= 0 {
		return func() {}
	}
	start := time.Now()
	return func() {
		d := time.Since(start)
		if d >= s.cfg.DBOperationTimeMonitorLimit {
			log.Printf("WARN high latency %s for %s", d, desc)
		}
	}
}

// Out writes an object into the space. If ttl is zero, the default (72 hours) is used.
func (s *Space) Out(object json.RawMessage, access *Access, ttl time.Duration) (retID string, retErr error) {
	defer s.logSlowOp("out")()
	if ttl == 0 {
		ttl = defaultTTL
	}

	canonical, err := canonicalize(object)
	if err != nil {
		return "", validationErr(fmt.Errorf("canonicalize: %w", err))
	}
	object = canonical

	if err := s.cfg.Limits.ValidateTTL(ttl); err != nil {
		return "", validationErr(err)
	}
	if err := s.cfg.Limits.ValidateObject(object); err != nil {
		return "", validationErr(err)
	}
	if access != nil {
		raw, err := json.Marshal(access)
		if err != nil {
			return "", validationErr(fmt.Errorf("marshal access: %w", err))
		}
		if err := s.cfg.Limits.ValidateAccess(raw); err != nil {
			return "", validationErr(err)
		}
		if !s.cfg.InsecureIDs {
			if err := ValidateAccessPrefixes(access); err != nil {
				return "", err
			}
		}
		resolved, err := s.ResolveAccess(access)
		if err != nil {
			return "", err
		}
		access = resolved
	}

	branches, err := ExtractBranches(object)
	if err != nil {
		return "", validationErr(fmt.Errorf("extract branches: %w", err))
	}

	id := s.idgen.Next()
	expires := time.Now().UTC().Add(ttl).Format(timestampFormat)
	jsonStr := string(object)

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			retErr = errors.Join(retErr, fmt.Errorf("rollback: %w", err))
		}
	}()

	if _, err := tx.Exec("INSERT INTO objects (id, json, expires) VALUES (?, ?, ?)", id, jsonStr, expires); err != nil {
		return "", fmt.Errorf("insert object: %w", err)
	}

	if access != nil {
		if access.In != nil && len(access.In) == 0 {
			if _, err := tx.Exec("INSERT INTO access (id, type, iid) VALUES (?, 'in', '!')", id); err != nil {
				return "", fmt.Errorf("insert access in deny: %w", err)
			}
		}
		for _, iid := range access.In {
			if _, err := tx.Exec("INSERT INTO access (id, type, iid) VALUES (?, 'in', ?)", id, iid); err != nil {
				return "", fmt.Errorf("insert access in: %w", err)
			}
		}
		if access.Rd != nil && len(access.Rd) == 0 {
			if _, err := tx.Exec("INSERT INTO access (id, type, iid) VALUES (?, 'rd', '!')", id); err != nil {
				return "", fmt.Errorf("insert access rd deny: %w", err)
			}
		}
		for _, iid := range access.Rd {
			if _, err := tx.Exec("INSERT INTO access (id, type, iid) VALUES (?, 'rd', ?)", id, iid); err != nil {
				return "", fmt.Errorf("insert access rd: %w", err)
			}
		}
	}

	for _, b := range branches {
		if _, err := tx.Exec("INSERT INTO branches (id, b) VALUES (?, ?)", id, b); err != nil {
			return "", fmt.Errorf("insert branch: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	if s.notifier != nil {
		if err := s.notifier.Notify(object); err != nil {
			return id, fmt.Errorf("notify: %w", err)
		}
	}

	return id, nil
}

// In finds and removes the earliest matching object. If wait is positive and no
// match exists, In blocks until a match appears or the deadline passes.
func (s *Space) In(ctx context.Context, callerID string, pattern json.RawMessage, wait time.Duration, since string) (*Result, error) {
	return s.fetch(ctx, callerID, pattern, wait, since, "in", true)
}

// Rd finds the earliest matching object without removing it. Blocking behavior matches In.
func (s *Space) Rd(ctx context.Context, callerID string, pattern json.RawMessage, wait time.Duration, since string) (*Result, error) {
	return s.fetch(ctx, callerID, pattern, wait, since, "rd", false)
}

func (s *Space) fetch(ctx context.Context, callerID string, pattern json.RawMessage, wait time.Duration, since string, accessType string, remove bool) (*Result, error) {
	if err := s.cfg.Limits.ValidatePattern(pattern); err != nil {
		return nil, validationErr(err)
	}
	if err := s.cfg.Limits.ValidateCallerID(callerID); err != nil {
		return nil, validationErr(err)
	}

	parsed, err := ParsePattern(pattern)
	if err != nil {
		return nil, validationErr(fmt.Errorf("parse pattern: %w", err))
	}

	queryFn := func() (*Result, error) {
		return s.executeMatch(ctx, parsed, accessType, callerID, since, remove)
	}

	result, err := queryFn()
	if err != nil {
		return nil, err
	}
	if result != nil || wait <= 0 {
		return result, nil
	}

	if s.notifier != nil && !parsed.HasEmbeddings() {
		return s.waitNotify(ctx, pattern, queryFn, time.Now().Add(wait))
	}
	return s.poll(ctx, queryFn, time.Now().Add(wait))
}

func (s *Space) executeMatch(ctx context.Context, pattern ParsedPattern, accessType string, callerID string, since string, remove bool) (_ *Result, retErr error) {
	defer s.logSlowOp(accessType)()
	if pattern.HasEmbeddings() {
		return s.executeEmbeddingMatch(ctx, pattern, accessType, callerID, since, remove)
	}

	query, args := BuildMatchQuery(pattern.Exact, accessType, callerID, since, time.Now())

	if !remove {
		var id, jsonStr string
		err := s.db.QueryRow(query, args...).Scan(&id, &jsonStr)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("query: %w", err)
		}
		return &Result{ID: id, Object: json.RawMessage(jsonStr)}, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			retErr = errors.Join(retErr, fmt.Errorf("rollback: %w", err))
		}
	}()

	var id, jsonStr string
	err = tx.QueryRow(query, args...).Scan(&id, &jsonStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	result := &Result{ID: id, Object: json.RawMessage(jsonStr)}

	if s.cfg.Deletes {
		did, err := generateDeleteID()
		if err != nil {
			return nil, fmt.Errorf("generate delete id: %w", err)
		}
		invisibleUntil := time.Now().UTC().Add(s.cfg.VisibilityTimeout).Format(timestampFormat)
		if _, err := tx.Exec("UPDATE objects SET delete_id = ?, invisible_until = ? WHERE id = ?", did, invisibleUntil, id); err != nil {
			return nil, fmt.Errorf("mark invisible: %w", err)
		}
		result.DeleteID = did
	} else {
		if _, err := tx.Exec("DELETE FROM objects WHERE id = ?", id); err != nil {
			return nil, fmt.Errorf("delete object: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	if s.notifier != nil && result.DeleteID != "" {
		obj := json.RawMessage(jsonStr)
		t := time.AfterFunc(s.cfg.VisibilityTimeout, func() {
			select {
			case <-s.stop:
				return
			default:
			}
			s.notifier.Notify(obj)
		})
		s.trackTimer(t)
	}

	return result, nil
}

type matchCandidate struct {
	ID     string
	Object json.RawMessage
}

func (s *Space) executeEmbeddingMatch(ctx context.Context, pattern ParsedPattern, accessType string, callerID string, since string, remove bool) (*Result, error) {
	candidates, err := s.listMatchCandidates(pattern.Exact, accessType, callerID, since)
	if err != nil {
		return nil, err
	}

	for _, candidate := range candidates {
		ok, err := matchWithProvider(ctx, candidate.Object, pattern, s.embedder)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		if !remove {
			return &Result{ID: candidate.ID, Object: candidate.Object}, nil
		}

		result, claimed, err := s.claimCandidate(candidate)
		if err != nil {
			return nil, err
		}
		if claimed {
			return result, nil
		}
	}

	return nil, nil
}

func (s *Space) listMatchCandidates(branches []PatternBranch, accessType string, callerID string, since string) ([]matchCandidate, error) {
	query, args := BuildScanQuery(branches, accessType, callerID, since, time.Now())

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var candidates []matchCandidate
	for rows.Next() {
		var id string
		var object string
		if err := rows.Scan(&id, &object); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		candidates = append(candidates, matchCandidate{
			ID:     id,
			Object: json.RawMessage(object),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidates: %w", err)
	}
	return candidates, nil
}

func (s *Space) claimCandidate(candidate matchCandidate) (_ *Result, claimed bool, retErr error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			retErr = errors.Join(retErr, fmt.Errorf("rollback: %w", err))
		}
	}()

	now := time.Now().UTC().Format(timestampFormat)
	result := &Result{
		ID:     candidate.ID,
		Object: candidate.Object,
	}

	if s.cfg.Deletes {
		did, err := generateDeleteID()
		if err != nil {
			return nil, false, fmt.Errorf("generate delete id: %w", err)
		}
		invisibleUntil := time.Now().UTC().Add(s.cfg.VisibilityTimeout).Format(timestampFormat)
		res, err := tx.Exec(
			"UPDATE objects SET delete_id = ?, invisible_until = ? WHERE id = ? AND expires > ? AND (invisible_until IS NULL OR invisible_until <= ?)",
			did, invisibleUntil, candidate.ID, now, now,
		)
		if err != nil {
			return nil, false, fmt.Errorf("mark invisible: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return nil, false, fmt.Errorf("rows affected: %w", err)
		}
		if rows == 0 {
			return nil, false, nil
		}
		result.DeleteID = did
	} else {
		res, err := tx.Exec(
			"DELETE FROM objects WHERE id = ? AND expires > ? AND (invisible_until IS NULL OR invisible_until <= ?)",
			candidate.ID, now, now,
		)
		if err != nil {
			return nil, false, fmt.Errorf("delete object: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return nil, false, fmt.Errorf("rows affected: %w", err)
		}
		if rows == 0 {
			return nil, false, nil
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit: %w", err)
	}

	if s.notifier != nil && result.DeleteID != "" {
		obj := append(json.RawMessage(nil), candidate.Object...)
		t := time.AfterFunc(s.cfg.VisibilityTimeout, func() {
			select {
			case <-s.stop:
				return
			default:
			}
			s.notifier.Notify(obj)
		})
		s.trackTimer(t)
	}

	return result, true, nil
}

func (s *Space) waitNotify(ctx context.Context, pattern json.RawMessage, queryFn func() (*Result, error), deadline time.Time) (*Result, error) {
	wid, ch, err := s.notifier.Register(pattern)
	if err != nil {
		return nil, fmt.Errorf("register notification: %w", err)
	}
	defer func() {
		if err := s.notifier.Deregister(wid); err != nil {
			log.Printf("deregister waiter: %v", err)
		}
	}()

	result, err := queryFn()
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()

	for {
		select {
		case <-ch:
			result, err := queryFn()
			if err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
		case <-timer.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.stop:
			return nil, nil
		}
	}
}

func (s *Space) poll(ctx context.Context, queryFn func() (*Result, error), deadline time.Time) (*Result, error) {
	intervals := []time.Duration{
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		500 * time.Millisecond,
	}
	idx := 0
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		delay := intervals[idx]
		if idx < len(intervals)-1 {
			idx++
		}
		if delay > remaining {
			delay = remaining
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.stop:
			return nil, nil
		}
		result, err := queryFn()
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
	}
}

// Del permanently deletes an object using the delete_id returned by In
// when explicit deletes are enabled.
func (s *Space) Del(deleteID string) (bool, error) {
	defer s.logSlowOp("del")()
	if deleteID == "" {
		return false, fmt.Errorf("delete_id is required")
	}
	now := time.Now().UTC().Format(timestampFormat)
	res, err := s.db.Exec("DELETE FROM objects WHERE delete_id = ? AND invisible_until > ?", deleteID, now)
	if err != nil {
		return false, fmt.Errorf("delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

func generateDeleteID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// DeleteExpired removes all objects past their TTL and returns the count deleted.
func (s *Space) DeleteExpired() (int64, error) {
	defer s.logSlowOp("delete expired")()
	now := time.Now().UTC().Format(timestampFormat)
	res, err := s.db.Exec("DELETE FROM objects WHERE expires <= ?", now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func canonicalize(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("object must be a JSON object, not null")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	return b[:len(b)-1], nil // Encode appends a newline
}

// Stats reports storage statistics.
type Stats struct {
	Objects           int     `json:"objects"`
	Expired           int     `json:"expired"`
	Branches          int     `json:"branches"`
	AccessRecords     int     `json:"access_records"`
	AvgBranchLength   float64 `json:"avg_branch_length"`
	AvgBranchesPerObj float64 `json:"avg_branches_per_object"`
	AvgAccessIn       float64 `json:"avg_access_in_per_object"`
	AvgAccessRd       float64 `json:"avg_access_rd_per_object"`
}

// Stats returns storage statistics.
func (s *Space) Stats() (*Stats, error) {
	defer s.logSlowOp("stats")()
	var st Stats

	if err := s.db.QueryRow("SELECT COUNT(*) FROM objects").Scan(&st.Objects); err != nil {
		return nil, fmt.Errorf("count objects: %w", err)
	}
	now := time.Now().UTC().Format(timestampFormat)
	if err := s.db.QueryRow("SELECT COUNT(*) FROM objects WHERE expires <= ?", now).Scan(&st.Expired); err != nil {
		return nil, fmt.Errorf("count expired: %w", err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM branches").Scan(&st.Branches); err != nil {
		return nil, fmt.Errorf("count branches: %w", err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM access").Scan(&st.AccessRecords); err != nil {
		return nil, fmt.Errorf("count access: %w", err)
	}

	if st.Branches > 0 {
		if err := s.db.QueryRow("SELECT AVG(LENGTH(b)) FROM branches").Scan(&st.AvgBranchLength); err != nil {
			return nil, fmt.Errorf("avg branch length: %w", err)
		}
	}

	if st.Objects > 0 {
		if err := s.db.QueryRow("SELECT AVG(cnt) FROM (SELECT COUNT(*) as cnt FROM branches GROUP BY id)").Scan(&st.AvgBranchesPerObj); err != nil {
			return nil, fmt.Errorf("avg branches per object: %w", err)
		}

		var avgIn sql.NullFloat64
		if err := s.db.QueryRow("SELECT CAST(COUNT(*) AS REAL) / ? FROM access WHERE type = 'in'", st.Objects).Scan(&avgIn); err != nil {
			return nil, fmt.Errorf("avg access in: %w", err)
		}
		if avgIn.Valid {
			st.AvgAccessIn = avgIn.Float64
		}

		var avgRd sql.NullFloat64
		if err := s.db.QueryRow("SELECT CAST(COUNT(*) AS REAL) / ? FROM access WHERE type = 'rd'", st.Objects).Scan(&avgRd); err != nil {
			return nil, fmt.Errorf("avg access rd: %w", err)
		}
		if avgRd.Valid {
			st.AvgAccessRd = avgRd.Float64
		}
	}

	return &st, nil
}
