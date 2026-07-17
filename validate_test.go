package schemix

import (
	"testing"
)

// TestValidate_NoOutputAllocation verifies that Validate() returns correct
// results for valid data without needing Output allocation.
func TestValidate_NoOutputAllocation(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int & >=0 & <=150
	}`)

	valid, errs := v.Validate(map[string]any{
		"name": "Alice",
		"age":  int64(30),
	})

	if !valid {
		t.Errorf("expected valid=true, got false")
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

// TestValidate_ReportsErrors verifies that Validate() correctly reports
// validation errors for invalid data.
func TestValidate_ReportsErrors(t *testing.T) {
	v := MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"
	}`)

	valid, errs := v.Validate(map[string]any{
		"pan":      "ABC",
		"amount":   int64(-1),
		"currency": "999",
	})

	if valid {
		t.Errorf("expected valid=false, got true")
	}
	if len(errs) == 0 {
		t.Errorf("expected errors, got none")
	}

	// Verify specific error paths are reported
	pathsSeen := map[string]bool{}
	for _, e := range errs {
		pathsSeen[e.Path] = true
	}
	for _, expected := range []string{"pan", "amount", "currency"} {
		if !pathsSeen[expected] {
			t.Errorf("expected error for path %q, not found in %v", expected, errs)
		}
	}
}

// TestValidate_WithBlob verifies that Validate() correctly handles @blob fields:
// - bool-returning @blob rules still validate (can fail)
// - non-bool (computed) @blob rules execute but Output is irrelevant since
//   Validate() doesn't return Output anyway.
func TestValidate_WithBlob(t *testing.T) {
	v := MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"

		pan_check:  bool   @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
		fee:        number @blob(if this.currency == "156" { 0 } else { (this.amount * 0.015).ceil() })
	}`)

	t.Run("valid data with blob", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"pan": "6222021234567890", "amount": int64(10000), "currency": "156",
		})
		if !valid {
			t.Errorf("expected valid=true, got false; errors: %v", errs)
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("blob bool validation fails", func(t *testing.T) {
		// PAN starts with "99" — neither "62" nor "4", so pan_check @blob returns false
		valid, errs := v.Validate(map[string]any{
			"pan": "9900021234567890", "amount": int64(10000), "currency": "156",
		})
		if valid {
			t.Errorf("expected valid=false, got true")
		}
		// Should have an error for pan_check
		found := false
		for _, e := range errs {
			if e.Path == "pan_check" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error for pan_check, got: %v", errs)
		}
	})

	t.Run("CUE validation fails with blob schema", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"pan": "INVALID", "amount": int64(-1), "currency": "999",
		})
		if valid {
			t.Errorf("expected valid=false, got true")
		}
		if len(errs) == 0 {
			t.Errorf("expected errors, got none")
		}
	})
}

// TestValidate_OriginalDataUnmodified ensures Validate() does not mutate the input.
func TestValidate_OriginalDataUnmodified(t *testing.T) {
	v := MustNew(`{
		name:   string
		upper:  string @blob(this.name.uppercase())
	}`)

	data := map[string]any{"name": "alice"}
	v.Validate(data)

	// Original data should be unchanged
	if data["name"] != "alice" {
		t.Errorf("input data was modified: name=%v", data["name"])
	}
	// No "upper" key should be injected into original data
	if _, exists := data["upper"]; exists {
		t.Errorf("Validate() should not inject computed fields into input data")
	}
}

// TestValidate_MetaConditionalRequired verifies @meta conditional rules work
// correctly through the Validate() path.
func TestValidate_MetaConditionalRequired(t *testing.T) {
	v := MustNew(`{
		payment_type: "credit" | "debit"
		cvv?: string @meta(conditional, required_if=this.payment_type == "credit")
	}`)

	t.Run("conditional required triggered", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"payment_type": "credit",
			// cvv is missing — should fail
		})
		if valid {
			t.Errorf("expected valid=false when cvv missing for credit")
		}
		found := false
		for _, e := range errs {
			if e.Path == "cvv" && e.Code == CodeCondRequired {
				found = true
			}
		}
		if !found {
			t.Errorf("expected CodeCondRequired for cvv, got: %v", errs)
		}
	})

	t.Run("conditional not triggered", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"payment_type": "debit",
			// cvv is missing — OK for debit
		})
		if !valid {
			t.Errorf("expected valid=true for debit without cvv, errors: %v", errs)
		}
	})
}

// BenchmarkValidate_NoDeepCopy compares Validate vs Process to show that
// Validate avoids the deepCopy overhead.
func BenchmarkValidate_NoDeepCopy(b *testing.B) {
	v := MustNew(benchSchema)

	b.Run("Process", func(b *testing.B) {
		for b.Loop() {
			v.Process(benchDataValid)
		}
	})

	b.Run("Validate", func(b *testing.B) {
		for b.Loop() {
			v.Validate(benchDataValid)
		}
	})
}

// BenchmarkValidate_NoDeepCopy_Nested compares with nested data to amplify the
// deepCopy savings.
func BenchmarkValidate_NoDeepCopy_Nested(b *testing.B) {
	v := MustNew(benchNestedSchema)

	b.Run("Process", func(b *testing.B) {
		for b.Loop() {
			v.Process(benchNestedData)
		}
	})

	b.Run("Validate", func(b *testing.B) {
		for b.Loop() {
			v.Validate(benchNestedData)
		}
	})
}
