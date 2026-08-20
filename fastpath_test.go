package schemix

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

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

// ─── Lists of scalar elements ───────────────────────────────────────────────
//
// A list has no fast descriptor, so the whole value goes to cue.Value.Unify:
// measured 6367 ns and 116 allocations for a single-element [...string], against
// 54 ns and zero allocations for three scalar fields. When every element is a
// scalar the descriptor already knows how to check, that cost buys nothing.

// TestListFastpath_ServesScalarElementLists asserts the descriptor is extracted
// for an open list of scalars and that it handles the value, which is what lets
// the encode be skipped.
func TestListFastpath_ServesScalarElementLists(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		field  string
		input  map[string]any
	}{
		{"strings", `{ tags: [...string] }`, "tags", map[string]any{"tags": []any{"a", "b"}}},
		{"empty", `{ tags: [...string] }`, "tags", map[string]any{"tags": []any{}}},
		{"ints-with-range", `{ n: [...int & >0] }`, "n", map[string]any{"n": []any{int64(1), int64(2)}}},
		{"floats", `{ r: [...float & <=1.0] }`, "r", map[string]any{"r": []any{0.25, 0.5}}},
		{"regex", `{ p: [...=~"^[0-9]{2}$"] }`, "p", map[string]any{"p": []any{"12"}}},
		{"enum", `{ c: [..."a" | "b"] }`, "c", map[string]any{"c": []any{"a"}}},
		{"bools", `{ b: [...bool] }`, "b", map[string]any{"b": []any{true, false}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeRecorder{}
			v := MustNew(tc.schema, WithMetricsRecorder(rec))

			r := v.Process(tc.input)
			if !r.Valid {
				t.Fatalf("expected valid input to pass, got errors=%v", r.Errors)
			}

			rec.mu.Lock()
			calls := append([]fastpathObservation(nil), rec.fastpathCalls...)
			rec.mu.Unlock()

			var served bool
			for _, c := range calls {
				if c.fieldPath == tc.field {
					if !c.hit {
						t.Fatalf("field %q held a descriptor but did not handle the value", tc.field)
					}
					served = true
				}
			}
			if !served {
				t.Fatalf("field %q has no fast descriptor; a list of scalars must not "+
					"need cue.Value.Unify", tc.field)
			}
		})
	}
}

// TestListFastpath_SkipsEncode pins the payoff. cue.Context.Encode alone costs
// 39 allocations, so staying below it proves no cue.Value was built.
func TestListFastpath_SkipsEncode(t *testing.T) {
	v := MustNew(`{ tags: [...string] }`)
	data := map[string]any{"tags": []any{"alpha", "beta", "gamma"}}

	if ok, errs := v.Validate(data); !ok {
		t.Fatalf("precondition failed, schema should accept data: %v", errs)
	}

	const encodeAllocs = 39
	got := testing.AllocsPerRun(200, func() {
		v.Validate(data)
	})

	if got >= encodeAllocs {
		t.Errorf("Validate allocated %.0f objects, want < %d — a list of scalar "+
			"elements still appears to build a cue.Value", got, encodeAllocs)
	}
}

// TestListFastpath_ReportsEveryOffendingElement pins the error contract against
// CUE: one error per rejected element, indexed, not just the first one.
func TestListFastpath_ReportsEveryOffendingElement(t *testing.T) {
	cases := []struct {
		name      string
		schema    string
		input     map[string]any
		wantPaths []string
		wantCode  ErrorCode
	}{
		{
			name:      "every element rejected",
			schema:    `{ tags: [...string] }`,
			input:     map[string]any{"tags": []any{int64(1), int64(2)}},
			wantPaths: []string{"tags[0]", "tags[1]"},
			wantCode:  CodeTypeMismatch,
		},
		{
			name:      "trailing elements rejected",
			schema:    `{ tags: [...string] }`,
			input:     map[string]any{"tags": []any{"ok", int64(2), int64(3)}},
			wantPaths: []string{"tags[1]", "tags[2]"},
			wantCode:  CodeTypeMismatch,
		},
		{
			name:      "range violated per element",
			schema:    `{ n: [...int & >0] }`,
			input:     map[string]any{"n": []any{int64(1), int64(-1), int64(-2)}},
			wantPaths: []string{"n[1]", "n[2]"},
			wantCode:  CodeRangeViolation,
		},
		{
			name:      "enum violated per element",
			schema:    `{ c: [..."a" | "b"] }`,
			input:     map[string]any{"c": []any{"z", "y"}},
			wantPaths: []string{"c[0]", "c[1]"},
			wantCode:  CodeEnumInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MustNew(tc.schema).ProcessWithMode(tc.input, FailAll)
			if got.Valid {
				t.Fatal("expected invalid")
			}

			paths := make([]string, 0, len(got.Errors))
			for _, e := range got.Errors {
				paths = append(paths, e.Path)
				if e.Code != tc.wantCode {
					t.Errorf("error at %s has code %s, want %s", e.Path, e.Code, tc.wantCode)
				}
				if e.Type != TypeCUE {
					t.Errorf("error at %s has type %q, want %q", e.Path, e.Type, TypeCUE)
				}
			}
			if !slices.Equal(paths, tc.wantPaths) {
				t.Errorf("error paths = %v, want %v", paths, tc.wantPaths)
			}

			// The same schema driven purely through CUE must agree on the paths.
			oracle := processCUEOnly(t, tc.schema, tc.input)
			oraclePaths := make([]string, 0, len(oracle.Errors))
			for _, e := range oracle.Errors {
				oraclePaths = append(oraclePaths, e.Path)
			}
			if !slices.Equal(paths, oraclePaths) {
				t.Errorf("fast path reported %v, CUE oracle reported %v", paths, oraclePaths)
			}
		})
	}
}

