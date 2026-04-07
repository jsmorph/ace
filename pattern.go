package ace

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PatternBranch holds the alternative branch strings for one pattern leaf.
type PatternBranch struct {
	Alternatives []string
}

// ParsedPattern separates exact branch matches from embeddings predicates.
type ParsedPattern struct {
	Exact      []PatternBranch
	Embeddings []embeddingPredicate
	Questions  []questionPredicate
}

func (p ParsedPattern) HasEmbeddings() bool {
	return len(p.Embeddings) > 0
}

func (p ParsedPattern) HasQuestions() bool {
	return len(p.Questions) > 0
}

func (p ParsedPattern) HasDynamic() bool {
	return p.HasEmbeddings() || p.HasQuestions()
}

type patternProperty struct {
	Name       string
	Embeddings *embeddingSpec
	Question   *questionSpec
}

type embeddingSpec struct {
	Metric    embeddingMetric
	Threshold float64
}

type questionSpec struct{}

// ExtractPatternBranches returns the branches for a JSON pattern.
func ExtractPatternBranches(data []byte) ([]PatternBranch, error) {
	parsed, err := ParsePattern(data)
	if err != nil {
		return nil, err
	}
	if parsed.HasDynamic() {
		return nil, fmt.Errorf("pattern uses non-exact matching")
	}
	return parsed.Exact, nil
}

// ParsePattern parses a JSON pattern into exact and embeddings predicates.
func ParsePattern(data []byte) (ParsedPattern, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return ParsedPattern{}, fmt.Errorf("pattern is not a JSON object: %w", err)
	}
	var parsed ParsedPattern
	if err := extractPatternFromObject(obj, nil, nil, &parsed); err != nil {
		return ParsedPattern{}, err
	}
	return parsed, nil
}

func extractPatternFromObject(obj map[string]interface{}, rawPath, branchPath []string, out *ParsedPattern) error {
	for k, v := range obj {
		property, err := parsePatternProperty(k)
		if err != nil {
			return err
		}

		raw := append(rawPath[:len(rawPath):len(rawPath)], property.Name)
		branch := append(branchPath[:len(branchPath):len(branchPath)], escapeProperty(property.Name))

		if property.Embeddings != nil {
			query, ok := v.(string)
			if !ok {
				return fmt.Errorf("embeddings pattern at %q requires a string value", strings.Join(raw, "."))
			}
			out.Embeddings = append(out.Embeddings, embeddingPredicate{
				Path:      raw,
				PathText:  strings.Join(raw, "."),
				Metric:    property.Embeddings.Metric,
				Threshold: property.Embeddings.Threshold,
				Query:     query,
			})
			continue
		}
		if property.Question != nil {
			contextText, ok := v.(string)
			if !ok {
				return fmt.Errorf("LLM pattern at %q requires a string value", strings.Join(raw, "."))
			}
			out.Questions = append(out.Questions, questionPredicate{
				Path:        raw,
				PathText:    strings.Join(raw, "."),
				ContextText: contextText,
			})
			continue
		}

		switch val := v.(type) {
		case map[string]interface{}:
			if err := extractPatternFromObject(val, raw, branch, out); err != nil {
				return err
			}
		case []interface{}:
			prefix := strings.Join(branch, ".") + "="
			alts := make([]string, len(val))
			for i, elem := range val {
				if _, ok := elem.(map[string]interface{}); ok {
					return fmt.Errorf("pattern array element cannot be an object")
				}
				if _, ok := elem.([]interface{}); ok {
					return fmt.Errorf("pattern array element cannot be an array")
				}
				leaf, err := encodeLeaf(elem)
				if err != nil {
					return err
				}
				alts[i] = prefix + leaf
			}
			out.Exact = append(out.Exact, PatternBranch{Alternatives: alts})
		default:
			leaf, err := encodeLeaf(v)
			if err != nil {
				return err
			}
			out.Exact = append(out.Exact, PatternBranch{
				Alternatives: []string{strings.Join(branch, ".") + "=" + leaf},
			})
		}
	}
	return nil
}

