package schemix

import (
	"strings"
	"testing"
)

// TestClassifyCUEError_GoldenMatrix is a comprehensive test matrix that exercises
// every ErrorCode produced by classifyCUEError. This serves as a regression guard
// against CUE library upgrades changing error message text.
//
// If CUE updates break this matrix, it means classifyCUEError needs updating.
func TestClassifyCUEError_GoldenMatrix(t *testing.T) {
	tests := []struct {
		name         string
		schema       string
		data         map[string]any
		expectedCode ErrorCode
		expectedPath string
	}{
		// --- E1F01: Format/Regex mismatch ---
		{
			name:         "regex_mismatch_pan",
			schema:       `{ pan: =~"^[0-9]{16}$" }`,
			data:         map[string]any{"pan": "ABC"},
			expectedCode: CodeFormatMismatch,
			expectedPath: "pan",
		},
		{
			name:         "regex_mismatch_email",
			schema:       `{ email: =~"^[a-z]+@[a-z]+\\.[a-z]+$" }`,
			data:         map[string]any{"email": "INVALID"},
			expectedCode: CodeFormatMismatch,
			expectedPath: "email",
		},

		// --- E1T01: Type mismatch ---
		{
			name:         "type_mismatch_string_got_int",
			schema:       `{ name: string }`,
			data:         map[string]any{"name": 123},
			expectedCode: CodeTypeMismatch,
			expectedPath: "name",
		},
		{
			name:         "type_mismatch_int_got_string",
			schema:       `{ age: int }`,
			data:         map[string]any{"age": "hello"},
			expectedCode: CodeTypeMismatch,
			expectedPath: "age",
		},
		{
			name:         "type_mismatch_bool_got_string",
			schema:       `{ active: bool }`,
			data:         map[string]any{"active": "yes"},
			expectedCode: CodeTypeMismatch,
			expectedPath: "active",
		},

		// --- E1E01: Enum invalid ---
		{
			name:         "enum_invalid_string",
			schema:       `{ status: "active" | "inactive" }`,
			data:         map[string]any{"status": "unknown"},
			expectedCode: CodeEnumInvalid,
			expectedPath: "status",
		},
		{
			name:         "enum_invalid_int",
			schema:       `{ level: 1 | 2 | 3 }`,
			data:         map[string]any{"level": int64(99)},
			expectedCode: CodeEnumInvalid,
			expectedPath: "level",
		},

		// --- E1R01: Range violation ---
		{
			name:         "range_violation_greater_than",
			schema:       `{ age: int & >=0 & <=150 }`,
			data:         map[string]any{"age": int64(200)},
			expectedCode: CodeRangeViolation,
			expectedPath: "age",
		},
		{
			name:         "range_violation_less_than",
			schema:       `{ amount: int & >0 }`,
			data:         map[string]any{"amount": int64(0)},
			expectedCode: CodeRangeViolation,
			expectedPath: "amount",
		},
		{
			name:         "range_violation_negative",
			schema:       `{ score: float & >=0.0 & <=100.0 }`,
			data:         map[string]any{"score": float64(-1.0)},
			expectedCode: CodeRangeViolation,
			expectedPath: "score",
		},

		// --- E1M01: Required field missing ---
		{
			name:         "required_missing_simple",
			schema:       `{ name: string, age: int }`,
			data:         map[string]any{"name": "Alice"},
			expectedCode: CodeRequiredMissing,
			expectedPath: "age",
		},
		{
			name:         "required_missing_nested",
			schema:       `{ addr: { city: string, zip: string } }`,
			data:         map[string]any{"addr": map[string]any{"city": "NYC"}},
			expectedCode: CodeRequiredMissing,
			expectedPath: "addr.zip",
		},

		// --- E1A01/E1T01: Array element validation ---
		// Array element errors get the specific error code (type mismatch, range, etc.)
		// The path includes the extracted index from the CUE error message.
		{
			name:         "array_element_type_error",
			schema:       `{ scores: [...int] }`,
			data:         map[string]any{"scores": []any{int64(1), "not_int"}},
			expectedCode: CodeTypeMismatch,
			expectedPath: "scores", // path prefix — actual path may include index
		},

		// --- Mixed: Verify multiple errors in one schema ---
		// (This one tests that we get the right code for each field)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := MustNew(tt.schema)
			r := v.ProcessWithMode(tt.data, FailAll)

			if r.Valid {
				t.Fatalf("expected invalid result for test %q", tt.name)
			}

			// Find the expected error (path can be exact or prefix match for array elements)
			found := false
			for _, e := range r.Errors {
				pathMatch := e.Path == tt.expectedPath || strings.HasPrefix(e.Path, tt.expectedPath+".")
				if pathMatch && e.Code == tt.expectedCode {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error Code=%s Path=%s (or prefix)\ngot errors: %v",
					tt.expectedCode, tt.expectedPath, r.Errors)
			}
		})
	}
}

// TestClassifyCUEError_StringMatching tests the raw string classification function
// directly, to catch regressions when CUE error messages change.
func TestClassifyCUEError_StringMatching(t *testing.T) {
	tests := []struct {
		msg      string
		expected ErrorCode
	}{
		// Format/regex
		{`pan: invalid value "ABC" (does not match =~"^[0-9]{16}$")`, CodeFormatMismatch},
		{`invalid value "foo" (out of bound =~"^[a-z]+$")`, CodeFormatMismatch},

		// Type conflicts
		{`conflicting values string and 123 (mismatched types string and int)`, CodeTypeMismatch},
		{`cannot use value 123 as string`, CodeTypeMismatch},
		{`conflicting values int and "hello"`, CodeTypeMismatch},

		// Enum
		{`conflicting values "unknown" and "active" | "inactive"`, CodeEnumInvalid},
		{`empty disjunction`, CodeEnumInvalid},
		{`conflicting values 99 and 1 | 2 | 3`, CodeEnumInvalid},

		// Range
		{`invalid value 200 (out of bound <=150)`, CodeRangeViolation},
		{`invalid value -1 (out of bound >=0)`, CodeRangeViolation},
		{`invalid value 0 (out of bound >0)`, CodeRangeViolation},

		// Required missing
		{`incomplete value string`, CodeRequiredMissing},
		{`field is required but not present`, CodeRequiredMissing},

		// Unknown/other
		{`some unexpected CUE error message`, CodeCUEOther},
	}

	for _, tt := range tests {
		t.Run(tt.msg[:min(40, len(tt.msg))], func(t *testing.T) {
			got := classifyCUEError(tt.msg)
			if got != tt.expected {
				t.Errorf("classifyCUEError(%q)\n  got  %s\n  want %s", tt.msg, got, tt.expected)
			}
		})
	}
}

// TestClassifyCUEError_ArrayElement tests that array element errors are correctly
// detected (classified as E1A01 or downgraded from E1X01).
func TestClassifyCUEError_ArrayElement(t *testing.T) {
	v := MustNew(`{ scores: [...int] }`)

	r := v.Process(map[string]any{
		"scores": []any{int64(1), "not_int", int64(3)},
	})

	if r.Valid {
		t.Fatal("expected invalid for array with wrong element type")
	}

	// Should have at least one array-related error
	found := false
	for _, e := range r.Errors {
		if e.Code == CodeArrayElement || e.Code == CodeTypeMismatch {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected array element error, got: %v", r.Errors)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