// TestListFastpath_RefusesShapesItCannotRepresent keeps the descriptor away from
// lists whose verdict does not reduce to a per-element scalar check.
func TestListFastpath_RefusesShapesItCannotRepresent(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		field  string
		input  map[string]any
	}{
		{"list-level conjunct", "import \"list\"\n{ tags: [...string] & list.MinItems(2) }", "tags",
			map[string]any{"tags": []any{"a", "b"}}},
		{"fixed-arity tuple", `{ p: [string, string] }`, "p",
			map[string]any{"p": []any{"a", "b"}}},
		{"fixed head, open tail", `{ p: [string, ...int] }`, "p",
			map[string]any{"p": []any{"a", int64(1)}}},
		{"disjunction of list types", `{ tags: [...string] | [...int] }`, "tags",
			map[string]any{"tags": []any{int64(1)}}},
		{"struct elements", `{ items: [...{qty: int}] }`, "items",
			map[string]any{"items": []any{map[string]any{"qty": int64(1)}}}},
		{"nested lists", `{ m: [...[...string]] }`, "m",
			map[string]any{"m": []any{[]any{"a"}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeRecorder{}
			v := MustNew(tc.schema, WithMetricsRecorder(rec))

			r := v.Process(tc.input)
			if !r.Valid {
				t.Fatalf("precondition failed, schema should accept data: %v", r.Errors)
			}

			rec.mu.Lock()
			calls := append([]fastpathObservation(nil), rec.fastpathCalls...)
			rec.mu.Unlock()

			for _, c := range calls {
				if c.fieldPath == tc.field && c.hit {
					t.Fatalf("field %q was served by the fast path, but %s does not "+
						"reduce to a per-element scalar check", tc.field, tc.schema)
				}
			}
		})
	}
}

// TestListFastpath_FallsBackWhenAnElementIsUnrepresentable covers a value the
// element descriptor cannot decide. Unsigned integers make the scalar check
// return Handled=false, and a list must then hand the whole value to CUE rather
// than accept the elements it happens to understand.
func TestListFastpath_FallsBackWhenAnElementIsUnrepresentable(t *testing.T) {
	const schema = `{ n: [...int & >0] }`
	input := map[string]any{"n": []any{int64(1), uint64(2), int64(-3)}}

	got := MustNew(schema).ProcessWithMode(input, FailAll)
	want := processCUEOnly(t, schema, input)

	if got.Valid != want.Valid {
		t.Fatalf("Valid = %v, want %v (CUE oracle); got errors=%v, oracle errors=%v",
			got.Valid, want.Valid, got.Errors, want.Errors)
	}
	gotPaths := make([]string, 0, len(got.Errors))
	for _, e := range got.Errors {
		gotPaths = append(gotPaths, e.Path)
	}
	wantPaths := make([]string, 0, len(want.Errors))
	for _, e := range want.Errors {
		wantPaths = append(wantPaths, e.Path)
	}
	if !slices.Equal(gotPaths, wantPaths) {
		t.Errorf("error paths = %v, want %v (CUE oracle)", gotPaths, wantPaths)
	}
}

// ─── Enum lookup strategy ───────────────────────────────────────────────────

