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
	"regexp"
	"strings"

	"github.com/warpstreamlabs/bento/public/bloblang"
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

// Option configures a Validator during construction.
type Option func(*validatorConfig)

// validatorConfig holds optional configuration for Validator construction.
type validatorConfig struct {
	errorFormatter       ErrorFormatter
	customFuncs          []customFuncEntry
	funcMapErr           error    // propagated from FuncMap validation
	allowMethodOverrides []string // built-in method names allowed to be overridden
	allowFuncOverrides   []string // built-in function names allowed to be overridden
	overrideAll          bool     // disable all conflict checks
}

// customFuncEntry stores one custom function/method registration.
type customFuncEntry struct {
	name string
	kind customFuncKind
	// V1 style (simple)
	funcV1   bloblang.FunctionConstructor
	methodV1 bloblang.MethodConstructor
	// V2 style (with PluginSpec)
	spec     *bloblang.PluginSpec
	funcV2   bloblang.FunctionConstructorV2
	methodV2 bloblang.MethodConstructorV2
}

type customFuncKind int

const (
	kindFuncV1   customFuncKind = iota // RegisterFunction
	kindFuncV2                         // RegisterFunctionV2
	kindMethodV1                       // RegisterMethod
	kindMethodV2                       // RegisterMethodV2
)

// WithErrorFormatter sets a custom error message formatter.
// When set, all ValidationError.Message values will be generated by this function
// instead of the default English messages.
func WithErrorFormatter(f ErrorFormatter) Option {
	return func(cfg *validatorConfig) {
		cfg.errorFormatter = f
	}
}

// WithOverride explicitly allows overriding one or more built-in validators
// in their respective namespace. Use WithOverrideMethod / WithOverrideFunc for
// namespace-specific overrides, or WithOverrideAll to allow overriding everything.
//
// Example:
//
//	// Override specific built-in methods
//	schemix.WithOverrideMethod("is_email", "luhn_valid")
//
//	// Override specific built-in functions
//	schemix.WithOverrideFunc("is_valid_date")
//
//	// Override everything — no conflict checks at all
//	schemix.WithOverrideAll()
func WithOverrideMethod(names ...string) Option {
	return func(cfg *validatorConfig) {
		cfg.allowMethodOverrides = append(cfg.allowMethodOverrides, names...)
	}
}

// WithOverrideFunc allows overriding specific built-in functions by name.
func WithOverrideFunc(names ...string) Option {
	return func(cfg *validatorConfig) {
		cfg.allowFuncOverrides = append(cfg.allowFuncOverrides, names...)
	}
}

// WithOverrideAll disables all built-in conflict checks — any name can be
// registered regardless of whether it conflicts with a built-in.
func WithOverrideAll() Option {
	return func(cfg *validatorConfig) {
		cfg.overrideAll = true
	}
}

// WithFunction registers a custom function using Bloblang's FunctionConstructor signature.
// This is the same signature as bloblang.RegisterFunction — a factory that receives
// arguments and returns a Function closure.
//
// Example:
//
//	v, _ := schemix.New(schema, schemix.WithFunction("is_even", func(args ...any) (bloblang.Function, error) {
//	    n, ok := args[0].(int64)
//	    if !ok {
//	        return nil, fmt.Errorf("is_even requires int64")
//	    }
//	    return func() (any, error) {
//	        return n%2 == 0, nil
//	    }, nil
//	}))
//
// In schema: check: bool @blob(is_even(this.amount))
func WithFunction(name string, fn bloblang.FunctionConstructor) Option {
	return func(cfg *validatorConfig) {
		if err := validateName(name); err != nil {
			cfg.funcMapErr = err
			return
		}
		cfg.customFuncs = append(cfg.customFuncs, customFuncEntry{
			name:   name,
			kind:   kindFuncV1,
			funcV1: fn,
		})
	}
}

