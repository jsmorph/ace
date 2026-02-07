package ace

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"quamina.net/go/quamina"
)

// WaiterID identifies a registered pattern for deregistration.
type WaiterID uint64

type waiter struct {
	ch chan struct{}
}

// Notifier wakes blocked In and Rd callers when a matching object arrives.
type Notifier struct {
	mu      sync.Mutex
	q       *quamina.Quamina
	waiters map[WaiterID]*waiter
	nextID  atomic.Uint64
}

// NewNotifier returns a Notifier backed by a Quamina matching engine.
func NewNotifier() (*Notifier, error) {
	q, err := quamina.New(quamina.WithPatternDeletion(true))
	if err != nil {
		return nil, err
	}
	return &Notifier{
		q:       q,
		waiters: make(map[WaiterID]*waiter),
	}, nil
}

// Register adds a pattern and returns a channel that signals when a matching object arrives.
func (n *Notifier) Register(pattern json.RawMessage) (WaiterID, <-chan struct{}, error) {
	qp, err := ToQuaminaPattern(pattern)
	if err != nil {
		return 0, nil, err
	}

	id := WaiterID(n.nextID.Add(1))
	w := &waiter{ch: make(chan struct{}, 1)}

	n.mu.Lock()
	defer n.mu.Unlock()

	if err := n.q.AddPattern(id, string(qp)); err != nil {
		return 0, nil, err
	}
	n.waiters[id] = w
	return id, w.ch, nil
}

// Deregister removes a previously registered pattern.
func (n *Notifier) Deregister(id WaiterID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.waiters, id)
	n.q.DeletePatterns(id)
}

// Notify tests an object against all registered patterns and signals matching waiters.
func (n *Notifier) Notify(object json.RawMessage) error {
	n.mu.Lock()
	matches, err := n.q.MatchesForEvent([]byte(object))
	if err != nil {
		n.mu.Unlock()
		return fmt.Errorf("MatchesForEvent: %w", err)
	}
	var targets []chan struct{}
	for _, x := range matches {
		wid := x.(WaiterID)
		if w, ok := n.waiters[wid]; ok {
			targets = append(targets, w.ch)
		}
	}
	n.mu.Unlock()

	for _, ch := range targets {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

// ToQuaminaPattern converts an ACE pattern to Quamina format by wrapping atomic leaves in arrays.
func ToQuaminaPattern(acePattern json.RawMessage) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(acePattern, &obj); err != nil {
		return nil, err
	}
	converted := convertToQuamina(obj)
	return json.Marshal(converted)
}

func convertToQuamina(obj map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(obj))
	for k, v := range obj {
		if strings.HasPrefix(k, "#") {
			continue
		}
		switch val := v.(type) {
		case map[string]interface{}:
			result[k] = convertToQuamina(val)
		case []interface{}:
			result[k] = val
		default:
			result[k] = []interface{}{val}
		}
	}
	return result
}
