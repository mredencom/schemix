package schemix

import (
	"math"
	"testing"
)

// TestOracle_FastPathVsCUE verifies that the fast path produces results
// identical to the CUE oracle for a comprehensive corpus of (schema, value) pairs.
//
// Oracle contract:
//   - For finite values, fast path and forced-CUE validation MUST agree.
//   - Fast-only guards required by the spec may reject NaN, Inf, or floats on int fields.
func TestOracle_FastPathVsCUE(t *testing.T) {
	type testCase struct {
		name   string
		schema string
		input  map[string]any
	}

	cases := []testCase{
		// Integer types
		{"int64/valid", `{ n: int & >=0 & <=100 }`, map[string]any{"n": int64(50)}},
		{"int64/boundary-low", `{ n: int & >=0 & <=100 }`, map[string]any{"n": int64(0)}},
		{"int64/boundary-high", `{ n: int & >=0 & <=100 }`, map[string]any{"n": int64(100)}},
		{"int64/below-range", `{ n: int & >=0 & <=100 }`, map[string]any{"n": int64(-1)}},
		{"int64/above-range", `{ n: int & >=0 & <=100 }`, map[string]any{"n": int64(101)}},
		{"int64/large", `{ n: int & >0 }`, map[string]any{"n": int64(9007199254740993)}},
		{"int64/max", `{ n: int & >0 }`, map[string]any{"n": int64(math.MaxInt64)}},
		{"int64/min", `{ n: int & <0 }`, map[string]any{"n": int64(math.MinInt64)}},

		// Int subtype values
		{"int32/valid", `{ n: int & >=0 }`, map[string]any{"n": int32(42)}},
		{"int16/valid", `{ n: int & >=0 }`, map[string]any{"n": int16(100)}},
		{"int8/valid", `{ n: int & >=-128 & <=127 }`, map[string]any{"n": int8(127)}},
		{"int8/out-of-range", `{ n: int & >=0 & <=50 }`, map[string]any{"n": int8(100)}},

		// Unsigned integers (fast path falls back to CUE)
		{"uint64/max", `{ n: int & >0 }`, map[string]any{"n": uint64(math.MaxUint64)}},
		{"uint32/valid", `{ n: int & >=0 }`, map[string]any{"n": uint32(42)}},
		{"uint16/valid", `{ n: int & >=0 }`, map[string]any{"n": uint16(1000)}},
		{"uint8/valid", `{ n: int & >=0 & <=255 }`, map[string]any{"n": uint8(200)}},

		// Float on int (rejected by fast path)
		{"float64-on-int/reject", `{ n: int & >=0 }`, map[string]any{"n": float64(25.0)}},
		{"float32-on-int/reject", `{ n: int & >=0 }`, map[string]any{"n": float32(25.0)}},

		// Float fields
		{"float64/valid", `{ r: float & >=0.0 & <=1.0 }`, map[string]any{"r": 0.5}},
		{"float64/boundary-low", `{ r: float & >=0.0 & <=1.0 }`, map[string]any{"r": 0.0}},
		{"float64/boundary-high", `{ r: float & >=0.0 & <=1.0 }`, map[string]any{"r": 1.0}},
		{"float64/below", `{ r: float & >=0.0 & <=1.0 }`, map[string]any{"r": -0.1}},
		{"float64/above", `{ r: float & >=0.0 & <=1.0 }`, map[string]any{"r": 1.1}},

		// NaN/Inf — fast path rejects these as extra safety; CUE may accept (can't represent non-finite)
		{"nan/on-float", `{ r: float & >=0.0 }`, map[string]any{"r": math.NaN()}},
		{"inf/on-float", `{ r: float & >=0.0 }`, map[string]any{"r": math.Inf(1)}},
		{"neg-inf/on-float", `{ r: float & >=0.0 }`, map[string]any{"r": math.Inf(-1)}},
		{"nan/on-int", `{ n: int & >=0 }`, map[string]any{"n": math.NaN()}},

		// Number kind — KNOWN: extractNumericRange doesn't extract bounds for number kind
		{"number/int-valid", `{ n: number & >=0 }`, map[string]any{"n": int64(5)}},
		{"number/float-valid", `{ n: number & >=0 }`, map[string]any{"n": 5.5}},
		{"number/negative", `{ n: number & >=0 }`, map[string]any{"n": -1.0}},

		// Number type-only (no range)
		{"number-type/int", `{ n: number }`, map[string]any{"n": int64(5)}},
		{"number-type/float", `{ n: number }`, map[string]any{"n": 5.5}},
		{"number-type/string-mismatch", `{ n: number }`, map[string]any{"n": "five"}},

		// String
		{"string/valid", `{ s: string }`, map[string]any{"s": "hello"}},
		{"string/type-mismatch", `{ s: string }`, map[string]any{"s": int64(42)}},

		// Regex
		{"regex/match", `{ p: =~"^[0-9]{4}$" }`, map[string]any{"p": "1234"}},
		{"regex/no-match", `{ p: =~"^[0-9]{4}$" }`, map[string]any{"p": "abc"}},
		{"regex/type-mismatch", `{ p: =~"^[0-9]{4}$" }`, map[string]any{"p": int64(1234)}},

		// String enum
		{"string-enum/match", `{ c: "a" | "b" | "c" }`, map[string]any{"c": "b"}},
		{"string-enum/no-match", `{ c: "a" | "b" | "c" }`, map[string]any{"c": "d"}},

		// Int enum
		{"int-enum/match", `{ n: 1 | 2 | 3 }`, map[string]any{"n": int64(2)}},
		{"int-enum/no-match", `{ n: 1 | 2 | 3 }`, map[string]any{"n": int64(4)}},
		{"int-enum/int8", `{ n: 1 | 2 | 3 }`, map[string]any{"n": int8(2)}},
		{"int-enum/int16", `{ n: 1 | 2 | 3 }`, map[string]any{"n": int16(3)}},

		// Float enum
		{"float-enum/match", `{ r: 0.5 | 1.0 | 1.5 }`, map[string]any{"r": 1.0}},
		{"float-enum/no-match", `{ r: 0.5 | 1.0 | 1.5 }`, map[string]any{"r": 0.7}},

		// Bool
		{"bool/true", `{ b: bool }`, map[string]any{"b": true}},
		{"bool/false", `{ b: bool }`, map[string]any{"b": false}},
		{"bool/type-mismatch", `{ b: bool }`, map[string]any{"b": "yes"}},

		// Exclusive bounds
		{"excl-min/at-bound", `{ n: int & >0 & <100 }`, map[string]any{"n": int64(0)}},
		{"excl-min/above", `{ n: int & >0 & <100 }`, map[string]any{"n": int64(1)}},
		{"excl-max/at-bound", `{ n: int & >0 & <100 }`, map[string]any{"n": int64(100)}},
		{"excl-max/below", `{ n: int & >0 & <100 }`, map[string]any{"n": int64(99)}},
	}

	fastOnlyGuards := map[string]bool{
		"float64-on-int/reject": true,
		"float32-on-int/reject": true,
		"nan/on-float":          true,
		"inf/on-float":          true,
		"neg-inf/on-float":      true,
		"nan/on-int":            true,
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := MustNew(tc.schema)
			fastResult := v.Process(tc.input)
			cueResult := processCUEOnly(t, tc.schema, tc.input)

			mismatch := fastResult.Valid != cueResult.Valid
			allowedFastReject := fastOnlyGuards[tc.name] && !fastResult.Valid && cueResult.Valid
			if mismatch && !allowedFastReject {
				t.Fatalf("oracle validity mismatch: fast=%v cue=%v; fast errors=%v; cue errors=%v",
					fastResult.Valid, cueResult.Valid, fastResult.Errors, cueResult.Errors)
			}

			if !fastResult.Valid && !cueResult.Valid && len(fastResult.Errors) > 0 && len(cueResult.Errors) > 0 {
				if fastResult.Errors[0].Path != cueResult.Errors[0].Path {
					t.Errorf("oracle path mismatch: fast=%q, cue=%q",
						fastResult.Errors[0].Path, cueResult.Errors[0].Path)
				}
			}
		})
	}
}

// processCUEOnly runs validation purely through CUE (no fast path).
func processCUEOnly(t *testing.T, schema string, input map[string]any) Result {
	t.Helper()
	v := MustNew(schema)
	disableFastPath(v.cueFields)
	return v.Process(input)
}

// disableFastPath recursively sets fast=nil on all cueFields.
func disableFastPath(fields []cueField) {
	for i := range fields {
		fields[i].fast = nil
		if len(fields[i].children) > 0 {
			disableFastPath(fields[i].children)
		}
	}
}
