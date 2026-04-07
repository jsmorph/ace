package ace

import (
	"context"
	"encoding/json"
	"fmt"
)

// Match reports whether an object matches a pattern. It does not require a database.
func Match(object, pattern json.RawMessage) (bool, error) {
	return MatchWithEmbeddingsURL(object, pattern, "")
}

func MatchWithEmbeddingsURL(object, pattern json.RawMessage, endpoint string) (bool, error) {
	parsed, err := ParsePattern(pattern)
	if err != nil {
		return false, fmt.Errorf("pattern: %w", err)
	}
	return matchWithProvider(context.Background(), object, parsed, newOpenAIEmbeddingProvider(endpoint))
}

func matchWithProvider(ctx context.Context, object json.RawMessage, pattern ParsedPattern, provider embeddingProvider) (bool, error) {
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
		return true, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(object, &obj); err != nil {
		return false, fmt.Errorf("object: not a JSON object: %w", err)
	}

	ok, err := newEmbeddingMatcher(provider).matches(ctx, obj, pattern.Embeddings)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	return true, nil
}
