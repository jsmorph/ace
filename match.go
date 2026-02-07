package ace

import (
	"encoding/json"
	"fmt"
)

// Match reports whether an object matches a pattern. It does not require a database.
func Match(object, pattern json.RawMessage) (bool, error) {
	branches, err := ExtractBranches(object)
	if err != nil {
		return false, fmt.Errorf("object: %w", err)
	}

	pbs, err := ExtractPatternBranches(pattern)
	if err != nil {
		return false, fmt.Errorf("pattern: %w", err)
	}

	set := make(map[string]bool, len(branches))
	for _, b := range branches {
		set[b] = true
	}

	for _, pb := range pbs {
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

	return true, nil
}
