package schemix

import (
	"strings"
	"testing"
)

// TestConditionalImpliesOptional locks in the behavior that @meta(conditional)
// implies @meta(optional) — see parsefieldMeta in compile.go, which sets
// meta.Optional = true whenever the conditional flag is present.
//
// The consequence is that processInternal's `meta.Optional && fieldVal == nil`
// branch handles every conditional field, and the later
// `meta.Conditional && fieldVal == nil` branch is unreachable. These tests pin
// the observable behavior so that removing the unreachable branch is provably
// safe.
func TestConditionalImpliesOptional(t *testing.T) {
	const condSchema = `{
		payment_type: "credit" | "debit"
		cvv?: string @meta(conditional, required_if=this.payment_type == "credit")
	}`
	const optSchema = `{
		payment_type: "credit" | "debit"
		cvv?: string @meta(optional, required_if=this.payment_type == "credit")
	}`

	cond := MustNew(condSchema)
	opt := MustNew(optSchema)

	cases := []struct {
		name      string
		data      map[string]any
		wantValid bool
		wantCode  ErrorCode
		wantPath  string
	}{
		{
			name:      "required_if true and field absent -> E3C01",
			data:      map[string]any{"payment_type": "credit"},
			wantValid: false,
			wantCode:  CodeCondRequired,
			wantPath:  "cvv",
		},
		{
			name:      "required_if false and field absent -> valid",
			data:      map[string]any{"payment_type": "debit"},
			wantValid: true,
		},
		{
			name:      "field present -> valid regardless",
			data:      map[string]any{"payment_type": "credit", "cvv": "123"},
			wantValid: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := cond.Process(tc.data)
			if r.Valid != tc.wantValid {
				t.Fatalf("conditional: Valid = %v, want %v (errors: %v)", r.Valid, tc.wantValid, r.Errors)
			}
			if !tc.wantValid {
				if !r.HasCode(tc.wantCode) {
					t.Errorf("conditional: want code %s, got %v", tc.wantCode, r.Errors)
				}
				if got := r.ErrorsByPath(tc.wantPath); len(got) == 0 {
					t.Errorf("conditional: want error at path %q, got %v", tc.wantPath, r.Errors)
				}
			}

			// The optional-flagged schema must behave identically: this is what
			// makes the unreachable conditional branch redundant rather than
			// merely untested.
			ro := opt.Process(tc.data)
			if ro.Valid != r.Valid {
				t.Fatalf("optional vs conditional: Valid %v != %v", ro.Valid, r.Valid)
			}
			if len(ro.Errors) != len(r.Errors) {
				t.Fatalf("optional vs conditional: error count %d != %d\n opt=%v\ncond=%v",
					len(ro.Errors), len(r.Errors), ro.Errors, r.Errors)
			}
			for i := range r.Errors {
				if ro.Errors[i].Code != r.Errors[i].Code || ro.Errors[i].Path != r.Errors[i].Path {
					t.Errorf("optional vs conditional: error[%d] %s@%s != %s@%s", i,
						ro.Errors[i].Code, ro.Errors[i].Path, r.Errors[i].Code, r.Errors[i].Path)
				}
			}
		})
	}
}

// TestConditionalRequiredIfRuntimeError pins the E3X01 path: a required_if
// expression that fails at runtime must surface CodeMetaRuntimeError, and must
// do so identically for conditional and optional fields.
func TestConditionalRequiredIfRuntimeError(t *testing.T) {
	cond := MustNew(`{ x?: string @meta(conditional, required_if=this.a > 0) }`)
	opt := MustNew(`{ x?: string @meta(optional, required_if=this.a > 0) }`)

	// "a" is absent, so `this.a > 0` fails to evaluate.
	data := map[string]any{}

	rc := cond.Process(data)
	ro := opt.Process(data)

	if rc.Valid {
		t.Fatalf("conditional: expected invalid, got valid")
	}
	if !rc.HasCode(CodeMetaRuntimeError) {
		t.Errorf("conditional: want %s, got %v", CodeMetaRuntimeError, rc.Errors)
	}
	if ro.Valid != rc.Valid || len(ro.Errors) != len(rc.Errors) {
		t.Fatalf("optional vs conditional diverged: opt=%v cond=%v", ro.Errors, rc.Errors)
	}
	if ro.Errors[0].Code != rc.Errors[0].Code {
		t.Errorf("optional vs conditional: %s != %s", ro.Errors[0].Code, rc.Errors[0].Code)
	}

	// FailFast must return immediately from inside the required_if error path.
	fast := cond.ProcessWithMode(data, FailFast)
	if fast.Valid {
		t.Fatal("FailFast: expected invalid")
	}
	if len(fast.Errors) != 1 {
		t.Fatalf("FailFast: want exactly 1 error, got %d: %v", len(fast.Errors), fast.Errors)
	}
	if fast.Errors[0].Code != CodeMetaRuntimeError {
		t.Errorf("FailFast: want %s, got %s", CodeMetaRuntimeError, fast.Errors[0].Code)
	}
}

// TestConditionalFailFast pins the FailFast early-return inside the
// optional/required_if branch, which the previous test suite never exercised
// (covermode=count showed the two `if mode == FailFast` blocks at 0).
func TestConditionalFailFast(t *testing.T) {
	v := MustNew(`{
		payment_type: "credit" | "debit"
		cvv?:  string @meta(conditional, required_if=this.payment_type == "credit", priority=1)
		memo?: string @meta(conditional, required_if=this.payment_type == "credit", priority=2)
	}`)

	data := map[string]any{"payment_type": "credit"}

	all := v.ProcessWithMode(data, FailAll)
	if len(all.Errors) != 2 {
		t.Fatalf("FailAll: want 2 errors (cvv, memo), got %d: %v", len(all.Errors), all.Errors)
	}

	fast := v.ProcessWithMode(data, FailFast)
	if len(fast.Errors) != 1 {
		t.Fatalf("FailFast: want exactly 1 error, got %d: %v", len(fast.Errors), fast.Errors)
	}
	if fast.Errors[0].Code != CodeCondRequired {
		t.Errorf("FailFast: want %s, got %s", CodeCondRequired, fast.Errors[0].Code)
	}
}

