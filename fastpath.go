package schemix

import (
	"fmt"
	"regexp"

	"cuelang.org/go/cue"
)

// constraintKind indicates what type of Go-native fast check to perform.
type constraintKind int

const (
	constraintNone  constraintKind = iota // no fast path — use CUE
	constraintType                        // pure type assertion
	constraintRegex                       // string + regex match
	constraintRange                       // numeric range bounds
	constraintEnum                        // string/int enum set
)

// fastConstraint holds pre-extracted Go-native constraint data for a single field.
// When populated, the field can be validated without CUE Encode+Unify.
type fastConstraint struct {
	kind constraintKind

	// Type constraint: expected Go kind
	expectString bool
	expectInt    bool
	expectFloat  bool
	expectBool   bool
	expectNumber bool // int or float

	// Regex constraint (implies expectString)
	regex *regexp.Regexp

	// Range constraint (implies numeric)
	hasMin    bool
	hasMax    bool
	min       float64 // inclusive lower bound (or exclusive, see minExcl)
	max       float64 // inclusive upper bound (or exclusive, see maxExcl)
	minExcl   bool    // true = > (exclusive), false = >= (inclusive)
	maxExcl   bool    // true = < (exclusive), false = <= (inclusive)

	// Enum constraint
	stringEnums []string
	intEnums    []int64
}

// extractFastConstraint analyzes a CUE field schema at compile time and
// attempts to extract a pure-Go constraint descriptor. Returns nil if
// the field has complex constraints that require CUE evaluation.
func extractFastConstraint(schema cue.Value) *fastConstraint {
	// Eval() resolves definition references (e.g. #PAN → =~"^[0-9]{16}$")
	schema = schema.Eval()
	kind := schema.IncompleteKind()

	switch kind {
	case cue.StringKind:
		return extractStringConstraint(schema)
	case cue.IntKind:
		return extractIntConstraint(schema)
	case cue.FloatKind:
		return extractFloatConstraint(schema)
	case cue.NumberKind:
		return extractNumberConstraint(schema)
	case cue.BoolKind:
		return &fastConstraint{kind: constraintType, expectBool: true}
	default:
		// struct, list, or complex — no fast path
		return nil
	}
}

// extractStringConstraint handles string fields: pure string, regex, or enum.
func extractStringConstraint(schema cue.Value) *fastConstraint {
	// Check for enum (disjunction of string literals)
	if enums := extractStringEnums(schema); enums != nil {
		return &fastConstraint{
			kind:         constraintEnum,
			expectString: true,
			stringEnums:  enums,
		}
	}

	// Check for regex bound (=~"pattern")
	if re := extractRegex(schema); re != nil {
		return &fastConstraint{
			kind:         constraintRegex,
			expectString: true,
			regex:        re,
		}
	}

	// Pure string type check
	return &fastConstraint{kind: constraintType, expectString: true}
}

// extractIntConstraint handles int fields: pure int, range, or enum.
func extractIntConstraint(schema cue.Value) *fastConstraint {
	// Check for enum (disjunction of int literals)
	if enums := extractIntEnums(schema); enums != nil {
		return &fastConstraint{
			kind:     constraintEnum,
			expectInt: true,
			intEnums: enums,
		}
	}

	// Check for range bounds
	if fc := extractNumericRange(schema, true); fc != nil {
		return fc
	}

	// Pure int type check
	return &fastConstraint{kind: constraintType, expectInt: true}
}

// extractFloatConstraint handles float fields.
func extractFloatConstraint(schema cue.Value) *fastConstraint {
	if fc := extractNumericRange(schema, false); fc != nil {
		return fc
	}
	return &fastConstraint{kind: constraintType, expectFloat: true}
}

// extractNumberConstraint handles number fields (int or float).
func extractNumberConstraint(schema cue.Value) *fastConstraint {
	if fc := extractNumericRange(schema, false); fc != nil {
		fc.expectNumber = true
		return fc
	}
	return &fastConstraint{kind: constraintType, expectNumber: true}
}

