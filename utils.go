package schemix

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue"
	cueerrors "cuelang.org/go/cue/errors"
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

// sortBlobRules sorts rules by priority (stable), overflow-safe via cmp.Compare.
func sortBlobRules(rules []blobRule) {
	slices.SortStableFunc(rules, func(a, b blobRule) int {
		return cmp.Compare(a.Meta.Priority, b.Meta.Priority)
	})
}

// formatCUEErrorPath formats a structured CUE error path using Go-style array indices.
func formatCUEErrorPath(parent string, err error) string {
	segments := cueerrors.Path(err)
	if len(segments) == 0 {
		return parent
	}

	var path strings.Builder
	for _, segment := range segments {
		if _, parseErr := strconv.ParseUint(segment, 10, 64); parseErr == nil {
			path.WriteByte('[')
			path.WriteString(segment)
			path.WriteByte(']')
			continue
		}
		if path.Len() > 0 {
			path.WriteByte('.')
		}
		path.WriteString(segment)
	}

	formatted := path.String()
	if parent == "" || formatted == parent || strings.HasPrefix(formatted, parent+".") ||
		strings.HasPrefix(formatted, parent+"[") {
		return formatted
	}
	return parent + "." + formatted
}

// extractFieldPriority reads the @meta(priority=N) value from a CUE field value.
// Returns 0 (default priority) if no priority is specified or if parsing fails.
func extractFieldPriority(fieldSchema cue.Value) int {
	metaAttr := fieldSchema.Attribute(attrMeta)
	if metaAttr.Err() != nil {
		return 0
	}
	for i := range metaAttr.NumArgs() {
		key, val := metaAttr.Arg(i)
		key = strings.TrimSpace(key)
		if key == metaPriority && val != "" {
			p, err := strconv.Atoi(val)
			if err == nil {
				return p
			}
		}
	}
	return 0
}

// suggestion is dropped. Guessing past this point produces noise rather than
// help, so no suggestion is preferable.
const maxSuggestionDistance = 2

// that "usd" suggests "USD".
func suggestClosest(value string, candidates []string) string {
	if value == "" || len(candidates) == 0 {
		return ""
	}

	best, bestDist := "", maxSuggestionDistance+1
	for _, c := range candidates {
		d := levenshteinFold(value, c)
		if d < bestDist {
			best, bestDist = c, d
		}
	}
	if bestDist > maxSuggestionDistance {
		return ""
	}
	// A short value must not be "corrected" to something entirely different:
	// require the distance to stay below the value's own length.
	if bestDist >= utf8.RuneCountInString(value) {
		return ""
	}
	return best
}

// levenshteinBufSize bounds the stack buffer used for edit distance. Enum values
// longer than this fall back to a heap allocation, which is acceptable because
// the path only runs when validation has already failed.
const levenshteinBufSize = 64

// levenshteinFold computes the case-insensitive edit distance between two
// strings using a single rolling row. Case folding happens per rune during
// comparison rather than by lowercasing both inputs up front, which would
// allocate two strings per candidate. The row lives on the stack for the short
// strings that enum values almost always are, so a failed validation allocates
// nothing here. Only invoked on the error path, never during successful
// validation.
func levenshteinFold(a, b string) int {
	la, lb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	var buf [levenshteinBufSize + 1]int
	var prev []int
	if lb < levenshteinBufSize {
		prev = buf[:lb+1]
	} else {
		prev = make([]int, lb+1)
	}
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	i := 0
	for _, ca := range a {
		i++
		diag := prev[0] // prev[j-1] before it is overwritten
		prev[0] = i
		j := 0
		for _, cb := range b {
			j++
			cost := 1
			if ca == cb || unicode.ToLower(ca) == unicode.ToLower(cb) {
				cost = 0
			}
			cur := min(prev[j]+1, min(prev[j-1]+1, diag+cost))
			diag, prev[j] = prev[j], cur
		}
	}
	return prev[lb]
}