func parsePatternProperty(raw string) (patternProperty, error) {
	if strings.HasPrefix(raw, "#") {
		return patternProperty{}, fmt.Errorf(
			"pattern property %q starts with #; metadata properties are not matchable", raw)
	}

	if strings.HasSuffix(raw, "?") {
		name := strings.TrimSuffix(raw, "?")
		if name == "" {
			return patternProperty{}, fmt.Errorf("pattern property %q has empty name before ?", raw)
		}
		if strings.Contains(name, "~") {
			return patternProperty{}, fmt.Errorf("pattern property %q mixes ? and ~ suffixes", raw)
		}
		return patternProperty{
			Name:     name,
			Question: &questionSpec{},
		}, nil
	}

	name, suffix, hasSuffix := strings.Cut(raw, "~")
	if !hasSuffix {
		return patternProperty{Name: raw}, nil
	}
	if name == "" {
		return patternProperty{}, fmt.Errorf("pattern property %q has empty name before ~", raw)
	}
	if strings.Contains(name, "?") {
		return patternProperty{}, fmt.Errorf("pattern property %q mixes ? and ~ suffixes", raw)
	}
	if suffix == "" {
		return patternProperty{
			Name: name,
			Embeddings: &embeddingSpec{
				Metric:    embeddingMetricCosine,
				Threshold: DefaultEmbeddingThreshold,
			},
		}, nil
	}

	metricText, thresholdText, ok := strings.Cut(suffix, "<")
	if !ok || metricText == "" || thresholdText == "" {
		return patternProperty{}, fmt.Errorf(
			"pattern property %q has invalid embeddings suffix; use field~ or field~METRIC<threshold>", raw)
	}

	metric, err := parseEmbeddingMetric(metricText)
	if err != nil {
		return patternProperty{}, err
	}
	threshold, err := parseEmbeddingThreshold(thresholdText)
	if err != nil {
		return patternProperty{}, err
	}

	return patternProperty{
		Name: name,
		Embeddings: &embeddingSpec{
			Metric:    metric,
			Threshold: threshold,
		},
	}, nil
}

// BuildMatchQuery constructs the SQL query that finds the earliest matching object.
func BuildMatchQuery(branches []PatternBranch, accessType string, callerID string, since string, now time.Time) (string, []interface{}) {
	return buildMatchQuery(branches, accessType, callerID, since, now, true)
}

func BuildScanQuery(branches []PatternBranch, accessType string, callerID string, since string, now time.Time) (string, []interface{}) {
	return buildMatchQuery(branches, accessType, callerID, since, now, false)
}

func buildMatchQuery(branches []PatternBranch, accessType string, callerID string, since string, now time.Time, limitOne bool) (string, []interface{}) {
	var b strings.Builder
	var args []interface{}

	b.WriteString("SELECT o.id, o.json FROM objects o WHERE o.expires > ?")
	args = append(args, now.UTC().Format(timestampFormat))

	b.WriteString(" AND (o.invisible_until IS NULL OR o.invisible_until <= ?)")
	args = append(args, now.UTC().Format(timestampFormat))

	b.WriteString(" AND o.id > ?")
	args = append(args, since)

	for _, pb := range branches {
		if len(pb.Alternatives) == 1 {
			b.WriteString(" AND EXISTS (SELECT 1 FROM branches br WHERE br.id = o.id AND br.b = ?)")
			args = append(args, pb.Alternatives[0])
		} else {
			b.WriteString(" AND EXISTS (SELECT 1 FROM branches br WHERE br.id = o.id AND br.b IN (")
			for i, alt := range pb.Alternatives {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString("?")
				args = append(args, alt)
			}
			b.WriteString("))")
		}
	}

	b.WriteString(" AND (NOT EXISTS (SELECT 1 FROM access a WHERE a.id = o.id AND a.type = ?)")
	args = append(args, accessType)
	b.WriteString(" OR EXISTS (SELECT 1 FROM access a WHERE a.id = o.id AND a.type = ? AND a.iid = ?))")
	args = append(args, accessType, callerID)

	b.WriteString(" ORDER BY o.id ASC")
	if limitOne {
		b.WriteString(" LIMIT 1")
	}

	return b.String(), args
}
