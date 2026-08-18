package schemix

import (
	"testing"
)

func TestEnumErrorSuggestsClosestCandidate(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		data       map[string]any
		suggestion string
	}{
		{
			name:       "one character off",
			schema:     `{ currency: "CNY" | "USD" | "EUR" }`,
			data:       map[string]any{"currency": "USE"},
			suggestion: "USD",
		},
		{
			name:       "case difference",
			schema:     `{ currency: "CNY" | "USD" }`,
			data:       map[string]any{"currency": "usd"},
			suggestion: "USD",
		},
		{
			name:       "transposition",
			schema:     `{ role: "admin" | "guest" }`,
			data:       map[string]any{"role": "adnim"},
			suggestion: "admin",
		},
		{
			name:       "nothing close enough",
			schema:     `{ currency: "CNY" | "USD" }`,
			data:       map[string]any{"currency": "completely-different"},
			suggestion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := firstError(t, tt.schema, tt.data)
			if e.Suggestion != tt.suggestion {
				t.Errorf("Suggestion = %q, want %q", e.Suggestion, tt.suggestion)
			}
		})
	}
}

// TestSuggestionOnlyForEnums documents the deliberate scope: a range or regex
// violation has no meaningful "did you mean", so the field stays empty.
func TestSuggestionOnlyForEnums(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		data   map[string]any
	}{
		{"range", `{ age: int & >=0 & <=150 }`, map[string]any{"age": int64(999)}},
		{"regex", `{ pan: =~"^[0-9]{16}$" }`, map[string]any{"pan": "abc"}},
		{"type", `{ age: int }`, map[string]any{"age": "old"}},
		{"required", `{ age: int }`, map[string]any{}},
		{"blob rule", `{ age: int @blob(this.age >= 18) }`, map[string]any{"age": int64(10)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if e := firstError(t, tt.schema, tt.data); e.Suggestion != "" {
				t.Errorf("Suggestion = %q, want empty for a %s violation", e.Suggestion, tt.name)
			}
		})
	}
}

// ─── FieldType ──────────────────────────────────────────────────────────────