// TestEnumLookup_LargeEnumsUseHashLookup asserts the lookup structure directly,
// because that structure *is* the behaviour under test here and a wall-clock
// assertion in a unit test would be flaky. The benchmark gate in CI covers the
// timing side.
//
// Measured with the value already boxed in an any and the hit at the average
// position, a scan costs 3.1 ns at n=3 but 158 ns at n=180, while a hash lookup
// stays near 5 ns. Below the threshold the scan wins, so small enums — the
// common case — must keep it.
func TestEnumLookup_LargeEnumsUseHashLookup(t *testing.T) {
	build := func(n int) string {
		alts := make([]string, n)
		for i := range alts {
			alts[i] = fmt.Sprintf("%q", fmt.Sprintf("code_%03d", i))
		}
		return "{ c: " + strings.Join(alts, " | ") + " }"
	}

	t.Run("small enum keeps the scan", func(t *testing.T) {
		v := MustNew(build(enumSetThreshold - 1))
		fc := v.cueFields[0].fast
		if fc == nil || fc.kind != constraintEnum {
			t.Fatalf("expected an enum descriptor, got %+v", fc)
		}
		if fc.stringEnumSet != nil {
			t.Errorf("built a hash set for %d candidates; a scan is faster below %d",
				enumSetThreshold-1, enumSetThreshold)
		}
	})

	t.Run("large enum uses a set", func(t *testing.T) {
		const n = 60
		v := MustNew(build(n))
		fc := v.cueFields[0].fast
		if fc == nil || fc.kind != constraintEnum {
			t.Fatalf("expected an enum descriptor, got %+v", fc)
		}
		if fc.stringEnumSet == nil {
			t.Fatalf("still scanning %d candidates linearly", n)
		}
		if len(fc.stringEnumSet) != n {
			t.Errorf("set holds %d candidates, want %d", len(fc.stringEnumSet), n)
		}
	})

	t.Run("large int enum uses a set", func(t *testing.T) {
		const n = 60
		alts := make([]string, n)
		for i := range alts {
			alts[i] = strconv.Itoa(i)
		}
		v := MustNew("{ n: " + strings.Join(alts, " | ") + " }")
		fc := v.cueFields[0].fast
		if fc == nil || fc.kind != constraintEnum {
			t.Fatalf("expected an enum descriptor, got %+v", fc)
		}
		if fc.intEnumSet == nil {
			t.Fatalf("still scanning %d int candidates linearly", n)
		}
	})
}

// TestEnumLookup_StrategyDoesNotChangeBehaviour pins what must hold whichever
// lookup a schema ends up with. The listing order matters most: a map has none,
// so the candidate list in the message must keep coming from the ordered slice.
func TestEnumLookup_StrategyDoesNotChangeBehaviour(t *testing.T) {
	// Straddle the threshold so one schema scans and the other hashes.
	for _, n := range []int{3, 60} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			alts := make([]string, n)
			want := make([]string, n)
			for i := range alts {
				want[i] = fmt.Sprintf("code_%03d", i)
				alts[i] = fmt.Sprintf("%q", want[i])
			}
			schema := "{ c: " + strings.Join(alts, " | ") + " }"
			v := MustNew(schema)

			// Every candidate is accepted, wherever it sits in the list.
			for _, candidate := range want {
				if ok, errs := v.Validate(map[string]any{"c": candidate}); !ok {
					t.Fatalf("candidate %q rejected: %v", candidate, errs)
				}
			}

			// A non-candidate is rejected, and the message lists every candidate
			// in declaration order.
			r := v.Process(map[string]any{"c": "code_999"})
			if r.Valid {
				t.Fatal("expected a non-candidate to be rejected")
			}
			if len(r.Errors) != 1 {
				t.Fatalf("want exactly 1 error, got %d: %v", len(r.Errors), r.Errors)
			}
			e := r.Errors[0]
			if e.Code != CodeEnumInvalid {
				t.Errorf("code = %s, want %s", e.Code, CodeEnumInvalid)
			}
			if got := stringEnumDetail("code_999", want); e.Message != got {
				t.Errorf("message = %q, want %q — the candidate list must come from "+
					"the ordered slice, not from map iteration", e.Message, got)
			}

			// Suggestion still finds the closest candidate.
			r2 := v.Process(map[string]any{"c": "code_00"})
			if len(r2.Errors) != 1 || r2.Errors[0].Suggestion == "" {
				t.Errorf("expected a suggestion for a near-miss, got %v", r2.Errors)
			}

			// A wrong type is still a type error, not an enum error.
			r3 := v.Process(map[string]any{"c": int64(1)})
			if len(r3.Errors) != 1 || r3.Errors[0].Code != CodeTypeMismatch {
				t.Errorf("want a single %s, got %v", CodeTypeMismatch, r3.Errors)
			}
		})
	}
}

// TestFastResultSizeUnchanged locks the struct that carries every scalar field's
// verdict at 56 bytes.
//
// The comment on fastResult records a measurement: adding a slice header here
// grew it to 80 bytes and cost 6% on the scalar fast path, because the struct is
// returned by value for every scalar field of every validation — a cost paid
// even by schemas containing no list at all. 6% also exceeds the 5% benchmark
// gate.
//
// This matters because it is a tempting place to put things. Enum candidates and
// range bounds now reach ValidationError, and routing them through fastResult
// would look like the direct way to do it. It is not: the consumer already holds
// the *fastConstraint via cueField.fast, so the data is read where the error is
// built instead. If this test fails, that reasoning was undone — see
// docs/design-i18n-localizer.md §3.8 before changing the expected size.
func TestFastResultSizeUnchanged(t *testing.T) {
	const want = 56
	if got := unsafe.Sizeof(fastResult{}); got != want {
		t.Errorf("unsafe.Sizeof(fastResult{}) = %d, want %d — "+
			"a field was added to the scalar hot path; carry the data on cueField.fast instead", got, want)
	}
}
