package ace

import (
	"sync"
	"time"
)

const timestampFormat = "2006-01-02T15:04:05.000000000"

// IDGen generates monotonically increasing nanosecond-resolution timestamp identifiers.
type IDGen struct {
	mu   sync.Mutex
	last time.Time
}

// NewIDGen returns a new IDGen.
func NewIDGen() *IDGen {
	return &IDGen{}
}

// Next returns the next identifier, guaranteed to be greater than all previous ones.
func (g *IDGen) Next() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().UTC()
	if !now.After(g.last) {
		now = g.last.Add(1)
	}
	g.last = now
	return now.Format(timestampFormat)
}
