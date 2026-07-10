// Package schemix provides a schema-driven validation and transformation engine
// powered by CUE constraints and Bloblang dynamic expressions.
//
// It combines CUE's declarative type system (@blob() for dynamic rules,
// @meta() for field behavior control) with recursive multi-level validation,
// structured error codes, and configurable fail strategies.
package schemix

import (
	"errors"
	"fmt"
	"strings"

	"github.com/redpanda-data/benthos/v4/public/bloblang"
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
	CodeBizRuleFailed ErrorCode = "E2B01" // business rule returned false
	CodeExprExecError ErrorCode = "E2X01" // expression runtime error

	// Layer 3: Meta control
	CodeCondRequired ErrorCode = "E3C01" // conditional required not met
)

// ValidationError represents a single validation failure.
type ValidationError struct {
	Code    ErrorCode `json:"code"`    // structured error code
	Path    string    `json:"path"`    // field path (e.g. "merchant.country")
	Type    string    `json:"type"`    // "cue", "bloblang", or "meta"
	Message string    `json:"message"` // human-readable description
}

// Error implements the error interface for ValidationError.
func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Path, e.Message)
}

// FailMode controls how errors are collected during validation.
type FailMode int

const (
	// FailAll collects all errors before returning (default, good for forms).
	FailAll FailMode = iota
	// FailFast stops at the first error (good for gateways).
	FailFast
	// FailPriority stops when the current priority group has errors.
	FailPriority
)

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

// blobRule is an extracted @blob rule with its field path and meta controls.
type blobRule struct {
	Path string             // field path (e.g. "address.city")
	Exec *bloblang.Executor // compiled Bloblang expression (nil = pure meta node)
	Expr string             // raw expression text
	Meta fieldMeta          // field behavior controls
}

// fieldMeta holds all @meta() attribute parameters for a field.
type fieldMeta struct {
	Priority       int                // execution priority (lower = first)
	Optional       bool               // field absence is not an error
	Conditional    bool               // conditional optional (with required_if)
	SkipEmpty      bool               // skip validation when empty/zero
	FailFast       bool               // skip remaining rules for this field on failure
	OmitIfSkip     bool               // remove from output when skipped
	OmitEmpty      bool               // remove from output when empty
	SkipIf         *bloblang.Executor // conditional skip expression
	SkipIfExpr     string
	RequiredIf     *bloblang.Executor // conditional required expression
	RequiredIfExpr string
}
