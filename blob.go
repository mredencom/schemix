package schemix

import (
	"fmt"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// Registry is a thread-safe validator registry for use with Bloblang methods.
type Registry struct {
	mu         sync.RWMutex
	validators map[string]*Validator
	cueCtx     *cue.Context // shared CUE context for all validators in this registry
}

// NewRegistry creates an empty validator registry with a shared CUE context.
func NewRegistry() *Registry {
	return &Registry{
		validators: make(map[string]*Validator),
		cueCtx:     cuecontext.New(),
	}
}

// Register compiles and stores a named validator from a CUE schema string.
// It uses the registry's shared CUE context for efficient memory usage.
func (r *Registry) Register(name, cueSrc string) error {
	v, err := NewWithContext(r.cueCtx, cueSrc)
	if err != nil {
		return fmt.Errorf("register %q: %w", name, err)
	}
	r.mu.Lock()
	r.validators[name] = v
	r.mu.Unlock()
	return nil
}

// Get retrieves a validator by name.
func (r *Registry) Get(name string) (*Validator, bool) {
	r.mu.RLock()
	v, ok := r.validators[name]
	r.mu.RUnlock()
	return v, ok
}

// Has reports whether a validator with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.validators[name]
	r.mu.RUnlock()
	return ok
}

// Unregister removes a named validator from the registry.
// Returns true if the validator existed and was removed.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	_, ok := r.validators[name]
	if ok {
		delete(r.validators, name)
	}
	r.mu.Unlock()
	return ok
}

// List returns the names of all registered validators.
func (r *Registry) List() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.validators))
	for name := range r.validators {
		names = append(names, name)
	}
	r.mu.RUnlock()
	return names
}

// Len returns the number of registered validators.
func (r *Registry) Len() int {
	r.mu.RLock()
	n := len(r.validators)
	r.mu.RUnlock()
	return n
}

// parseFailMode converts a mode string to FailMode. Defaults to FailAll.
func parseFailMode(mode string) FailMode {
	switch mode {
	case ModeFast:
		return FailFast
	case ModePriority:
		return FailPriority
	default:
		return FailAll
	}
}

// resultToMap converts validation/process results to a bloblang-friendly map.
func resultToMap(valid bool, errs []ValidationError, output map[string]any) map[string]any {
	errList := make([]any, 0, len(errs))
	for _, e := range errs {
		errList = append(errList, map[string]any{
			keyCode: string(e.Code), keyPath: e.Path, keyType: e.Type, keyMessage: e.Message,
		})
	}
	m := map[string]any{keyValid: valid, keyErrors: errList}
	if output != nil {
		m[keyOutput] = output
	}
	return m
}

// RegisterMethods registers "validate_schema" and "process_schema"
// Bloblang methods that use validators from this registry.
//
// Usage in Bloblang mappings:
//
//	let r = this.validate_schema(name: "payment")
//	let r = this.validate_schema(name: "payment", mode: "fast")
//	let r = this.process_schema(name: "payment")
//	let r = this.process_schema(name: "payment", mode: "priority")
//
// Supported mode values: "all" (default), "fast", "priority".
func (r *Registry) RegisterMethods() error {
	// validate_schema method
	if err := bloblang.RegisterMethodV2(pluginValidateSchema,
		bloblang.NewPluginSpec().
			Category(categoryValidation).
			Description("Validate data using a registered CUE+Bloblang schema").
			Param(bloblang.NewStringParam(paramName).Description("validator name")).
			Param(bloblang.NewStringParam(paramMode).Description("fail mode: all, fast, priority").Default(ModeAll)),
		func(args *bloblang.ParsedParams) (bloblang.Method, error) {
			name, err := args.GetString(paramName)
			if err != nil {
				return nil, err
			}
			modeStr, err := args.GetString(paramMode)
			if err != nil {
				return nil, err
			}
			v, ok := r.Get(name)
			if !ok {
				return nil, fmt.Errorf("validator %q not registered", name)
			}
			mode := parseFailMode(modeStr)
			return bloblang.ObjectMethod(func(obj map[string]any) (any, error) {
				result := v.ProcessWithMode(obj, mode)
				return resultToMap(result.Valid, result.Errors, nil), nil
			}), nil
		},
	); err != nil {
		return fmt.Errorf("register %s method: %w", pluginValidateSchema, err)
	}

	// process_schema method
	if err := bloblang.RegisterMethodV2(pluginProcessSchema,
		bloblang.NewPluginSpec().
			Category(categoryValidation).
			Description("Validate and compute values using a registered CUE+Bloblang schema").
			Param(bloblang.NewStringParam(paramName).Description("validator name")).
			Param(bloblang.NewStringParam(paramMode).Description("fail mode: all, fast, priority").Default(ModeAll)),
		func(args *bloblang.ParsedParams) (bloblang.Method, error) {
			name, err := args.GetString(paramName)
			if err != nil {
				return nil, err
			}
			modeStr, err := args.GetString(paramMode)
			if err != nil {
				return nil, err
			}
			v, ok := r.Get(name)
			if !ok {
				return nil, fmt.Errorf("validator %q not registered", name)
			}
			mode := parseFailMode(modeStr)
			return bloblang.ObjectMethod(func(obj map[string]any) (any, error) {
				result := v.ProcessWithMode(obj, mode)
				return resultToMap(result.Valid, result.Errors, result.Output), nil
			}), nil
		},
	); err != nil {
		return fmt.Errorf("register %s method: %w", pluginProcessSchema, err)
	}

	return nil
}