// WithFunctionV2 registers a custom function using a PluginSpec for typed parameters.
// This matches Bloblang's RegisterFunctionV2 signature exactly.
//
// Example:
//
//	v, _ := schemix.New(schema, schemix.WithFunctionV2("calculate_fee",
//	    bloblang.NewPluginSpec().
//	        Param(bloblang.NewInt64Param("amount")).
//	        Param(bloblang.NewFloat64Param("rate")),
//	    func(args *bloblang.ParsedParams) (bloblang.Function, error) {
//	        amount, _ := args.GetInt64("amount")
//	        rate, _ := args.GetFloat64("rate")
//	        return func() (any, error) {
//	            return float64(amount) * rate, nil
//	        }, nil
//	    },
//	))
func WithFunctionV2(name string, spec *bloblang.PluginSpec, ctor bloblang.FunctionConstructorV2) Option {
	return func(cfg *validatorConfig) {
		if err := validateName(name); err != nil {
			cfg.funcMapErr = err
			return
		}
		cfg.customFuncs = append(cfg.customFuncs, customFuncEntry{
			name:   name,
			kind:   kindFuncV2,
			spec:   spec,
			funcV2: ctor,
		})
	}
}

// WithMethod registers a custom method using the simple style.
// Methods are called on a target value: this.field.my_method()
//
// Example:
//
//	v, _ := schemix.New(schema, schemix.WithMethod("is_valid_luhn", func(v any) (any, error) {
//	    s := v.(string)
//	    return luhnCheck(s), nil
//	}))
//
// In schema: check: bool @blob(this.pan.is_valid_luhn())
func WithMethod(name string, fn bloblang.Method) Option {
	return func(cfg *validatorConfig) {
		if err := validateName(name); err != nil {
			cfg.funcMapErr = err
			return
		}
		cfg.customFuncs = append(cfg.customFuncs, customFuncEntry{
			name: name,
			kind: kindMethodV1,
			methodV1: func(args ...any) (bloblang.Method, error) {
				return fn, nil
			},
		})
	}
}

// WithMethodV2 registers a custom method using a PluginSpec for typed parameters.
// This matches Bloblang's RegisterMethodV2 signature exactly.
//
// Example:
//
//	v, _ := schemix.New(schema, schemix.WithMethodV2("has_prefix_any",
//	    bloblang.NewPluginSpec().
//	        Param(bloblang.NewStringParam("prefixes").Description("comma-separated prefixes")),
//	    func(args *bloblang.ParsedParams) (bloblang.Method, error) {
//	        prefixes, _ := args.GetString("prefixes")
//	        parts := strings.Split(prefixes, ",")
//	        return func(v any) (any, error) {
//	            s := v.(string)
//	            for _, p := range parts {
//	                if strings.HasPrefix(s, p) { return true, nil }
//	            }
//	            return false, nil
//	        }, nil
//	    },
//	))
func WithMethodV2(name string, spec *bloblang.PluginSpec, ctor bloblang.MethodConstructorV2) Option {
	return func(cfg *validatorConfig) {
		if err := validateName(name); err != nil {
			cfg.funcMapErr = err
			return
		}
		cfg.customFuncs = append(cfg.customFuncs, customFuncEntry{
			name:     name,
			kind:     kindMethodV2,
			spec:     spec,
			methodV2: ctor,
		})
	}
}

// FuncMap is a reusable collection of custom functions and methods that can be
// shared across multiple Validators. Build it once, inject everywhere.
//
// Example:
//
//	funcs := schemix.NewFuncMap(
//	    schemix.Func("check_blacklist", myBlacklistFn),
//	    schemix.Func("calc_fee", myFeeFn),
//	    schemix.Method("is_valid_bin", myBinFn),
//	    schemix.MethodV2("in_range", rangeSpec, rangeCtor),
//	)
//
//	v1, _ := schemix.New(schema1, schemix.WithFuncMap(funcs))
//	v2, _ := schemix.New(schema2, schemix.WithFuncMap(funcs))
type FuncMap struct {
	entries []customFuncEntry
	err     error // first validation error (invalid name)
}

