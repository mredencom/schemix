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
