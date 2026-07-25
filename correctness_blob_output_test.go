package schemix

import (
	"testing"
)

// TestBlobStrictTypeContract verifies that @blob expressions producing non-bool
// results have their output verified against the CUE field type. If the computed
// value doesn't match the declared type, the result is E2T01 (CodeBlobTypeMismatch).
func TestBlobStrictTypeContract(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		data      map[string]any
		wantValid bool
		wantCode  ErrorCode
		wantPath  string
	}{
		{
			name:      "blob_string_matches_type",
			schema:    `{ name: string, greeting: string @blob("hello " + this.name) }`,
			data:      map[string]any{"name": "world"},
			wantValid: true,
		},
		{
			name:      "blob_int_matches_type",
			schema:    `{ a: int, double: int @blob(this.a * 2) }`,
			data:      map[string]any{"a": int64(5)},
			wantValid: true,
		},
		{
			name:      "blob_returns_int_for_string_field_E2T01",
			schema:    `{ a: int, result: string @blob(this.a * 2) }`,
			data:      map[string]any{"a": int64(5)},
			wantValid: false,
			wantCode:  CodeBlobTypeMismatch,
			wantPath:  "result",
		},
		{
			name:      "blob_returns_string_for_int_field_E2T01",
			schema:    `{ name: string, result: int @blob("not an int: " + this.name) }`,
			data:      map[string]any{"name": "hello"},
			wantValid: false,
			wantCode:  CodeBlobTypeMismatch,
			wantPath:  "result",
		},
		{
			name:      "blob_returns_string_for_number_field_E2T01",
			schema:    `{ result: number @blob("not a number" + "") }`,
			data:      map[string]any{},
			wantValid: false,
			wantCode:  CodeBlobTypeMismatch,
			wantPath:  "result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := New(tt.schema)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			r := v.Process(tt.data)
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid {
				if !r.HasCode(tt.wantCode) {
					t.Errorf("expected code %v, got errors: %v", tt.wantCode, r.Errors)
				}
				found := false
				for _, e := range r.Errors {
					if e.Code == tt.wantCode && e.Path == tt.wantPath {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected code %v at path %q, got errors: %v", tt.wantCode, tt.wantPath, r.Errors)
				}
			}
		})
	}
}

// TestOutputNilOnInvalid verifies the all-or-nothing Output contract for every FailMode.

// TestBlobRunsOnlyAfterCUEPasses verifies that a field's @blob rule is not
// evaluated when that same field already failed its CUE constraint.
func TestBlobRunsOnlyAfterCUEPasses(t *testing.T) {
	v := MustNew(`{ pan: =~"^[0-9]{16}$" @blob(this.pan.luhn_valid()) }`)

	tests := []struct {
		name      string
		pan       string
		wantValid bool
		wantCode  ErrorCode
	}{
		{name: "cue_failure_skips_blob", pan: "short", wantCode: CodeFormatMismatch},
		{name: "cue_passes_then_blob_passes", pan: "4111111111111111", wantValid: true},
		{name: "cue_passes_then_blob_fails", pan: "1234567890123456", wantCode: CodeBizRuleFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(map[string]any{"pan": tt.pan})
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid && !r.HasCode(tt.wantCode) {
				t.Fatalf("expected %s, got %v", tt.wantCode, r.Errors)
			}
			if tt.wantCode == CodeFormatMismatch && r.HasCode(CodeBizRuleFailed) {
				t.Fatalf("blob executed after CUE failure: %v", r.Errors)
			}
		})
	}
}
func TestOutputNilOnInvalid(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		data       map[string]any
		mode       FailMode
		wantValid  bool
		wantOutput map[string]any
	}{
		{
			name:       "valid_result_has_computed_output",
			schema:     `{ name: string, greeting: string @blob("hi " + this.name) }`,
			data:       map[string]any{"name": "Alice"},
			mode:       FailAll,
			wantValid:  true,
			wantOutput: map[string]any{"name": "Alice", "greeting": "hi Alice"},
		},
		{
			name:      "fail_all_cue_error",
			schema:    `{ name: string, age: int }`,
			data:      map[string]any{"name": "Alice", "age": "not-an-int"},
			mode:      FailAll,
			wantValid: false,
		},
		{
			name:      "fail_fast_cue_error",
			schema:    `{ name: string, age: int }`,
			data:      map[string]any{"name": "Alice", "age": "not-an-int"},
			mode:      FailFast,
			wantValid: false,
		},
		{
			name:      "fail_priority_cue_error",
			schema:    `{ name: string, age: int }`,
			data:      map[string]any{"name": "Alice", "age": "not-an-int"},
			mode:      FailPriority,
			wantValid: false,
		},
		{
			name:      "fail_all_blob_error",
			schema:    `{ age: int, adult: bool @blob(this.age >= 18) }`,
			data:      map[string]any{"age": int64(10)},
			mode:      FailAll,
			wantValid: false,
		},
		{
			name:      "fail_fast_blob_error",
			schema:    `{ age: int, adult: bool @blob(this.age >= 18) }`,
			data:      map[string]any{"age": int64(10)},
			mode:      FailFast,
			wantValid: false,
		},
		{
			name:      "fail_priority_blob_error",
			schema:    `{ age: int, adult: bool @blob(this.age >= 18) }`,
			data:      map[string]any{"age": int64(10)},
			mode:      FailPriority,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := New(tt.schema)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			r := v.ProcessWithMode(tt.data, tt.mode)
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid {
				if r.Output != nil {
					t.Fatalf("Output = %v, want nil", r.Output)
				}
				return
			}
			if len(r.Output) != len(tt.wantOutput) {
				t.Fatalf("Output = %v, want %v", r.Output, tt.wantOutput)
			}
			for key, want := range tt.wantOutput {
				if got := r.Output[key]; got != want {
					t.Errorf("Output[%q] = %v, want %v", key, got, want)
				}
			}
		})
	}
}