// TestMetaOptionalOverridesCUERequiredness pins a non-obvious compile-time
// behavior: @meta(optional) and @meta(conditional) mark the field
// absent-tolerant at the CUE layer too, even when the CUE syntax declares it
// required (no `?`). compileCUEFields scans the @meta attribute and sets
// cueField.optional accordingly.
//
// The consequence is that `cvv: string @meta(conditional, required_if=…)` and
// `cvv?: string @meta(conditional, required_if=…)` behave identically: CUE does
// not report E1M01, so required_if runs and reports E3C01. Writing the `?` is
// preferred for clarity, not required for correctness.
func TestMetaOptionalOverridesCUERequiredness(t *testing.T) {
	data := map[string]any{"payment_type": "credit"} // cvv absent

	schemas := map[string]string{
		"cue-required + @meta(conditional)": `{
			payment_type: "credit" | "debit"
			cvv: string @meta(conditional, required_if=this.payment_type == "credit")
		}`,
		"cue-optional + @meta(conditional)": `{
			payment_type: "credit" | "debit"
			cvv?: string @meta(conditional, required_if=this.payment_type == "credit")
		}`,
		"cue-required + @meta(optional)": `{
			payment_type: "credit" | "debit"
			cvv: string @meta(optional, required_if=this.payment_type == "credit")
		}`,
	}

	for name, src := range schemas {
		t.Run(name, func(t *testing.T) {
			r := MustNew(src).ProcessWithMode(data, FailAll)

			if r.Valid {
				t.Fatal("expected invalid")
			}
			if !r.HasCode(CodeCondRequired) {
				t.Errorf("want %s from required_if, got %v", CodeCondRequired, r.Errors)
			}
			// The point of the test: the CUE layer must NOT have short-circuited
			// with a required-missing error, or required_if would never run.
			if r.HasCode(CodeRequiredMissing) {
				t.Errorf("did not expect %s: @meta(optional|conditional) should make "+
					"the field absent-tolerant at the CUE layer; got %v",
					CodeRequiredMissing, r.Errors)
			}
			if len(r.Errors) != 1 {
				t.Errorf("want exactly 1 error, got %d: %v", len(r.Errors), r.Errors)
			}
		})
	}
}

// A field with no @meta escape hatch still reports E1M01 when missing, which is
// the contrast that makes the override above meaningful.
func TestPlainRequiredFieldReportsMissing(t *testing.T) {
	v := MustNew(`{
		payment_type: "credit" | "debit"
		cvv: string
	}`)

	r := v.ProcessWithMode(map[string]any{"payment_type": "credit"}, FailAll)
	if !r.HasCode(CodeRequiredMissing) {
		t.Errorf("want %s, got %v", CodeRequiredMissing, r.Errors)
	}
}

// TestConditionalOmitEmpty pins the `meta.OmitEmpty && result.Output != nil`
// delete inside the optional branch, also previously uncovered.
func TestConditionalOmitEmpty(t *testing.T) {
	v := MustNew(`{
		name:  string
		memo?: string @meta(optional, omit_empty)
	}`)

	r := v.Process(map[string]any{"name": "x"})
	if !r.Valid {
		t.Fatalf("expected valid, got %v", r.Errors)
	}
	if _, present := r.Output["memo"]; present {
		t.Errorf("omit_empty: memo should be absent from Output, got %#v", r.Output)
	}
}

// TestValidate_MetaConditionalRequired verifies @meta conditional rules work
// correctly through the Validate() path.
func TestValidate_MetaConditionalRequired(t *testing.T) {
	v := MustNew(`{
		payment_type: "credit" | "debit"
		cvv?: string @meta(conditional, required_if=this.payment_type == "credit")
	}`)

	t.Run("conditional required triggered", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"payment_type": "credit",
			// cvv is missing — should fail
		})
		if valid {
			t.Errorf("expected valid=false when cvv missing for credit")
		}
		found := false
		for _, e := range errs {
			if e.Path == "cvv" && e.Code == CodeCondRequired {
				found = true
			}
		}
		if !found {
			t.Errorf("expected CodeCondRequired for cvv, got: %v", errs)
		}
	})

	t.Run("conditional not triggered", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"payment_type": "debit",
			// cvv is missing — OK for debit
		})
		if !valid {
			t.Errorf("expected valid=true for debit without cvv, errors: %v", errs)
		}
	})
}

// ========== Result Chain API ==========

func TestProcess_PreservesInt64InOutput(t *testing.T) {
	v := MustNew(`{
		amount: int & >0
		doubled: number @blob(this.amount * 2)
	}`)
	r := v.Process(map[string]any{"amount": int64(500)})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}
	// Original field should still be int64
	if _, ok := r.Output["amount"].(int64); !ok {
		t.Errorf("expected output.amount to be int64, got %T", r.Output["amount"])
	}
}

// ========== deepCopy ==========

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

// ─── Lazy CUE Encode ────────────────────────────────────────────────────────
//
// cue.Context.Encode converts the whole input map into a cue.Value and costs
// ~1.67µs / 39 allocations. When every field is served by the Go-native fast
// path, that value is never read, so the encode must not happen at all.
//
// These tests pin both halves of the contract: the encode is skipped when it is
// provably unnecessary, and validation still agrees with CUE when it is needed.
