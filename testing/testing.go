// Package testing (import path ".../schemix/testing", package name "schemix")
// provides a table-driven test helper for schemix.Validator.
//
// It is meant to be used from your own *_test.go files via an aliased import:
//
//	import schemixtest "github.com/mredencom/schemix/testing"
//
// or, since the package name is "schemix", side-by-side with the root
// package under a distinct local name to avoid collisions:
//
//	import (
//	    "github.com/mredencom/schemix"
//	    schemixtest "github.com/mredencom/schemix/testing"
//	)
//
// Example:
//
//	v := schemix.MustNew(`{
//	    pan:  =~"^[0-9]{16}$"
//	    luhn: bool @blob(this.pan.luhn_valid())
//	}`)
//
//	schemixtest.Test(t, v, []schemixtest.TestCase{
//	    {
//	        Name:      "valid visa",
//	        Input:     map[string]any{"pan": "4111111111111111"},
//	        WantValid: true,
//	    },
//	    {
//	        Name:     "invalid luhn",
//	        Input:    map[string]any{"pan": "4111111111111112"},
//	        WantCode: schemix.CodeBizRuleFailed,
//	    },
//	})
package schemix

import (
	"reflect"
	"testing"

	root "github.com/mredencom/schemix"
)

// TestCase describes a single table-driven test scenario for a Validator.
//
// Only the fields relevant to the scenario need to be set — Test only asserts
// on fields that are explicitly populated (see per-field doc comments for the
// exact "zero value means skip" rules).
type TestCase struct {
	// Name is the subtest name, passed to t.Run.
	Name string

	// Input is the data passed to Validator.Process (or ProcessWithMode when
	// Mode is set).
	Input map[string]any

	// Mode selects the FailMode used for this case. Zero value is
	// schemix.FailAll, which is also the default used by Validator.Process.
	Mode root.FailMode

	// WantValid asserts result.Valid == WantValid.
	//
	// This field is always checked — there is no way to "skip" it, since a
	// zero value (false) is itself a meaningful expectation. If you only care
	// about error codes, set WantValid: false explicitly (or leave it false,
	// which is the default and correct for invalid cases).
	WantValid bool

	// WantOutput, when non-nil, asserts result.Output deep-equals this value.
	// Leave nil to skip the Output assertion entirely (e.g. for invalid cases
	// where Output is always nil, or when you don't care about the exact
	// computed output).
	WantOutput map[string]any

	// WantCode, when non-empty, asserts that result.Errors contains at least
	// one error with this ErrorCode (via Result.HasCode). Leave empty to skip.
	WantCode root.ErrorCode

	// WantCodes, when non-empty, asserts that result.Errors contains at least
	// one error for every code listed (via Result.HasCode). Use this instead
	// of WantCode when a case is expected to fail more than one rule.
	WantCodes []root.ErrorCode

	// WantErrorCount, when non-nil, asserts len(result.Errors) == *WantErrorCount.
	// Use a pointer so zero (0 errors, i.e. valid case) can be asserted
	// explicitly without being confused with "not set".
	WantErrorCount *int

	// WantPath, when non-empty, asserts that result.Errors contains at least
	// one error at this field path (via Result.HasErrorsAt). Leave empty to skip.
	WantPath string

	// Check, when non-nil, is called with the resulting Result after all
	// built-in assertions above have run. Use it for assertions that don't
	// fit the declarative fields (e.g. inspecting a specific computed value,
	// or a custom error message check).
	Check func(t *testing.T, result root.Result)
}

// Test runs each TestCase as a subtest via t.Run(tc.Name, ...), processing
// tc.Input through v and asserting the declared expectations.
//
// It fails (via t.Errorf) rather than aborting (t.Fatalf) on individual
// mismatches, so a single subtest reports every violated expectation instead
// of stopping at the first one.
func Test(t *testing.T, v *root.Validator, cases []TestCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Helper()
			result := v.ProcessWithMode(tc.Input, tc.Mode)
			assertResult(t, tc, result)
		})
	}
}

// assertResult checks a single Result against a TestCase's expectations.
func assertResult(t *testing.T, tc TestCase, result root.Result) {
	t.Helper()

	if result.Valid != tc.WantValid {
		t.Errorf("Valid = %v, want %v (errors: %s)", result.Valid, tc.WantValid, result.ErrorMessages())
	}

	if tc.WantOutput != nil {
		if !reflect.DeepEqual(result.Output, tc.WantOutput) {
			t.Errorf("Output mismatch:\n got:  %#v\n want: %#v", result.Output, tc.WantOutput)
		}
	}

	if tc.WantCode != "" && !result.HasCode(tc.WantCode) {
		t.Errorf("expected error code %s not found; got errors: %s", tc.WantCode, result.ErrorMessages())
	}

	for _, code := range tc.WantCodes {
		if !result.HasCode(code) {
			t.Errorf("expected error code %s not found; got errors: %s", code, result.ErrorMessages())
		}
	}

	if tc.WantErrorCount != nil && len(result.Errors) != *tc.WantErrorCount {
		t.Errorf("error count = %d, want %d; errors: %s", len(result.Errors), *tc.WantErrorCount, result.ErrorMessages())
	}

	if tc.WantPath != "" && !result.HasErrorsAt(tc.WantPath) {
		t.Errorf("expected error at path %q not found; got errors: %s", tc.WantPath, result.ErrorMessages())
	}

	if tc.Check != nil {
		tc.Check(t, result)
	}
}
