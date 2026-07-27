package schemix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// Processable is an interface that types can implement to provide custom
// map conversion logic for use with ProcessValue/ValidateValue.
// This gives callers full control over how their data is represented.
type Processable interface {
	ToMap() map[string]any
}

// toMapAny converts various input types to map[string]any for internal processing.
// Supported types:
//   - map[string]any: returned directly (zero-cost)
//   - Processable: calls ToMap()
//   - []byte: treated as JSON, unmarshaled to map
//   - struct / *struct: converted via JSON round-trip (respects json tags)
//
// Returns an error for unsupported types (nil, non-struct pointers, channels, etc.)
func toMapAny(data any) (map[string]any, error) {
	if data == nil {
		return nil, fmt.Errorf("schemix: input data is nil")
	}

	// Fast path: already a map
	if m, ok := data.(map[string]any); ok {
		return m, nil
	}

	// Interface path: caller implements Processable
	if p, ok := data.(Processable); ok {
		return p.ToMap(), nil
	}

	// JSON bytes path
	if b, ok := data.([]byte); ok {
		return unmarshalJSONToMap(b)
	}

	// Struct path: use reflection to check, then JSON round-trip
	rv := reflect.ValueOf(data)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("schemix: input is nil pointer")
		}
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Struct {
		return structToMap(data)
	}

	return nil, fmt.Errorf("schemix: unsupported input type %T; expected map[string]any, struct, *struct, []byte (JSON), or Processable", data)
}

// unmarshalJSONToMap parses JSON bytes into map[string]any.
// Uses json.Number to preserve integer precision, then converts numbers:
// integers become int64, decimals become float64.
func unmarshalJSONToMap(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("schemix: empty JSON input")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("schemix: invalid JSON input: %w", err)
	}
	convertJSONNumbers(m)
	return m, nil
}

// convertJSONNumbers recursively converts json.Number values to int64 or float64.
func convertJSONNumbers(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case json.Number:
			m[k] = jsonNumberToNative(val)
		case map[string]any:
			convertJSONNumbers(val)
		case []any:
			convertJSONNumbersSlice(val)
		}
	}
}

func convertJSONNumbersSlice(s []any) {
	for i, v := range s {
		switch val := v.(type) {
		case json.Number:
			s[i] = jsonNumberToNative(val)
		case map[string]any:
			convertJSONNumbers(val)
		case []any:
			convertJSONNumbersSlice(val)
		}
	}
}

// jsonNumberToNative converts a json.Number to int64 if it represents an integer,
// otherwise to float64.
func jsonNumberToNative(n json.Number) any {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		return i
	}
	f, _ := n.Float64()
	return f
}

// structToMap converts a struct (or pointer to struct) to map[string]any via JSON
// round-trip. This respects json struct tags for field naming and omitempty.
// Integer fields are preserved as int64 (not float64).
//
// Performance note: For hot paths, callers should implement Processable to avoid
// the JSON round-trip overhead, or pass map[string]any directly.
func structToMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("schemix: failed to marshal struct: %w", err)
	}
	return unmarshalJSONToMap(b)
}
