package schemix

import (
	"testing"
)

// TestRequiredField_MissingShouldFail verifies that missing required fields
// produce CodeRequiredMissing (E1M01) errors.
func TestRequiredField_MissingShouldFail(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int
	}`)

	// Missing "age" — should fail
	r := v.Process(map[string]any{"name": "Alice"})
	if r.Valid {
		t.Fatal("expected invalid result when required field 'age' is missing")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing && e.Path == "age" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected E1M01 error on path 'age', got errors: %v", r.Errors)
	}
}

// TestRequiredField_AllMissingShouldFail verifies that ALL missing required
// fields are reported in FailAll mode.
func TestRequiredField_AllMissingShouldFail(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int
	}`)

	// Both fields missing
	r := v.ProcessWithMode(map[string]any{}, FailAll)
	if r.Valid {
		t.Fatal("expected invalid result when all required fields are missing")
	}

	paths := map[string]bool{}
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing {
			paths[e.Path] = true
		}
	}
	if !paths["name"] {
		t.Error("expected E1M01 for 'name'")
	}
	if !paths["age"] {
		t.Error("expected E1M01 for 'age'")
	}
}

// TestRequiredField_OptionalShouldNotError verifies that optional fields
// (marked with ?) do NOT produce errors when missing.
func TestRequiredField_OptionalShouldNotError(t *testing.T) {
	v := MustNew(`{
		name:  string
		memo?: string
	}`)

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Errorf("expected valid result when optional field is missing, got errors: %v", r.Errors)
	}
}

// TestRequiredField_BlobShouldNotError verifies that @blob fields do NOT
// produce required-missing errors (they are computed, not user-supplied).
func TestRequiredField_BlobShouldNotError(t *testing.T) {
	v := MustNew(`{
		amount:   int & >0
		doubled:  number @blob(this.amount * 2)
	}`)

	r := v.Process(map[string]any{"amount": int64(100)})
	if !r.Valid {
		t.Errorf("expected valid result — @blob fields are computed, got errors: %v", r.Errors)
	}
}

// TestRequiredField_NestedStructMissing verifies that required fields
// inside nested structs are also detected.
func TestRequiredField_NestedStructMissing(t *testing.T) {
	v := MustNew(`{
		name: string
		address: {
			city:    string
			country: string
		}
	}`)

	// address present but city is missing
	r := v.Process(map[string]any{
		"name":    "Alice",
		"address": map[string]any{"country": "CN"},
	})
	if r.Valid {
		t.Fatal("expected invalid when nested required field 'address.city' is missing")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing && e.Path == "address.city" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected E1M01 on 'address.city', got errors: %v", r.Errors)
	}
}

// TestRequiredField_NullableFieldAllowsNil verifies that a field declared as
// `null | string` does NOT report required-missing when nil is passed.
func TestRequiredField_NullableFieldAllowsNil(t *testing.T) {
	v := MustNew(`{
		name:  string
		memo:  null | string
	}`)

	// memo explicitly set to nil — should be valid (nullable schema)
	r := v.Process(map[string]any{"name": "Alice", "memo": nil})
	if !r.Valid {
		t.Errorf("expected valid for nullable field with nil value, got errors: %v", r.Errors)
	}
}

