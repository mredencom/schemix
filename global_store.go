package schemix

import (
	"fmt"
	"slices"
	"sync"
)

// The package-level store below is deprecated in favour of Registry.
//
// A process-global registry cannot be scoped to a test, an environment, or a
// tenant: two components sharing this process share one namespace, and a name
// collision between them is silent. Registry makes the ownership explicit, and
// Registry.Put accepts an already-constructed Validator, which is what the
// package-level Register did.
//
// globalStore uses sync.Map for lock-free reads in the common case.
// This is ideal because validators are registered at init/startup (few writes)
// and looked up per-request (many concurrent reads).
var globalStore sync.Map // map[string]*Validator

// Register stores a pre-compiled Validator globally under name.
// If name already exists, it is silently replaced (use MustRegister to reject duplicates).
//
// Deprecated: Use a Registry — reg := schemix.NewRegistry(); reg.Put(name, v).
// Removed in v0.3.0.
func Register(name string, v *Validator) {
	globalStore.Store(name, v)
}

// MustRegister stores a pre-compiled Validator globally under name.
// Panics if name already exists (conflict).
//
// Deprecated: Use a Registry — check reg.Has(name), then reg.Put(name, v).
// Removed in v0.3.0.
func MustRegister(name string, v *Validator) {
	if _, loaded := globalStore.LoadOrStore(name, v); loaded {
		panic(fmt.Sprintf("schemix.MustRegister: %q already registered", name))
	}
}

// Get retrieves a globally registered Validator by name.
// Returns (nil, false) if name is not registered.
//
// Deprecated: Use a Registry — reg.Get(name). Removed in v0.3.0.
func Get(name string) (*Validator, bool) {
	val, ok := globalStore.Load(name)
	if !ok {
		return nil, false
	}
	return val.(*Validator), true
}

// MustGet retrieves a globally registered Validator by name.
// Panics if name is not registered.
//
// Deprecated: Use a Registry — reg.Get(name), and panic yourself if the
// lookup misses. Removed in v0.3.0.
func MustGet(name string) *Validator {
	v, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("schemix.MustGet: %q not registered", name))
	}
	return v
}

// Unregister removes a globally registered Validator by name.
// Returns true if it existed and was removed.
//
// Deprecated: Use a Registry — reg.Unregister(name). Removed in v0.3.0.
func Unregister(name string) bool {
	_, loaded := globalStore.LoadAndDelete(name)
	return loaded
}

// Has reports whether a Validator with the given name is globally registered.
//
// Deprecated: Use a Registry — reg.Has(name). Removed in v0.3.0.
func Has(name string) bool {
	_, ok := globalStore.Load(name)
	return ok
}

// List returns the names of all globally registered Validators, sorted
// lexicographically.
//
// The order is guaranteed so that callers can display, diff, or assert on the
// result directly. sync.Map.Range visits entries in an unspecified order, so
// without the sort the result varied between calls.
//
// Deprecated: Use a Registry — reg.List(). Removed in v0.3.0.
func List() []string {
	var names []string
	globalStore.Range(func(key, _ any) bool {
		names = append(names, key.(string))
		return true
	})
	slices.Sort(names)
	return names
}

// Len returns the number of globally registered Validators.
//
// Deprecated: Use a Registry — reg.Len(). Removed in v0.3.0.
func Len() int {
	n := 0
	globalStore.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// ProcessWith validates and processes data using a globally registered Validator.
// Accepts any supported input type (map, struct, JSON bytes, Processable).
// Returns a config error if the name is not registered.
//
// Deprecated: Use a Registry — v, ok := reg.Get(name), then v.ProcessValue(data)
// (v.Process(data) from v0.3.0). Removed in v0.3.0.
func ProcessWith(name string, data any) Result {
	return ProcessWithMode(name, data, FailAll)
}

// ProcessWithMode validates and processes data using a named global Validator with mode.
//
// Deprecated: this shares a name with the Validator method while taking a name
// where that takes data, so a mixed-up call still compiles. Use a Registry —
// v, ok := reg.Get(name), then v.ProcessValueWithMode(data, mode)
// (v.Process(data, mode) from v0.3.0). Removed in v0.3.0.
func ProcessWithMode(name string, data any, mode FailMode) Result {
	v, ok := Get(name)
	if !ok {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: fmt.Sprintf("schemix: validator %q not registered", name)}},
		}
	}
	return v.ProcessValueWithMode(data, mode)
}

// ValidateWith validates data using a globally registered Validator (no Output).
//
// Deprecated: Use a Registry — v, ok := reg.Get(name), then v.ValidateValue(data)
// (v.Validate(data) from v0.3.0). Removed in v0.3.0.
func ValidateWith(name string, data any) (bool, []ValidationError) {
	v, ok := Get(name)
	if !ok {
		return false, []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: fmt.Sprintf("schemix: validator %q not registered", name)}}
	}
	return v.ValidateValue(data)
}

// resetGlobalStore clears the global store. For testing only.
func resetGlobalStore() {
	globalStore.Range(func(key, _ any) bool {
		globalStore.Delete(key)
		return true
	})
}
