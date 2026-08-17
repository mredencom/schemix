package schemix

import (
	"fmt"
	"math"
	"regexp"
	"strconv"

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
		fc.expectFloat = false
		fc.expectNumber = true
		return fc
	}
	if op, _ := schema.Expr(); op != cue.NoOp {
		// A constrained number that cannot be represented exactly by the fast
		// descriptor must remain on the CUE correctness path.
		return nil
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

// fastResult is the outcome of a Go-native fast-path check for a single field.
//
//   - Handled=false: the fast path cannot determine the result (e.g. an
//     unsigned integer type it doesn't precisely support); the caller MUST
//     fall through to CUE Unify. Valid/Code/Detail are meaningless in this case.
//   - Handled=true, Valid=true: the field passes the constraint.
//   - Handled=true, Valid=false: the field fails; Code/Detail describe why.
type fastResult struct {
	Handled bool
	Valid   bool
	Code    ErrorCode
	Detail  string

	// Suggestion carries the closest valid value for enum violations. Empty for
	// every other constraint kind — see ValidationError.Suggestion.
	Suggestion string
}

// fallbackToCUE signals that the fast path cannot precisely evaluate this
// value (e.g. an unsigned integer or other representation outside its
// supported set) and the caller must fall through to the CUE Unify path.
func fallbackToCUE() fastResult {
	return fastResult{Handled: false}
}

// pass reports that the fast path handled the field and it satisfies the constraint.
func pass() fastResult {
	return fastResult{Handled: true, Valid: true}
}

// fail reports that the fast path handled the field and it violates the constraint.
func fail(code ErrorCode, detail string) fastResult {
	return fastResult{Handled: true, Valid: false, Code: code, Detail: detail}
}

// failEnum reports an enum violation, optionally carrying the closest candidate.
func failEnum(detail, suggestion string) fastResult {
	return fastResult{
		Handled:    true,
		Valid:      false,
		Code:       CodeEnumInvalid,
		Detail:     detail,
		Suggestion: suggestion,
	}
}

// validateFast performs pure-Go validation of a field value against a fastConstraint.
// See fastResult for how to interpret the outcome.
func validateFast(fc *fastConstraint, val any) fastResult {
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
		return pass() // should not reach here
	}
}

func validateFastType(fc *fastConstraint, val any) fastResult {
	switch {
	case fc.expectString:
		if _, ok := val.(string); !ok {
			return fail(CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val))
		}
	case fc.expectInt:
		return validateFastIntType(val)
	case fc.expectFloat:
		return validateFastFloatType(val)
	case fc.expectNumber:
		return validateFastNumberType(val)
	case fc.expectBool:
		if _, ok := val.(bool); !ok {
			return fail(CodeTypeMismatch, fmt.Sprintf("expected bool, got %T", val))
		}
	}
	return pass()
}

// validateFastIntType implements the int type guard:
// - signed int types: handled+valid
// - float32/float64: handled+invalid (E1T01 — float on int)
// - uint/uint*: NOT handled → fall through to CUE
func validateFastIntType(val any) fastResult {
	switch val.(type) {
	case int, int8, int16, int32, int64:
		return pass()
	case float32, float64:
		return fail(CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val))
	default:
		// uint types and others — cannot precisely handle, fall through to CUE
		return fallbackToCUE()
	}
}

// validateFastFloatType validates float type with NaN/Inf rejection.
func validateFastFloatType(val any) fastResult {
	switch v := val.(type) {
	case float64:
		return validateFiniteFloat(v, val, "float")
	case float32:
		return validateFiniteFloat(float64(v), val, "float")
	case int, int8, int16, int32, int64:
		return pass() // int is valid for float
	default:
		return fail(CodeTypeMismatch, fmt.Sprintf("expected float, got %T", val))
	}
}

// validateFastNumberType validates number (int or float) with NaN/Inf rejection.
func validateFastNumberType(val any) fastResult {
	switch v := val.(type) {
	case float64:
		return validateFiniteFloat(v, val, "number")
	case float32:
		return validateFiniteFloat(float64(v), val, "number")
	case int, int8, int16, int32, int64:
		return pass()
	default:
		return fail(CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val))
	}
}

func validateFiniteFloat(n float64, original any, expected string) fastResult {
	if math.IsNaN(n) {
		return fail(CodeTypeMismatch, fmt.Sprintf("expected finite %s, got %v", expected, original))
	}
	if math.IsInf(n, 0) {
		return fail(CodeRangeViolation, fmt.Sprintf("%s value out of finite range: %v", expected, original))
	}
	return pass()
}

func validateFastRegex(fc *fastConstraint, val any) fastResult {
	s, ok := val.(string)
	if !ok {
		return fail(CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val))
	}
	if !fc.regex.MatchString(s) {
		return fail(CodeFormatMismatch, fmt.Sprintf("does not match %s", fc.regex.String()))
	}
	return pass()
}

