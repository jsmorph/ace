package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/morphism/ace"
	"golang.org/x/crypto/acme/autocert"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "out":
		cmdOut(os.Args[2:])
	case "in":
		cmdIn(os.Args[2:])
	case "rd":
		cmdRd(os.Args[2:])
	case "match":
		cmdMatchTest(os.Args[2:])
	case "del":
		cmdDel(os.Args[2:])
	case "stats":
		cmdStats(os.Args[2:])
	case "expire":
		cmdExpire(os.Args[2:])
	case "reg":
		cmdReg(os.Args[2:])
	case "regcheck":
		cmdRegCheck(os.Args[2:])
	case "test":
		cmdTest(os.Args[2:])
	case "doc":
		cmdDoc(os.Args[2:])
	case "help":
		cmdHelp()
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: ace <serve|out|in|rd|match|del|reg|regcheck|stats|expire|test|doc|help> [flags]\n")
}

func cmdHelp() {
	data, err := ace.Docs.ReadFile("docs/skill.md")
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(data)
}

// drainer wraps an http.Handler with a drain mode: once draining is
// set, new requests receive 503.  srv.Shutdown tracks in-flight
// request completion.
type drainer struct {
	handler  http.Handler
	draining atomic.Bool
}

func (d *drainer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d.draining.Load() {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"error":"service updating"}`)
		return
	}
	d.handler.ServeHTTP(w, r)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "localhost:8000", "listen address")
	dbPath := fs.String("db", "ace.db", "database file")
	limitsFile := fs.String("limits", "", "limits JSON file")
	blocking := fs.String("blocking", "notify", "blocking implementation: polling or notify")
	scavenge := fs.String("scavenge", "PT1H", "scavenge interval (ISO 8601 duration)")
	maxWaiters := fs.Int("max-waiters", 0, "max concurrent blocking clients (0 = unlimited)")
	deletes := fs.Bool("deletes", false, "enable explicit deletes (visibility timeout)")
	visTimeout := fs.String("visibility-timeout", "PT30S", "visibility timeout (ISO 8601 duration)")
	insecureIDs := fs.Bool("insecure-ids", false, "allow bare X-ACE-ID header (no key required)")
	identityTTLStr := fs.String("identity-ttl", "P40D", "identity expiration (ISO 8601 duration)")
	tlsHost := fs.String("tls", "", "hostname for automatic TLS via Let's Encrypt")
	tlsCache := fs.String("tls-cache", "certs", "directory for cached TLS certificates")
	throttle := fs.Int("throttle", 60, "max requests per minute per IP (0 = unlimited)")
	updates := fs.String("updates", "", "update source: GitHub releases URL or local directory")
	updateIntervalStr := fs.String("update-interval", "PT1H", "update check interval (ISO 8601)")
	fs.Parse(args)

	cfg := ace.DefaultConfig()

	if *deletes {
		cfg.Deletes = true
		vt, err := ace.ParseISO8601Duration(*visTimeout)
		if err != nil {
			log.Fatalf("parse visibility-timeout: %v", err)
		}
		cfg.VisibilityTimeout = vt
	}

	if *limitsFile != "" {
		data, err := os.ReadFile(*limitsFile)
		if err != nil {
			log.Fatalf("read limits file: %v", err)
		}
		if err := json.Unmarshal(data, &cfg.Limits); err != nil {
			log.Fatalf("parse limits: %v", err)
		}
	}

	cfg.Blocking = ace.BlockingMode(*blocking)
	cfg.InsecureIDs = *insecureIDs

	identityTTL, err := ace.ParseISO8601Duration(*identityTTLStr)
	if err != nil {
		log.Fatalf("parse identity-ttl: %v", err)
	}
	cfg.IdentityTTL = identityTTL

	scavengeInterval, err := ace.ParseISO8601Duration(*scavenge)
	if err != nil {
		log.Fatalf("parse scavenge interval: %v", err)
	}
	cfg.ScavengeInterval = scavengeInterval

	var updateInterval time.Duration
	if *updates != "" {
		ui, err := ace.ParseISO8601Duration(*updateIntervalStr)
		if err != nil {
			log.Fatalf("parse update-interval: %v", err)
		}
		updateInterval = ui
	}

	space, err := ace.NewSpace(*dbPath, cfg)
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		ticker := time.NewTicker(cfg.ScavengeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := space.DeleteExpired(); err != nil {
					log.Printf("deleteExpired: %v", err)
				}
				if rand.IntN(10) == 0 {
					if _, err := space.DeleteExpiredIdentities(); err != nil {
						log.Printf("deleteExpiredIdentities: %v", err)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	var handler http.Handler = ace.NewServer(space, *maxWaiters)
	if *throttle > 0 {
		t := ace.NewThrottler(handler, *throttle)
		go t.Run(ctx)
		handler = t
	}

	// When updates are enabled, wrap the handler with a drainer
	// that rejects new requests during shutdown.  updateDone
	// receives the new binary path after shutdown completes.
	var drain *drainer
	var updateDone chan string
	if *updates != "" {
		drain = &drainer{handler: handler}
		handler = drain
		updateDone = make(chan string, 1)
	}

	var srv *http.Server
	if *tlsHost != "" {
		m := &autocert.Manager{
			Cache:      autocert.DirCache(*tlsCache),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(*tlsHost),
		}
		srv = &http.Server{
			Addr:      ":443",
			Handler:   handler,
			TLSConfig: &tls.Config{GetCertificate: m.GetCertificate},
		}
		go func() {
			if err := http.ListenAndServe(":80", m.HTTPHandler(nil)); err != nil {
				log.Fatal(err)
			}
		}()
	} else {
		srv = &http.Server{
			Addr:    *addr,
			Handler: handler,
		}
	}

	if *updates != "" {
		updater := ace.NewUpdater(*updates, updateInterval, func(binPath string) {
			log.Printf("update: draining requests")
			drain.draining.Store(true)
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Printf("update: shutdown: %v", err)
			}
			log.Printf("update: shutdown complete")
			updateDone <- binPath
		})
		go updater.Run(ctx)
	}

	if *tlsHost != "" {
		log.Printf("listening on :443 (tls=%s, blocking=%s, max-waiters=%d, deletes=%v)", *tlsHost, cfg.Blocking, *maxWaiters, cfg.Deletes)
		if *updates != "" {
			ln := listenRetry("tcp", ":443")
			if err := srv.ServeTLS(ln, "", ""); err != http.ErrServerClosed {
				log.Fatal(err)
			}
		} else {
			if err := srv.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}
	} else {
		log.Printf("listening on %s (blocking=%s, max-waiters=%d, deletes=%v)", *addr, cfg.Blocking, *maxWaiters, cfg.Deletes)
		if *updates != "" {
			ln := listenRetry("tcp", *addr)
			if err := srv.Serve(ln); err != http.ErrServerClosed {
				log.Fatal(err)
			}
		} else {
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}
	}

	// If an update triggered the shutdown, wait for the shutdown
	// goroutine to finish, close the database, then start the new
	// process.  Serve returns before Shutdown completes (Shutdown
	// closes the listener first, then waits for handlers), so we
	// block on updateDone to ensure all handlers have finished
	// before closing the database.
	if drain != nil && drain.draining.Load() {
		binPath := <-updateDone
		cancel()
		space.Close()
		startUpdatedProcess(binPath)
	}
}

func startUpdatedProcess(binPath string) {
	log.Printf("update: starting new process: %s", binPath)
	cmd := exec.Command(binPath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatalf("update: start: %v", err)
	}
	os.Exit(0)
}

func listenRetry(network, addr string) net.Listener {
	for i := range 30 {
		ln, err := net.Listen(network, addr)
		if err == nil {
			return ln
		}
		log.Printf("listen %s: %v (attempt %d/30)", addr, err, i+1)
		time.Sleep(1 * time.Second)
	}
	log.Fatalf("listen %s: gave up after 30 attempts", addr)
	panic("unreachable")
}

func cliConfig() ace.Config {
	cfg := ace.DefaultConfig()
	cfg.Blocking = ace.BlockingPoll
	return cfg
}

func parseDuration(s string) (time.Duration, error) {
	return ace.ParseWait(s)
}

func resolveServer(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("ACE_URL")
}

func checkHTTPError(resp *http.Response) {
	if resp.StatusCode == http.StatusOK {
		return
	}
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
		log.Fatal(errResp.Error)
	}
	log.Fatalf("server returned %s", resp.Status)
}

func remoteOut(serverURL string, object json.RawMessage, accessStr string, ttlStr string) {
	type outReq struct {
		Object json.RawMessage `json:"object"`
		Access json.RawMessage `json:"access,omitempty"`
		TTL    string          `json:"ttl,omitempty"`
	}
	req := outReq{Object: object, TTL: ttlStr}
	if accessStr != "" {
		req.Access = json.RawMessage(accessStr)
	}
	data, err := json.Marshal(req)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := http.Post(serverURL+"/out", "application/json", bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	checkHTTPError(resp)
	io.Copy(os.Stdout, resp.Body)
}

func tryRemoteOut(serverURL string, object json.RawMessage, accessStr string, ttlStr string) error {
	type outReq struct {
		Object json.RawMessage `json:"object"`
		Access json.RawMessage `json:"access,omitempty"`
		TTL    string          `json:"ttl,omitempty"`
	}
	req := outReq{Object: object, TTL: ttlStr}
	if accessStr != "" {
		req.Access = json.RawMessage(accessStr)
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, err := http.Post(serverURL+"/out", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("server returned %s", resp.Status)
	}
	io.Copy(os.Stdout, resp.Body)
	return nil
}

func remoteMatch(serverURL string, pattern string, callerID string, clientKey string, wait time.Duration, since string, remove bool) {
	path := "/rd"
	if remove {
		path = "/in"
	}
	type matchReq struct {
		Pattern json.RawMessage `json:"pattern"`
		Wait    string          `json:"wait,omitempty"`
		Since   string          `json:"since,omitempty"`
	}
	body := matchReq{
		Pattern: json.RawMessage(pattern),
		Since:   since,
	}
	if wait > 0 {
		body.Wait = wait.String()
	}
	data, err := json.Marshal(body)
	if err != nil {
		log.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, serverURL+path, bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if clientKey != "" {
		req.Header.Set("X-ACE-Client-Key", clientKey)
	} else if callerID != "" {
		req.Header.Set("X-ACE-ID", callerID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	checkHTTPError(resp)
	io.Copy(os.Stdout, resp.Body)
}

func remoteDel(serverURL string, deleteID string) {
	type delReq struct {
		DeleteID string `json:"delete_id"`
	}
	data, err := json.Marshal(delReq{DeleteID: deleteID})
	if err != nil {
		log.Fatal(err)
	}
	resp, err := http.Post(serverURL+"/del", "application/json", bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	checkHTTPError(resp)
	io.Copy(os.Stdout, resp.Body)
}

func remoteStats(serverURL string) {
	resp, err := http.Get(serverURL + "/stats")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	checkHTTPError(resp)
	io.Copy(os.Stdout, resp.Body)
}

func cmdOut(args []string) {
	fs := flag.NewFlagSet("out", flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	server := fs.String("server", "", "ACE server URL (default: $ACE_URL)")
	object := fs.String("object", "", "JSON object")
	accessStr := fs.String("access", "", "access JSON")
	ttlStr := fs.String("ttl", "", "TTL (ISO 8601 duration)")
	fs.Parse(args)

	if url := resolveServer(*server); url != "" {
		if *object != "" {
			remoteOut(url, json.RawMessage(*object), *accessStr, *ttlStr)
			return
		}
		var errs int
		scanner := bufio.NewScanner(os.Stdin)
		for n := 1; scanner.Scan(); n++ {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if err := tryRemoteOut(url, json.RawMessage(line), *accessStr, *ttlStr); err != nil {
				log.Printf("line %d: %v", n, err)
				errs++
				continue
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
		if errs > 0 {
			os.Exit(1)
		}
		return
	}

	space, err := ace.NewSpace(*dbPath, cliConfig())
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	var access *ace.Access
	if *accessStr != "" {
		access = &ace.Access{}
		if err := json.Unmarshal([]byte(*accessStr), access); err != nil {
			log.Fatalf("parse access: %v", err)
		}
	}

	var ttl time.Duration
	if *ttlStr != "" {
		d, err := ace.ParseISO8601Duration(*ttlStr)
		if err != nil {
			log.Fatalf("parse ttl: %v", err)
		}
		if d <= 0 {
			log.Fatalf("ttl must be positive")
		}
		ttl = d
	}

	enc := json.NewEncoder(os.Stdout)
	emit := func(raw json.RawMessage) error {
		id, err := space.Out(raw, access, ttl)
		if err != nil {
			return err
		}
		return enc.Encode(struct {
			ID string `json:"id"`
		}{ID: id})
	}

	if *object != "" {
		if err := emit(json.RawMessage(*object)); err != nil {
			log.Fatal(err)
		}
		return
	}

	var errs int
	scanner := bufio.NewScanner(os.Stdin)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := emit(json.RawMessage(line)); err != nil {
			log.Printf("line %d: %v", n, err)
			errs++
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	if errs > 0 {
		os.Exit(1)
	}
}

func cmdIn(args []string) {
	cmdMatch(args, true)
}

func cmdRd(args []string) {
	cmdMatch(args, false)
}

func cmdMatch(args []string, remove bool) {
	name := "rd"
	if remove {
		name = "in"
	}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	server := fs.String("server", "", "ACE server URL (default: $ACE_URL)")
	pattern := fs.String("pattern", "", "JSON pattern (reads stdin if absent or -)")
	since := fs.String("since", "", "timestamp filter")
	callerID := fs.String("id", "", "caller identity for access control")
	key := fs.String("key", "", "client key (default: $ACE_CLIENT_KEY)")
	waitStr := fs.String("wait", "", "block duration (integer seconds, ISO 8601, or Go duration)")
	deletes := fs.Bool("deletes", false, "enable explicit deletes")
	fs.Parse(args)

	var wait time.Duration
	if *waitStr != "" {
		d, err := parseDuration(*waitStr)
		if err != nil {
			log.Fatalf("parse wait: %v", err)
		}
		if d < 0 {
			log.Fatalf("wait must not be negative")
		}
		wait = d
	}

	pat := *pattern
	if pat == "" || pat == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("read stdin: %v", err)
		}
		pat = strings.TrimSpace(string(data))
	}
	if pat == "" {
		log.Fatal("no pattern provided")
	}

	if url := resolveServer(*server); url != "" {
		if *deletes {
			log.Printf("warning: --deletes is ignored in remote mode; the server's configuration controls explicit deletes")
		}
		remoteMatch(url, pat, *callerID, resolveClientKey(*key), wait, *since, remove)
		return
	}

	cfg := cliConfig()
	if *deletes {
		cfg.Deletes = true
	}

	space, err := ace.NewSpace(*dbPath, cfg)
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	resolvedID := *callerID
	if ck := resolveClientKey(*key); ck != "" {
		ident, err := space.LookupKey(ck)
		if err != nil {
			log.Fatalf("key lookup: %v", err)
		}
		if ident == nil {
			log.Fatal("invalid client key")
		}
		resolvedID = ident.ID
	}

	ctx := context.Background()
	var result *ace.Result
	if remove {
		result, err = space.In(ctx, resolvedID, json.RawMessage(pat), wait, *since)
	} else {
		result, err = space.Rd(ctx, resolvedID, json.RawMessage(pat), wait, *since)
	}
	if err != nil {
		log.Fatal(err)
	}

	if result == nil {
		fmt.Println("null")
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal(err)
	}
}

func cmdDel(args []string) {
	fs := flag.NewFlagSet("del", flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	server := fs.String("server", "", "ACE server URL (default: $ACE_URL)")
	deleteID := fs.String("delete-id", "", "deletion ID")
	fs.Parse(args)

	if *deleteID == "" {
		log.Fatal("--delete-id is required")
	}

	if url := resolveServer(*server); url != "" {
		remoteDel(url, *deleteID)
		return
	}

	space, err := ace.NewSpace(*dbPath, cliConfig())
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	deleted, err := space.Del(*deleteID)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Deleted bool `json:"deleted"`
	}{Deleted: deleted}); err != nil {
		log.Fatal(err)
	}
}

func cmdStats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	server := fs.String("server", "", "ACE server URL (default: $ACE_URL)")
	fs.Parse(args)

	if url := resolveServer(*server); url != "" {
		remoteStats(url)
		return
	}

	space, err := ace.NewSpace(*dbPath, cliConfig())
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	stats, err := space.Stats()
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(stats); err != nil {
		log.Fatal(err)
	}
}

func cmdMatchTest(args []string) {
	fs := flag.NewFlagSet("match", flag.ExitOnError)
	object := fs.String("object", "", "JSON object")
	pattern := fs.String("pattern", "", "JSON pattern")
	fs.Parse(args)

	if *object == "" {
		log.Fatal("--object is required")
	}
	if *pattern == "" {
		log.Fatal("--pattern is required")
	}

	ok, err := ace.Match(json.RawMessage(*object), json.RawMessage(*pattern))
	if err != nil {
		log.Fatal(err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Match bool `json:"match"`
	}{Match: ok}); err != nil {
		log.Fatal(err)
	}
}

func cmdExpire(args []string) {
	fs := flag.NewFlagSet("expire", flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	fs.Parse(args)

	space, err := ace.NewSpace(*dbPath, cliConfig())
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	n, err := space.DeleteExpired()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("deleted %d expired objects\n", n)

	ni, err := space.DeleteExpiredIdentities()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("deleted %d expired identities\n", ni)
}

func resolveClientKey(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("ACE_CLIENT_KEY")
}

func cmdReg(args []string) {
	fs := flag.NewFlagSet("reg", flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	server := fs.String("server", "", "ACE server URL (default: $ACE_URL)")
	name := fs.String("name", "", "human-readable name")
	fs.Parse(args)

	if url := resolveServer(*server); url != "" {
		remoteReg(url, *name)
		return
	}

	space, err := ace.NewSpace(*dbPath, cliConfig())
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	ident, err := space.Register(*name)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Key  string `json:"key"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}{Key: ident.Key, ID: ident.ID, Name: ident.Name}); err != nil {
		log.Fatal(err)
	}
}

