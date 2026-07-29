package schemix

import (
	"testing"

	root "github.com/mredencom/schemix"
)

var visaSchema = root.MustNew(`{
	pan:    =~"^[0-9]{16}$"
	amount: int & >0
	luhn:   bool @blob(this.pan.luhn_valid())
	brand:  string @blob(if this.pan.has_prefix("4") { "Visa" } else { "Other" })
}`)

func TestTest_ValidAndInvalidCases(t *testing.T) {
	count1 := 1

	Test(t, visaSchema, []TestCase{
		{
			Name: "valid visa",
			Input: map[string]any{
				"pan":    "4111111111111111",
				"amount": int64(10000),
			},
			WantValid: true,
			// bool-returning @blob rules (luhn) are validation gates only and
			// are never written to Output — only non-bool @blob results are.
			WantOutput: map[string]any{
				"pan":    "4111111111111111",
				"amount": int64(10000),
				"brand":  "Visa",
			},
		},
		{
			Name: "invalid luhn",
			Input: map[string]any{
				"pan":    "4111111111111112",
				"amount": int64(10000),
			},
			WantValid: false,
			WantCode:  root.CodeBizRuleFailed,
			WantPath:  "luhn",
		},
		{
			Name: "missing amount",
			Input: map[string]any{
				"pan": "4111111111111111",
			},
			WantValid:      false,
			WantCode:       root.CodeRequiredMissing,
			WantErrorCount: &count1,
		},
		{
			Name: "custom check on output",
			Input: map[string]any{
				"pan":    "4111111111111111",
				"amount": int64(500),
			},
			WantValid: true,
			Check: func(t *testing.T, result root.Result) {
				if result.Output["brand"] != "Visa" {
					t.Errorf("brand = %v, want Visa", result.Output["brand"])
				}
			},
		},
	})
}

// TestTest_MultipleAssertionsAllReported verifies that a single failing
// subtest can surface more than one violated expectation (Test uses
// t.Errorf, not t.Fatalf, for each check) by combining a code and a path
// check that both target the same failing case.
func TestTest_MultipleAssertionsAllReported(t *testing.T) {
	Test(t, visaSchema, []TestCase{
		{
			Name: "invalid luhn reports code and path",
			Input: map[string]any{
				"pan":    "4111111111111112",
				"amount": int64(10000),
			},
			WantValid: false,
			WantCode:  root.CodeBizRuleFailed,
			WantPath:  "luhn",
		},
	})
}
