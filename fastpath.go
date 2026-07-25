package schemix

import (
	"fmt"
	"math"
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
	hasMin   bool
	hasMax   bool
	min      float64 // inclusive lower bound (or exclusive, see minExcl)
	max      float64 // inclusive upper bound (or exclusive, see maxExcl)
	minInt64 int64   // exact integer lower bound when useInt64 is true
	maxInt64 int64   // exact integer upper bound when useInt64 is true
	useInt64 bool    // integer ranges use exact arithmetic instead of float64
	minExcl  bool    // true = > (exclusive), false = >= (inclusive)
	maxExcl  bool    // true = < (exclusive), false = <= (inclusive)

	// Enum constraint
	stringEnums []string
	intEnums    []int64
	floatEnums  []float64
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
			kind:      constraintEnum,
			expectInt: true,
			intEnums:  enums,
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
	// Check for float enum (disjunction of float literals)
	if enums := extractFloatEnums(schema); enums != nil {
		return &fastConstraint{
			kind:        constraintEnum,
			expectFloat: true,
			floatEnums:  enums,
		}
	}
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

// extractFloatEnums tries to extract all float alternatives from a disjunction.
func extractFloatEnums(v cue.Value) []float64 {
	op, vals := v.Expr()
	if op != cue.OrOp || len(vals) < 2 {
		return nil
	}

	var enums []float64
	for _, alt := range vals {
		k := alt.IncompleteKind()
		// Accept both float and int literals in a float enum disjunction
		if k != cue.FloatKind && k != cue.IntKind && k != cue.NumberKind {
			return nil
		}
		f, err := alt.Float64()
		if err != nil {
			return nil
		}
		enums = append(enums, f)
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
//
//	Top: AndOp, vals = [int&>=0 (nested And), <=150 (LessThanEqualOp)]
//	Each bound op has subVals[0] as the bound value.
func extractNumericRange(v cue.Value, isInt bool) *fastConstraint {
	op, vals := v.Expr()

	if op == cue.NoOp || len(vals) == 0 || op != cue.AndOp {
		return nil
	}

	fc := &fastConstraint{kind: constraintRange, useInt64: isInt}
	if isInt {
		fc.expectInt = true
	} else {
		fc.expectFloat = true
	}

	hasBound, exact := extractBoundsRecursive(vals, fc)
	if !hasBound || !exact {
		return nil
	}
	return fc
}

// extractBoundsRecursive traverses the expression tree to find all bound operators.
// exact is false when an integer bound cannot be represented as int64, which
// disables the fast path so CUE remains the correctness oracle.
func extractBoundsRecursive(vals []cue.Value, fc *fastConstraint) (hasBound, exact bool) {
	exact = true
	for _, sub := range vals {
		subOp, subVals := sub.Expr()
		switch subOp {
		case cue.GreaterThanOp, cue.GreaterThanEqualOp, cue.LessThanOp, cue.LessThanEqualOp:
			if len(subVals) == 0 {
				continue
			}
			if !setFastRangeBound(fc, subOp, subVals[0]) {
				return hasBound, false
			}
			hasBound = true
		case cue.AndOp:
			nestedBound, nestedExact := extractBoundsRecursive(subVals, fc)
			if !nestedExact {
				return hasBound || nestedBound, false
			}
			hasBound = hasBound || nestedBound
		}
	}
	return hasBound, true
}

func setFastRangeBound(fc *fastConstraint, op cue.Op, bound cue.Value) bool {
	if fc.useInt64 {
		n, err := bound.Int64()
		if err != nil {
			return false
		}
		switch op {
		case cue.GreaterThanOp, cue.GreaterThanEqualOp:
			fc.hasMin = true
			fc.minInt64 = n
			fc.minExcl = op == cue.GreaterThanOp
		case cue.LessThanOp, cue.LessThanEqualOp:
			fc.hasMax = true
			fc.maxInt64 = n
			fc.maxExcl = op == cue.LessThanOp
		}
		return true
	}

	n, err := numVal(bound)
	if err != nil {
		return false
	}
	switch op {
	case cue.GreaterThanOp, cue.GreaterThanEqualOp:
		fc.hasMin = true
		fc.min = n
		fc.minExcl = op == cue.GreaterThanOp
	case cue.LessThanOp, cue.LessThanEqualOp:
		fc.hasMax = true
		fc.max = n
		fc.maxExcl = op == cue.LessThanOp
	}
	return true
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
// Returns (handled, valid, code, detail):
//   - handled=false: the fast path cannot determine the result; caller MUST fall through to CUE Unify
//   - handled=true, valid=true: field passes the constraint
//   - handled=true, valid=false: field fails with the given code and detail
func validateFast(fc *fastConstraint, val any) (handled bool, valid bool, code ErrorCode, detail string) {
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
		return true, true, "", "" // should not reach here
	}
}

func validateFastType(fc *fastConstraint, val any) (bool, bool, ErrorCode, string) {
	switch {
	case fc.expectString:
		if _, ok := val.(string); !ok {
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val)
		}
	case fc.expectInt:
		return validateFastIntType(val)
	case fc.expectFloat:
		return validateFastFloatType(val)
	case fc.expectNumber:
		return validateFastNumberType(val)
	case fc.expectBool:
		if _, ok := val.(bool); !ok {
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected bool, got %T", val)
		}
	}
	return true, true, "", ""
}

// validateFastIntType implements the int type guard:
// - signed int types: handled+valid
// - float32/float64: handled+invalid (E1T01 — float on int)
// - uint/uint*: NOT handled → fall through to CUE
func validateFastIntType(val any) (bool, bool, ErrorCode, string) {
	switch val.(type) {
	case int, int8, int16, int32, int64:
		return true, true, "", ""
	case float32, float64:
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val)
	default:
		// uint types and others — cannot precisely handle, fall through to CUE
		return false, false, "", ""
	}
}

// validateFastFloatType validates float type with NaN/Inf rejection.
func validateFastFloatType(val any) (bool, bool, ErrorCode, string) {
	switch v := val.(type) {
	case float64:
		return validateFiniteFloat(v, val, "float")
	case float32:
		return validateFiniteFloat(float64(v), val, "float")
	case int, int8, int16, int32, int64:
		return true, true, "", "" // int is valid for float
	default:
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected float, got %T", val)
	}
}

// validateFastNumberType validates number (int or float) with NaN/Inf rejection.
func validateFastNumberType(val any) (bool, bool, ErrorCode, string) {
	switch v := val.(type) {
	case float64:
		return validateFiniteFloat(v, val, "number")
	case float32:
		return validateFiniteFloat(float64(v), val, "number")
	case int, int8, int16, int32, int64:
		return true, true, "", ""
	default:
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val)
	}
}

