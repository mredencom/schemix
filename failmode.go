package schemix

import (
	"fmt"
)

// validateFailMode returns an error if mode is not a recognized FailMode value.
func validateFailMode(mode FailMode) error {
	switch mode {
	case FailAll, FailFast, FailPriority:
		return nil
	default:
		return fmt.Errorf("invalid FailMode(%d): must be FailAll, FailFast, or FailPriority", int(mode))
	}
}

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
