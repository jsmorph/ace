package ace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEmbeddingMetric    = "cosine"
	DefaultEmbeddingThreshold = 0.25
	defaultEmbeddingsURL      = "https://api.openai.com/v1/embeddings"
	defaultEmbeddingModel     = "text-embedding-3-small"
)

type EmbeddingComparison struct {
	Query                    string  `json:"query"`
	Object                   string  `json:"object"`
	Metric                   string  `json:"metric"`
	Distance                 float64 `json:"distance"`
	CosineDistance           float64 `json:"cosine_distance"`
	CosineSimilarity         float64 `json:"cosine_similarity"`
	DotProduct               float64 `json:"dot_product"`
	EuclideanDistance        float64 `json:"euclidean_distance"`
	SquaredEuclideanDistance float64 `json:"squared_euclidean_distance"`
}

type embeddingProvider interface {
	Embedding(ctx context.Context, input string) ([]float64, error)
}

type openAIEmbeddingProvider struct {
	client  *http.Client
	baseURL string
	model   string
}

func newOpenAIEmbeddingProvider(endpoint string) *openAIEmbeddingProvider {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = defaultEmbeddingsURL
	}
	return &openAIEmbeddingProvider{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: endpoint,
		model:   defaultEmbeddingModel,
	}
}

func (p *openAIEmbeddingProvider) Embedding(ctx context.Context, input string) ([]float64, error) {
	apiKey := resolveEmbeddingsAPIKey()
	if apiKey == "" {
		return nil, validationErr(fmt.Errorf(
			"embeddings filtering isn't available: EMBEDDINGS_API_KEY and OPENAI_API_KEY are not set"))
	}

	model := p.model
	if override := strings.TrimSpace(os.Getenv("OPENAI_EMBEDDINGS_MODEL")); override != "" {
		model = override
	}

	body, err := json.Marshal(struct {
		Input          string `json:"input"`
		Model          string `json:"model"`
		EncodingFormat string `json:"encoding_format"`
	}{
		Input:          input,
		Model:          model,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embeddings request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request embeddings: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embeddings response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("embeddings API %s: %s", resp.Status, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("embeddings API %s", resp.Status)
	}

	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings API returned no embedding")
	}
	return out.Data[0].Embedding, nil
}

func resolveEmbeddingsAPIKey() string {
	if apiKey := strings.TrimSpace(os.Getenv("EMBEDDINGS_API_KEY")); apiKey != "" {
		return apiKey
	}
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

type embeddingMetric string

const (
	embeddingMetricCosine      embeddingMetric = "cosine"
	embeddingMetricEuclidean   embeddingMetric = "euclidean"
	embeddingMetricSqEuclidean embeddingMetric = "sqeuclidean"
)

func parseEmbeddingMetric(raw string) (embeddingMetric, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DefaultEmbeddingMetric, "cos":
		return embeddingMetricCosine, nil
	case string(embeddingMetricEuclidean), "l2":
		return embeddingMetricEuclidean, nil
	case string(embeddingMetricSqEuclidean), "l2sq", "squared-euclidean":
		return embeddingMetricSqEuclidean, nil
	default:
		return "", fmt.Errorf("unsupported embeddings metric %q", raw)
	}
}

func (m embeddingMetric) Distance(a, b []float64) (float64, error) {
	stats, err := computeEmbeddingStats(a, b)
	if err != nil {
		return 0, err
	}

	switch m {
	case embeddingMetricCosine:
		return stats.CosineDistance()
	case embeddingMetricEuclidean:
		return stats.EuclideanDistance(), nil
	case embeddingMetricSqEuclidean:
		return stats.SquaredEuclideanDistance(), nil
	default:
		return 0, fmt.Errorf("unsupported embeddings metric %q", m)
	}
}

type embeddingStats struct {
	dot             float64
	normA           float64
	normB           float64
	squaredDistance float64
}

func computeEmbeddingStats(a, b []float64) (embeddingStats, error) {
	if len(a) == 0 || len(b) == 0 {
		return embeddingStats{}, fmt.Errorf("embedding distance requires non-empty vectors")
	}
	if len(a) != len(b) {
		return embeddingStats{}, fmt.Errorf("embedding vectors have different lengths: %d and %d", len(a), len(b))
	}

	var stats embeddingStats
	for i := range a {
		d := a[i] - b[i]
		stats.squaredDistance += d * d
		stats.dot += a[i] * b[i]
		stats.normA += a[i] * a[i]
		stats.normB += b[i] * b[i]
	}
	return stats, nil
}

func (s embeddingStats) CosineDistance() (float64, error) {
	if s.normA == 0 || s.normB == 0 {
		return 0, fmt.Errorf("embedding distance requires non-zero vectors")
	}
	return 1 - s.dot/(math.Sqrt(s.normA)*math.Sqrt(s.normB)), nil
}

func (s embeddingStats) CosineSimilarity() (float64, error) {
	distance, err := s.CosineDistance()
	if err != nil {
		return 0, err
	}
	return 1 - distance, nil
}

func (s embeddingStats) DotProduct() float64 {
	return s.dot
}

func (s embeddingStats) EuclideanDistance() float64 {
	return math.Sqrt(s.squaredDistance)
}

func (s embeddingStats) SquaredEuclideanDistance() float64 {
	return s.squaredDistance
}

type embeddingPredicate struct {
	Path      []string
	PathText  string
	Metric    embeddingMetric
	Threshold float64
	Query     string
}

type embeddingMatcher struct {
	provider embeddingProvider
	cache    map[string][]float64
}

func newEmbeddingMatcher(provider embeddingProvider) *embeddingMatcher {
	return &embeddingMatcher{
		provider: provider,
		cache:    make(map[string][]float64),
	}
}

func (m *embeddingMatcher) matches(ctx context.Context, object map[string]interface{}, predicates []embeddingPredicate) (bool, error) {
	if len(predicates) == 0 {
		return true, nil
	}
	if m.provider == nil {
		return false, fmt.Errorf("embedding provider is not configured")
	}

	for _, predicate := range predicates {
		values := lookupStringValues(object, predicate.Path)
		if len(values) == 0 {
			return false, nil
		}

		queryEmbedding, err := m.embedding(ctx, predicate.Query)
		if err != nil {
			return false, err
		}

		matched := false
		for _, value := range values {
			candidateEmbedding, err := m.embedding(ctx, value)
			if err != nil {
				return false, err
			}
			distance, err := predicate.Metric.Distance(queryEmbedding, candidateEmbedding)
			if err != nil {
				return false, fmt.Errorf("compare embeddings at %q: %w", predicate.PathText, err)
			}
			log.Printf(
				"embedding compare path=%q metric=%s distance=%g threshold=%g query=%q candidate=%q",
				predicate.PathText,
				predicate.Metric,
				distance,
				predicate.Threshold,
				predicate.Query,
				value,
			)
			if distance < predicate.Threshold {
				matched = true
				break
			}
		}

		if !matched {
			return false, nil
		}
	}

	return true, nil
}

func (m *embeddingMatcher) embedding(ctx context.Context, text string) ([]float64, error) {
	if cached, ok := m.cache[text]; ok {
		return cached, nil
	}
	embedding, err := m.provider.Embedding(ctx, text)
	if err != nil {
		return nil, err
	}
	m.cache[text] = embedding
	return embedding, nil
}

func CompareEmbeddings(ctx context.Context, query, object, metric string) (*EmbeddingComparison, error) {
	return CompareEmbeddingsAtURL(ctx, query, object, metric, "")
}

func CompareEmbeddingsAtURL(ctx context.Context, query, object, metric string, endpoint string) (*EmbeddingComparison, error) {
	return compareEmbeddingsWithProvider(ctx, query, object, metric, newOpenAIEmbeddingProvider(endpoint))
}

func compareEmbeddingsWithProvider(ctx context.Context, query, object, metric string, provider embeddingProvider) (*EmbeddingComparison, error) {
	parsedMetric, err := parseEmbeddingMetric(metric)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is not configured")
	}

	queryEmbedding, err := provider.Embedding(ctx, query)
	if err != nil {
		return nil, err
	}
	objectEmbedding, err := provider.Embedding(ctx, object)
	if err != nil {
		return nil, err
	}
	stats, err := computeEmbeddingStats(queryEmbedding, objectEmbedding)
	if err != nil {
		return nil, err
	}
	distance, err := parsedMetric.Distance(queryEmbedding, objectEmbedding)
	if err != nil {
		return nil, err
	}
	cosineDistance, err := stats.CosineDistance()
	if err != nil {
		return nil, err
	}
	cosineSimilarity, err := stats.CosineSimilarity()
	if err != nil {
		return nil, err
	}

	return &EmbeddingComparison{
		Query:                    query,
		Object:                   object,
		Metric:                   string(parsedMetric),
		Distance:                 distance,
		CosineDistance:           cosineDistance,
		CosineSimilarity:         cosineSimilarity,
		DotProduct:               stats.DotProduct(),
		EuclideanDistance:        stats.EuclideanDistance(),
		SquaredEuclideanDistance: stats.SquaredEuclideanDistance(),
	}, nil
}

func lookupStringValues(object map[string]interface{}, path []string) []string {
	var current interface{} = object
	for _, step := range path {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		next, ok := obj[step]
		if !ok {
			return nil
		}
		current = next
	}

	switch v := current.(type) {
	case string:
		return []string{v}
	case []interface{}:
		values := make([]string, 0, len(v))
		for _, elem := range v {
			s, ok := elem.(string)
			if ok {
				values = append(values, s)
			}
		}
		return values
	default:
		return nil
	}
}

func parseEmbeddingThreshold(raw string) (float64, error) {
	threshold, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid embeddings threshold %q: %w", raw, err)
	}
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 {
		return 0, fmt.Errorf("invalid embeddings threshold %q", raw)
	}
	return threshold, nil
}
