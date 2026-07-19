package schemix

import (
	"fmt"
	"strings"

	cueerrors "cuelang.org/go/cue/errors"
)

// classifyCUEErrorStructured classifies a CUE error by inspecting the stable
// Msg() format string first, falling back to error message string matching.
func classifyCUEErrorStructured(err cueerrors.Error) ErrorCode {
	format, args := err.Msg()

	if code := classifyByFormat(format, args); code != "" {
		return code
	}
	return classifyByMessage(err.Error())
}

// --- Format classification (stable CUE API) ---

// Known format strings → error code (O(1) lookup).
var formatCodes = map[string]ErrorCode{
	"conflicting values %s and %s (mismatched types %s and %s)": CodeTypeMismatch,
	"conflicting values %s and %s":                              CodeEnumInvalid,
	"%d errors in empty disjunction:":                           CodeEnumInvalid,
	"incomplete value %v":                                       CodeRequiredMissing,
}

func classifyByFormat(format string, args []any) ErrorCode {
	// Fast path: direct map lookup
	if code, ok := formatCodes[format]; ok {
		return code
	}

	// Bound expression: regex (=~) vs numeric range
	if format == "invalid value %v (out of bound %s)" {
		if len(args) >= 2 {
			if b := fmt.Sprintf("%v", args[1]); strings.HasPrefix(b, "=~") || strings.HasPrefix(b, "!~") {
				return CodeFormatMismatch
			}
		}
		return CodeRangeViolation
	}

	// Prefix/contains patterns
	if strings.HasPrefix(format, "cannot use") {
		return CodeTypeMismatch
	}
	if strings.Contains(format, "field is required") {
		return CodeRequiredMissing
	}

	return "" // not recognized → fall through
}

// --- Message classification (fallback for unknown formats) ---

// msgRule: code is returned when ALL "must" match, at least one "any" matches,
// and NONE of "not" match against the lowercased error message.
type msgRule struct {
	code ErrorCode
	must []string
	any  []string
	not  []string
}

var msgRules = []msgRule{
	{CodeFormatMismatch, s("does not match"), nil, nil},
	{CodeTypeMismatch, s("cannot use value"), nil, nil},
	{CodeEnumInvalid, s("empty disjunction"), nil, nil},
	{CodeEnumInvalid, s("conflicting values", "|"), nil, nil},
	{CodeEnumInvalid, s("conflicting values"), nil, s("string", "int", "bool", "number", "float")},
	{CodeTypeMismatch, s("conflicting values"), nil, nil},
	{CodeFormatMismatch, s("out of bound", "=~"), nil, nil},
	{CodeRangeViolation, s("out of bound"), nil, nil},
	{CodeRangeViolation, s("invalid value"), s(">=", "<=", "> ", "< "), nil},
	{CodeRequiredMissing, s("incomplete value"), nil, nil},
	{CodeRequiredMissing, s("field is required"), nil, nil},
}

// s is a shorthand for []string to keep the rule table compact.
func s(ss ...string) []string { return ss }

func classifyByMessage(errMsg string) ErrorCode {
	msg := strings.ToLower(errMsg)
	for i := range msgRules {
		if matchMsg(&msgRules[i], msg) {
			return msgRules[i].code
		}
	}
	return CodeCUEOther
}

func matchMsg(r *msgRule, msg string) bool {
	return containsAll(msg, r.must) && containsAny(msg, r.any) && containsNone(msg, r.not)
}

func containsAll(msg string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(msg, sub) {
			return false
		}
	}
	return true
}

func containsAny(msg string, subs []string) bool {
	if len(subs) == 0 {
		return true
	}
	for _, sub := range subs {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

func containsNone(msg string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(msg, sub) {
			return false
		}
	}
	return true
}
