package schemix

import (
	"fmt"
	"sync"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

// buildBlobEnv creates a Bloblang environment with built-in validators and
// any user-registered custom functions/methods.
//
// Optimization: built-in methods are registered once into a shared base
// environment (package-level sync.Once). When no custom functions are needed,
// the shared environment is returned directly (zero allocation). When custom
// functions exist, it clones the shared env and appends registrations.
//
// Conflict detection: if a user-registered name collides with a built-in
// method/function, an error is returned.
func buildBlobEnv(cfg *validatorConfig) (*bloblang.Environment, error) {
	base, err := getBaseEnv()
	if err != nil {
		return nil, err
	}

	// No custom functions — reuse shared environment directly (zero cost)
	if len(cfg.customFuncs) == 0 {
		return base, nil
	}

	// Check for conflicts with built-in names (skip if overrideAll)
	if !cfg.overrideAll {
		if err := checkBuiltinConflicts(cfg.customFuncs, cfg.allowMethodOverrides, cfg.allowFuncOverrides); err != nil {
			return nil, err
		}
	}

	// Clone the shared base and add custom registrations
	env := base.Clone()
	for _, entry := range cfg.customFuncs {
		var regErr error
		switch entry.kind {
		case kindFuncV1:
			regErr = env.RegisterFunction(entry.name, entry.funcV1)
		case kindFuncV2:
			regErr = env.RegisterFunctionV2(entry.name, entry.spec, entry.funcV2)
		case kindMethodV1:
			regErr = env.RegisterMethod(entry.name, entry.methodV1)
		case kindMethodV2:
			regErr = env.RegisterMethodV2(entry.name, entry.spec, entry.methodV2)
		}
		if regErr != nil {
			return nil, fmt.Errorf("register %q: %w", entry.name, regErr)
		}
	}
	return env, nil
}

// builtinMethodNames and builtinFuncNames hold built-in names by namespace.
var (
	builtinMethodNames = func() map[string]bool {
		names := make(map[string]bool)
		for _, m := range builtinMethods() {
			names[m.name] = true
		}
		return names
	}()

	builtinFuncNames = func() map[string]bool {
		names := make(map[string]bool)
		for _, f := range builtinFunctions() {
			names[f.name] = true
		}
		return names
	}()
)

// checkBuiltinConflicts returns an error if any user-registered name conflicts
// with a built-in in the SAME namespace, unless explicitly allowed.
func checkBuiltinConflicts(entries []customFuncEntry, allowedMethods, allowedFuncs []string) error {
	methodSet := make(map[string]bool, len(allowedMethods))
	for _, name := range allowedMethods {
		methodSet[name] = true
	}
	funcSet := make(map[string]bool, len(allowedFuncs))
	for _, name := range allowedFuncs {
		funcSet[name] = true
	}
	for _, e := range entries {
		switch e.kind {
		case kindMethodV1, kindMethodV2:
			if builtinMethodNames[e.name] && !methodSet[e.name] {
				return fmt.Errorf("method %q conflicts with a built-in validator; use WithOverrideMethod(%q) to allow", e.name, e.name)
			}
		case kindFuncV1, kindFuncV2:
			if builtinFuncNames[e.name] && !funcSet[e.name] {
				return fmt.Errorf("function %q conflicts with a built-in validator; use WithOverrideFunc(%q) to allow", e.name, e.name)
			}
		}
	}
	return nil
}

// baseEnv is the shared Bloblang environment with all schemix built-in methods
// pre-registered. Initialized once via sync.Once.
var (
	baseEnv     *bloblang.Environment
	baseEnvErr  error
	baseEnvOnce sync.Once
)

// getBaseEnv returns the shared base environment, initializing it on first call.
func getBaseEnv() (*bloblang.Environment, error) {
	baseEnvOnce.Do(func() {
		env := bloblang.NewEnvironment()
		baseEnvErr = registerBuiltins(env)
		if baseEnvErr == nil {
			baseEnv = env
		}
	})
	return baseEnv, baseEnvErr
}

// registerBuiltins registers all schemix built-in validation methods and functions
// into the given Bloblang environment.
func registerBuiltins(env *bloblang.Environment) error {
	for _, m := range builtinMethods() {
		if err := env.RegisterMethodV2(m.name, m.spec, m.ctor); err != nil {
			return err
		}
	}
	for _, f := range builtinFunctions() {
		if err := env.RegisterFunctionV2(f.name, f.spec, f.ctor); err != nil {
			return err
		}
	}
	return nil
}