// FuncMapOption defines a registration entry for NewFuncMap.
type FuncMapOption func(*FuncMap)

// nameRegex validates plugin names: snake_case only.
var nameRegex = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

func validateName(name string) error {
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("invalid name %q: must match /^[a-z0-9]+(_[a-z0-9]+)*$/ (snake_case)", name)
	}
	return nil
}

// NewFuncMap creates a FuncMap from the given registration options.
// Returns nil error in FuncMap.Err() if all names are valid.
func NewFuncMap(opts ...FuncMapOption) *FuncMap {
	m := &FuncMap{}
	for _, opt := range opts {
		if m.err != nil {
			break // stop on first error
		}
		opt(m)
	}
	return m
}

// Err returns the first validation error encountered during FuncMap construction
// (e.g. invalid function name). Returns nil if all registrations are valid.
func (m *FuncMap) Err() error {
	return m.err
}

// Func registers a custom function (V1 style).
// In schema: name(args...)
func Func(name string, fn bloblang.FunctionConstructor) FuncMapOption {
	return func(m *FuncMap) {
		if err := validateName(name); err != nil {
			m.err = err
			return
		}
		m.entries = append(m.entries, customFuncEntry{name: name, kind: kindFuncV1, funcV1: fn})
	}
}

// FuncV2 registers a custom function with typed parameters (V2 style).
// In schema: name(param1: value1, param2: value2)
func FuncV2(name string, spec *bloblang.PluginSpec, ctor bloblang.FunctionConstructorV2) FuncMapOption {
	return func(m *FuncMap) {
		if err := validateName(name); err != nil {
			m.err = err
			return
		}
		m.entries = append(m.entries, customFuncEntry{name: name, kind: kindFuncV2, spec: spec, funcV2: ctor})
	}
}

// Method registers a custom method (V1 style).
// In schema: this.field.name()
func Method(name string, fn bloblang.Method) FuncMapOption {
	return func(m *FuncMap) {
		if err := validateName(name); err != nil {
			m.err = err
			return
		}
		m.entries = append(m.entries, customFuncEntry{
			name: name, kind: kindMethodV1,
			methodV1: func(args ...any) (bloblang.Method, error) { return fn, nil },
		})
	}
}

// MethodV2 registers a custom method with typed parameters (V2 style).
// In schema: this.field.name(param1: value1, param2: value2)
func MethodV2(name string, spec *bloblang.PluginSpec, ctor bloblang.MethodConstructorV2) FuncMapOption {
	return func(m *FuncMap) {
		if err := validateName(name); err != nil {
			m.err = err
			return
		}
		m.entries = append(m.entries, customFuncEntry{name: name, kind: kindMethodV2, spec: spec, methodV2: ctor})
	}
}

// WithFuncMap injects a pre-built FuncMap into the Validator.
// If the FuncMap has a validation error (e.g. invalid name), New() will return that error.
func WithFuncMap(m *FuncMap) Option {
	return func(cfg *validatorConfig) {
		if m.err != nil {
			cfg.funcMapErr = m.err
			return
		}
		cfg.customFuncs = append(cfg.customFuncs, m.entries...)
	}
}

// FieldInfo describes a field in the schema. Returned by Validator.Fields().
// This is useful for generating documentation, API specs, or UI forms.
type FieldInfo struct {
	Name     string      `json:"name"`               // field name
	Path     string      `json:"path"`               // full dot-path
	Type     string      `json:"type"`               // "string", "int", "float", "bool", "struct", "list", "number", "unknown"
	Optional bool        `json:"optional"`           // whether the field is optional
	HasBlob  bool        `json:"has_blob"`           // has @blob() annotation
	Children []FieldInfo `json:"children,omitempty"` // nested struct fields
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
