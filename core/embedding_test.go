package core

import (
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

type stubEmbeddingProvider struct {
	values map[string][]float64
}

func (s stubEmbeddingProvider) Embedding(_ context.Context, input string) ([]float64, error) {
	embedding, ok := s.values[input]
	if !ok {
		return nil, fmt.Errorf("missing embedding for %q", input)
	}
	return embedding, nil
}

func TestParsePatternEmbeddingsDefault(t *testing.T) {
	parsed, err := ParsePattern([]byte(`{"type":"task","context~":"TexMex food"}`))
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Exact) != 1 {
		t.Fatalf("expected 1 exact branch, got %d", len(parsed.Exact))
	}
	if got := parsed.Exact[0].Alternatives[0]; got != `type="task"` {
		t.Fatalf("unexpected exact branch %q", got)
	}
	if len(parsed.Embeddings) != 1 {
		t.Fatalf("expected 1 embeddings predicate, got %d", len(parsed.Embeddings))
	}

	predicate := parsed.Embeddings[0]
	if predicate.PathText != "context" {
		t.Fatalf("expected path context, got %q", predicate.PathText)
	}
	if predicate.Metric != embeddingMetricCosine {
		t.Fatalf("expected cosine metric, got %q", predicate.Metric)
	}
	if predicate.Threshold != DefaultEmbeddingThreshold {
		t.Fatalf("expected threshold %v, got %v", DefaultEmbeddingThreshold, predicate.Threshold)
	}
	if predicate.Query != "TexMex food" {
		t.Fatalf("unexpected query %q", predicate.Query)
	}
}

func TestParsePatternEmbeddingsExplicit(t *testing.T) {
	parsed, err := ParsePattern([]byte(`{"context~euclidean<1e-3":"TexMex food"}`))
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Embeddings) != 1 {
		t.Fatalf("expected 1 embeddings predicate, got %d", len(parsed.Embeddings))
	}
	predicate := parsed.Embeddings[0]
	if predicate.Metric != embeddingMetricEuclidean {
		t.Fatalf("expected euclidean metric, got %q", predicate.Metric)
	}
	if predicate.Threshold != 1e-3 {
		t.Fatalf("expected threshold 1e-3, got %v", predicate.Threshold)
	}
}

func TestParsePatternEmbeddingsRequireStringValue(t *testing.T) {
	_, err := ParsePattern([]byte(`{"context~":1}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires a string value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAIEmbeddingProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_EMBEDDINGS_MODEL", "")
	t.Setenv("EMBEDDINGS_API_KEY", "")

	var seenAuth string
	var seenBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		seenAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		seenBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"embedding":[0.1,0.2],"index":0,"object":"embedding"}]}`)
	}))
	defer ts.Close()

	provider := &openAIEmbeddingProvider{
		client:  ts.Client(),
		baseURL: ts.URL,
		model:   defaultEmbeddingModel,
	}

	embedding, err := provider.Embedding(context.Background(), "TexMex food")
	if err != nil {
		t.Fatal(err)
	}
	if len(embedding) != 2 || embedding[0] != 0.1 || embedding[1] != 0.2 {
		t.Fatalf("unexpected embedding %v", embedding)
	}
	if seenAuth != "Bearer test-key" {
		t.Fatalf("unexpected auth header %q", seenAuth)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(seenBody, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["input"] != "TexMex food" {
		t.Fatalf("unexpected input %v", payload["input"])
	}
	if payload["model"] != defaultEmbeddingModel {
		t.Fatalf("unexpected model %v", payload["model"])
	}
	if payload["encoding_format"] != "float" {
		t.Fatalf("unexpected encoding format %v", payload["encoding_format"])
	}
}

func TestOpenAIEmbeddingProviderPrefersEmbeddingsAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "wrong-key")
	t.Setenv("EMBEDDINGS_API_KEY", "preferred-key")

	var seenAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"embedding":[0.1,0.2],"index":0,"object":"embedding"}]}`)
	}))
	defer ts.Close()

	provider := newOpenAIEmbeddingProvider(ts.URL)
	if _, err := provider.Embedding(context.Background(), "TexMex food"); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer preferred-key" {
		t.Fatalf("unexpected auth header %q", seenAuth)
	}
}

func TestCompareEmbeddingsWithProvider(t *testing.T) {
	comparison, err := compareEmbeddingsWithProvider(context.Background(), "TexMex food", "tacos and queso", "cosine", stubEmbeddingProvider{
		values: map[string][]float64{
			"TexMex food":     {0, 1},
			"tacos and queso": {0, 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Query != "TexMex food" || comparison.Object != "tacos and queso" {
		t.Fatalf("unexpected comparison %+v", comparison)
	}
	if comparison.Metric != "cosine" {
		t.Fatalf("unexpected metric %q", comparison.Metric)
	}
	if comparison.Distance != 0 {
		t.Fatalf("unexpected distance %v", comparison.Distance)
	}
	if comparison.CosineDistance != 0 || comparison.CosineSimilarity != 1 {
		t.Fatalf("unexpected cosine values %+v", comparison)
	}
	if comparison.DotProduct != 1 {
		t.Fatalf("unexpected dot product %v", comparison.DotProduct)
	}
	if comparison.EuclideanDistance != 0 || comparison.SquaredEuclideanDistance != 0 {
		t.Fatalf("unexpected euclidean values %+v", comparison)
	}
}

func TestCompareEmbeddingsAtURLUsesEndpoint(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("EMBEDDINGS_API_KEY", "")

	var seenPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"embedding":[0.1,0.2],"index":0,"object":"embedding"}]}`)
	}))
	defer ts.Close()

	comparison, err := CompareEmbeddingsAtURL(context.Background(), "TexMex food", "tacos and queso", "cosine", ts.URL+"/custom")
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Metric != "cosine" {
		t.Fatalf("unexpected metric %q", comparison.Metric)
	}
	if seenPath != "/custom" {
		t.Fatalf("unexpected path %q", seenPath)
	}
}

