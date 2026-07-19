package schemix

import (
	"slices"
	"strings"
)

// getNestedValue retrieves a value from a nested map by dot-separated path.
func getNestedValue(data map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var current any = data
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			current = v[part]
		default:
			return nil
		}
	}
	return current
}

// setNestedValue sets a value in a nested map by dot-separated path,
// creating intermediate maps as needed.
func setNestedValue(data map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
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
	parts := strings.Split(path, ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			delete(current, part)
			return
		}
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

// sortBlobRules sorts rules by priority (stable) using the standard library.
func sortBlobRules(rules []blobRule) {
	slices.SortStableFunc(rules, func(a, b blobRule) int {
		return a.Meta.Priority - b.Meta.Priority
	})
}

// extractIndex attempts to extract an array index path from a CUE error message.
func extractIndex(errMsg string) string {
	if idx := strings.Index(errMsg, ":"); idx > 0 {
		path := strings.TrimSpace(errMsg[:idx])
		if len(path) > 0 && path[0] >= '0' && path[0] <= '9' {
			return path
		}
	}
	return ""
}
