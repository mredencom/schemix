package schemix

import (
	"fmt"
	"strconv"
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

// disjunctionSummaryMarker identifies the header CUE emits before listing the
// rejected branches of a failed disjunction.
const disjunctionSummaryMarker = "errors in empty disjunction"

// conflictMarker prefixes each rejected branch of a failed disjunction.
const conflictMarker = "conflicting values "

// collapseDisjunctionErrors merges the several errors CUE emits for a single
// failed disjunction into one enum error per path.
//
// CUE reports a failed disjunction as a summary line plus one line per rejected
// branch, all sharing the same path:
//
//	items.0.cur: 2 errors in empty disjunction:
//	items.0.cur: conflicting values "CNY" and "XXX"
//	items.0.cur: conflicting values "USD" and "XXX"
//
// Three errors for one bad field, worded in CUE's internal vocabulary. This
// rewrites them into the same shape the fast path produces:
//
//	items[0].cur: value "XXX" not in enum ["CNY", "USD"]
//
// The wording being parsed is CUE's internal format, so the rewrite is
// best-effort: when the shape is not recognised the original errors are returned
// untouched. Degrading to the previous behaviour is correct; inventing a message
// from an unrecognised format would not be.
func collapseDisjunctionErrors(errs []ValidationError) []ValidationError {
	// Group by path, preserving first-seen order.
	order := make([]string, 0, len(errs))
	groups := make(map[string][]ValidationError, len(errs))
	for _, e := range errs {
		if _, seen := groups[e.Path]; !seen {
			order = append(order, e.Path)
		}
		groups[e.Path] = append(groups[e.Path], e)
	}

	out := make([]ValidationError, 0, len(errs))
	for _, path := range order {
		group := groups[path]
		if merged, ok := mergeDisjunctionGroup(group); ok {
			out = append(out, merged)
			continue
		}
		out = append(out, group...)
	}
	return out
}

// mergeDisjunctionGroup collapses one path's disjunction errors, reporting
// whether the group had the expected shape.
func mergeDisjunctionGroup(group []ValidationError) (ValidationError, bool) {
	if len(group) < 2 {
		return ValidationError{}, false
	}

	var summary *ValidationError
	var candidates []string
	var input string

	for i := range group {
		msg := group[i].Message
		switch {
		case strings.Contains(msg, disjunctionSummaryMarker):
			summary = &group[i]
		case strings.Contains(msg, conflictMarker):
			cand, in, ok := parseConflict(msg)
			if !ok {
				return ValidationError{}, false
			}
			candidates = append(candidates, cand)
			// Every branch reports the same offending input value.
			if input != "" && input != in {
				return ValidationError{}, false
			}
			input = in
		default:
			// An unrelated error shares this path — leave the group alone.
			return ValidationError{}, false
		}
	}

	if summary == nil || len(candidates) == 0 {
		return ValidationError{}, false
	}

	merged := *summary
	merged.Code = CodeEnumInvalid
	merged.Message = fmt.Sprintf("value %s not in enum [%s]", input, strings.Join(candidates, ", "))
	if unquoted, err := strconv.Unquote(input); err == nil {
		merged.Suggestion = suggestClosest(unquoted, unquoteAll(candidates))
	}
	return merged, true
}

// parseConflict splits `…conflicting values "CNY" and "XXX"` into candidate and
// input. LastIndex is used for the separator so that a candidate containing
// " and " is handled correctly.
func parseConflict(msg string) (candidate, input string, ok bool) {
	i := strings.Index(msg, conflictMarker)
	if i < 0 {
		return "", "", false
	}
	rest := msg[i+len(conflictMarker):]
	// Drop a trailing parenthetical such as " (mismatched types …)".
	if p := strings.LastIndex(rest, " ("); p > 0 {
		rest = rest[:p]
	}
	sep := strings.LastIndex(rest, " and ")
	if sep < 0 {
		return "", "", false
	}
	candidate = strings.TrimSpace(rest[:sep])
	input = strings.TrimSpace(rest[sep+len(" and "):])
	if candidate == "" || input == "" {
		return "", "", false
	}
	return candidate, input, true
}

// unquoteAll strips Go quoting from candidates that carry it, so edit distance
// is computed on the values themselves.
func unquoteAll(vals []string) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		if u, err := strconv.Unquote(v); err == nil {
			out[i] = u
			continue
		}
		out[i] = v
	}
	return out
}

// suggestClosest returns the candidate nearest to value, or "" when none is
// close enough to be a confident correction. Comparison is case-insensitive so
