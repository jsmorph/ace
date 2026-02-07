package ace

import (
	"sync"
	"time"
)

const idFormat = "2006-01-02T15:04:05.000000000"

type IDGen struct {
	mu   sync.Mutex
	last time.Time
}

func NewIDGen() *IDGen {
	return &IDGen{}
}

func (g *IDGen) Next() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().UTC()
	if !now.After(g.last) {
		now = g.last.Add(1)
	}
	g.last = now
	return now.Format(idFormat)
}
