package ace

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type PatternBranch struct {
	Alternatives []string
}

func ExtractPatternBranches(data []byte) ([]PatternBranch, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("pattern is not a JSON object: %w", err)
	}
	var branches []PatternBranch
	if err := extractPatternFromObject(obj, nil, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func extractPatternFromObject(obj map[string]interface{}, path []string, out *[]PatternBranch) error {
	for k, v := range obj {
		if strings.HasPrefix(k, "#") {
			return fmt.Errorf("pattern property %q starts with #; metadata properties are not matchable", k)
		}
		p := append(path, escapeProperty(k))
		switch val := v.(type) {
		case map[string]interface{}:
			if err := extractPatternFromObject(val, p, out); err != nil {
				return err
			}
		case []interface{}:
			prefix := strings.Join(p, ".") + "="
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
			*out = append(*out, PatternBranch{Alternatives: alts})
		default:
			leaf, err := encodeLeaf(v)
			if err != nil {
				return err
			}
			branch := strings.Join(p, ".") + "=" + leaf
			*out = append(*out, PatternBranch{Alternatives: []string{branch}})
		}
	}
	return nil
}

func BuildMatchQuery(branches []PatternBranch, accessType string, callerID string, since string, now time.Time) (string, []interface{}) {
	var b strings.Builder
	var args []interface{}

	b.WriteString("SELECT o.id, o.json FROM objects o WHERE o.expires > ?")
	args = append(args, now.UTC().Format(idFormat))

	b.WriteString(" AND (o.invisible_until IS NULL OR o.invisible_until <= ?)")
	args = append(args, now.UTC().Format(idFormat))

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

	b.WriteString(" ORDER BY o.id ASC LIMIT 1")

	return b.String(), args
}