func remoteReg(serverURL string, name string) {
	type regReq struct {
		Name string `json:"name,omitempty"`
	}
	data, err := json.Marshal(regReq{Name: name})
	if err != nil {
		log.Fatal(err)
	}
	resp, err := http.Post(serverURL+"/reg", "application/json", bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	checkHTTPError(resp)
	io.Copy(os.Stdout, resp.Body)
}

func cmdRegCheck(args []string) {
	fs := flag.NewFlagSet("regcheck", flag.ExitOnError)
	dbPath := fs.String("db", "ace.db", "database file")
	server := fs.String("server", "", "ACE server URL (default: $ACE_URL)")
	key := fs.String("key", "", "client key (default: $ACE_CLIENT_KEY)")
	id := fs.String("id", "", "identity ID")
	name := fs.String("name", "", "identity name")
	fs.Parse(args)

	clientKey := resolveClientKey(*key)

	if url := resolveServer(*server); url != "" {
		remoteRegCheck(url, clientKey, *id, *name)
		return
	}

	space, err := ace.NewSpace(*dbPath, cliConfig())
	if err != nil {
		log.Fatalf("open space: %v", err)
	}
	defer func() {
		if err := space.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	var ident *ace.Identity
	switch {
	case clientKey != "":
		ident, err = space.LookupKey(clientKey)
	case *id != "":
		ident, err = space.LookupID(*id)
	case *name != "":
		ident, err = space.LookupName(*name)
	default:
		log.Fatal("provide --key, --id, or --name")
	}
	if err != nil {
		log.Fatal(err)
	}
	if ident == nil {
		log.Fatal("identity not found")
	}

	var v interface{}
	switch {
	case clientKey != "":
		v = struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{ID: ident.ID, Name: ident.Name}
	case *id != "":
		v = struct {
			Name string `json:"name"`
		}{Name: ident.Name}
	default:
		v = struct {
			ID string `json:"id"`
		}{ID: ident.ID}
	}
	if err := json.NewEncoder(os.Stdout).Encode(v); err != nil {
		log.Fatal(err)
	}
}

func remoteRegCheck(serverURL string, key string, id string, name string) {
	req, err := http.NewRequest(http.MethodGet, serverURL+"/regcheck", nil)
	if err != nil {
		log.Fatal(err)
	}
	q := req.URL.Query()
	switch {
	case key != "":
		req.Header.Set("X-ACE-Client-Key", key)
	case id != "":
		q.Set("id", id)
	case name != "":
		q.Set("name", name)
	default:
		log.Fatal("provide --key, --id, or --name")
	}
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	checkHTTPError(resp)
	io.Copy(os.Stdout, resp.Body)
}

func cmdDoc(args []string) {
	if len(args) == 0 {
		fmt.Println("ACE is a coordination service for software agents based on the tuple-space model.")
		fmt.Println()
		for _, name := range ace.DocFiles {
			fmt.Printf("  %s\n", name)
		}
		return
	}
	data, err := ace.Docs.ReadFile(args[0])
	if err != nil {
		// Try without docs/ prefix for backward compatibility.
		data, err = ace.Docs.ReadFile("docs/" + args[0])
	}
	if err != nil {
		log.Fatalf("unknown document: %s", args[0])
	}
	os.Stdout.Write(data)
}