// extractStringEnums tries to extract all string alternatives from a disjunction.
// Returns nil if the value is not a simple string enum.
func extractStringEnums(v cue.Value) []string {
	op, vals := v.Expr()
	if op != cue.OrOp || len(vals) < 2 {
		return nil
	}

	var enums []string
	for _, alt := range vals {
		if alt.IncompleteKind() != cue.StringKind {
			return nil
		}
		s, err := alt.String()
		if err != nil {
			return nil
		}
		enums = append(enums, s)
	}
	return enums
}

// extractIntEnums tries to extract all int alternatives from a disjunction.
func extractIntEnums(v cue.Value) []int64 {
	op, vals := v.Expr()
	if op != cue.OrOp || len(vals) < 2 {
		return nil
	}

	var enums []int64
	for _, alt := range vals {
		if alt.IncompleteKind() != cue.IntKind {
			return nil
		}
		n, err := alt.Int64()
		if err != nil {
			return nil
		}
		enums = append(enums, n)
	}
	return enums
}

// extractRegex tries to extract a regex pattern from a bound expression (=~"pattern").
func extractRegex(v cue.Value) *regexp.Regexp {
	op, vals := v.Expr()

	// Direct bound: =~"pattern" — op is RegexMatchOp, vals[0] is the pattern string
	if op == cue.RegexMatchOp && len(vals) >= 1 {
		pattern, err := vals[0].String()
		if err != nil {
			return nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil
		}
		return re
	}

	// Conjunction: string & =~"pattern" (less common, but possible)
	if op == cue.AndOp {
		for _, sub := range vals {
			if re := extractRegex(sub); re != nil {
				return re
			}
		}
	}

	return nil
}

// extractNumericRange tries to extract range bounds from an int/float field.
// e.g. int & >0 & <=100
// CUE Expr structure for "int & >=0 & <=150":
//   Top: AndOp, vals = [int&>=0 (nested And), <=150 (LessThanEqualOp)]
//   Each bound op has subVals[0] as the bound value.
func extractNumericRange(v cue.Value, isInt bool) *fastConstraint {
	op, vals := v.Expr()

	if op == cue.NoOp || len(vals) == 0 {
		return nil
	}

	// Must be a conjunction for ranges
	if op != cue.AndOp {
		return nil
	}

	fc := &fastConstraint{kind: constraintRange}
	if isInt {
		fc.expectInt = true
	} else {
		fc.expectFloat = true
	}

	hasBound := extractBoundsRecursive(vals, fc)
	if !hasBound {
		return nil
	}
	return fc
}

// extractBoundsRecursive traverses the expression tree to find all bound operators.
func extractBoundsRecursive(vals []cue.Value, fc *fastConstraint) bool {
	hasBound := false
	for _, sub := range vals {
		subOp, subVals := sub.Expr()
		switch subOp {
		case cue.GreaterThanOp:
			if len(subVals) >= 1 {
				if n, err := numVal(subVals[0]); err == nil {
					fc.hasMin = true
					fc.min = n
					fc.minExcl = true
					hasBound = true
				}
			}
		case cue.GreaterThanEqualOp:
			if len(subVals) >= 1 {
				if n, err := numVal(subVals[0]); err == nil {
					fc.hasMin = true
					fc.min = n
					fc.minExcl = false
					hasBound = true
				}
			}
		case cue.LessThanOp:
			if len(subVals) >= 1 {
				if n, err := numVal(subVals[0]); err == nil {
					fc.hasMax = true
					fc.max = n
					fc.maxExcl = true
					hasBound = true
				}
			}
		case cue.LessThanEqualOp:
			if len(subVals) >= 1 {
				if n, err := numVal(subVals[0]); err == nil {
					fc.hasMax = true
					fc.max = n
					fc.maxExcl = false
					hasBound = true
				}
			}
		case cue.AndOp:
			// Nested conjunction — recurse
			if extractBoundsRecursive(subVals, fc) {
				hasBound = true
			}
		}
	}
	return hasBound
}

