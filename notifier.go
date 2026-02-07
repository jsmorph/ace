package ace

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"quamina.net/go/quamina"
)

type waiterID uint64

type waiter struct {
	ch chan struct{}
}

type Notifier struct {
	mu      sync.Mutex
	q       *quamina.Quamina
	waiters map[waiterID]*waiter
	nextID  atomic.Uint64
}

func NewNotifier() (*Notifier, error) {
	q, err := quamina.New(quamina.WithPatternDeletion(true))
	if err != nil {
		return nil, err
	}
	return &Notifier{
		q:       q,
		waiters: make(map[waiterID]*waiter),
	}, nil
}

func (n *Notifier) Register(pattern json.RawMessage) (waiterID, <-chan struct{}, error) {
	qp, err := ToQuaminaPattern(pattern)
	if err != nil {
		return 0, nil, err
	}

	id := waiterID(n.nextID.Add(1))
	w := &waiter{ch: make(chan struct{}, 1)}

	n.mu.Lock()
	defer n.mu.Unlock()

	if err := n.q.AddPattern(id, string(qp)); err != nil {
		return 0, nil, err
	}
	n.waiters[id] = w
	return id, w.ch, nil
}

func (n *Notifier) Deregister(id waiterID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.waiters, id)
	n.q.DeletePatterns(id)
}

func (n *Notifier) Notify(object json.RawMessage) error {
	n.mu.Lock()
	matches, err := n.q.MatchesForEvent([]byte(object))
	if err != nil {
		n.mu.Unlock()
		return fmt.Errorf("MatchesForEvent: %w", err)
	}
	var targets []chan struct{}
	for _, x := range matches {
		wid := x.(waiterID)
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
