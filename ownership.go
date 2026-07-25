package schemix

import (
	"fmt"
	"sync"

	"github.com/warpstreamlabs/bento/public/bloblang"
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
