package schemix

import (
	"fmt"
	"sync"
)

// globalStore uses sync.Map for lock-free reads in the common case.
// This is ideal because validators are registered at init/startup (few writes)
// and looked up per-request (many concurrent reads).
var globalStore sync.Map // map[string]*Validator

// Register stores a pre-compiled Validator globally under name.
// If name already exists, it is silently replaced (use MustRegister to reject duplicates).
func Register(name string, v *Validator) {
	globalStore.Store(name, v)
}

// MustRegister stores a pre-compiled Validator globally under name.
// Panics if name already exists (conflict).
func MustRegister(name string, v *Validator) {
	if _, loaded := globalStore.LoadOrStore(name, v); loaded {
		panic(fmt.Sprintf("schemix.MustRegister: %q already registered", name))
	}
}

// Get retrieves a globally registered Validator by name.
// Returns (nil, false) if name is not registered.
func Get(name string) (*Validator, bool) {
	val, ok := globalStore.Load(name)
	if !ok {
		return nil, false
	}
	return val.(*Validator), true
}

// MustGet retrieves a globally registered Validator by name.
// Panics if name is not registered.
func MustGet(name string) *Validator {
	v, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("schemix.MustGet: %q not registered", name))
	}
	return v
}

// Unregister removes a globally registered Validator by name.
// Returns true if it existed and was removed.
func Unregister(name string) bool {
	_, loaded := globalStore.LoadAndDelete(name)
	return loaded
}

// Has reports whether a Validator with the given name is globally registered.
func Has(name string) bool {
	_, ok := globalStore.Load(name)
	return ok
}

// List returns the names of all globally registered Validators.
func List() []string {
	var names []string
	globalStore.Range(func(key, _ any) bool {
		names = append(names, key.(string))
		return true
	})
	return names
}

// Len returns the number of globally registered Validators.
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
func ProcessWith(name string, data any) Result {
	return ProcessWithMode(name, data, FailAll)
}

// ProcessWithMode validates and processes data using a named global Validator with mode.
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
