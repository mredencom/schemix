package schemix

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

// TestNilHandling verifies correct nil behavior for required, nullable, and optional fields.
// R1: nil on non-nullable → E1M01; nullable nil → valid; optional absent → valid.
// R2: optional present nil → E1M01; optional present wrong type → E1T01/E1E01.
func TestNilHandling(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		data      map[string]any
		wantValid bool
		wantCode  ErrorCode // expected error code (ignored if wantValid)
		wantPath  string    // expected error path (ignored if wantValid)
	}{
		// R1: nil on non-nullable required fields
		{
			name:      "required_string_nil_E1M01",
			schema:    `{ name: string }`,
			data:      map[string]any{"name": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "name",
		},
		{
			name:      "required_int_nil_E1M01",
			schema:    `{ age: int }`,
			data:      map[string]any{"age": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "age",
		},
		{
			name:      "required_bool_nil_E1M01",
			schema:    `{ flag: bool }`,
			data:      map[string]any{"flag": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "flag",
		},
		// R1: nullable allows nil
		{
			name:      "nullable_string_nil_valid",
			schema:    `{ memo: null | string }`,
			data:      map[string]any{"memo": nil},
			wantValid: true,
		},
		{
			name:      "nullable_int_nil_valid",
			schema:    `{ count: null | int }`,
			data:      map[string]any{"count": nil},
			wantValid: true,
		},
		// R1: optional absent
		{
			name:      "optional_absent_valid",
			schema:    `{ memo?: string }`,
			data:      map[string]any{},
			wantValid: true,
		},
		// R2: optional present nil → E1M01 (optional means "can be absent" not "can be nil")
		{
			name:      "optional_present_nil_E1M01",
			schema:    `{ memo?: string }`,
			data:      map[string]any{"memo": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "memo",
		},
		{
			name:      "optional_int_present_nil_E1M01",
			schema:    `{ count?: int }`,
			data:      map[string]any{"count": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "count",
		},
		// R2: optional present wrong type → appropriate error
		{
			name:      "optional_list_wrong_type_E1T01",
			schema:    `{ items?: [...{id: string}] }`,
			data:      map[string]any{"items": "not-a-list"},
			wantValid: false,
			wantCode:  CodeTypeMismatch,
			wantPath:  "items",
		},
		{
			name:      "optional_struct_wrong_type_E1T01",
			schema:    `{ addr?: { city: string } }`,
			data:      map[string]any{"addr": 123},
			wantValid: false,
			wantCode:  CodeTypeMismatch,
			wantPath:  "addr",
		},
		// R2: optional nullable wrong type — moved to standalone subtest below
		// (uses HasCode + path scan instead of Errors[0] order dependency)
		// R2: optional nullable present nil is valid (nullable trumps)
		{
			name:      "optional_nullable_present_nil_valid",
			schema:    `{ memo?: null | string }`,
			data:      map[string]any{"memo": nil},
			wantValid: true,
		},
		// R1: required with valid value still passes
		{
			name:      "required_string_valid_value",
			schema:    `{ name: string }`,
			data:      map[string]any{"name": "Alice"},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := MustNew(tt.schema)
			r := v.Process(tt.data)

			if r.Valid != tt.wantValid {
				t.Fatalf("valid=%v, want %v; errors=%v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid {
				if len(r.Errors) == 0 {
					t.Fatal("expected errors but got none")
				}
				if r.Errors[0].Code != tt.wantCode {
					t.Errorf("code=%s, want %s; msg=%s", r.Errors[0].Code, tt.wantCode, r.Errors[0].Message)
				}
				if r.Errors[0].Path != tt.wantPath {
					t.Errorf("path=%s, want %s", r.Errors[0].Path, tt.wantPath)
				}
			}
		})
	}

	// R2: optional nullable wrong type — spec requires errors CONTAIN E1T01.
	// CUE emits E1E01 (disjunction) + E1T01 (type mismatch per branch); order is
	// implementation detail. We assert HasCode + path without order dependency.
	t.Run("optional_nullable_wrong_type_contains_E1T01", func(t *testing.T) {
		v := MustNew(`{ note?: null | string }`)
		r := v.Process(map[string]any{"note": 42})

		if r.Valid {
			t.Fatal("expected validation failure for wrong type on nullable optional")
		}
		if !r.HasCode(CodeTypeMismatch) {
			t.Errorf("expected errors to contain E1T01 (CodeTypeMismatch); got: %v", r.Errors)
		}
		// Verify E1T01 is reported on the correct path.
		var foundE1T01AtPath bool
		for _, e := range r.Errors {
			if e.Code == CodeTypeMismatch && e.Path == "note" {
				foundE1T01AtPath = true
				break
			}
		}
		if !foundE1T01AtPath {
			t.Errorf("expected E1T01 at path \"note\"; errors: %v", r.Errors)
		}
	})
}

// TestNewFromValueNilHandling verifies nil handling through NewFromValue with CUE definitions.
func TestNewFromValueNilHandling(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		data      map[string]any
		wantValid bool
		wantCode  ErrorCode
		wantPath  string
	}{
		{
			name: "definition_ref_nullable_nil_valid",
			schema: `{
				#T: null | string
				name: #T
			}`,
			data:      map[string]any{"name": nil},
			wantValid: true,
		},
		{
			name: "definition_ref_nonnullable_nil_E1M01",
			schema: `{
				#T: string
				name: #T
			}`,
			data:      map[string]any{"name": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := cuecontext.New()
			val := ctx.CompileString(tt.schema)
			if val.Err() != nil {
				t.Fatalf("compile: %v", val.Err())
			}

			v, err := NewFromValue(val)
			if err != nil {
				t.Fatalf("NewFromValue: %v", err)
			}

			r := v.Process(tt.data)
			if r.Valid != tt.wantValid {
				t.Fatalf("valid=%v, want %v; errors=%v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid {
				if len(r.Errors) == 0 {
					t.Fatal("expected errors but got none")
				}
				if r.Errors[0].Code != tt.wantCode {
					t.Errorf("code=%s, want %s; msg=%s", r.Errors[0].Code, tt.wantCode, r.Errors[0].Message)
				}
				if r.Errors[0].Path != tt.wantPath {
					t.Errorf("path=%s, want %s", r.Errors[0].Path, tt.wantPath)
				}
			}
		})
	}
}