// RegisterFunctions registers "validate_schema" and "process_schema"
// as Bloblang functions (not methods). This allows calling them without a
// target value:
//
//	let r = validate_schema(data: this, name: "payment")
//	let r = validate_schema(data: this, name: "payment", mode: "fast")
//	let r = process_schema(data: this, name: "payment")
//	let r = process_schema(data: this, name: "payment", mode: "priority")
//
// The data parameter is dynamically evaluated on each invocation, so expressions
// like "this.payload" work correctly.
// Supported mode values: "all" (default), "fast", "priority".
func (r *Registry) RegisterFunctions() error {
	// validate_schema function (advanced — supports dynamic data param)
	if err := bloblang.RegisterAdvancedFunction(pluginValidateSchema,
		bloblang.NewPluginSpec().
			Category(categoryValidation).
			Description("Validate data using a registered CUE+Bloblang schema (function form)").
			Param(bloblang.NewQueryParam(paramData, true).Description("data object to validate")).
			Param(bloblang.NewStringParam(paramName).Description("validator name")).
			Param(bloblang.NewStringParam(paramMode).Description("fail mode: all, fast, priority").Default(ModeAll)),
		func(args *bloblang.ParsedParams) (bloblang.AdvancedFunction, error) {
			dataFn, err := args.GetQuery(paramData)
			if err != nil {
				return nil, err
			}
			name, err := args.GetString(paramName)
			if err != nil {
				return nil, err
			}
			modeStr, err := args.GetString(paramMode)
			if err != nil {
				return nil, err
			}
			v, ok := r.Get(name)
			if !ok {
				return nil, fmt.Errorf("validator %q not registered", name)
			}
			mode := parseFailMode(modeStr)
			return func(ctx *bloblang.ExecContext) (any, error) {
				dataRaw, err := ctx.Exec(dataFn)
				if err != nil {
					return nil, fmt.Errorf("%s: failed to evaluate data param: %w", pluginValidateSchema, err)
				}
				obj, ok := dataRaw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: data param must be an object, got %T", pluginValidateSchema, dataRaw)
				}
				result := v.ProcessWithMode(obj, mode)
				return resultToMap(result.Valid, result.Errors, nil), nil
			}, nil
		},
	); err != nil {
		return fmt.Errorf("register %s function: %w", pluginValidateSchema, err)
	}

	// process_schema function (advanced — supports dynamic data param)
	if err := bloblang.RegisterAdvancedFunction(pluginProcessSchema,
		bloblang.NewPluginSpec().
			Category(categoryValidation).
			Description("Validate and compute values using a registered CUE+Bloblang schema (function form)").
			Param(bloblang.NewQueryParam(paramData, true).Description("data object to validate")).
			Param(bloblang.NewStringParam(paramName).Description("validator name")).
			Param(bloblang.NewStringParam(paramMode).Description("fail mode: all, fast, priority").Default(ModeAll)),
		func(args *bloblang.ParsedParams) (bloblang.AdvancedFunction, error) {
			dataFn, err := args.GetQuery(paramData)
			if err != nil {
				return nil, err
			}
			name, err := args.GetString(paramName)
			if err != nil {
				return nil, err
			}
			modeStr, err := args.GetString(paramMode)
			if err != nil {
				return nil, err
			}
			v, ok := r.Get(name)
			if !ok {
				return nil, fmt.Errorf("validator %q not registered", name)
			}
			mode := parseFailMode(modeStr)
			return func(ctx *bloblang.ExecContext) (any, error) {
				dataRaw, err := ctx.Exec(dataFn)
				if err != nil {
					return nil, fmt.Errorf("%s: failed to evaluate data param: %w", pluginProcessSchema, err)
				}
				obj, ok := dataRaw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: data param must be an object, got %T", pluginProcessSchema, dataRaw)
				}
				result := v.ProcessWithMode(obj, mode)
				return resultToMap(result.Valid, result.Errors, result.Output), nil
			}, nil
		},
	); err != nil {
		return fmt.Errorf("register %s function: %w", pluginProcessSchema, err)
	}

	return nil
}

// RegisterAll registers both method and function forms of validate_schema and
// process_schema. This is a convenience method combining RegisterMethods
// and RegisterFunctions.
func (r *Registry) RegisterAll() error {
	if err := r.RegisterMethods(); err != nil {
		return err
	}
	return r.RegisterFunctions()
}