func TestMatchWithEmbeddings(t *testing.T) {
	parsed, err := ParsePattern([]byte(`{"type":"task","context~":"TexMex food"}`))
	if err != nil {
		t.Fatal(err)
	}

	ok, err := matchWithProviders(context.Background(), json.RawMessage(`{"type":"task","context":"tacos and queso"}`), parsed, stubEmbeddingProvider{
		values: map[string][]float64{
			"TexMex food":     {0, 1},
			"tacos and queso": {0, 1},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}
}

func TestRdEmbeddingsSkipsEarlierNonMatch(t *testing.T) {
	s := newTestSpace(t)
	s.embedder = stubEmbeddingProvider{
		values: map[string][]float64{
			"TexMex food":     {0, 1},
			"pizza":           {1, 0},
			"tacos and queso": {0, 1},
		},
	}

	ctx := context.Background()
	if _, err := s.Out(json.RawMessage(`{"type":"task","context":"pizza"}`), nil, 0); err != nil {
		t.Fatal(err)
	}
	id, err := s.Out(json.RawMessage(`{"type":"task","context":"tacos and queso"}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.Rd(ctx, "anyone", json.RawMessage(`{"type":"task","context~":"TexMex food"}`), 0, "")
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

func TestInEmbeddingsConsumesMatchingObjectOnly(t *testing.T) {
	s := newTestSpace(t)
	s.embedder = stubEmbeddingProvider{
		values: map[string][]float64{
			"TexMex food":     {0, 1},
			"pizza":           {1, 0},
			"tacos and queso": {0, 1},
		},
	}

	ctx := context.Background()
	if _, err := s.Out(json.RawMessage(`{"type":"task","context":"pizza"}`), nil, 0); err != nil {
		t.Fatal(err)
	}
	id, err := s.Out(json.RawMessage(`{"type":"task","context":"tacos and queso"}`), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.In(ctx, "anyone", json.RawMessage(`{"type":"task","context~":"TexMex food"}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected a result")
	}
	if result.ID != id {
		t.Fatalf("expected id %q, got %q", id, result.ID)
	}

	remaining, err := s.Rd(ctx, "anyone", json.RawMessage(`{"context":"pizza"}`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if remaining == nil {
		t.Fatal("expected earlier non-matching object to remain")
	}
}

func TestNotifyModeEmbeddingsFallBackToPolling(t *testing.T) {
	cfg := notifyConfig()
	s := newTestSpaceWithConfig(t, cfg)
	s.embedder = stubEmbeddingProvider{
		values: map[string][]float64{
			"TexMex food":     {0, 1},
			"tacos and queso": {0, 1},
		},
	}

	done := make(chan *Result, 1)
	go func() {
		r, err := s.Rd(context.Background(), "anyone", json.RawMessage(`{"context~":"TexMex food"}`), time.Second, "")
		if err != nil {
			t.Errorf("rd: %v", err)
			return
		}
		done <- r
	}()

	time.Sleep(100 * time.Millisecond)
	if _, err := s.Out(json.RawMessage(`{"context":"tacos and queso"}`), nil, 0); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if result == nil {
			t.Fatal("expected a result")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for embeddings match")
	}
}