// numVal extracts a float64 from a CUE numeric value.
func numVal(v cue.Value) (float64, error) {
	if i, err := v.Int64(); err == nil {
		return float64(i), nil
	}
	if f, err := v.Float64(); err == nil {
		return f, nil
	}
	return 0, fmt.Errorf("not a number")
}

// validateFast performs pure-Go validation of a field value against a fastConstraint.
// Returns (valid bool, errorCode ErrorCode, detail string).
// If valid is true, errorCode and detail are meaningless.
func validateFast(fc *fastConstraint, val any) (bool, ErrorCode, string) {
	switch fc.kind {
	case constraintType:
		return validateFastType(fc, val)
	case constraintRegex:
		return validateFastRegex(fc, val)
	case constraintRange:
		return validateFastRange(fc, val)
	case constraintEnum:
		return validateFastEnum(fc, val)
	default:
		return true, "", "" // should not reach here
	}
}

func validateFastType(fc *fastConstraint, val any) (bool, ErrorCode, string) {
	switch {
	case fc.expectString:
		if _, ok := val.(string); !ok {
			return false, CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val)
		}
	case fc.expectInt:
		if !isIntLike(val) {
			return false, CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val)
		}
	case fc.expectFloat:
		if !isNumeric(val) {
			return false, CodeTypeMismatch, fmt.Sprintf("expected float, got %T", val)
		}
	case fc.expectNumber:
		if !isNumeric(val) {
			return false, CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val)
		}
	case fc.expectBool:
		if _, ok := val.(bool); !ok {
			return false, CodeTypeMismatch, fmt.Sprintf("expected bool, got %T", val)
		}
	}
	return true, "", ""
}

func validateFastRegex(fc *fastConstraint, val any) (bool, ErrorCode, string) {
	s, ok := val.(string)
	if !ok {
		return false, CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val)
	}
	if !fc.regex.MatchString(s) {
		return false, CodeFormatMismatch, fmt.Sprintf("does not match %s", fc.regex.String())
	}
	return true, "", ""
}

func validateFastRange(fc *fastConstraint, val any) (bool, ErrorCode, string) {
	n, ok := toFloat64(val)
	if !ok {
		if fc.expectInt {
			return false, CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val)
		}
		return false, CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val)
	}

	if fc.hasMin {
		if fc.minExcl {
			if n <= fc.min {
				return false, CodeRangeViolation, fmt.Sprintf("value %v out of bound >%v", val, fc.min)
			}
		} else {
			if n < fc.min {
				return false, CodeRangeViolation, fmt.Sprintf("value %v out of bound >=%v", val, fc.min)
			}
		}
	}

	if fc.hasMax {
		if fc.maxExcl {
			if n >= fc.max {
				return false, CodeRangeViolation, fmt.Sprintf("value %v out of bound <%v", val, fc.max)
			}
		} else {
			if n > fc.max {
				return false, CodeRangeViolation, fmt.Sprintf("value %v out of bound <=%v", val, fc.max)
			}
		}
	}

	return true, "", ""
}

func validateFastEnum(fc *fastConstraint, val any) (bool, ErrorCode, string) {
	if fc.stringEnums != nil {
		s, ok := val.(string)
		if !ok {
			return false, CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val)
		}
		for _, e := range fc.stringEnums {
			if s == e {
				return true, "", ""
			}
		}
		return false, CodeEnumInvalid, fmt.Sprintf("value %q not in enum", s)
	}

	if fc.intEnums != nil {
		n, ok := toInt64(val)
		if !ok {
			return false, CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val)
		}
		for _, e := range fc.intEnums {
			if n == e {
				return true, "", ""
			}
		}
		return false, CodeEnumInvalid, fmt.Sprintf("value %v not in enum", val)
	}

	return true, "", ""
}

// --- helpers ---

func isIntLike(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64:
		return true
	}
	return false
}

func isNumeric(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, float32, float64:
		return true
	}
	return false
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	}
	return 0, false
}
