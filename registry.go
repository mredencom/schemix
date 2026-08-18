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

// globalBloblangEnvironment is a stable wrapper around Bento's process-global
// Bloblang environment. Bento returns a new wrapper from GlobalEnvironment on
// each call, so schemix must retain one pointer for ownership enforcement.
var globalBloblangEnvironment = bloblang.GlobalEnvironment()

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
	v, err := NewWithContext(r.cueCtx, cueSrc, WithName(name))
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

// parseFailMode converts a mode string to FailMode. Returns error for unrecognized values.
func parseFailMode(mode string) (FailMode, error) {
	switch mode {
	case ModeAll:
		return FailAll, nil
	case ModeFast:
		return FailFast, nil
	case ModePriority:
		return FailPriority, nil
	default:
		return 0, fmt.Errorf("invalid mode %q: must be %q, %q, or %q", mode, ModeAll, ModeFast, ModePriority)
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

// RegisterMethodsTo registers "validate_schema" and "process_schema" as
// Bloblang methods into a specific environment. Ownership is enforced:
// the same env cannot be registered by a different Registry.
func (r *Registry) RegisterMethodsTo(env *bloblang.Environment) error {
	if err := claimComponent(env, r, true, false); err != nil {
		return err
	}
	return r.registerMethodsTo(env)
}

// RegisterFunctionsTo registers "validate_schema" and "process_schema" as
// Bloblang functions into a specific environment. Ownership is enforced.
func (r *Registry) RegisterFunctionsTo(env *bloblang.Environment) error {
	if err := claimComponent(env, r, false, true); err != nil {
		return err
	}
	return r.registerFunctionsTo(env)
}

// RegisterAllTo registers both method and function forms into a specific
// environment. Ownership is enforced.
func (r *Registry) RegisterAllTo(env *bloblang.Environment) error {
	if err := claimComponent(env, r, true, true); err != nil {
		return err
	}
	if err := r.registerMethodsTo(env); err != nil {
		return err
	}
	return r.registerFunctionsTo(env)
}

// RegisterMethods registers "validate_schema" and "process_schema"
// Bloblang methods into the global environment.
//
// Deprecated: Use RegisterMethodsTo with an explicit environment.
func (r *Registry) RegisterMethods() error {
	return r.RegisterMethodsTo(globalBloblangEnvironment)
}

// registerMethodsTo performs the actual method registration into a given env.
func (r *Registry) registerMethodsTo(env *bloblang.Environment) error {
	// validate_schema method
	if err := env.RegisterMethodV2(pluginValidateSchema,
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
			mode, err := parseFailMode(modeStr)
			if err != nil {
				return nil, err
			}
			return bloblang.ObjectMethod(func(obj map[string]any) (any, error) {
				result := v.ProcessWithMode(obj, mode)
				return resultToMap(result.Valid, result.Errors, nil), nil
			}), nil
		},
	); err != nil {
		return fmt.Errorf("register %s method: %w", pluginValidateSchema, err)
	}

	// process_schema method
	if err := env.RegisterMethodV2(pluginProcessSchema,
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
			mode, err := parseFailMode(modeStr)
			if err != nil {
				return nil, err
			}
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
// as Bloblang functions into the global environment.
//
// Deprecated: Use RegisterFunctionsTo with an explicit environment.
func (r *Registry) RegisterFunctions() error {
	return r.RegisterFunctionsTo(globalBloblangEnvironment)
}

// registerFunctionsTo performs the actual function registration into a given env.
func (r *Registry) registerFunctionsTo(env *bloblang.Environment) error {
	// validate_schema function (advanced — supports dynamic data param)
	if err := env.RegisterAdvancedFunction(pluginValidateSchema,
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
			mode, err := parseFailMode(modeStr)
			if err != nil {
				return nil, err
			}
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
	if err := env.RegisterAdvancedFunction(pluginProcessSchema,
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
			mode, err := parseFailMode(modeStr)
			if err != nil {
				return nil, err
			}
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
// process_schema into the global environment.
//
// Deprecated: Use RegisterAllTo with an explicit environment.
func (r *Registry) RegisterAll() error {
	return r.RegisterAllTo(globalBloblangEnvironment)
}

// Plugin names registered with Bloblang.
const (
	pluginValidateSchema = "validate_schema"
	pluginProcessSchema  = "process_schema"
)

// Plugin spec metadata.
const categoryValidation = "Validation"

// Parameter names used in Bloblang plugin specs.
const (
	paramName = "name"
	paramMode = "mode"
	paramData = "data"
)

// Result map keys returned by validate_schema / process_schema.
const (
	keyValid   = "valid"
	keyErrors  = "errors"
	keyOutput  = "output"
	keyCode    = "code"
	keyPath    = "path"
	keyType    = "type"
	keyMessage = "message"
)
