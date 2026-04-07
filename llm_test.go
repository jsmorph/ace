package ace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubLLMJudge struct {
	answers map[string]bool
}

func (s stubLLMJudge) Related(_ context.Context, text, contextText string) (bool, string, error) {
	key := text + "\x00" + contextText
	answer, ok := s.answers[key]
	if !ok {
		return false, "", fmt.Errorf("missing answer for %q", key)
	}
	if answer {
		return true, "yes", nil
	}
	return false, "no", nil
}

func TestParsePatternQuestion(t *testing.T) {
	parsed, err := ParsePattern([]byte(`{"type":"task","comment?":"TexMex food"}`))
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Exact) != 1 {
		t.Fatalf("expected 1 exact branch, got %d", len(parsed.Exact))
	}
	if len(parsed.Questions) != 1 {
		t.Fatalf("expected 1 question predicate, got %d", len(parsed.Questions))
	}
	if parsed.Questions[0].PathText != "comment" {
		t.Fatalf("unexpected path %q", parsed.Questions[0].PathText)
	}
	if parsed.Questions[0].ContextText != "TexMex food" {
		t.Fatalf("unexpected context text %q", parsed.Questions[0].ContextText)
	}
}

func TestParsePatternQuestionRequiresStringValue(t *testing.T) {
	_, err := ParsePattern([]byte(`{"comment?":1}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "LLM pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAILLMJudge(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("LLM_API_KEY", "")

	var seenAuth string
	var seenBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		seenBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"yes"}}]}`)
	}))
	defer ts.Close()

	judge := newOpenAILLMJudge(ts.URL, "gpt-5-mini")
	related, raw, err := judge.Related(context.Background(), "tacos and queso", "TexMex food")
	if err != nil {
		t.Fatal(err)
	}
	if !related || raw != "yes" {
		t.Fatalf("unexpected result related=%v raw=%q", related, raw)
	}
	if seenAuth != "Bearer test-key" {
		t.Fatalf("unexpected auth header %q", seenAuth)
	}

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(seenBody, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "gpt-5-mini" {
		t.Fatalf("unexpected model %q", payload.Model)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("unexpected messages %+v", payload.Messages)
	}
	if payload.Messages[0].Role != "system" {
		t.Fatalf("unexpected first role %q", payload.Messages[0].Role)
	}
	if payload.Messages[1].Role != "user" {
		t.Fatalf("unexpected second role %q", payload.Messages[1].Role)
	}
	if !strings.Contains(payload.Messages[1].Content, "TEXT:") || !strings.Contains(payload.Messages[1].Content, "CTEXT:") {
		t.Fatalf("unexpected prompt %q", payload.Messages[1].Content)
	}
}

func TestOpenAILLMJudgePrefersLLMAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "wrong-key")
	t.Setenv("LLM_API_KEY", "preferred-key")

	var seenAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"yes"}}]}`)
	}))
	defer ts.Close()

	judge := newOpenAILLMJudge(ts.URL, "gpt-5-mini")
	if _, _, err := judge.Related(context.Background(), "tacos and queso", "TexMex food"); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer preferred-key" {
		t.Fatalf("unexpected auth header %q", seenAuth)
	}
}

func TestOpenAILLMJudgeRejectsUnexpectedAnswer(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("LLM_API_KEY", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"maybe"}}]}`)
	}))
	defer ts.Close()

	judge := newOpenAILLMJudge(ts.URL, "gpt-5-mini")
	_, _, err := judge.Related(context.Background(), "tacos and queso", "TexMex food")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `expected yes or no`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMatchWithQuestion(t *testing.T) {
	parsed, err := ParsePattern([]byte(`{"type":"task","comment?":"TexMex food"}`))
	if err != nil {
		t.Fatal(err)
	}

	ok, err := matchWithProviders(
		context.Background(),
		json.RawMessage(`{"type":"task","comment":"tacos and queso"}`),
		parsed,
		nil,
		stubLLMJudge{
			answers: map[string]bool{
				"tacos and queso\x00TexMex food": true,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}
}

func TestRdQuestionSkipsEarlierNonMatch(t *testing.T) {
	s := newTestSpace(t)
	s.judge = stubLLMJudge{
		answers: map[string]bool{
			"astronomy and stars\x00TexMex food": false,
			"tacos and queso\x00TexMex food":     true,
		},
	}

	ctx := context.Background()
	if _, err := s.Out(json.RawMessage(`{"type":"task","comment":"astronomy and stars"}`), nil, 0); err != nil {
		t.Fatal(err)
	}
	id, err := s.Out(json.RawMessage(`{"type":"task","comment":"tacos and queso"}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.Rd(ctx, "anyone", json.RawMessage(`{"type":"task","comment?":"TexMex food"}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected a result")
	}
	if result.ID != id {
		t.Fatalf("expected id %q, got %q", id, result.ID)
	}
}

func TestNotifyModeQuestionFallsBackToPolling(t *testing.T) {
	cfg := notifyConfig()
	s := newTestSpaceWithConfig(t, cfg)
	s.judge = stubLLMJudge{
		answers: map[string]bool{
			"tacos and queso\x00TexMex food": true,
		},
	}

	done := make(chan *Result, 1)
	go func() {
		r, err := s.Rd(context.Background(), "anyone", json.RawMessage(`{"comment?":"TexMex food"}`), time.Second, "")
		if err != nil {
			t.Errorf("rd: %v", err)
			return
		}
		done <- r
	}()

	time.Sleep(100 * time.Millisecond)
	if _, err := s.Out(json.RawMessage(`{"comment":"tacos and queso"}`), nil, 0); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if result == nil {
			t.Fatal("expected a result")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for question match")
	}
}

func TestServerRdQuestionRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/out", bytes.NewBufferString(`{"object":{"type":"task","comment":"tacos and queso"}}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("out: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/rd", bytes.NewBufferString(`{"pattern":{"type":"task","comment?":"TexMex food"}}`))
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "LLM filtering isn't available") {
		t.Fatalf("unexpected response body %q", w.Body.String())
	}
}
