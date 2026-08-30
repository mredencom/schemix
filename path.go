package schemix

import (
	"slices"
	"strings"
)

// getNestedValue retrieves a value from a nested map by dot-separated path.
//
// The path is walked segment by segment via strings.IndexByte rather than
// strings.Split, which would allocate a []string on every call. These
// functions sit on the hot path of every @blob rule that reads or writes a
// nested field.
func getNestedValue(data map[string]any, path string) any {
	var current any = data
	for {
		var part string
		if i := strings.IndexByte(path, '.'); i >= 0 {
			part, path = path[:i], path[i+1:]
		} else {
			part = path
			m, ok := current.(map[string]any)
			if !ok {
				return nil
			}
			return m[part]
		}
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
}

// setNestedValue sets a value in a nested map by dot-separated path,
// creating intermediate maps as needed.
func setNestedValue(data map[string]any, path string, value any) {
	current := data
	for {
		i := strings.IndexByte(path, '.')
		if i < 0 {
			current[path] = value
			return
		}
		part := path[:i]
		path = path[i+1:]
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
}

// deleteNestedKey removes a key from a nested map by dot-separated path.
func deleteNestedKey(data map[string]any, path string) {
	current := data
	for {
		i := strings.IndexByte(path, '.')
		if i < 0 {
			delete(current, path)
			return
		}
		part := path[:i]
		path = path[i+1:]
		next, ok := current[part].(map[string]any)
		if !ok {
			return
		}
		current = next
	}
}

// deepCopy creates a deep copy of a value, preserving original types.
// Unlike JSON round-trip, this correctly preserves int64, []byte, and other
// non-JSON-native types.
func deepCopy(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

// deepCopyValue recursively copies a single value.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopy(val)
	case []any:
		cp := make([]any, len(val))
		for i, elem := range val {
			cp[i] = deepCopyValue(elem)
		}
		return cp
	case []byte:
		return slices.Clone(val)
	default:
		// Scalars (string, int64, float64, bool, nil) are immutable — safe to share.
		return val
	}
}

// isEmpty reports whether a value is considered "empty" for skip_empty / omit_empty logic.
func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case int:
		return val == 0
	case int64:
		return val == 0
	case float64:
		return val == 0
	case bool:
		return !val
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	default:
		return false
	}
}
