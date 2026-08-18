package schemix

import (
	"testing"
)

func TestDeepCopy_PreservesInt64(t *testing.T) {
	src := map[string]any{
		"amount": int64(10000),
		"nested": map[string]any{
			"value": int64(42),
		},
	}
	dst := deepCopy(src)

	if v, ok := dst["amount"].(int64); !ok || v != 10000 {
		t.Errorf("expected int64(10000), got %T(%v)", dst["amount"], dst["amount"])
	}
	nested := dst["nested"].(map[string]any)
	if v, ok := nested["value"].(int64); !ok || v != 42 {
		t.Errorf("expected int64(42), got %T(%v)", nested["value"], nested["value"])
	}

	// Mutating dst should not affect src
	dst["amount"] = int64(0)
	if src["amount"] != int64(10000) {
		t.Error("deepCopy did not isolate src from dst")
	}
}

func TestDeepCopy_PreservesSlice(t *testing.T) {
	src := map[string]any{
		"items": []any{
			map[string]any{"id": int64(1)},
			map[string]any{"id": int64(2)},
		},
	}
	dst := deepCopy(src)

	items := dst["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	item0 := items[0].(map[string]any)
	if v, ok := item0["id"].(int64); !ok || v != 1 {
		t.Errorf("expected int64(1), got %T(%v)", item0["id"], item0["id"])
	}

	// Mutating dst slice should not affect src
	items[0].(map[string]any)["id"] = int64(99)
	srcItems := src["items"].([]any)
	if srcItems[0].(map[string]any)["id"] != int64(1) {
		t.Error("deepCopy did not isolate slice elements")
	}
}

func TestDeepCopy_NilMap(t *testing.T) {
	if got := deepCopy(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ========== isEmpty ==========

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"non-empty string", "hello", false},
		{"int zero", int(0), true},
		{"int non-zero", int(42), false},
		{"int64 zero", int64(0), true},
		{"int64 non-zero", int64(100), false},
		{"float64 zero", float64(0), true},
		{"float64 non-zero", float64(3.14), false},
		{"bool false", false, true},
		{"bool true", true, false},
		{"empty slice", []any{}, true},
		{"non-empty slice", []any{1}, false},
		{"empty map", map[string]any{}, true},
		{"non-empty map", map[string]any{"k": "v"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEmpty(tt.input)
			if got != tt.expected {
				t.Errorf("isEmpty(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
