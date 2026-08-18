package schemix

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCode is a structured error identifier with format E{layer}{category}{seq}.
//
//	Layer 1: CUE structural/type validation
//	Layer 2: Bloblang business rules
//	Layer 3: Meta control violations
type ErrorCode string

const (
	// Layer 1: CUE structural validation
	CodeFormatMismatch  ErrorCode = "E1F01" // regex format mismatch
	CodeTypeMismatch    ErrorCode = "E1T01" // type conflict
	CodeEnumInvalid     ErrorCode = "E1E01" // enum value not allowed
	CodeRangeViolation  ErrorCode = "E1R01" // numeric range exceeded
	CodeRequiredMissing ErrorCode = "E1M01" // required field missing
	CodeArrayElement    ErrorCode = "E1A01" // array element validation failed
	CodeCUEOther        ErrorCode = "E1X01" // other CUE error

	// Layer 2: Bloblang business rules
	CodeBizRuleFailed    ErrorCode = "E2B01" // business rule returned false
	CodeExprExecError    ErrorCode = "E2X01" // expression runtime error
	CodeBlobTypeMismatch ErrorCode = "E2T01" // @blob type contract violation (WU2)

	// Layer 3: Meta control
	CodeCondRequired     ErrorCode = "E3C01" // conditional required not met
	CodeMetaRuntimeError ErrorCode = "E3X01" // meta expression runtime error (required_if/skip_if Query failure)

	// Layer 0: Configuration / invocation errors
	CodeConfigError ErrorCode = "E0C01" // invalid configuration (e.g. undefined FailMode)
)

// ValidationError represents a single validation failure.
type ValidationError struct {
	Code ErrorCode `json:"code"` // structured error code
	Path string    `json:"path"` // field path (e.g. "merchant.country")
	Type string    `json:"type"` // "cue", "bloblang", or "meta"

	// FieldType is the schema type of the offending field — "string", "int",
	// "float", "number", "bool", "struct" or "list". Empty when the error is not
	// tied to a declared field (e.g. a configuration error).
	FieldType string `json:"field_type,omitempty"`

	// Message is the raw diagnostic: the CUE/Bloblang wording, or the output of
	// a custom ErrorFormatter when one is configured. Use it for logs and
	// debugging; use FriendlyMessage for user-facing text.
	Message string `json:"message"`

	// Suggestion names the closest valid value when one can be determined with
	// confidence. Only enum violations populate it — a range or regex violation
	// has no meaningful correction to guess, and inventing one would mislead.
	Suggestion string `json:"suggestion,omitempty"`
}

// Error implements the error interface for ValidationError.
func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Path, e.Message)
}

// FriendlyMessage renders a user-facing sentence for the error.
//
// Message and FriendlyMessage are both always available, which is deliberate:
// a service typically logs the raw diagnostic and renders the friendly one, and
// needing both at once is the common case rather than a mode to switch between.
//
//	log.Warn(e.Message)              // raw CUE/Bloblang wording
//	json.Encode(e.FriendlyMessage()) // user-facing text
//
// A custom ErrorFormatter replaces Message entirely; FriendlyMessage is derived
// from the structured fields (Code, Path, FieldType, Suggestion) and therefore
// stays stable regardless of formatter configuration.
func (e ValidationError) FriendlyMessage() string {
	field := e.Path
	if field == "" {
		field = "value"
	}

	switch e.Code {
	case CodeRequiredMissing:
		return fmt.Sprintf("%s is required", field)

	case CodeCondRequired:
		return fmt.Sprintf("%s is required for this request", field)

	case CodeTypeMismatch:
		if e.FieldType != "" {
			return fmt.Sprintf("%s must be of type %s", field, e.FieldType)
		}
		return fmt.Sprintf("%s has the wrong type", field)

	case CodeEnumInvalid:
		msg := fmt.Sprintf("%s is not one of the allowed values", field)
		if opts := enumOptionsFromDetail(e.Message); opts != "" {
			msg = fmt.Sprintf("%s must be one of %s", field, opts)
		}
		if e.Suggestion != "" {
			msg += fmt.Sprintf(" — did you mean %q?", e.Suggestion)
		}
		return msg

	case CodeRangeViolation:
		if bound := boundFromDetail(e.Message); bound != "" {
			return fmt.Sprintf("%s must be %s", field, bound)
		}
		return fmt.Sprintf("%s is out of the allowed range", field)

	case CodeFormatMismatch:
		return fmt.Sprintf("%s has an invalid format", field)

	case CodeArrayElement:
		return fmt.Sprintf("%s contains an invalid item", field)

	case CodeBizRuleFailed:
		return fmt.Sprintf("%s does not satisfy a validation rule", field)

	case CodeBlobTypeMismatch:
		return fmt.Sprintf("%s produced a value of the wrong type", field)

	case CodeExprExecError, CodeMetaRuntimeError:
		return fmt.Sprintf("%s could not be evaluated", field)

	case CodeConfigError:
		return "the validation configuration is invalid"

	case CodeCUEOther:
		return fmt.Sprintf("%s is invalid", field)
	}

	// Unknown code — never return empty, so a UI can call this unconditionally.
	return fmt.Sprintf("%s is invalid", field)
}

