package ace

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Updater monitors a source for new binaries and calls onUpdate with
// the path to the new executable.
type Updater struct {
	source   string
	interval time.Duration
	onUpdate func(path string)
	isDir    bool
	binName  string

	// Tracks last-seen state to detect changes.
	lastETag     string
	lastModified string
	lastModTime  time.Time
	firstCheck   bool
}

// NewUpdater creates an updater that polls source at the given
// interval.  Source is a URL or a directory path.  The onUpdate
// callback receives the path to the new binary.
func NewUpdater(source string, interval time.Duration, onUpdate func(path string)) *Updater {
	isDir := false
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		isDir = true
	}
	return &Updater{
		source:     source,
		interval:   interval,
		onUpdate:   onUpdate,
		isDir:      isDir,
		binName:    fmt.Sprintf("ace-%s-%s", runtime.GOOS, runtime.GOARCH),
		firstCheck: true,
	}
}

// Run polls for updates until ctx is canceled.
func (u *Updater) Run(ctx context.Context) {
	ticker := time.NewTicker(u.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if u.isDir {
				u.checkDir()
			} else {
				u.checkURL(ctx)
			}
		}
	}
}

func (u *Updater) checkDir() {
	path := filepath.Join(u.source, u.binName)
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("update check: stat %s: %v", path, err)
		}
		return
	}

	modTime := info.ModTime()
	if u.firstCheck {
		u.lastModTime = modTime
		u.firstCheck = false
		log.Printf("update baseline: %s (mtime %s)", path, modTime.Format(time.RFC3339))
		return
	}

	if !modTime.After(u.lastModTime) {
		return
	}

	u.lastModTime = modTime
	log.Printf("update detected: %s (mtime %s)", path, modTime.Format(time.RFC3339))

	if err := checkExecutable(path); err != nil {
		log.Printf("update rejected: %v", err)
		return
	}

	u.onUpdate(path)
}

func (u *Updater) checkURL(ctx context.Context) {
	url := u.source + "/latest/download/" + u.binName

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		log.Printf("update check: %v", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("update check: HEAD %s: %v", url, err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("update check: HEAD %s: %s", url, resp.Status)
		return
	}

	etag := resp.Header.Get("ETag")
	lastMod := resp.Header.Get("Last-Modified")

	if u.firstCheck {
		u.lastETag = etag
		u.lastModified = lastMod
		u.firstCheck = false
		if etag == "" && lastMod == "" {
			log.Printf("update baseline: %s (no ETag or Last-Modified; change detection will not work)", url)
		} else {
			log.Printf("update baseline: %s (etag=%s, last-modified=%s)", url, etag, lastMod)
		}
		return
	}

	if etag == u.lastETag && lastMod == u.lastModified {
		return
	}

	log.Printf("update detected: %s (etag=%s, last-modified=%s)", url, etag, lastMod)

	path, err := u.download(ctx, url)
	if err != nil {
		log.Printf("update download failed: %v", err)
		return
	}

	u.lastETag = etag
	u.lastModified = lastMod
	u.onUpdate(path)
}

func (u *Updater) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp("", "ace-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("chmod: %w", err)
	}

	return tmp.Name(), nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}