// TestRequiredField_FailFastStopsAtFirst verifies FailFast mode stops
// after the first required-missing error.
func TestRequiredField_FailFastStopsAtFirst(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int
		city: string
	}`)

	r := v.ProcessWithMode(map[string]any{}, FailFast)
	if r.Valid {
		t.Fatal("expected invalid in FailFast with all fields missing")
	}
	if len(r.Errors) != 1 {
		t.Errorf("FailFast should produce exactly 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
	if r.Errors[0].Code != CodeRequiredMissing {
		t.Errorf("expected CodeRequiredMissing, got %s", r.Errors[0].Code)
	}
}

// TestRequiredField_ErrorFields verifies the error structure fields.
func TestRequiredField_ErrorFields(t *testing.T) {
	v := MustNew(`{
		username: string
	}`)

	r := v.Process(map[string]any{})
	if r.Valid {
		t.Fatal("expected invalid")
	}
	if len(r.Errors) == 0 {
		t.Fatal("expected at least 1 error")
	}

	e := r.Errors[0]
	if e.Code != CodeRequiredMissing {
		t.Errorf("Code: want %s, got %s", CodeRequiredMissing, e.Code)
	}
	if e.Path != "username" {
		t.Errorf("Path: want 'username', got %q", e.Path)
	}
	if e.Type != TypeCUE {
		t.Errorf("Type: want %q, got %q", TypeCUE, e.Type)
	}
	if e.Message == "" {
		t.Error("Message should not be empty")
	}
}

// ========== Validate() Fast Path ==========

// TestArrayPathFormatting verifies exact indexed paths for nested array element errors.
func TestArrayPathFormatting(t *testing.T) {
	tests := []struct {
		name     string
		items    []any
		wantPath string
	}{
		{
			name: "first_element",
			items: []any{
				map[string]any{"name": "Alice", "age": "not-int"},
			},
			wantPath: "items[0].age",
		},
		{
			name: "second_element",
			items: []any{
				map[string]any{"name": "Alice", "age": int64(30)},
				map[string]any{"name": "Bob", "age": "not-int"},
			},
			wantPath: "items[1].age",
		},
	}

	v := MustNew(`{ items: [...{ name: string, age: int }] }`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(map[string]any{"items": tt.items})
			if r.Valid {
				t.Fatal("expected Valid=false for array element type mismatch")
			}

			for _, validationErr := range r.Errors {
				if validationErr.Code == CodeTypeMismatch && validationErr.Path == tt.wantPath {
					return
				}
			}
			t.Fatalf("expected E1T01 at %q, got %v", tt.wantPath, r.Errors)
		})
	}
}

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

// allFastSchema has only scalar constraints, so every field gets a
// fastConstraint and no cue.Value is ever required.
const allFastSchema = `{
	pan:      =~"^[0-9]{16}$"
	amount:   int & >0
	currency: "156" | "840"
	age:      int & >=0 & <=150
	active:   bool
}`

var allFastData = map[string]any{
	"pan":      "6222021234567890",
	"amount":   int64(10000),
	"currency": "156",
	"age":      int64(30),
	"active":   true,
}

// TestLazyCUEEncode_SkippedWhenAllFieldsFastPathed asserts the encode is
// actually skipped. cue.Context.Encode alone accounts for 39 allocations, so an
// allocation count below that threshold is proof it never ran.
func TestLazyCUEEncode_SkippedWhenAllFieldsFastPathed(t *testing.T) {
	v := MustNew(allFastSchema)

	// Warm up so first-call lazy initialisation is not attributed to the run.
	if ok, errs := v.Validate(allFastData); !ok {
		t.Fatalf("precondition failed, schema should accept data: %v", errs)
	}

	const encodeAllocs = 39 // measured cost of cue.Context.Encode alone
	got := testing.AllocsPerRun(200, func() {
		v.Validate(allFastData)
	})

	if got >= encodeAllocs {
		t.Errorf("Validate allocated %.0f objects, want < %d — cue.Context.Encode "+
			"appears to still run even though every field is fast-pathed", got, encodeAllocs)
	}
}

// TestLazyCUEEncode_StillCorrectWhenEncodeNeeded covers the paths that DO need a
// cue.Value: unsigned integers make the fast path return Handled=false, and
// struct/list/@blob fields have no fast descriptor at all. Each case is compared
// against the CUE oracle so a skipped encode can never change a verdict.
func TestLazyCUEEncode_StillCorrectWhenEncodeNeeded(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		data   map[string]any
	}{
		{
			name:   "uint falls back to CUE mid-validation",
			schema: allFastSchema,
			data: map[string]any{
				"pan": "6222021234567890", "amount": uint64(10000),
				"currency": "156", "age": int64(30), "active": true,
			},
		},
		{
			name:   "uint out of range still rejected",
			schema: `{ age: int & >=0 & <=150 }`,
			data:   map[string]any{"age": uint32(999)},
		},
		{
			name:   "nested struct requires navigation",
			schema: `{ user: { name: string, age: int & >0 } }`,
			data:   map[string]any{"user": map[string]any{"name": "Alice", "age": int64(30)}},
		},
		{
			name:   "nested struct invalid leaf",
			schema: `{ user: { name: string, age: int & >0 } }`,
			data:   map[string]any{"user": map[string]any{"name": "Alice", "age": int64(-1)}},
		},
		{
			name:   "list requires unify",
			schema: `{ items: [...{ qty: int & >0 }] }`,
			data:   map[string]any{"items": []any{map[string]any{"qty": int64(1)}}},
		},
		{
			name:   "list invalid element",
			schema: `{ items: [...{ qty: int & >0 }] }`,
			data:   map[string]any{"items": []any{map[string]any{"qty": int64(0)}}},
		},
		{
			name:   "all-fast schema with invalid scalar",
			schema: allFastSchema,
			data: map[string]any{
				"pan": "ABC", "amount": int64(-1),
				"currency": "999", "age": int64(999), "active": true,
			},
		},
		{
			name:   "missing required field",
			schema: allFastSchema,
			data:   map[string]any{"pan": "6222021234567890"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MustNew(tt.schema).Process(tt.data)
			want := processCUEOnly(t, tt.schema, tt.data)

			if got.Valid != want.Valid {
				t.Fatalf("Valid = %v, want %v (CUE oracle); got errors=%v, oracle errors=%v",
					got.Valid, want.Valid, got.Errors, want.Errors)
			}
			if len(got.Errors) > 0 && len(want.Errors) > 0 {
				if got.Errors[0].Path != want.Errors[0].Path {
					t.Errorf("first error path = %q, want %q", got.Errors[0].Path, want.Errors[0].Path)
				}
			}
		})
	}
}

// TestLazyCUEEncode_BlobRulesUnaffected guards that @blob rules, which read the
// raw Go map rather than a cue.Value, keep working and keep producing computed
// output when the encode is skipped.
func TestLazyCUEEncode_BlobRulesUnaffected(t *testing.T) {
	v := MustNew(`{
		amount: int & >0
		fee:    number @blob((this.amount * 0.015).ceil())
		ok:     bool   @blob(this.amount > 100)
	}`)

	r := v.Process(map[string]any{"amount": int64(10000)})
	if !r.Valid {
		t.Fatalf("Valid = false, want true; errors: %v", r.Errors)
	}
	if got := r.Output["fee"]; got != int64(150) {
		t.Errorf("Output[fee] = %v (%T), want 150", got, got)
	}
}

// ─── Attributes inside array elements ───────────────────────────────────────
//
// extractRules only recurses into StructKind, so @blob/@meta written inside an
// array element schema was silently dropped: New() succeeded, the rule never
// ran, and invalid data passed validation. Failing open is unacceptable for a
// validator, so such schemas must be rejected at construction time with a
// message that points at the supported form.
