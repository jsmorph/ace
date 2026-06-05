package netapi

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Throttler provides per-IP rate limiting as HTTP middleware.
type Throttler struct {
	handler  http.Handler
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
}

// NewThrottler returns rate-limiting middleware that wraps handler at
// rpm requests per minute per IP.  Call Run in a goroutine to start
// background cleanup of stale entries.
func NewThrottler(handler http.Handler, rpm int) *Throttler {
	return &Throttler{
		handler:  handler,
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(float64(rpm) / 60.0),
		burst:    rpm,
	}
}

// Run removes stale visitor entries until ctx is canceled.
func (t *Throttler) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			for ip, v := range t.visitors {
				if time.Since(v.lastSeen) > 5*time.Minute {
					delete(t.visitors, ip)
				}
			}
			t.mu.Unlock()
		}
	}
}

func (t *Throttler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	t.mu.Lock()
	v, ok := t.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(t.rate, t.burst)}
		t.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	t.mu.Unlock()

	if !v.limiter.Allow() {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	t.handler.ServeHTTP(w, r)
}
