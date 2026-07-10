package schemix

import "strings"

// classifyCUEError derives a structured ErrorCode from a CUE error message.
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