// enumOptionsFromDetail lifts the candidate list out of an enum detail such as
// `value "USE" not in enum ["CNY", "USD"]`, returning `["CNY", "USD"]`.
func enumOptionsFromDetail(detail string) string {
	open := strings.LastIndex(detail, "[")
	if open < 0 || !strings.HasSuffix(detail, "]") {
		return ""
	}
	return detail[open:]
}

// boundFromDetail lifts the comparison out of a range detail such as
// `value 999 out of bound <=150`, returning `<=150`.
func boundFromDetail(detail string) string {
	const marker = "out of bound "
	i := strings.Index(detail, marker)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(detail[i+len(marker):])
}

// maxSuggestionDistance caps how far a value may be from a candidate before the

// Result holds the output of a Process call.
type Result struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
	Output map[string]any    `json:"output"`
}

// Err returns nil if validation passed, or a combined error from all
// validation failures. This is convenient for Go-style error checking:
//
//	if err := v.Process(data).Err(); err != nil { ... }
func (r Result) Err() error {
	if r.Valid {
		return nil
	}
	errs := make([]error, len(r.Errors))
	for i := range r.Errors {
		errs[i] = r.Errors[i]
	}
	return errors.Join(errs...)
}

// FirstError returns the first validation error, or nil if validation passed.
func (r Result) FirstError() *ValidationError {
	if len(r.Errors) == 0 {
		return nil
	}
	return &r.Errors[0]
}

// ErrorsByPath returns all errors for a specific field path.
func (r Result) ErrorsByPath(path string) []ValidationError {
	var out []ValidationError
	for _, e := range r.Errors {
		if e.Path == path {
			out = append(out, e)
		}
	}
	return out
}

// HasCode reports whether any error has the specified error code.
func (r Result) HasCode(code ErrorCode) bool {
	for _, e := range r.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// ErrorsByCode returns all errors matching the specified error code.
func (r Result) ErrorsByCode(code ErrorCode) []ValidationError {
	var out []ValidationError
	for _, e := range r.Errors {
		if e.Code == code {
			out = append(out, e)
		}
	}
	return out
}

// ErrorsByType returns all errors of the specified type ("cue", "bloblang", "meta").
func (r Result) ErrorsByType(typ string) []ValidationError {
	var out []ValidationError
	for _, e := range r.Errors {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// HasErrorsAt reports whether there are any errors at the specified field path.
func (r Result) HasErrorsAt(path string) bool {
	for _, e := range r.Errors {
		if e.Path == path {
			return true
		}
	}
	return false
}

// ErrorMessages returns all error messages joined by newline.
func (r Result) ErrorMessages() string {
	if len(r.Errors) == 0 {
		return ""
	}
	msgs := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// ErrorFormatter customizes the human-readable message in ValidationError.
// It receives the error code, field path, and the default detail message
// (which is the raw CUE error or expression text). Return the desired
// user-facing message string.
//
// Example (i18n):
//
//	func myFormatter(code ErrorCode, path, detail string) string {
//	    return i18n.T("zh-CN", string(code), path)
//	}
type ErrorFormatter func(code ErrorCode, path string, detail string) string

// Validation error type identifiers (user-facing, for filtering ValidationError.Type).
const (
	TypeCUE      = "cue"
	TypeBloblang = "bloblang"
	TypeMeta     = "meta"
	TypeConfig   = "config"
)
