package core

import (
	"context"
	"encoding/json"
	"fmt"
)

// Match reports whether an object matches a pattern. It does not require a database.
func Match(object, pattern json.RawMessage) (bool, error) {
	return MatchWithConfig(object, pattern, DefaultConfig())
}

func MatchWithEmbeddingsURL(object, pattern json.RawMessage, endpoint string) (bool, error) {
	cfg := DefaultConfig()
	cfg.EmbeddingsURL = endpoint
	return MatchWithConfig(object, pattern, cfg)
}

func MatchWithConfig(object, pattern json.RawMessage, cfg Config) (bool, error) {
	return MatchWithConfigContext(context.Background(), object, pattern, cfg)
}

func MatchWithConfigContext(ctx context.Context, object, pattern json.RawMessage, cfg Config) (bool, error) {
	parsed, err := ParsePattern(pattern)
	if err != nil {
		return false, fmt.Errorf("pattern: %w", err)
	}
	return matchWithProviders(
		ctx,
		object,
		parsed,
		newOpenAIEmbeddingProvider(cfg.EmbeddingsURL),
		newOpenAILLMJudge(cfg.LLMURL, cfg.LLMModel),
	)
}

func matchWithProviders(ctx context.Context, object json.RawMessage, pattern ParsedPattern, embedder embeddingProvider, judge llmJudge) (bool, error) {
	branches, err := ExtractBranches(object)
	if err != nil {
		return false, fmt.Errorf("object: %w", err)
	}

	set := make(map[string]bool, len(branches))
	for _, b := range branches {
		set[b] = true
	}

	for _, pb := range pattern.Exact {
		found := false
		for _, alt := range pb.Alternatives {
			if set[alt] {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}

	if !pattern.HasEmbeddings() {
		if !pattern.HasQuestions() {
			return true, nil
		}
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(object, &obj); err != nil {
		return false, fmt.Errorf("object: not a JSON object: %w", err)
	}

	if pattern.HasEmbeddings() {
		ok, err := newEmbeddingMatcher(embedder).matches(ctx, obj, pattern.Embeddings)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	if pattern.HasQuestions() {
		ok, err := newQuestionMatcher(judge).matches(ctx, obj, pattern.Questions)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}
