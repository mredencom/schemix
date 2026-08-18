package schemix

import (
	"fmt"
	"regexp"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

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
