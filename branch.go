package ace

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func ExtractBranches(data []byte) ([]string, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	var branches []string
	if err := extractFromObject(obj, nil, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func extractFromObject(obj map[string]interface{}, path []string, out *[]string) error {
	for k, v := range obj {
		if strings.HasPrefix(k, "#") {
			continue
		}
		p := append(path, escapeProperty(k))
		switch val := v.(type) {
		case map[string]interface{}:
			if err := extractFromObject(val, p, out); err != nil {
				return err
			}
		case []interface{}:
			return fmt.Errorf("arrays are not permitted in object values")
		default:
			leaf, err := encodeLeaf(v)
			if err != nil {
				return err
			}
			branch := strings.Join(p, ".") + "=" + leaf
			*out = append(*out, branch)
		}
	}
	return nil
}

func encodeLeaf(v interface{}) (string, error) {
	switch val := v.(type) {
	case string:
		return `"` + escapeString(val) + `"`, nil
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case bool:
		if val {
			return "true", nil
		}
		return "false", nil
	case nil:
		return "null", nil
	default:
		return "", fmt.Errorf("unsupported leaf type %T", v)
	}
}

func escapeProperty(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case '.', '=', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func escapeString(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case '"', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}
