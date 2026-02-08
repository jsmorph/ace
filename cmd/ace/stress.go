package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morphism/ace"
)

// HTTP helpers

func httpOut(client *http.Client, baseURL string, object string, access string, ttl string) (string, error) {
	parts := []string{fmt.Sprintf(`"object":%s`, object)}
	if access != "" {
		parts = append(parts, fmt.Sprintf(`"access":%s`, access))
	}
	if ttl != "" {
		parts = append(parts, fmt.Sprintf(`"ttl":%q`, ttl))
	}
	body := "{" + strings.Join(parts, ",") + "}"
	resp, err := client.Post(baseURL+"/out", "application/json", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func httpMatch(client *http.Client, baseURL string, op string, pattern string, callerID string, wait int, since string) (*ace.Result, int, error) {
	parts := []string{fmt.Sprintf(`"pattern":%s`, pattern)}
	if wait > 0 {
		parts = append(parts, fmt.Sprintf(`"wait":%d`, wait))
	}
	if since != "" {
		parts = append(parts, fmt.Sprintf(`"since":%q`, since))
	}
	body := "{" + strings.Join(parts, ",") + "}"
	req, err := http.NewRequest("POST", baseURL+"/"+op, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if callerID != "" {
		req.Header.Set("X-ACE-ID", callerID)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	var result *ace.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 200, err
	}
	return result, 200, nil
}

func httpRd(client *http.Client, baseURL string, pattern string, callerID string) (*ace.Result, error) {
	r, _, err := httpMatch(client, baseURL, "rd", pattern, callerID, 0, "")
	return r, err
}

func httpRdSince(client *http.Client, baseURL string, pattern string, callerID string, since string) (*ace.Result, error) {
	r, _, err := httpMatch(client, baseURL, "rd", pattern, callerID, 0, since)
	return r, err
}

func httpIn(client *http.Client, baseURL string, pattern string, callerID string) (*ace.Result, error) {
	r, _, err := httpMatch(client, baseURL, "in", pattern, callerID, 0, "")
	return r, err
}

func httpStats(client *http.Client, baseURL string) (*ace.Stats, error) {
	resp, err := client.Get(baseURL + "/stats")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	var st ace.Stats
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

func httpOutRaw(client *http.Client, baseURL string, body string) (int, error) {
	resp, err := client.Post(baseURL+"/out", "application/json", strings.NewReader(body))
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

// Scenario infrastructure

type scenario struct {
	name string
	fn   func(client *http.Client, url string) error
}

func runScenario(s scenario) bool {
	return runScenarioWithConfig(s, nil)
}

func runScenarioWithConfig(s scenario, cfgFn func(*ace.Config)) bool {
	dir, err := os.MkdirTemp("", "ace-test-*")
	if err != nil {
		fmt.Printf("  FAIL %s: create temp dir: %v\n", s.name, err)
		return false
	}
	defer os.RemoveAll(dir)

	cfg := ace.DefaultConfig()
	cfg.Blocking = ace.BlockingNotify
	if cfgFn != nil {
		cfgFn(&cfg)
	}
	dbPath := filepath.Join(dir, "test.db")
	space, err := ace.NewSpace(dbPath, cfg)
	if err != nil {
		fmt.Printf("  FAIL %s: create space: %v\n", s.name, err)
		return false
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	srv := ace.NewServer(space, 0)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := ts.Client()
	if err := s.fn(client, ts.URL); err != nil {
		fmt.Printf("  FAIL %s: %v\n", s.name, err)
		return false
	}
	fmt.Printf("  PASS %s\n", s.name)
	return true
}

// Scenarios

func scenarioOutRdIn(client *http.Client, url string) error {
	id, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpRd(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("rd: %w", err)
	}
	if r == nil || r.ID != id {
		return fmt.Errorf("rd: expected id %q, got %v", id, r)
	}

	r, err = httpIn(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("in: %w", err)
	}
	if r == nil || r.ID != id {
		return fmt.Errorf("in: expected id %q, got %v", id, r)
	}

	r, err = httpIn(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("in2: %w", err)
	}
	if r != nil {
		return fmt.Errorf("in2: expected null after removal, got %v", r)
	}
	return nil
}

func scenarioRdDoesNotRemove(client *http.Client, url string) error {
	id, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	for i := 0; i < 3; i++ {
		r, err := httpRd(client, url, `{"a":1}`, "")
		if err != nil {
			return fmt.Errorf("rd %d: %w", i, err)
		}
		if r == nil || r.ID != id {
			return fmt.Errorf("rd %d: expected id %q, got %v", i, id, r)
		}
	}
	return nil
}

func scenarioOrdering(client *http.Client, url string) error {
	id1, err := httpOut(client, url, `{"x":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out1: %w", err)
	}
	id2, err := httpOut(client, url, `{"x":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out2: %w", err)
	}

	r, err := httpRd(client, url, `{"x":1}`, "")
	if err != nil {
		return fmt.Errorf("rd: %w", err)
	}
	if r == nil || r.ID != id1 {
		return fmt.Errorf("rd: expected earliest %q, got %v", id1, r)
	}

	r, err = httpIn(client, url, `{"x":1}`, "")
	if err != nil {
		return fmt.Errorf("in1: %w", err)
	}
	if r == nil || r.ID != id1 {
		return fmt.Errorf("in1: expected %q, got %v", id1, r)
	}

	r, err = httpRd(client, url, `{"x":1}`, "")
	if err != nil {
		return fmt.Errorf("rd2: %w", err)
	}
	if r == nil || r.ID != id2 {
		return fmt.Errorf("rd2: expected %q, got %v", id2, r)
	}
	return nil
}

func scenarioExactMatch(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpRd(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("rd match: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd match: expected result")
	}

	r, err = httpRd(client, url, `{"a":2}`, "")
	if err != nil {
		return fmt.Errorf("rd nomatch: %w", err)
	}
	if r != nil {
		return fmt.Errorf("rd nomatch: expected null, got %v", r)
	}
	return nil
}

func scenarioArrayPattern(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out1: %w", err)
	}
	_, err = httpOut(client, url, `{"a":3}`, "", "")
	if err != nil {
		return fmt.Errorf("out2: %w", err)
	}

	r, err := httpRd(client, url, `{"a":[1,2]}`, "")
	if err != nil {
		return fmt.Errorf("rd [1,2] vs 1: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd [1,2] vs 1: expected match")
	}

	// Consume the matching one and check the non-matching one.
	_, err = httpIn(client, url, `{"a":[1,2]}`, "")
	if err != nil {
		return fmt.Errorf("in [1,2]: %w", err)
	}

	r, err = httpRd(client, url, `{"a":[1,2]}`, "")
	if err != nil {
		return fmt.Errorf("rd [1,2] vs 3: %w", err)
	}
	if r != nil {
		return fmt.Errorf("rd [1,2] vs 3: expected null, got %v", r)
	}
	return nil
}

func scenarioNestedPattern(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":{"b":1,"c":2,"d":3}}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpRd(client, url, `{"a":{"b":1,"c":2}}`, "")
	if err != nil {
		return fmt.Errorf("rd match: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd match: expected result")
	}

	r, err = httpRd(client, url, `{"a":{"b":1,"c":9}}`, "")
	if err != nil {
		return fmt.Errorf("rd nomatch: %w", err)
	}
	if r != nil {
		return fmt.Errorf("rd nomatch: expected null, got %v", r)
	}
	return nil
}

func scenarioEmptyPattern(client *http.Client, url string) error {
	for i := 0; i < 3; i++ {
		_, err := httpOut(client, url, fmt.Sprintf(`{"i":%d}`, i), "", "")
		if err != nil {
			return fmt.Errorf("out %d: %w", i, err)
		}
	}

	for i := 0; i < 3; i++ {
		r, err := httpIn(client, url, `{}`, "")
		if err != nil {
			return fmt.Errorf("in %d: %w", i, err)
		}
		if r == nil {
			return fmt.Errorf("in %d: expected result", i)
		}
	}

	r, err := httpIn(client, url, `{}`, "")
	if err != nil {
		return fmt.Errorf("in final: %w", err)
	}
	if r != nil {
		return fmt.Errorf("in final: expected null after consuming all, got %v", r)
	}
	return nil
}

func scenarioAccessIn(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, `{"in":["alpha"]}`, "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpIn(client, url, `{"a":1}`, "beta")
	if err != nil {
		return fmt.Errorf("in beta: %w", err)
	}
	if r != nil {
		return fmt.Errorf("in beta: expected null, got %v", r)
	}

	r, err = httpIn(client, url, `{"a":1}`, "alpha")
	if err != nil {
		return fmt.Errorf("in alpha: %w", err)
	}
	if r == nil {
		return fmt.Errorf("in alpha: expected result")
	}
	return nil
}

func scenarioAccessRd(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, `{"rd":["alpha"]}`, "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpRd(client, url, `{"a":1}`, "beta")
	if err != nil {
		return fmt.Errorf("rd beta: %w", err)
	}
	if r != nil {
		return fmt.Errorf("rd beta: expected null, got %v", r)
	}

	r, err = httpRd(client, url, `{"a":1}`, "alpha")
	if err != nil {
		return fmt.Errorf("rd alpha: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd alpha: expected result")
	}
	return nil
}

func scenarioNoAccessNoHeader(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpRd(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("rd no header: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd no header: expected result")
	}

	r, err = httpIn(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("in no header: %w", err)
	}
	if r == nil {
		return fmt.Errorf("in no header: expected result")
	}
	return nil
}

func scenarioAccessMixed(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"kind":"restricted"}`, `{"in":["alpha"]}`, "")
	if err != nil {
		return fmt.Errorf("out restricted: %w", err)
	}
	id2, err := httpOut(client, url, `{"kind":"open"}`, "", "")
	if err != nil {
		return fmt.Errorf("out open: %w", err)
	}

	// beta can only see the unrestricted object.
	r, err := httpIn(client, url, `{}`, "beta")
	if err != nil {
		return fmt.Errorf("in beta: %w", err)
	}
	if r == nil {
		return fmt.Errorf("in beta: expected the unrestricted object")
	}
	if r.ID != id2 {
		return fmt.Errorf("in beta: expected id %q (open), got %q", id2, r.ID)
	}

	// beta gets null now: only the restricted object remains.
	r, err = httpIn(client, url, `{}`, "beta")
	if err != nil {
		return fmt.Errorf("in beta 2: %w", err)
	}
	if r != nil {
		return fmt.Errorf("in beta 2: expected null, got %v", r)
	}

	// alpha can consume the restricted object.
	r, err = httpIn(client, url, `{}`, "alpha")
	if err != nil {
		return fmt.Errorf("in alpha: %w", err)
	}
	if r == nil {
		return fmt.Errorf("in alpha: expected the restricted object")
	}
	return nil
}

func scenarioAccessEmptyArray(client *http.Client, url string) error {
	// Empty in list: nobody can consume, anyone can read.
	_, err := httpOut(client, url, `{"a":1}`, `{"in":[]}`, "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpIn(client, url, `{"a":1}`, "agent-1")
	if err != nil {
		return fmt.Errorf("in: %w", err)
	}
	if r != nil {
		return fmt.Errorf("in: expected null (empty in list denies all)")
	}

	r, err = httpRd(client, url, `{"a":1}`, "agent-1")
	if err != nil {
		return fmt.Errorf("rd: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd: expected result (rd is unrestricted)")
	}

	// Empty rd list: nobody can read, anyone can consume.
	_, err = httpOut(client, url, `{"b":1}`, `{"rd":[]}`, "")
	if err != nil {
		return fmt.Errorf("out2: %w", err)
	}

	r, err = httpRd(client, url, `{"b":1}`, "agent-1")
	if err != nil {
		return fmt.Errorf("rd2: %w", err)
	}
	if r != nil {
		return fmt.Errorf("rd2: expected null (empty rd list denies all)")
	}

	r, err = httpIn(client, url, `{"b":1}`, "agent-1")
	if err != nil {
		return fmt.Errorf("in2: %w", err)
	}
	if r == nil {
		return fmt.Errorf("in2: expected result (in is unrestricted)")
	}

	// Both empty: should be rejected.
	status, err := httpOutRaw(client, url, `{"object":{"c":1},"access":{"in":[],"rd":[]}}`)
	if err != nil {
		return fmt.Errorf("out3: %w", err)
	}
	if status != 400 {
		return fmt.Errorf("out3: expected 400 for inaccessible object, got %d", status)
	}

	return nil
}

func scenarioSinceFiltering(client *http.Client, url string) error {
	id1, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out1: %w", err)
	}
	id2, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out2: %w", err)
	}

	r, err := httpRdSince(client, url, `{"a":1}`, "", id1)
	if err != nil {
		return fmt.Errorf("rd since: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd since: expected second object")
	}
	if r.ID != id2 {
		return fmt.Errorf("rd since: expected %q, got %q", id2, r.ID)
	}
	return nil
}

func scenarioTTLExpiration(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, "", "PT1S")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	time.Sleep(1100 * time.Millisecond)

	r, err := httpRd(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("rd: %w", err)
	}
	if r != nil {
		return fmt.Errorf("rd: expected null after TTL expiration, got %v", r)
	}
	return nil
}

func scenarioCanonicalization(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"b":1,"a":2}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpRd(client, url, `{}`, "")
	if err != nil {
		return fmt.Errorf("rd: %w", err)
	}
	if r == nil {
		return fmt.Errorf("rd: expected result")
	}
	if string(r.Object) != `{"a":2,"b":1}` {
		return fmt.Errorf("rd: expected canonical JSON {\"a\":2,\"b\":1}, got %s", r.Object)
	}
	return nil
}

func scenarioLimitsEnforced(client *http.Client, url string) error {
	big := `{"object":{"x":"` + strings.Repeat("a", 3000) + `"}}`
	status, err := httpOutRaw(client, url, big)
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}
	if status != 400 {
		return fmt.Errorf("expected 400 for oversized object, got %d", status)
	}
	return nil
}

func scenarioStats(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, `{"in":["x"],"rd":["y","z"]}`, "")
	if err != nil {
		return fmt.Errorf("out1: %w", err)
	}
	_, err = httpOut(client, url, `{"b":2}`, "", "")
	if err != nil {
		return fmt.Errorf("out2: %w", err)
	}

	st, err := httpStats(client, url)
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	if st.Objects != 2 {
		return fmt.Errorf("expected 2 objects, got %d", st.Objects)
	}
	if st.AccessRecords != 3 {
		return fmt.Errorf("expected 3 access records (1 in + 2 rd), got %d", st.AccessRecords)
	}
	return nil
}

func httpDel(client *http.Client, baseURL string, deleteID string) (bool, error) {
	body := fmt.Sprintf(`{"delete_id":%q}`, deleteID)
	resp, err := client.Post(baseURL+"/del", "application/json", strings.NewReader(body))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return false, err
	}
	return out.Deleted, nil
}

func httpInWithDelete(client *http.Client, baseURL string, pattern string, callerID string) (*ace.Result, error) {
	r, _, err := httpMatch(client, baseURL, "in", pattern, callerID, 0, "")
	return r, err
}

func scenarioExplicitDelete(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpInWithDelete(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("in: %w", err)
	}
	if r == nil {
		return fmt.Errorf("in: expected result")
	}
	if r.DeleteID == "" {
		return fmt.Errorf("in: expected delete_id")
	}

	// Object should be invisible.
	r2, err := httpRd(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("rd after in: %w", err)
	}
	if r2 != nil {
		return fmt.Errorf("rd after in: expected null (object should be invisible)")
	}

	// Del should succeed.
	deleted, err := httpDel(client, url, r.DeleteID)
	if err != nil {
		return fmt.Errorf("del: %w", err)
	}
	if !deleted {
		return fmt.Errorf("del: expected deleted=true")
	}

	// Second del should return false.
	deleted, err = httpDel(client, url, r.DeleteID)
	if err != nil {
		return fmt.Errorf("del2: %w", err)
	}
	if deleted {
		return fmt.Errorf("del2: expected deleted=false")
	}

	return nil
}

func scenarioExplicitDeleteTimeout(client *http.Client, url string) error {
	_, err := httpOut(client, url, `{"a":1}`, "", "")
	if err != nil {
		return fmt.Errorf("out: %w", err)
	}

	r, err := httpInWithDelete(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("in: %w", err)
	}
	if r == nil || r.DeleteID == "" {
		return fmt.Errorf("in: expected result with delete_id")
	}

	// Wait for visibility timeout to expire (server uses 100ms for test).
	time.Sleep(200 * time.Millisecond)

	// Object should reappear.
	r2, err := httpRd(client, url, `{"a":1}`, "")
	if err != nil {
		return fmt.Errorf("rd after timeout: %w", err)
	}
	if r2 == nil {
		return fmt.Errorf("rd after timeout: expected object to reappear")
	}

	// Del with expired id should fail.
	deleted, err := httpDel(client, url, r.DeleteID)
	if err != nil {
		return fmt.Errorf("del expired: %w", err)
	}
	if deleted {
		return fmt.Errorf("del expired: expected deleted=false")
	}

	return nil
}

// Concurrent throughput test

type stressObject struct {
	W int `json:"w"`
	S int `json:"s"`
}

func scenarioConcurrent(writers, readers, requests int, wait int, maxWaiters int, blocking string) error {
	totalObjects := writers * requests

	cfg := ace.DefaultConfig()
	cfg.Blocking = ace.BlockingMode(blocking)

	dir, err := os.MkdirTemp("", "ace-concurrent-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "stress.db")
	space, err := ace.NewSpace(dbPath, cfg)
	if err != nil {
		return fmt.Errorf("create space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close space: %v", err)
		}
	}()

	srv := ace.NewServer(space, maxWaiters)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: writers + readers,
		},
	}

	fmt.Printf("  concurrent: %d writers x %d readers x %d requests = %d objects (blocking=%s, max-waiters=%d)\n",
		writers, readers, requests, totalObjects, blocking, maxWaiters)

	consumed := make(chan stressObject, totalObjects)
	writersDone := make(chan struct{})
	var writeErrors atomic.Int64
	var readErrors atomic.Int64
	var rejects atomic.Int64

	start := time.Now()

	var writerWG sync.WaitGroup
	for w := 0; w < writers; w++ {
		writerWG.Add(1)
		go func(writerID int) {
			defer writerWG.Done()
			for s := 0; s < requests; s++ {
				body := fmt.Sprintf(`{"object":{"w":%d,"s":%d}}`, writerID, s)
				resp, err := client.Post(ts.URL+"/out", "application/json", strings.NewReader(body))
				if err != nil {
					writeErrors.Add(1)
					log.Printf("writer %d seq %d: %v", writerID, s, err)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					writeErrors.Add(1)
					log.Printf("writer %d seq %d: status %d", writerID, s, resp.StatusCode)
				}
			}
		}(w)
	}

	var readerWG sync.WaitGroup
	for r := 0; r < readers; r++ {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			for {
				obj, status, err := doIn(client, ts.URL, wait)
				if err != nil {
					readErrors.Add(1)
					log.Printf("reader: %v", err)
					continue
				}
				if status == 503 {
					rejects.Add(1)
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if obj != nil {
					consumed <- *obj
					continue
				}
				select {
				case <-writersDone:
					drain(client, ts.URL, consumed, &readErrors)
					return
				default:
				}
			}
		}()
	}

	writerWG.Wait()
	close(writersDone)
	readerWG.Wait()
	close(consumed)

	elapsed := time.Since(start)

	seen := make(map[stressObject]int)
	for obj := range consumed {
		seen[obj]++
	}

	var missing, duplicated, unexpected int
	for w := 0; w < writers; w++ {
		for s := 0; s < requests; s++ {
			key := stressObject{W: w, S: s}
			count := seen[key]
			if count == 0 {
				missing++
				if missing <= 10 {
					log.Printf("MISSING: writer=%d seq=%d", w, s)
				}
			} else if count > 1 {
				duplicated++
				if duplicated <= 10 {
					log.Printf("DUPLICATE: writer=%d seq=%d count=%d", w, s, count)
				}
			}
			delete(seen, key)
		}
	}
	for key, count := range seen {
		unexpected++
		if unexpected <= 10 {
			log.Printf("UNEXPECTED: writer=%d seq=%d count=%d", key.W, key.S, count)
		}
	}

	throughput := float64(totalObjects) / elapsed.Seconds()
	fmt.Printf("  elapsed: %.3fs, throughput: %.1f obj/sec, 503s: %d\n", elapsed.Seconds(), throughput, rejects.Load())

	if missing > 0 || duplicated > 0 || unexpected > 0 || writeErrors.Load() > 0 || readErrors.Load() > 0 {
		return fmt.Errorf("missing=%d duplicated=%d unexpected=%d write_errors=%d read_errors=%d",
			missing, duplicated, unexpected, writeErrors.Load(), readErrors.Load())
	}
	return nil
}

func doIn(client *http.Client, baseURL string, wait int) (*stressObject, int, error) {
	body := fmt.Sprintf(`{"pattern":{},"wait":%d}`, wait)
	req, err := http.NewRequest("POST", baseURL+"/in", strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-ACE-ID", "stress")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 503 {
		io.Copy(io.Discard, resp.Body)
		return nil, 503, nil
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, b)
	}

	var result *ace.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 200, fmt.Errorf("decode: %w", err)
	}
	if result == nil {
		return nil, 200, nil
	}

	var obj stressObject
	if err := json.Unmarshal(result.Object, &obj); err != nil {
		return nil, 200, fmt.Errorf("unmarshal object: %w", err)
	}
	return &obj, 200, nil
}

func drain(client *http.Client, baseURL string, consumed chan<- stressObject, readErrors *atomic.Int64) {
	for {
		obj, status, err := doIn(client, baseURL, 0)
		if err != nil {
			readErrors.Add(1)
			log.Printf("drain: %v", err)
			return
		}
		if status == 503 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if obj == nil {
			return
		}
		consumed <- *obj
	}
}

// Entry point

func cmdTest(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	writers := fs.Int("writers", 4, "number of writer goroutines")
	readers := fs.Int("readers", 4, "number of reader goroutines")
	requests := fs.Int("requests", 100, "objects per writer")
	wait := fs.Int("wait", 2, "blocking read timeout (seconds)")
	maxWaiters := fs.Int("max-waiters", 0, "max concurrent blocking clients (0 = unlimited)")
	blocking := fs.String("blocking", "notify", "blocking mode: polling or notify")
	fs.Parse(args)

	scenarios := []scenario{
		{"out-rd-in", scenarioOutRdIn},
		{"rd-does-not-remove", scenarioRdDoesNotRemove},
		{"ordering", scenarioOrdering},
		{"exact-match", scenarioExactMatch},
		{"array-pattern", scenarioArrayPattern},
		{"nested-pattern", scenarioNestedPattern},
		{"empty-pattern", scenarioEmptyPattern},
		{"access-in", scenarioAccessIn},
		{"access-rd", scenarioAccessRd},
		{"no-access-no-header", scenarioNoAccessNoHeader},
		{"access-mixed", scenarioAccessMixed},
		{"access-empty-array", scenarioAccessEmptyArray},
		{"since-filtering", scenarioSinceFiltering},
		{"ttl-expiration", scenarioTTLExpiration},
		{"canonicalization", scenarioCanonicalization},
		{"limits-enforced", scenarioLimitsEnforced},
		{"stats", scenarioStats},
	}

	deleteScenarios := []scenario{
		{"explicit-delete", scenarioExplicitDelete},
		{"explicit-delete-timeout", scenarioExplicitDeleteTimeout},
	}
	deleteCfg := func(cfg *ace.Config) {
		cfg.Deletes = true
		cfg.VisibilityTimeout = 100 * time.Millisecond
	}

	failed := 0
	fmt.Println("scenarios:")
	for _, s := range scenarios {
		if !runScenario(s) {
			failed++
		}
	}
	for _, s := range deleteScenarios {
		if !runScenarioWithConfig(s, deleteCfg) {
			failed++
		}
	}

	fmt.Println("\nconcurrent:")
	if err := scenarioConcurrent(*writers, *readers, *requests, *wait, *maxWaiters, *blocking); err != nil {
		fmt.Printf("  FAIL concurrent: %v\n", err)
		failed++
	} else {
		fmt.Println("  PASS concurrent")
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d FAILED\n", failed)
		os.Exit(1)
	}
	fmt.Println("ALL PASSED")
}
