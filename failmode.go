package schemix

import "fmt"

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
		return 0, fmt.Errorf("invalid mode %q: must be %q, %q, or %q", mode, ModeAll, ModeFast, ModePriority)
	}
}

// failModeString converts FailMode to a label string.
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

// validateFailMode returns an error if mode is not a recognized FailMode value.
func validateFailMode(mode FailMode) error {
	switch mode {
	case FailAll, FailFast, FailPriority:
		return nil
	default:
		return fmt.Errorf("invalid FailMode(%d): must be FailAll, FailFast, or FailPriority", int(mode))
	}
}