func validateFiniteFloat(n float64, original any, expected string) (bool, bool, ErrorCode, string) {
	if math.IsNaN(n) {
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected finite %s, got %v", expected, original)
	}
	if math.IsInf(n, 0) {
		return true, false, CodeRangeViolation, fmt.Sprintf("%s value out of finite range: %v", expected, original)
	}
	return true, true, "", ""
}

func validateFastRegex(fc *fastConstraint, val any) (bool, bool, ErrorCode, string) {
	s, ok := val.(string)
	if !ok {
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val)
	}
	if !fc.regex.MatchString(s) {
		return true, false, CodeFormatMismatch, fmt.Sprintf("does not match %s", fc.regex.String())
	}
	return true, true, "", ""
}

func validateFastRange(fc *fastConstraint, val any) (bool, bool, ErrorCode, string) {
	if fc.expectInt {
		n, ok := toInt64(val)
		if ok {
			return validateFastInt64Range(fc, n)
		}
		switch val.(type) {
		case float32, float64:
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val)
		default:
			// Unsigned integers and unknown numeric representations fall back to CUE.
			return false, false, "", ""
		}
	}

	n, ok := toFloat64(val)
	if !ok {
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val)
	}
	if math.IsNaN(n) {
		return true, false, CodeTypeMismatch, fmt.Sprintf("expected finite number, got %v", val)
	}
	if math.IsInf(n, 0) {
		return true, false, CodeRangeViolation, fmt.Sprintf("value %v out of finite range", val)
	}

	if fc.hasMin {
		if fc.minExcl && n <= fc.min {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %v out of bound >%v", val, fc.min)
		}
		if !fc.minExcl && n < fc.min {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %v out of bound >=%v", val, fc.min)
		}
	}
	if fc.hasMax {
		if fc.maxExcl && n >= fc.max {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %v out of bound <%v", val, fc.max)
		}
		if !fc.maxExcl && n > fc.max {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %v out of bound <=%v", val, fc.max)
		}
	}
	return true, true, "", ""
}

func validateFastInt64Range(fc *fastConstraint, n int64) (bool, bool, ErrorCode, string) {
	if !fc.useInt64 {
		return false, false, "", ""
	}
	if fc.hasMin {
		if fc.minExcl && n <= fc.minInt64 {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %d out of bound >%d", n, fc.minInt64)
		}
		if !fc.minExcl && n < fc.minInt64 {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %d out of bound >=%d", n, fc.minInt64)
		}
	}
	if fc.hasMax {
		if fc.maxExcl && n >= fc.maxInt64 {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %d out of bound <%d", n, fc.maxInt64)
		}
		if !fc.maxExcl && n > fc.maxInt64 {
			return true, false, CodeRangeViolation, fmt.Sprintf("value %d out of bound <=%d", n, fc.maxInt64)
		}
	}
	return true, true, "", ""
}

func validateFastEnum(fc *fastConstraint, val any) (bool, bool, ErrorCode, string) {
	if fc.stringEnums != nil {
		s, ok := val.(string)
		if !ok {
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val)
		}
		for _, e := range fc.stringEnums {
			if s == e {
				return true, true, "", ""
			}
		}
		return true, false, CodeEnumInvalid, fmt.Sprintf("value %q not in enum", s)
	}

	if fc.intEnums != nil {
		// Type guard for int enums
		switch val.(type) {
		case int, int8, int16, int32, int64:
			// ok — proceed
		case float32, float64:
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val)
		default:
			// uint types — fall through to CUE
			return false, false, "", ""
		}
		n, _ := toInt64(val)
		for _, e := range fc.intEnums {
			if n == e {
				return true, true, "", ""
			}
		}
		return true, false, CodeEnumInvalid, fmt.Sprintf("value %v not in enum", val)
	}

	if fc.floatEnums != nil {
		n, ok := toFloat64(val)
		if !ok {
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val)
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return true, false, CodeTypeMismatch, fmt.Sprintf("expected finite number, got %v", val)
		}
		for _, e := range fc.floatEnums {
			if n == e {
				return true, true, "", ""
			}
		}
		return true, false, CodeEnumInvalid, fmt.Sprintf("value %v not in enum", val)
	}

	return true, true, "", ""
}

// --- helpers ---

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
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
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}
