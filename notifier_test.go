package ace

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestToQuaminaPatternAtomic(t *testing.T) {
	out, err := ToQuaminaPattern(json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	arr, ok := obj["a"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("expected [1], got %v", obj["a"])
	}
}

func TestToQuaminaPatternArray(t *testing.T) {
	out, err := ToQuaminaPattern(json.RawMessage(`{"a":[1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	arr, ok := obj["a"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("expected [1,2], got %v", obj["a"])
	}
}

func TestToQuaminaPatternNested(t *testing.T) {
	out, err := ToQuaminaPattern(json.RawMessage(`{"a":{"b":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	inner, ok := obj["a"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested object, got %T", obj["a"])
	}
	arr, ok := inner["b"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("expected [1], got %v", inner["b"])
	}
}

func TestToQuaminaPatternEmpty(t *testing.T) {
	out, err := ToQuaminaPattern(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{}" {
		t.Fatalf("expected {}, got %s", out)
	}
}

func TestToQuaminaPatternSkipsHashProperty(t *testing.T) {
	out, err := ToQuaminaPattern(json.RawMessage(`{"#meta":"x","a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if _, found := obj["#meta"]; found {
		t.Fatal("expected #meta to be stripped from Quamina pattern")
	}
	arr, ok := obj["a"].([]interface{})
	if !ok || len(arr) != 1 {
		t.Fatalf("expected {a:[1]}, got %v", obj)
	}
}

func TestNotifierRegisterAndNotify(t *testing.T) {
	n, err := NewNotifier()
	if err != nil {
		t.Fatal(err)
	}

	wid, ch, err := n.Register(json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}

	n.Notify(json.RawMessage(`{"a":1}`))

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected notification")
	}

	n.Deregister(wid)
}

func TestNotifierNoMatch(t *testing.T) {
	n, err := NewNotifier()
	if err != nil {
		t.Fatal(err)
	}

	_, ch, err := n.Register(json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}

	n.Notify(json.RawMessage(`{"a":2}`))

	select {
	case <-ch:
		t.Fatal("should not have received notification")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifierDeregisterStopsNotification(t *testing.T) {
	n, err := NewNotifier()
	if err != nil {
		t.Fatal(err)
	}

	wid, ch, err := n.Register(json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}

	n.Deregister(wid)
	n.Notify(json.RawMessage(`{"a":1}`))

	select {
	case <-ch:
		t.Fatal("should not notify after deregister")
	case <-time.After(50 * time.Millisecond):
	}
}

func notifyConfig() Config {
	cfg := DefaultConfig()
	cfg.Blocking = BlockingNotify
	return cfg
}

func TestNotifyWaitBlocking(t *testing.T) {
	s := newTestSpaceWithConfig(t, notifyConfig())
	ctx := context.Background()

	done := make(chan *Result, 1)
	go func() {
		r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 2*time.Second, "")
		if err != nil {
			t.Errorf("in: %v", err)
			return
		}
		done <- r
	}()

	time.Sleep(100 * time.Millisecond)
	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-done:
		if r == nil {
			t.Fatal("expected result from blocking in")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for blocking in")
	}
}

func TestNotifyWaitTimeout(t *testing.T) {
	s := newTestSpaceWithConfig(t, notifyConfig())
	ctx := context.Background()

	r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 100*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("expected nil after wait timeout with no matching object")
	}
}

func TestNotifyWaitContextCancel(t *testing.T) {
	s := newTestSpaceWithConfig(t, notifyConfig())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 5*time.Second, "")
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestNotifyCompetingIn(t *testing.T) {
	s := newTestSpaceWithConfig(t, notifyConfig())
	ctx := context.Background()

	results := make(chan *Result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r, err := s.In(ctx, "anyone", json.RawMessage(`{"a":1}`), 2*time.Second, "")
			if err != nil {
				t.Errorf("in: %v", err)
			}
			results <- r
		}()
	}

	time.Sleep(100 * time.Millisecond)
	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)

	// Write a second object so the other waiter can also complete
	_, err = s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	got := 0
	timeout := time.After(3 * time.Second)
	for got < 2 {
		select {
		case r := <-results:
			if r != nil {
				got++
			}
		case <-timeout:
			t.Fatalf("timed out, only got %d results", got)
		}
	}
}

func TestNotifyRdDoesNotRemove(t *testing.T) {
	s := newTestSpaceWithConfig(t, notifyConfig())
	ctx := context.Background()

	done1 := make(chan *Result, 1)
	done2 := make(chan *Result, 1)

	go func() {
		r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 2*time.Second, "")
		if err != nil {
			t.Errorf("rd waiter 1: %v", err)
		}
		done1 <- r
	}()
	go func() {
		r, err := s.Rd(ctx, "anyone", json.RawMessage(`{"a":1}`), 2*time.Second, "")
		if err != nil {
			t.Errorf("rd waiter 2: %v", err)
		}
		done2 <- r
	}()

	time.Sleep(100 * time.Millisecond)
	_, err := s.Out(json.RawMessage(`{"a":1}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	timeout := time.After(3 * time.Second)
	select {
	case r := <-done1:
		if r == nil {
			t.Fatal("rd waiter 1 should have gotten result")
		}
	case <-timeout:
		t.Fatal("timed out on waiter 1")
	}
	select {
	case r := <-done2:
		if r == nil {
			t.Fatal("rd waiter 2 should have gotten result")
		}
	case <-timeout:
		t.Fatal("timed out on waiter 2")
	}
}

func TestNotifyPatternMatchingSpecExamples(t *testing.T) {
	cases := []struct {
		pattern string
		object  string
		matches bool
	}{
		{`{"a":1}`, `{"a":1}`, true},
		{`{"a":[1,2]}`, `{"a":1}`, true},
		{`{"a":[1,2]}`, `{"a":2}`, true},
		{`{"a":[1,2]}`, `{"a":3}`, false},
		{`{"a":[1,2]}`, `{"a":1,"b":0}`, true},
		{`{"b":[1,2]}`, `{"a":1}`, false},
		{`{"b":[1,2]}`, `{"a":3,"b":1}`, true},
		{`{"a":{"b":1,"c":2}}`, `{"a":{"b":1,"c":2}}`, true},
		{`{"a":{"b":1,"c":2}}`, `{"a":{"b":1,"c":2,"d":3}}`, true},
		{`{"a":{"b":1,"c":2},"d":3}`, `{"a":{"b":1,"c":2,"d":3}}`, false},
	}

	for i, c := range cases {
		s := newTestSpaceWithConfig(t, notifyConfig())
		ctx := context.Background()

		done := make(chan *Result, 1)
		go func() {
			r, err := s.Rd(ctx, "anyone", json.RawMessage(c.pattern), 300*time.Millisecond, "")
			if err != nil {
				t.Errorf("case %d: rd: %v", i, err)
			}
			done <- r
		}()

		time.Sleep(50 * time.Millisecond)
		_, err := s.Out(json.RawMessage(c.object), nil, 0)
		if err != nil {
			t.Fatalf("case %d: out: %v", i, err)
		}

		select {
		case r := <-done:
			got := r != nil
			if got != c.matches {
				t.Errorf("case %d: pattern=%s object=%s: got match=%v, want %v", i, c.pattern, c.object, got, c.matches)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("case %d: timed out", i)
		}
	}
}

func TestStats(t *testing.T) {
	s := newTestSpace(t)

	acc := &Access{In: []string{"a1"}, Rd: []string{"r1", "r2"}}
	if _, err := s.Out(json.RawMessage(`{"x":1,"y":2}`), acc, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Out(json.RawMessage(`{"x":3}`), nil, 0); err != nil {
		t.Fatal(err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Objects != 2 {
		t.Fatalf("expected 2 objects, got %d", st.Objects)
	}
	if st.Branches != 3 {
		t.Fatalf("expected 3 branches, got %d", st.Branches)
	}
	if st.AccessRecords != 3 {
		t.Fatalf("expected 3 access records, got %d", st.AccessRecords)
	}
}
