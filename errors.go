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

// builtinErrorCodes is the closed set declared above, in the same order.
//
// Adding a code without adding it here makes it invisible to Catalog.Validate,
// which would then report a catalog as complete while that code renders as
// generic text. Keep the two in step.
var builtinErrorCodes = []ErrorCode{
	CodeFormatMismatch, CodeTypeMismatch, CodeEnumInvalid, CodeRangeViolation,
	CodeRequiredMissing, CodeArrayElement, CodeCUEOther,
	CodeBizRuleFailed, CodeExprExecError, CodeBlobTypeMismatch,
	CodeCondRequired, CodeMetaRuntimeError,
	CodeConfigError,
}

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

	// EnumOptions lists every value the schema accepts, in declaration order.
	// Only enum violations populate it.
	//
	// Values are unquoted: "CNY", not "\"CNY\"". Message renders string
	// candidates quoted and numeric ones bare, but that is a rendering decision,
	// and baking it into the data would force every consumer — a form's dropdown,
	// a translation layer — to strip the quotes back off.
	EnumOptions []string `json:"enum_options,omitempty"`

	// Bound is the comparison the value failed, such as "<=150" or ">0". Only
	// range violations populate it, and only the side that was actually broken:
	// a value above the maximum of `>=0 & <=150` reports "<=150".
	Bound string `json:"bound,omitempty"`
}

// attachStructuredFields lifts the data needed to rebuild a message out of the
// descriptor and the raw detail text.
//
// It must run before formatMessage. A custom ErrorFormatter replaces Message
// with arbitrary text, so anything recovered from Message afterwards would
// disappear the moment a formatter is configured — which is exactly the
// fragility these fields exist to remove.
//
// fc may be nil. A struct field, or a scalar the descriptor declined to decide,
// has no fastConstraint; Bound is still recoverable from the detail text, while
// EnumOptions is filled by the CUE path itself (see collapseDisjunctionErrors).
func attachStructuredFields(e *ValidationError, fc *fastConstraint, detail string) {
	switch e.Code {
	case CodeEnumInvalid:
		if e.EnumOptions == nil {
			e.EnumOptions = enumOptionsOf(fc)
		}
	case CodeRangeViolation:
		e.Bound = boundFromDetail(detail)
	}
}

// Error implements the error interface for ValidationError.
func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Path, e.Message)
}

// Result holds the output of a Process call.
type Result struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
	Output map[string]any    `json:"output"`

	// loc is the Localizer configured with WithLocalizer, used as the default by
	// LocalizedMessages.
	//
	// It lives here, unexported, rather than on ValidationError, because that
	// type is a DTO with a json tag on every field: an interface there would
	// appear in API responses and add a word to every error in the slice. Being
	// unexported also keeps it out of this struct's own JSON — see
	// TestResultJSONExcludesLocalizer.
	loc Localizer
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

// ErrorMessages returns the raw diagnostics, one per line.
//
// Each line carries the error code and the CUE/Bloblang wording, which makes
// this the string for a log, not for a response body. LocalizedMessages is the
// user-facing counterpart:
//
//	log.Warn(r.ErrorMessages())    // [E1R01] age: value 200 out of bound <=150
//	r.LocalizedMessages()          // ["age must be <=150"]
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

// Validation error type identifiers (user-facing, for filtering ValidationError.Type).
const (
	TypeCUE      = "cue"
	TypeBloblang = "bloblang"
	TypeMeta     = "meta"
	TypeConfig   = "config"
)
