package schemix

import (
	"cmp"
	"math"
	"testing"
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
			handled, valid, code, _ := validateFast(tt.constraint, tt.val)
			if handled != tt.wantHandled {
				t.Errorf("handled = %v, want %v", handled, tt.wantHandled)
			}
			if valid != tt.wantValid {
				t.Errorf("valid = %v, want %v", valid, tt.wantValid)
			}
			if tt.wantCode != "" && code != tt.wantCode {
				t.Errorf("code = %v, want %v", code, tt.wantCode)
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
