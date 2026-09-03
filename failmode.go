package schemix

import "fmt"

// FailMode controls how errors are collected during validation.
//
// Use the FailAll, FailFast and FailPriority values; the type cannot be
// constructed from outside this package, because its only field is unexported.
// That is deliberate. An undefined mode used to come back as an invalid Result
// carrying E0C01, so a handler mapping invalid results to 422 reported a
// server-side configuration bug as the caller's fault. There is now no way to
// reach that state.
//
// FailAll is the zero value, so an unset FailMode field means "collect
// everything" — schemixtest relies on this.
type FailMode struct{ n uint8 }

var (
	// FailAll collects all errors before returning (default, good for forms).
	FailAll = FailMode{}
	// FailFast stops at the first error (good for gateways).
	FailFast = FailMode{n: 1}
	// FailPriority stops when the current priority group has errors.
	FailPriority = FailMode{n: 2}
)

// Mode string values for FailMode selection (user-facing).
const (
	ModeAll      = "all"
	ModeFast     = "fast"
	ModePriority = "priority"
)

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
		return FailAll, fmt.Errorf("invalid mode %q: must be %q, %q, or %q", mode, ModeAll, ModeFast, ModePriority)
	}
}

// failModeString converts FailMode to a label string.
//
// The default arm is unreachable from outside the package but kept as a
// defensive label for metrics: an internal mistake should show up as "unknown"
// in a dashboard rather than as a mode that was never selected.
func failModeString(mode FailMode) string {
	switch mode {
	case FailAll:
		return ModeAll
	case FailFast:
		return ModeFast
	case FailPriority:
		return ModePriority
	default:
		return "unknown"
	}
}
