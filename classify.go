package schemix

import (
	"fmt"
	"strings"

	cueerrors "cuelang.org/go/cue/errors"
)

// classifyCUEErrorStructured classifies a CUE error using the structured Msg()
// interface first (stable format strings), falling back to string matching.
// This is more resilient to CUE library upgrades than pure string matching.
func classifyCUEErrorStructured(err cueerrors.Error) ErrorCode {
	format, args := err.Msg()

	switch {
	// "invalid value %v (out of bound %s)" — range or regex
	case format == "invalid value %v (out of bound %s)":
		if len(args) >= 2 {
			bound := fmt.Sprintf("%v", args[1])
			if strings.HasPrefix(bound, "=~") || strings.HasPrefix(bound, "!~") {
				return CodeFormatMismatch
			}
		}
		return CodeRangeViolation

	// "conflicting values %s and %s (mismatched types %s and %s)" — type mismatch
	case format == "conflicting values %s and %s (mismatched types %s and %s)":
		return CodeTypeMismatch

	// "conflicting values %s and %s" — enum or value conflict
	case format == "conflicting values %s and %s":
		return CodeEnumInvalid

	// "%d errors in empty disjunction:" — enum exhausted
	case format == "%d errors in empty disjunction:":
		return CodeEnumInvalid

	// "incomplete value %v" — required field missing
	case format == "incomplete value %v":
		return CodeRequiredMissing

	// "cannot use %s (type %s) as type %s" — type mismatch
	case strings.HasPrefix(format, "cannot use"):
		return CodeTypeMismatch

	// "field is required but not present" — required
	case strings.Contains(format, "field is required"):
		return CodeRequiredMissing

	default:
		// Fallback to string matching for any unrecognized format
		return classifyCUEError(err.Error())
	}
}

// classifyCUEError derives a structured ErrorCode from a CUE error message string.
// This is the fallback classifier used when structural Msg() format is unrecognized.
func classifyCUEError(errMsg string) ErrorCode {
	msg := strings.ToLower(errMsg)
	switch {
	case strings.Contains(msg, "does not match"):
		return CodeFormatMismatch
	case strings.Contains(msg, "cannot use value"):
		return CodeTypeMismatch
	case strings.Contains(msg, "empty disjunction"):
		return CodeEnumInvalid
	case strings.Contains(msg, "conflicting values") && strings.Contains(msg, "|"):
		return CodeEnumInvalid
	case strings.Contains(msg, "conflicting values") &&
		!strings.Contains(msg, "string") && !strings.Contains(msg, "int") &&
		!strings.Contains(msg, "bool") && !strings.Contains(msg, "number") &&
		!strings.Contains(msg, "float"):
		return CodeEnumInvalid
	case strings.Contains(msg, "conflicting values"):
		return CodeTypeMismatch
	case strings.Contains(msg, "out of bound") && strings.Contains(msg, "=~"):
		return CodeFormatMismatch
	case strings.Contains(msg, "out of bound"),
		strings.Contains(msg, "invalid value") && (strings.Contains(msg, ">=") ||
			strings.Contains(msg, "<=") || strings.Contains(msg, "> ") ||
			strings.Contains(msg, "< ")):
		return CodeRangeViolation
	case strings.Contains(msg, "incomplete value"),
		strings.Contains(msg, "field is required"):
		return CodeRequiredMissing
	default:
		return CodeCUEOther
	}
}