func validateFastRange(fc *fastConstraint, val any) fastResult {
	if fc.expectInt {
		n, ok := toInt64(val)
		if ok {
			return validateFastInt64Range(fc, n)
		}
		switch val.(type) {
		case float32, float64:
			return fail(CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val))
		default:
			// Unsigned integers and unknown numeric representations fall back to CUE.
			return fallbackToCUE()
		}
	}

	n, ok := toFloat64(val)
	if !ok {
		return fail(CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val))
	}
	if math.IsNaN(n) {
		return fail(CodeTypeMismatch, fmt.Sprintf("expected finite number, got %v", val))
	}
	if math.IsInf(n, 0) {
		return fail(CodeRangeViolation, fmt.Sprintf("value %v out of finite range", val))
	}

	if fc.hasMin {
		if fc.minExcl && n <= fc.min {
			return fail(CodeRangeViolation, fmt.Sprintf("value %v out of bound >%v", val, fc.min))
		}
		if !fc.minExcl && n < fc.min {
			return fail(CodeRangeViolation, fmt.Sprintf("value %v out of bound >=%v", val, fc.min))
		}
	}
	if fc.hasMax {
		if fc.maxExcl && n >= fc.max {
			return fail(CodeRangeViolation, fmt.Sprintf("value %v out of bound <%v", val, fc.max))
		}
		if !fc.maxExcl && n > fc.max {
			return fail(CodeRangeViolation, fmt.Sprintf("value %v out of bound <=%v", val, fc.max))
		}
	}
	return pass()
}

func validateFastInt64Range(fc *fastConstraint, n int64) fastResult {
	if !fc.useInt64 {
		return fallbackToCUE()
	}
	if fc.hasMin {
		if fc.minExcl && n <= fc.minInt64 {
			return fail(CodeRangeViolation, fmt.Sprintf("value %d out of bound >%d", n, fc.minInt64))
		}
		if !fc.minExcl && n < fc.minInt64 {
			return fail(CodeRangeViolation, fmt.Sprintf("value %d out of bound >=%d", n, fc.minInt64))
		}
	}
	if fc.hasMax {
		if fc.maxExcl && n >= fc.maxInt64 {
			return fail(CodeRangeViolation, fmt.Sprintf("value %d out of bound <%d", n, fc.maxInt64))
		}
		if !fc.maxExcl && n > fc.maxInt64 {
			return fail(CodeRangeViolation, fmt.Sprintf("value %d out of bound <=%d", n, fc.maxInt64))
		}
	}
	return pass()
}

func validateFastEnum(fc *fastConstraint, val any) fastResult {
	if fc.stringEnums != nil {
		s, ok := val.(string)
		if !ok {
			return fail(CodeTypeMismatch, fmt.Sprintf("expected string, got %T", val))
		}
		for _, e := range fc.stringEnums {
			if s == e {
				return pass()
			}
		}
		return failEnum(
			stringEnumDetail(s, fc.stringEnums),
			suggestClosest(s, fc.stringEnums),
		)
	}

	if fc.intEnums != nil {
		// Type guard for int enums
		switch val.(type) {
		case int, int8, int16, int32, int64:
			// ok — proceed
		case float32, float64:
			return fail(CodeTypeMismatch, fmt.Sprintf("expected int, got %T", val))
		default:
			// uint types — fall through to CUE
			return fallbackToCUE()
		}
		n, _ := toInt64(val)
		for _, e := range fc.intEnums {
			if n == e {
				return pass()
			}
		}
		return failEnum(int64EnumDetail(n, fc.intEnums), "")
	}

	if fc.floatEnums != nil {
		n, ok := toFloat64(val)
		if !ok {
			return fail(CodeTypeMismatch, fmt.Sprintf("expected number, got %T", val))
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fail(CodeTypeMismatch, fmt.Sprintf("expected finite number, got %v", val))
		}
		for _, e := range fc.floatEnums {
			if n == e {
				return pass()
			}
		}
		return failEnum(float64EnumDetail(n, fc.floatEnums), "")
	}

	return pass()
}

// The enum detail builders below assemble the whole message into one buffer.
// Rendering the candidate list separately and handing it to fmt.Sprintf costs
// six allocations — one per strconv.Quote, one for the list, two for Sprintf —
// on a path that runs for every rejected value.

// enumDetailSize estimates the buffer needed so the append loop never grows it.
func enumDetailSize(gotLen, count, valueLen int) int {
	const fixed = len(`value  not in enum []`) + 2 // message text plus quotes
	return fixed + gotLen + count*(valueLen+4)
}

// stringEnumDetail renders `value "USE" not in enum ["CNY", "USD"]`.
func stringEnumDetail(got string, enums []string) string {
	n := enumDetailSize(len(got), len(enums), 0)
	for _, e := range enums {
		n += len(e)
	}
	buf := make([]byte, 0, n)
	buf = append(buf, "value "...)
	buf = strconv.AppendQuote(buf, got)
	buf = append(buf, " not in enum ["...)
	for i, e := range enums {
		if i > 0 {
			buf = append(buf, ", "...)
		}
		buf = strconv.AppendQuote(buf, e)
	}
	buf = append(buf, ']')
	return string(buf)
}

// int64EnumDetail renders `value 5 not in enum [1, 2, 3]`.
func int64EnumDetail(got int64, enums []int64) string {
	buf := make([]byte, 0, enumDetailSize(20, len(enums), 20))
	buf = append(buf, "value "...)
	buf = strconv.AppendInt(buf, got, 10)
	buf = append(buf, " not in enum ["...)
	for i, e := range enums {
		if i > 0 {
			buf = append(buf, ", "...)
		}
		buf = strconv.AppendInt(buf, e, 10)
	}
	buf = append(buf, ']')
	return string(buf)
}

// float64EnumDetail renders `value 0.7 not in enum [0.5, 1]`.
func float64EnumDetail(got float64, enums []float64) string {
	buf := make([]byte, 0, enumDetailSize(24, len(enums), 24))
	buf = append(buf, "value "...)
	buf = strconv.AppendFloat(buf, got, 'g', -1, 64)
	buf = append(buf, " not in enum ["...)
	for i, e := range enums {
		if i > 0 {
			buf = append(buf, ", "...)
		}
		buf = strconv.AppendFloat(buf, e, 'g', -1, 64)
	}
	buf = append(buf, ']')
	return string(buf)
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
