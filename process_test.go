package schemix

import "testing"

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
