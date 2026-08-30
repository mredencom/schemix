package schemix

import (
	"fmt"
	"slices"
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
//
// Construction options are forwarded to the validator, so a schema exposed to a
// Benthos pipeline can also use custom Bloblang functions, a metrics recorder,
// or a localizer:
//
//	reg.Register("payment", src, schemix.WithMethod("is_allowed_bin", fn))
//
// WithName is applied last and therefore cannot be overridden: the registry key
// is what labels metrics, so a validator filed under one name must not report
// itself under another.
//
// Nothing is stored if construction fails.
func (r *Registry) Register(name, cueSrc string, opts ...Option) error {
	v, err := NewWithContext(r.cueCtx, cueSrc, withRegistryName(name, opts)...)
	if err != nil {
		return fmt.Errorf("register %q: %w", name, err)
	}
	r.mu.Lock()
	r.validators[name] = v
	r.mu.Unlock()
	return nil
}

// withRegistryName returns opts followed by WithName(name), without writing
// into the caller's backing array.
//
// Appending to opts directly would be a bug: a caller passing a slice with
// spare capacity (reg.Register(n, src, myOpts...)) would have the element after
// myOpts silently overwritten. The explicit allocation is sized exactly, so it
// costs one allocation per registration — irrelevant next to compiling a
// schema.
func withRegistryName(name string, opts []Option) []Option {
	all := make([]Option, 0, len(opts)+1)
	all = append(all, opts...)
	return append(all, WithName(name))
}

// Put stores an already-constructed validator under the given name, replacing
// any existing entry.
//
// Register builds from CUE source, which leaves no way to file a validator that
// was built any other way — one from NewFromValue with shared definitions, or
// one sharing a FuncMap with its siblings:
//
//	v, _ := schemix.NewFromValue(schema)
//	reg.Put("composed", v)
//
// Unlike Register, Put cannot make the validator's metrics label agree with the
// registry key, because a Validator is immutable once constructed. Pass
// WithName yourself if the two need to match.
//
// A nil validator is rejected: storing one would make Get return (nil, true),
// and the Bloblang plugins call what Get returns without a nil check, so the
// panic would surface inside a pipeline rather than here.
func (r *Registry) Put(name string, v *Validator) error {
	if v == nil {
		return fmt.Errorf("put %q: nil validator", name)
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
// List returns the names of all registered schemas, sorted lexicographically.
//
// The order is guaranteed so that callers can display, diff, or assert on the
// result directly. Without it the names came straight out of the internal map
// and varied between calls.
func (r *Registry) List() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.validators))
	for name := range r.validators {
		names = append(names, name)
	}
	r.mu.RUnlock()
	slices.Sort(names)
	return names
}

// Len returns the number of registered validators.
func (r *Registry) Len() int {
	r.mu.RLock()
	n := len(r.validators)
	r.mu.RUnlock()
	return n
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
				result := v.processMap(obj, mode)
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
				result := v.processMap(obj, mode)
				return resultToMap(result.Valid, result.Errors, result.Output), nil
			}), nil
		},
	); err != nil {
		return fmt.Errorf("register %s method: %w", pluginProcessSchema, err)
	}

	return nil
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
				result := v.processMap(obj, mode)
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
				result := v.processMap(obj, mode)
				return resultToMap(result.Valid, result.Errors, result.Output), nil
			}, nil
		},
	); err != nil {
		return fmt.Errorf("register %s function: %w", pluginProcessSchema, err)
	}

	return nil
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

// registrationOwner tracks which Registry has registered plugins on a given
// *bloblang.Environment, and which component types (methods/functions) are claimed.
type registrationOwner struct {
	registry  *Registry
	methods   bool // true = methods already registered by this registry
	functions bool // true = functions already registered by this registry
}

var (
	ownerMu  sync.Mutex
	ownerMap = make(map[*bloblang.Environment]*registrationOwner)
)

// claimComponent checks and records that reg owns the given component (methods/functions)
// on env. Rules:
//   - First registration on env: claim succeeds
//   - Same Registry + same env + different component: merge (allows methods then functions)
//   - Same Registry + same env + same component already registered: error (duplicate)
//   - Different Registry + same env + any overlap: error (conflict)
func claimComponent(env *bloblang.Environment, reg *Registry, wantMethods, wantFunctions bool) error {
	if env == nil {
		return fmt.Errorf("schemix: bloblang environment must not be nil")
	}
	ownerMu.Lock()
	defer ownerMu.Unlock()

	existing, ok := ownerMap[env]
	if !ok {
		ownerMap[env] = &registrationOwner{registry: reg, methods: wantMethods, functions: wantFunctions}
		return nil
	}
	if existing.registry != reg {
		return fmt.Errorf("schemix: bloblang environment already owned by another Registry")
	}
	// Same registry — check for duplicate component registration
	if wantMethods && existing.methods {
		return fmt.Errorf("schemix: methods already registered on this environment by this Registry")
	}
	if wantFunctions && existing.functions {
		return fmt.Errorf("schemix: functions already registered on this environment by this Registry")
	}
	// Merge — same registry registering the other component
	if wantMethods {
		existing.methods = true
	}
	if wantFunctions {
		existing.functions = true
	}
	return nil
}

// releaseEnv removes ownership. For testing teardown.
func releaseEnv(env *bloblang.Environment) {
	ownerMu.Lock()
	delete(ownerMap, env)
	ownerMu.Unlock()
}
