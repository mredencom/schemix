package schemix

import (
	"cmp"
	"math"
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// =============================================================================
// Meta (@meta) compile & runtime tests
// =============================================================================

// TestMetaCompileReject verifies parsefieldMeta fails on invalid @meta() params (R9).
func TestMetaCompileReject(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string // substring in compile error
	}{
		// Unknown params must error
		{
			name:    "unknown_param_rejects",
			schema:  `{ x: string @meta(foo_bar) }`,
			wantErr: "unknown @meta parameter",
		},
		// priority must be a valid integer
		{
			name:    "priority_non_numeric",
			schema:  `{ x: string @meta(priority=abc) }`,
			wantErr: "priority",
		},
		{
			name:    "priority_overflow",
			schema:  `{ x: string @meta(priority=99999999999999999999) }`,
			wantErr: "priority",
		},
		// Negative priority IS valid (not an error)
		// required_if with empty expression
		{
			name:    "required_if_empty_expr",
			schema:  `{ x?: string @meta(conditional, required_if=) }`,
			wantErr: "required_if",
		},
		// skip_if with empty expression
		{
			name:    "skip_if_empty_expr",
			schema:  `{ x: string @meta(skip_if=) }`,
			wantErr: "skip_if",
		},
		// required_if with invalid bloblang expression (valid CUE syntax)
		{
			name:    "required_if_parse_error",
			schema:  `{ x?: string @meta(conditional, required_if=this.!!!bad) }`,
			wantErr: "required_if",
		},
		// skip_if with invalid bloblang expression (valid CUE syntax)
		{
			name:    "skip_if_parse_error",
			schema:  `{ x: string @meta(skip_if=this.!!!bad) }`,
			wantErr: "skip_if",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)
			if err == nil {
				t.Fatal("expected compile error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestMetaCompileAccept verifies valid @meta() params compile successfully.
func TestMetaCompileAccept(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{"negative_priority", `{ x: string @meta(priority=-1) }`},
		{"flag_optional", `{ x?: string @meta(optional) }`},
		{"flag_conditional", `{ x?: string @meta(conditional) }`},
		{"flag_skip_empty", `{ x: string @meta(skip_empty) }`},
		{"flag_fail_fast", `{ x: string @meta(fail_fast) }`},
		{"flag_omit_if_skip", `{ x: string @meta(omit_if_skip) }`},
		{"flag_omit_empty", `{ x: string @meta(omit_empty) }`},
		{"valid_required_if", `{ x?: string @meta(conditional, required_if=true) }`},
		{"valid_skip_if", `{ x: string @meta(skip_if=true) }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
		})
	}
}

// TestMetaRuntimeQueryError verifies runtime Query errors in required_if/skip_if
// produce E3X01 (not silent swallow) with the original expression (R10).
func TestMetaRuntimeQueryError(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		data     map[string]any
		mode     FailMode
		wantCode ErrorCode
		wantExpr string // expression must appear in Message
	}{
		// required_if Query error in conditional branch (fieldVal==nil, x absent)
		{
			name:     "conditional_required_if_query_error",
			schema:   `{ x?: string @meta(conditional, required_if=this.a > 0) }`,
			data:     map[string]any{},
			mode:     FailAll,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.a > 0",
		},
		// required_if Query error in optional branch (fieldVal==nil, x absent)
		{
			name:     "optional_required_if_query_error",
			schema:   `{ x?: string @meta(optional, required_if=this.a > 0) }`,
			data:     map[string]any{},
			mode:     FailAll,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.a > 0",
		},
		// skip_if Query error (x present, but skip_if references missing field)
		{
			name:     "skip_if_query_error",
			schema:   `{ x: string @meta(skip_if=this.a > 0) }`,
			data:     map[string]any{"x": "hello"},
			mode:     FailAll,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.a > 0",
		},
		// FailFast returns on first Query error
		{
			name:     "failfast_skip_if_query_error",
			schema:   `{ x: string @meta(skip_if=this.b > 0) }`,
			data:     map[string]any{"x": "hello"},
			mode:     FailFast,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.b > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := New(tt.schema)
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
			r := v.ProcessWithMode(tt.data, tt.mode)
			if r.Valid {
				t.Fatal("expected validation failure, got Valid=true (silent swallow)")
			}
			if len(r.Errors) == 0 {
				t.Fatal("expected at least one error")
			}
			found := false
			for _, e := range r.Errors {
				if e.Code == tt.wantCode && strings.Contains(e.Message, tt.wantExpr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no error with code=%s containing expr=%q; got: %v",
					tt.wantCode, tt.wantExpr, r.Errors)
			}
		})
	}
}

// =============================================================================
// FailPriority layer isolation tests
// =============================================================================

// TestFailPriorityFullLayerIsolation verifies that FailPriority mode groups errors
// by priority across BOTH CUE and Blob layers: if priority group N fails,
// higher-priority groups (N+1, N+2, ...) are not executed in the Blob layer.
func TestFailPriorityFullLayerIsolation(t *testing.T) {
	// Schema with two priority groups:
	// Priority 1: pan must be 16 digits (CUE regex)
	// Priority 2: luhn check (blob)
	// If priority 1 fails, priority 2 should NOT be executed
	schema := `{
		pan:        =~"^[0-9]{16}$" @meta(priority=1)
		luhn_check: bool @blob(this.pan.luhn_valid()) @meta(priority=2)
	}`

	t.Run("p1_cue_fails_p2_blob_skipped", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// pan is too short — CUE regex fails at priority 1
		data := map[string]any{"pan": "123"}
		r := v.ProcessWithMode(data, FailPriority)

		if r.Valid {
			t.Fatal("expected Valid=false")
		}
		// Should only have priority 1 errors (CUE regex), NOT priority 2 (blob)
		for _, e := range r.Errors {
			if e.Path == "luhn_check" {
				t.Errorf("FailPriority should have skipped priority 2 blob, got error: %v", e)
			}
		}
		if !r.HasCode(CodeFormatMismatch) {
			t.Errorf("expected E1F01 for pan regex failure, got: %v", r.Errors)
		}
	})

	t.Run("p1_passes_p2_executes", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// pan is 16 digits but invalid luhn — priority 1 passes, priority 2 should execute
		data := map[string]any{"pan": "1234567890123456"}
		r := v.ProcessWithMode(data, FailPriority)

		if r.Valid {
			t.Fatal("expected Valid=false (luhn should fail)")
		}
		if !r.HasCode(CodeBizRuleFailed) {
			t.Errorf("expected E2B01 for luhn failure, got: %v", r.Errors)
		}
	})

	t.Run("both_pass_valid", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// Valid Visa card number (passes luhn)
		data := map[string]any{"pan": "4111111111111111"}
		r := v.ProcessWithMode(data, FailPriority)

		if !r.Valid {
			t.Fatalf("expected Valid=true, got errors: %v", r.Errors)
		}
	})

	t.Run("same_priority_cue_and_blob_errors_are_reported", func(t *testing.T) {
		v := MustNew(`{
			name:  string @meta(priority=1)
			adult: bool @blob(false) @meta(priority=1)
		}`)

		r := v.ProcessWithMode(map[string]any{"name": int64(42)}, FailPriority)
		if r.Valid {
			t.Fatal("expected Valid=false")
		}
		if !r.HasCode(CodeTypeMismatch) || !r.HasCode(CodeBizRuleFailed) {
			t.Fatalf("expected same-group E1T01 and E2B01, got %v", r.Errors)
		}
	})
}

// TestFailPriorityCUEGrouping verifies that CUE errors are grouped by the field's
// priority metadata, and only the lowest-failed priority group's CUE errors are reported.
func TestFailPriorityCUEGrouping(t *testing.T) {
	// Two CUE fields at different priorities:
	// priority 1: name must be string
	// priority 2: age must be int
	schema := `{
		name: string @meta(priority=1)
		age:  int    @meta(priority=2)
	}`

	t.Run("p1_cue_fails_p2_cue_not_reported", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// Both fields wrong — only priority 1 should be reported
		data := map[string]any{"name": int64(123), "age": "not-int"}
		r := v.ProcessWithMode(data, FailPriority)

		if r.Valid {
			t.Fatal("expected Valid=false")
		}
		// Should only report name error (priority 1), not age (priority 2)
		for _, e := range r.Errors {
			if e.Path == "age" {
				t.Errorf("FailPriority should not report priority 2 CUE error, got: %v", e)
			}
		}
		if !r.HasErrorsAt("name") {
			t.Errorf("expected error at path 'name', got: %v", r.Errors)
		}
	})
}

// =============================================================================
// FailMode validation tests
// =============================================================================

// TestInvalidFailModeProcess verifies that ProcessWithMode with an undefined
// FailMode value does NOT panic and returns a structured error result.
func TestInvalidFailModeProcess(t *testing.T) {
	v := MustNew(`{ name: string }`)
	data := map[string]any{"name": "Alice"}

	// Must not panic
	r := v.ProcessWithMode(data, FailMode(99))

	if r.Valid {
		t.Fatal("expected Valid=false for invalid FailMode")
	}
	if r.Output != nil {
		t.Fatalf("expected nil Output for invalid FailMode, got %v", r.Output)
	}
	if !r.HasCode(CodeConfigError) {
		t.Fatalf("expected error code E0C01, got errors: %v", r.Errors)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
	e := r.Errors[0]
	if e.Type != "config" {
		t.Errorf("expected Type=%q, got %q", "config", e.Type)
	}
	if e.Path != "" {
		t.Errorf("expected empty Path for config error, got %q", e.Path)
	}
}

// TestValidFailModesStillWork ensures FailAll, FailFast, FailPriority execute normally.
func TestValidFailModesStillWork(t *testing.T) {
	v := MustNew(`{ name: string, age: int }`)
	data := map[string]any{"name": "Bob", "age": int64(25)}

	modes := []struct {
		name string
		mode FailMode
	}{
		{"FailAll", FailAll},
		{"FailFast", FailFast},
		{"FailPriority", FailPriority},
	}

	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			r := v.ProcessWithMode(data, tc.mode)
			if !r.Valid {
				t.Errorf("expected Valid=true for %s, got errors: %v", tc.name, r.Errors)
			}
			if r.Output == nil {
				t.Errorf("expected non-nil Output for %s", tc.name)
			}
		})
	}
}

// TestInvalidFailModeBloblangMapping verifies that Bloblang plugins reject
// invalid mode strings at mapping construction time (not at execution time).
func TestInvalidFailModeBloblangMapping(t *testing.T) {
	releaseEnv(globalBloblangEnvironment)
	t.Cleanup(func() { releaseEnv(globalBloblangEnvironment) })
	reg := NewRegistry()
	if err := reg.Register("test", `{ name: string }`); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterAll(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		mapping string
	}{
		{"method_validate", `root = this.validate_schema(name: "test", mode: "invalid")`},
		{"method_process", `root = this.process_schema(name: "test", mode: "invalid")`},
		{"func_validate", `root = validate_schema(data: this, name: "test", mode: "invalid")`},
		{"func_process", `root = process_schema(data: this, name: "test", mode: "invalid")`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := bloblang.GlobalEnvironment()
			_, err := env.Parse(tc.mapping)
			if err == nil {
				t.Fatal("expected error from mapping construction with invalid mode, got nil")
			}
		})
	}
}

// TestValidModeBloblangMapping verifies that valid mode strings construct and execute.
func TestValidModeBloblangMapping(t *testing.T) {
	releaseEnv(globalBloblangEnvironment)
	t.Cleanup(func() { releaseEnv(globalBloblangEnvironment) })
	reg := NewRegistry()
	if err := reg.Register("test", `{ name: string }`); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterAll(); err != nil {
		t.Fatal(err)
	}

	validModes := []string{"all", "fast", "priority"}

	for _, mode := range validModes {
		t.Run("method_validate_"+mode, func(t *testing.T) {
			env := bloblang.GlobalEnvironment()
			exe, err := env.Parse(`root = this.validate_schema(name: "test", mode: "` + mode + `")`)
			if err != nil {
				t.Fatalf("expected no error for mode %q, got: %v", mode, err)
			}
			res, err := exe.Query(map[string]any{"name": "Alice"})
			if err != nil {
				t.Fatalf("execution failed: %v", err)
			}
			m, ok := res.(map[string]any)
			if !ok {
				t.Fatalf("expected map result, got %T", res)
			}
			if m["valid"] != true {
				t.Errorf("expected valid=true for mode %q, got %v", mode, m["valid"])
			}
		})
	}
}

// =============================================================================
// Fast path tests (three-state, precision, types, arrays)
// =============================================================================

// TestFastpathThreeState verifies the three-state return signature:
// (handled bool, valid bool, code ErrorCode, detail string)
// - handled=false: caller MUST fall through to CUE Unify
// - handled=true, valid=true: field passes fast check
// - handled=true, valid=false: field fails with code+detail
func TestFastpathThreeState(t *testing.T) {
	tests := []struct {
		name        string
		constraint  *fastConstraint
		val         any
		wantHandled bool
		wantValid   bool
		wantCode    ErrorCode
	}{
		// --- handled=true, valid=true: normal passing cases ---
		{
			name: "int_passes_range",
			constraint: &fastConstraint{
				kind: constraintRange, expectInt: true, useInt64: true,
				hasMin: true, minInt64: 0, hasMax: true, maxInt64: 100,
			},
			val:         int64(50),
			wantHandled: true,
			wantValid:   true,
		},
		{
			name:        "string_passes_type",
			constraint:  &fastConstraint{kind: constraintType, expectString: true},
			val:         "hello",
			wantHandled: true,
			wantValid:   true,
		},
		// --- handled=true, valid=false: known rejection ---
		{
			name:        "float64_on_int_rejected_E1T01",
			constraint:  &fastConstraint{kind: constraintType, expectInt: true},
			val:         float64(3.14),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeTypeMismatch,
		},
		{
			name:        "float32_on_int_rejected_E1T01",
			constraint:  &fastConstraint{kind: constraintType, expectInt: true},
			val:         float32(1.5),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeTypeMismatch,
		},
		{
			name:        "float64_on_int_range_rejected_E1T01",
			constraint:  &fastConstraint{kind: constraintRange, expectInt: true, hasMin: true, min: 0, hasMax: true, max: 100},
			val:         float64(50.0),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeTypeMismatch,
		},
		// --- NaN/Inf rejection ---
		{
			name:        "NaN_on_float_rejected_E1T01",
			constraint:  &fastConstraint{kind: constraintRange, expectFloat: true, hasMin: true, min: 0, hasMax: true, max: 100},
			val:         math.NaN(),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeTypeMismatch,
		},
		{
			name:        "PosInf_on_float_rejected_E1R01",
			constraint:  &fastConstraint{kind: constraintRange, expectFloat: true, hasMin: true, min: 0, hasMax: true, max: 100},
			val:         math.Inf(1),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeRangeViolation,
		},
		{
			name:        "NegInf_on_float_rejected_E1R01",
			constraint:  &fastConstraint{kind: constraintRange, expectFloat: true, hasMin: true, min: 0, hasMax: true, max: 100},
			val:         math.Inf(-1),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeRangeViolation,
		},
		{
			name:        "NaN_on_number_type_rejected_E1T01",
			constraint:  &fastConstraint{kind: constraintType, expectFloat: true},
			val:         math.NaN(),
			wantHandled: true,
			wantValid:   false,
			wantCode:    CodeTypeMismatch,
		},
		// --- handled=false: uint types on expectInt → fall through to CUE ---
		{
			name:        "uint_on_int_type_not_handled",
			constraint:  &fastConstraint{kind: constraintType, expectInt: true},
			val:         uint(42),
			wantHandled: false,
			wantValid:   false,
			wantCode:    "",
		},
		{
			name:        "uint64_on_int_range_not_handled",
			constraint:  &fastConstraint{kind: constraintRange, expectInt: true, hasMin: true, min: 0, hasMax: true, max: 100},
			val:         uint64(50),
			wantHandled: false,
			wantValid:   false,
			wantCode:    "",
		},
		{
			name:        "uint32_on_int_enum_not_handled",
			constraint:  &fastConstraint{kind: constraintEnum, expectInt: true, intEnums: []int64{1, 2, 3}},
			val:         uint32(2),
			wantHandled: false,
			wantValid:   false,
			wantCode:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := validateFast(tt.constraint, tt.val)
			if fr.Handled != tt.wantHandled {
				t.Errorf("handled = %v, want %v", fr.Handled, tt.wantHandled)
			}
			if fr.Valid != tt.wantValid {
				t.Errorf("valid = %v, want %v", fr.Valid, tt.wantValid)
			}
			if tt.wantCode != "" && fr.Code != tt.wantCode {
				t.Errorf("code = %v, want %v", fr.Code, tt.wantCode)
			}
		})
	}
}

// TestCmpCompareSortOverflowSafe verifies that sortBlobRules uses cmp.Compare
// instead of integer subtraction, preventing overflow on extreme priority values.
func TestCmpCompareSortOverflowSafe(t *testing.T) {
	rules := []blobRule{
		{Path: "c", Meta: fieldMeta{Priority: math.MaxInt}},
		{Path: "a", Meta: fieldMeta{Priority: math.MinInt}},
		{Path: "b", Meta: fieldMeta{Priority: 0}},
	}

	sortBlobRules(rules)

	// Expected order: MinInt < 0 < MaxInt
	expected := []string{"a", "b", "c"}
	for i, r := range rules {
		if r.Path != expected[i] {
			t.Errorf("rules[%d].Path = %q, want %q", i, r.Path, expected[i])
		}
	}

	// Verify the old subtraction would have overflowed:
	// math.MinInt - math.MaxInt would wrap positive, corrupting sort.
	// We check that cmp.Compare gives the correct answer:
	if cmp.Compare(math.MinInt, math.MaxInt) != -1 {
		t.Error("cmp.Compare(MinInt, MaxInt) should be -1")
	}
}

// TestIntegrationFloatOnInt verifies that passing a float value to an int-typed
// CUE field produces E1T01 at the public API level via the fast path.
func TestIntegrationFloatOnInt(t *testing.T) {
	v := MustNew(`{ age: int & >=0 & <=150 }`)

	tests := []struct {
		name      string
		data      map[string]any
		wantValid bool
		wantCode  ErrorCode
	}{
		{
			name:      "int64_passes",
			data:      map[string]any{"age": int64(25)},
			wantValid: true,
		},
		{
			name:      "float64_on_int_field_E1T01",
			data:      map[string]any{"age": float64(25.0)},
			wantValid: false,
			wantCode:  CodeTypeMismatch,
		},
		{
			name:      "float64_fractional_on_int_E1T01",
			data:      map[string]any{"age": float64(25.5)},
			wantValid: false,
			wantCode:  CodeTypeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(tt.data)
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid && !r.HasCode(tt.wantCode) {
				t.Errorf("expected code %v, got: %v", tt.wantCode, r.Errors)
			}
			if !tt.wantValid && r.Output != nil {
				t.Errorf("expected nil Output on invalid result, got: %v", r.Output)
			}
		})
	}
}

// TestIntegrationNaNInf verifies NaN/Inf rejection and error classification.
func TestIntegrationNaNInf(t *testing.T) {
	v := MustNew(`{ score: float & >=0.0 & <=100.0 }`)

	tests := []struct {
		name      string
		value     float64
		wantValid bool
		wantCode  ErrorCode
	}{
		{name: "valid_float", value: 85.5, wantValid: true},
		{name: "NaN_is_type_mismatch", value: math.NaN(), wantCode: CodeTypeMismatch},
		{name: "positive_inf_is_range_violation", value: math.Inf(1), wantCode: CodeRangeViolation},
		{name: "negative_inf_is_range_violation", value: math.Inf(-1), wantCode: CodeRangeViolation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(map[string]any{"score": tt.value})
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid && !r.HasCode(tt.wantCode) {
				t.Errorf("expected %s, got %v", tt.wantCode, r.Errors)
			}
		})
	}
}

// TestInt64PrecisionRange verifies exact int64 comparisons around the 2^53
// float64 precision boundary and CUE fallback for unrepresentable schema bounds.
func TestInt64PrecisionRange(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		value     int64
		wantValid bool
	}{
		{
			name:      "value_one_above_2pow53_rejected_by_inclusive_max",
			schema:    `{ id: int & <=9007199254740992 }`,
			value:     9007199254740993,
			wantValid: false,
		},
		{
			name:      "value_one_above_2pow53_passes_exclusive_min",
			schema:    `{ id: int & >9007199254740992 }`,
			value:     9007199254740993,
			wantValid: true,
		},
		{
			name:      "bound_above_int64_falls_back_to_cue",
			schema:    `{ id: int & <9223372036854775808 }`,
			value:     math.MaxInt64,
			wantValid: true,
		},
		{
			name:      "max_int64_inclusive",
			schema:    `{ id: int & <=9223372036854775807 }`,
			value:     math.MaxInt64,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := MustNew(tt.schema)
			r := v.Process(map[string]any{"id": tt.value})
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
		})
	}
}

func TestSignedIntegerEnumTypes(t *testing.T) {
	v := MustNew(`{ n: 1 | 2 | 3 }`)
	tests := []struct {
		name  string
		value any
	}{
		{name: "int", value: int(2)},
		{name: "int8", value: int8(2)},
		{name: "int16", value: int16(2)},
		{name: "int32", value: int32(2)},
		{name: "int64", value: int64(2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(map[string]any{"n": tt.value})
			if !r.Valid {
				t.Fatalf("expected %T value to match integer enum, got %v", tt.value, r.Errors)
			}
		})
	}
}

// TestFloatEnumFastpath verifies that float enums work via the fast path.
func TestFloatEnumFastpath(t *testing.T) {
	v := MustNew(`{ rate: 0.5 | 1.0 | 1.5 | 2.0 }`)

	tests := []struct {
		name      string
		data      map[string]any
		wantValid bool
	}{
		{"valid_0.5", map[string]any{"rate": float64(0.5)}, true},
		{"valid_1.0", map[string]any{"rate": float64(1.0)}, true},
		{"valid_2.0", map[string]any{"rate": float64(2.0)}, true},
		{"invalid_0.7", map[string]any{"rate": float64(0.7)}, false},
		{"invalid_3.0", map[string]any{"rate": float64(3.0)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(tt.data)
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
		})
	}
}

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

func TestUnsignedIntegerFallsBackToCUE(t *testing.T) {
	v := MustNew(`{ n: int & >0 }`)
	tests := []struct {
		name      string
		value     any
		wantValid bool
		wantCode  ErrorCode
	}{
		{name: "uint32_positive", value: uint32(42), wantValid: true},
		{name: "uint64_max", value: uint64(math.MaxUint64), wantValid: true},
		{name: "uint64_zero", value: uint64(0), wantCode: CodeRangeViolation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := v.Process(map[string]any{"n": tt.value})
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid && !r.HasCode(tt.wantCode) {
				t.Fatalf("expected %s, got %v", tt.wantCode, r.Errors)
			}
		})
	}
}

func TestNumericFastpathTypes(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		value     any
		wantValid bool
		wantCode  ErrorCode
	}{
		{name: "pure_number_float64", schema: `{ n: number }`, value: float64(1.5), wantValid: true},
		{name: "pure_number_nan", schema: `{ n: number }`, value: math.NaN(), wantCode: CodeTypeMismatch},
		{name: "pure_number_inf", schema: `{ n: number }`, value: math.Inf(1), wantCode: CodeRangeViolation},
		{name: "pure_float_negative_inf", schema: `{ n: float }`, value: math.Inf(-1), wantCode: CodeRangeViolation},
		{name: "number_range_int", schema: `{ n: number & >=0 & <=10 }`, value: int(5), wantValid: true},
		{name: "number_range_int8", schema: `{ n: number & >=0 & <=10 }`, value: int8(5), wantValid: true},
		{name: "number_range_int16", schema: `{ n: number & >=0 & <=10 }`, value: int16(5), wantValid: true},
		{name: "number_range_int32", schema: `{ n: number & >=0 & <=10 }`, value: int32(5), wantValid: true},
		{name: "number_range_int64", schema: `{ n: number & >=0 & <=10 }`, value: int64(5), wantValid: true},
		{name: "number_range_float32", schema: `{ n: number & >=0 & <=10 }`, value: float32(5.5), wantValid: true},
		{name: "number_range_float64", schema: `{ n: number & >=0 & <=10 }`, value: float64(5.5), wantValid: true},
		{name: "number_range_violation", schema: `{ n: number & >=0 & <=10 }`, value: float64(11), wantCode: CodeRangeViolation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := MustNew(tt.schema)
			r := v.Process(map[string]any{"n": tt.value})
			if r.Valid != tt.wantValid {
				t.Fatalf("Valid = %v, want %v; errors: %v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid && !r.HasCode(tt.wantCode) {
				t.Fatalf("expected %s, got %v", tt.wantCode, r.Errors)
			}
		})
	}
}

// =============================================================================
// Nil / nullable / optional handling tests
// =============================================================================

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

// =============================================================================
// Blob output & type contract tests
// =============================================================================

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

// TestOutputNilOnInvalid verifies the all-or-nothing Output contract for every FailMode.
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
