package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCheckDirBaselineSkipsCallback(t *testing.T) {
	dir := t.TempDir()
	binName := fmt.Sprintf("ace-%s-%s", runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	called := false
	u := NewUpdater(dir, time.Hour, func(string) { called = true })
	u.checkDir()

	if called {
		t.Fatal("onUpdate should not fire on first check")
	}
	if u.firstCheck {
		t.Fatal("firstCheck should be false after baseline")
	}
}

func TestCheckDirDetectsChange(t *testing.T) {
	dir := t.TempDir()
	binName := fmt.Sprintf("ace-%s-%s", runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	var got string
	u := NewUpdater(dir, time.Hour, func(p string) { got = p })

	// Baseline.
	u.checkDir()
	if got != "" {
		t.Fatal("onUpdate should not fire on baseline")
	}

	// Advance the modtime.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	u.checkDir()
	if got != path {
		t.Fatalf("expected onUpdate with %s, got %q", path, got)
	}
}

func TestCheckDirIgnoresSameModtime(t *testing.T) {
	dir := t.TempDir()
	binName := fmt.Sprintf("ace-%s-%s", runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	called := 0
	u := NewUpdater(dir, time.Hour, func(string) { called++ })

	u.checkDir()
	u.checkDir()
	u.checkDir()

	if called != 0 {
		t.Fatalf("expected 0 callbacks, got %d", called)
	}
}

func TestCheckDirRejectsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	binName := fmt.Sprintf("ace-%s-%s", runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	called := false
	u := NewUpdater(dir, time.Hour, func(string) { called = true })

	// Baseline.
	u.checkDir()

	// Advance modtime but leave non-executable.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	u.checkDir()
	if called {
		t.Fatal("onUpdate should not fire for non-executable file")
	}
}

func TestCheckDirMissingFile(t *testing.T) {
	dir := t.TempDir()
	called := false
	u := NewUpdater(dir, time.Hour, func(string) { called = true })

	u.checkDir()
	if !u.firstCheck {
		t.Fatal("firstCheck should remain true when file is missing")
	}
	if called {
		t.Fatal("onUpdate should not fire when file is missing")
	}
}

func TestCheckURLBaselineSkipsCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	called := false
	u := NewUpdater(srv.URL, time.Hour, func(string) { called = true })
	u.checkURL(context.Background())

	if called {
		t.Fatal("onUpdate should not fire on baseline")
	}
	if u.firstCheck {
		t.Fatal("firstCheck should be false after baseline")
	}
	if u.lastETag != `"v1"` {
		t.Fatalf("expected ETag %q, got %q", `"v1"`, u.lastETag)
	}
}

func TestCheckURLDetectsETagChange(t *testing.T) {
	etag := `"v1"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		if r.Method == http.MethodGet {
			w.Write([]byte("binary-data"))
		}
	}))
	defer srv.Close()

	var got string
	u := NewUpdater(srv.URL, time.Hour, func(p string) { got = p })

	// Baseline.
	u.checkURL(context.Background())

	// Change ETag.
	etag = `"v2"`
	u.checkURL(context.Background())

	if got == "" {
		t.Fatal("onUpdate should have fired")
	}
	defer os.Remove(got)

	if u.lastETag != `"v2"` {
		t.Fatalf("expected lastETag %q, got %q", `"v2"`, u.lastETag)
	}
}

func TestCheckURLDetectsLastModifiedChange(t *testing.T) {
	lastMod := "Mon, 01 Jan 2024 00:00:00 GMT"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", lastMod)
		if r.Method == http.MethodGet {
			w.Write([]byte("binary-data"))
		}
	}))
	defer srv.Close()

	var got string
	u := NewUpdater(srv.URL, time.Hour, func(p string) { got = p })

	u.checkURL(context.Background())

	lastMod = "Tue, 02 Jan 2024 00:00:00 GMT"
	u.checkURL(context.Background())

	if got == "" {
		t.Fatal("onUpdate should have fired")
	}
	defer os.Remove(got)
}

func TestCheckURLNoChangeNoCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	called := 0
	u := NewUpdater(srv.URL, time.Hour, func(string) { called++ })

	u.checkURL(context.Background())
	u.checkURL(context.Background())
	u.checkURL(context.Background())

	if called != 0 {
		t.Fatalf("expected 0 callbacks, got %d", called)
	}
}

func TestCheckURLDownloadFailurePreservesState(t *testing.T) {
	headCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headCount++
		if headCount <= 1 {
			w.Header().Set("ETag", `"v1"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("ETag", `"v2"`)
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	called := false
	u := NewUpdater(srv.URL, time.Hour, func(string) { called = true })

	// Baseline.
	u.checkURL(context.Background())

	// Change detected but download fails.
	u.checkURL(context.Background())
	if called {
		t.Fatal("onUpdate should not fire when download fails")
	}
	if u.lastETag != `"v1"` {
		t.Fatalf("lastETag should remain %q after failed download, got %q", `"v1"`, u.lastETag)
	}
}

func TestDownload(t *testing.T) {
	content := "#!/bin/sh\necho hello\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer srv.Close()

	u := NewUpdater(srv.URL, time.Hour, func(string) {})
	path, err := u.download(context.Background(), srv.URL+"/binary")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("expected %q, got %q", content, string(data))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatal("downloaded file should be executable")
	}
}

func TestDownloadNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u := NewUpdater(srv.URL, time.Hour, func(string) {})
	_, err := u.download(context.Background(), srv.URL+"/missing")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestCheckURLHeadNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	called := false
	u := NewUpdater(srv.URL, time.Hour, func(string) { called = true })
	u.checkURL(context.Background())

	if !u.firstCheck {
		t.Fatal("firstCheck should remain true after non-200 HEAD")
	}
	if called {
		t.Fatal("onUpdate should not fire on non-200 HEAD")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	u := NewUpdater(dir, 10*time.Millisecond, func(string) {})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		u.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
