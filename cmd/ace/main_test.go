package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/morphism/ace"
)

func TestRunEmbCmp(t *testing.T) {
	orig := compareEmbeddings
	t.Cleanup(func() {
		compareEmbeddings = orig
	})

	compareEmbeddings = func(_ context.Context, query, object, metric string, endpoint string) (*ace.EmbeddingComparison, error) {
		if endpoint != "http://emb.example/v1/embeddings" {
			t.Fatalf("unexpected endpoint %q", endpoint)
		}
		return &ace.EmbeddingComparison{
			Query:                    query,
			Object:                   object,
			Metric:                   metric,
			Distance:                 0.42,
			CosineDistance:           0.42,
			CosineSimilarity:         0.58,
			DotProduct:               1.23,
			EuclideanDistance:        0.9,
			SquaredEuclideanDistance: 0.81,
		}, nil
	}

	var out bytes.Buffer
	if err := runEmbCmp(&out, "TexMex food", "tacos and queso", "cosine", 0.5, "http://emb.example/v1/embeddings"); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Query                    string  `json:"query"`
		Object                   string  `json:"object"`
		Metric                   string  `json:"metric"`
		Distance                 float64 `json:"distance"`
		CosineDistance           float64 `json:"cosine_distance"`
		CosineSimilarity         float64 `json:"cosine_similarity"`
		DotProduct               float64 `json:"dot_product"`
		EuclideanDistance        float64 `json:"euclidean_distance"`
		SquaredEuclideanDistance float64 `json:"squared_euclidean_distance"`
		Threshold                float64 `json:"threshold"`
		Match                    bool    `json:"match"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Query != "TexMex food" || got.Object != "tacos and queso" {
		t.Fatalf("unexpected texts: %+v", got)
	}
	if got.Metric != "cosine" {
		t.Fatalf("unexpected metric %q", got.Metric)
	}
	if got.Distance != 0.42 {
		t.Fatalf("unexpected distance %v", got.Distance)
	}
	if got.CosineDistance != 0.42 || got.CosineSimilarity != 0.58 {
		t.Fatalf("unexpected cosine values %+v", got)
	}
	if got.DotProduct != 1.23 {
		t.Fatalf("unexpected dot product %v", got.DotProduct)
	}
	if got.EuclideanDistance != 0.9 || got.SquaredEuclideanDistance != 0.81 {
		t.Fatalf("unexpected euclidean values %+v", got)
	}
	if got.Threshold != 0.5 {
		t.Fatalf("unexpected threshold %v", got.Threshold)
	}
	if !got.Match {
		t.Fatal("expected match")
	}
}
