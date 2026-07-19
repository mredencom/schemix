package schemix

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

// ========== Required Field Detection ==========

// TestRequiredField_MissingShouldFail verifies that missing required fields
// produce CodeRequiredMissing (E1M01) errors.
func TestRequiredField_MissingShouldFail(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int
	}`)

	// Missing "age" — should fail
	r := v.Process(map[string]any{"name": "Alice"})
	if r.Valid {
		t.Fatal("expected invalid result when required field 'age' is missing")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing && e.Path == "age" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected E1M01 error on path 'age', got errors: %v", r.Errors)
	}
}

// TestRequiredField_AllMissingShouldFail verifies that ALL missing required
// fields are reported in FailAll mode.
func TestRequiredField_AllMissingShouldFail(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int
	}`)

	// Both fields missing
	r := v.ProcessWithMode(map[string]any{}, FailAll)
	if r.Valid {
		t.Fatal("expected invalid result when all required fields are missing")
	}

	paths := map[string]bool{}
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing {
			paths[e.Path] = true
		}
	}
	if !paths["name"] {
		t.Error("expected E1M01 for 'name'")
	}
	if !paths["age"] {
		t.Error("expected E1M01 for 'age'")
	}
}

// TestRequiredField_OptionalShouldNotError verifies that optional fields
// (marked with ?) do NOT produce errors when missing.
func TestRequiredField_OptionalShouldNotError(t *testing.T) {
	v := MustNew(`{
		name:  string
		memo?: string
	}`)

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Errorf("expected valid result when optional field is missing, got errors: %v", r.Errors)
	}
}

// TestRequiredField_BlobShouldNotError verifies that @blob fields do NOT
// produce required-missing errors (they are computed, not user-supplied).
func TestRequiredField_BlobShouldNotError(t *testing.T) {
	v := MustNew(`{
		amount:   int & >0
		doubled:  number @blob(this.amount * 2)
	}`)

	r := v.Process(map[string]any{"amount": int64(100)})
	if !r.Valid {
		t.Errorf("expected valid result — @blob fields are computed, got errors: %v", r.Errors)
	}
}

// TestRequiredField_NestedStructMissing verifies that required fields
// inside nested structs are also detected.
func TestRequiredField_NestedStructMissing(t *testing.T) {
	v := MustNew(`{
		name: string
		address: {
			city:    string
			country: string
		}
	}`)

	// address present but city is missing
	r := v.Process(map[string]any{
		"name":    "Alice",
		"address": map[string]any{"country": "CN"},
	})
	if r.Valid {
		t.Fatal("expected invalid when nested required field 'address.city' is missing")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing && e.Path == "address.city" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected E1M01 on 'address.city', got errors: %v", r.Errors)
	}
}

// TestRequiredField_NullableFieldAllowsNil verifies that a field declared as
// `null | string` does NOT report required-missing when nil is passed.
func TestRequiredField_NullableFieldAllowsNil(t *testing.T) {
	v := MustNew(`{
		name:  string
		memo:  null | string
	}`)

	// memo explicitly set to nil — should be valid (nullable schema)
	r := v.Process(map[string]any{"name": "Alice", "memo": nil})
	if !r.Valid {
		t.Errorf("expected valid for nullable field with nil value, got errors: %v", r.Errors)
	}
}

// TestRequiredField_FailFastStopsAtFirst verifies FailFast mode stops
// after the first required-missing error.
func TestRequiredField_FailFastStopsAtFirst(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int
		city: string
	}`)

	r := v.ProcessWithMode(map[string]any{}, FailFast)
	if r.Valid {
		t.Fatal("expected invalid in FailFast with all fields missing")
	}
	if len(r.Errors) != 1 {
		t.Errorf("FailFast should produce exactly 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
	if r.Errors[0].Code != CodeRequiredMissing {
		t.Errorf("expected CodeRequiredMissing, got %s", r.Errors[0].Code)
	}
}

// TestRequiredField_ErrorFields verifies the error structure fields.
func TestRequiredField_ErrorFields(t *testing.T) {
	v := MustNew(`{
		username: string
	}`)

	r := v.Process(map[string]any{})
	if r.Valid {
		t.Fatal("expected invalid")
	}
	if len(r.Errors) == 0 {
		t.Fatal("expected at least 1 error")
	}

	e := r.Errors[0]
	if e.Code != CodeRequiredMissing {
		t.Errorf("Code: want %s, got %s", CodeRequiredMissing, e.Code)
	}
	if e.Path != "username" {
		t.Errorf("Path: want 'username', got %q", e.Path)
	}
	if e.Type != TypeCUE {
		t.Errorf("Type: want %q, got %q", TypeCUE, e.Type)
	}
	if e.Message == "" {
		t.Error("Message should not be empty")
	}
}

// ========== Validate() Fast Path ==========

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

// ========== Result Chain API ==========

func TestResult_HasCode(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeFormatMismatch, Path: "pan", Type: "cue", Message: "format mismatch"},
			{Code: CodeBizRuleFailed, Path: "luhn_check", Type: "bloblang", Message: "rule failed"},
		},
	}

	if !r.HasCode(CodeFormatMismatch) {
		t.Error("expected HasCode(CodeFormatMismatch) = true")
	}
	if !r.HasCode(CodeBizRuleFailed) {
		t.Error("expected HasCode(CodeBizRuleFailed) = true")
	}
	if r.HasCode(CodeTypeMismatch) {
		t.Error("expected HasCode(CodeTypeMismatch) = false")
	}
	if r.HasCode(CodeCondRequired) {
		t.Error("expected HasCode(CodeCondRequired) = false")
	}
}

func TestResult_ErrorsByCode(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeFormatMismatch, Path: "pan", Type: "cue", Message: "format mismatch"},
			{Code: CodeFormatMismatch, Path: "email", Type: "cue", Message: "format mismatch"},
			{Code: CodeBizRuleFailed, Path: "luhn_check", Type: "bloblang", Message: "rule failed"},
			{Code: CodeTypeMismatch, Path: "amount", Type: "cue", Message: "type error"},
		},
	}

	got := r.ErrorsByCode(CodeFormatMismatch)
	if len(got) != 2 {
		t.Fatalf("expected 2 errors with CodeFormatMismatch, got %d", len(got))
	}
	if got[0].Path != "pan" || got[1].Path != "email" {
		t.Errorf("unexpected paths: %v, %v", got[0].Path, got[1].Path)
	}

	got = r.ErrorsByCode(CodeBizRuleFailed)
	if len(got) != 1 {
		t.Fatalf("expected 1 error with CodeBizRuleFailed, got %d", len(got))
	}
	if got[0].Path != "luhn_check" {
		t.Errorf("expected path luhn_check, got %s", got[0].Path)
	}

	got = r.ErrorsByCode(CodeCondRequired)
	if len(got) != 0 {
		t.Fatalf("expected 0 errors with CodeCondRequired, got %d", len(got))
	}
}

func TestResult_ErrorsByType(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeFormatMismatch, Path: "pan", Type: "cue", Message: "format mismatch"},
			{Code: CodeTypeMismatch, Path: "amount", Type: "cue", Message: "type error"},
			{Code: CodeBizRuleFailed, Path: "luhn_check", Type: "bloblang", Message: "rule failed"},
			{Code: CodeCondRequired, Path: "cvv", Type: "meta", Message: "conditionally required"},
		},
	}

	got := r.ErrorsByType("cue")
	if len(got) != 2 {
		t.Fatalf("expected 2 cue errors, got %d", len(got))
	}

	got = r.ErrorsByType("bloblang")
	if len(got) != 1 {
		t.Fatalf("expected 1 bloblang error, got %d", len(got))
	}
	if got[0].Path != "luhn_check" {
		t.Errorf("expected path luhn_check, got %s", got[0].Path)
	}

	got = r.ErrorsByType("meta")
	if len(got) != 1 {
		t.Fatalf("expected 1 meta error, got %d", len(got))
	}
	if got[0].Path != "cvv" {
		t.Errorf("expected path cvv, got %s", got[0].Path)
	}

	got = r.ErrorsByType("unknown")
	if len(got) != 0 {
		t.Fatalf("expected 0 errors for unknown type, got %d", len(got))
	}
}

func TestResult_HasErrorsAt(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeFormatMismatch, Path: "pan", Type: "cue", Message: "format mismatch"},
			{Code: CodeBizRuleFailed, Path: "luhn_check", Type: "bloblang", Message: "rule failed"},
		},
	}

	if !r.HasErrorsAt("pan") {
		t.Error("expected HasErrorsAt(pan) = true")
	}
	if !r.HasErrorsAt("luhn_check") {
		t.Error("expected HasErrorsAt(luhn_check) = true")
	}
	if r.HasErrorsAt("amount") {
		t.Error("expected HasErrorsAt(amount) = false")
	}
	if r.HasErrorsAt("") {
		t.Error("expected HasErrorsAt('') = false")
	}
}

func TestResult_ChainMethods_EmptyResult(t *testing.T) {
	r := Result{Valid: true, Errors: []ValidationError{}, Output: map[string]any{"pan": "6222"}}

	if r.HasCode(CodeFormatMismatch) {
		t.Error("expected HasCode = false on empty result")
	}
	if got := r.ErrorsByCode(CodeFormatMismatch); len(got) != 0 {
		t.Errorf("expected empty slice, got %d", len(got))
	}
	if got := r.ErrorsByType("cue"); len(got) != 0 {
		t.Errorf("expected empty slice, got %d", len(got))
	}
	if r.HasErrorsAt("pan") {
		t.Error("expected HasErrorsAt = false on empty result")
	}
}

// ========== ErrorFormatter ==========

// TestErrorFormatter_DefaultBehavior verifies that without a formatter,
// the default message is used (backward compatible).
func TestErrorFormatter_DefaultBehavior(t *testing.T) {
	v := MustNew(`{ name: string, age: int & >=0 }`)

	r := v.Process(map[string]any{"name": 123, "age": int64(-1)})
	if r.Valid {
		t.Fatal("expected invalid")
	}

	// Default messages should be CUE raw error strings
	for _, e := range r.Errors {
		if e.Message == "" {
			t.Errorf("expected non-empty default message for %s", e.Path)
		}
	}
}

// TestErrorFormatter_CustomFormatter verifies that a custom formatter
// is called for every error and its output is used as the Message.
func TestErrorFormatter_CustomFormatter(t *testing.T) {
	calls := 0
	formatter := func(code ErrorCode, path, detail string) string {
		calls++
		return fmt.Sprintf("CUSTOM[%s]:%s", code, path)
	}

	v := MustNew(`{
		name: string
		age:  int & >=0 & <=150
	}`, WithErrorFormatter(formatter))

	r := v.Process(map[string]any{"name": 123, "age": int64(200)})
	if r.Valid {
		t.Fatal("expected invalid")
	}

	if calls == 0 {
		t.Fatal("formatter was never called")
	}

	for _, e := range r.Errors {
		if !strings.HasPrefix(e.Message, "CUSTOM[") {
			t.Errorf("expected custom message, got %q", e.Message)
		}
	}
}

// TestErrorFormatter_RequiredMissing verifies formatter is called for required field errors.
func TestErrorFormatter_RequiredMissing(t *testing.T) {
	formatter := func(code ErrorCode, path, detail string) string {
		if code == CodeRequiredMissing {
			return fmt.Sprintf("字段 %q 为必填", path)
		}
		return detail
	}

	v := MustNew(`{ name: string, age: int }`, WithErrorFormatter(formatter))

	r := v.Process(map[string]any{"name": "Alice"})
	if r.Valid {
		t.Fatal("expected invalid — age is missing")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeRequiredMissing && e.Path == "age" {
			if e.Message != `字段 "age" 为必填` {
				t.Errorf("unexpected message: %q", e.Message)
			}
			found = true
		}
	}
	if !found {
		t.Error("expected RequiredMissing error for age")
	}
}

// TestErrorFormatter_BlobRuleFailed verifies formatter is called for @blob validation errors.
func TestErrorFormatter_BlobRuleFailed(t *testing.T) {
	formatter := func(code ErrorCode, path, detail string) string {
		if code == CodeBizRuleFailed {
			return fmt.Sprintf("业务规则失败: %s", path)
		}
		return detail
	}

	v := MustNew(`{
		amount: int & >0
		check:  bool @blob(this.amount > 100)
	}`, WithErrorFormatter(formatter))

	r := v.Process(map[string]any{"amount": int64(50)})
	if r.Valid {
		t.Fatal("expected invalid — amount <= 100")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeBizRuleFailed && e.Path == "check" {
			if e.Message != "业务规则失败: check" {
				t.Errorf("unexpected message: %q", e.Message)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected BizRuleFailed error for check, got: %v", r.Errors)
	}
}

// TestErrorFormatter_ConditionalRequired verifies formatter for meta conditional errors.
func TestErrorFormatter_ConditionalRequired(t *testing.T) {
	formatter := func(code ErrorCode, path, detail string) string {
		if code == CodeCondRequired {
			return fmt.Sprintf("条件必填: %s", path)
		}
		return detail
	}

	v := MustNew(`{
		payment_type: "credit" | "debit"
		cvv?: string @meta(conditional, required_if=this.payment_type == "credit")
	}`, WithErrorFormatter(formatter))

	r := v.Process(map[string]any{"payment_type": "credit"})
	if r.Valid {
		t.Fatal("expected invalid — cvv is conditionally required")
	}

	found := false
	for _, e := range r.Errors {
		if e.Code == CodeCondRequired && e.Path == "cvv" {
			if e.Message != "条件必填: cvv" {
				t.Errorf("unexpected message: %q", e.Message)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected CondRequired error for cvv, got: %v", r.Errors)
	}
}

// TestErrorFormatter_FormatterReceivesDetail verifies the detail parameter
// contains the original default message.
func TestErrorFormatter_FormatterReceivesDetail(t *testing.T) {
	var receivedDetails []string
	formatter := func(code ErrorCode, path, detail string) string {
		receivedDetails = append(receivedDetails, detail)
		return detail // pass through
	}

	v := MustNew(`{ age: int & >=0 }`, WithErrorFormatter(formatter))
	v.Process(map[string]any{"age": int64(-1)})

	if len(receivedDetails) == 0 {
		t.Fatal("formatter never called")
	}

	// The detail for a range violation should be the raw CUE error
	for _, d := range receivedDetails {
		if d == "" {
			t.Error("detail should not be empty")
		}
	}
}

// TestErrorFormatter_Validate verifies formatter works with Validate() too.
func TestErrorFormatter_Validate(t *testing.T) {
	formatter := func(code ErrorCode, path, detail string) string {
		return "FORMATTED"
	}

	v := MustNew(`{ name: string }`, WithErrorFormatter(formatter))

	valid, errs := v.Validate(map[string]any{})
	if valid {
		t.Fatal("expected invalid")
	}
	if len(errs) == 0 {
		t.Fatal("expected errors")
	}
	if errs[0].Message != "FORMATTED" {
		t.Errorf("expected 'FORMATTED', got %q", errs[0].Message)
	}
}

// TestErrorFormatter_NilFormatter verifies nil formatter is a no-op (same as default).
func TestErrorFormatter_NilFormatter(t *testing.T) {
	v := MustNew(`{ name: string }`, WithErrorFormatter(nil))

	r := v.Process(map[string]any{})
	if r.Valid {
		t.Fatal("expected invalid")
	}
	// Should use default message (not panic)
	if r.Errors[0].Message == "" {
		t.Error("expected non-empty default message")
	}
}

// ========== NewFromValue (Schema Composition) ==========

func TestNewFromValue_Basic(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{ name: string, age: int & >=0 }`)

	v, err := NewFromValue(schema)
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	r := v.Process(map[string]any{"name": "Alice", "age": int64(30)})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}

	r = v.Process(map[string]any{"name": "Alice", "age": int64(-1)})
	if r.Valid {
		t.Error("expected invalid for negative age")
	}
}

func TestNewFromValue_WithDefinitions(t *testing.T) {
	ctx := cuecontext.New()

	// Definitions and schema in a single compilation unit (CUE standard approach)
	combined := ctx.CompileString(`{
		#PAN:      =~"^[0-9]{16}$"
		#Amount:   int & >0
		#Currency: "156" | "840"

		pan:      #PAN
		amount:   #Amount
		currency: #Currency
	}`)
	if combined.Err() != nil {
		t.Fatalf("compile: %v", combined.Err())
	}

	v, err := NewFromValue(combined)
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	// Valid data
	r := v.Process(map[string]any{
		"pan": "6222021234567890", "amount": int64(100), "currency": "156",
	})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}

	// Invalid pan
	r = v.Process(map[string]any{
		"pan": "ABC", "amount": int64(100), "currency": "156",
	})
	if r.Valid {
		t.Error("expected invalid for bad PAN")
	}
}

func TestNewFromValue_WithBlob(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{
		amount:  int & >0
		doubled: number @blob(this.amount * 2)
	}`)

	v, err := NewFromValue(schema)
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	r := v.Process(map[string]any{"amount": int64(50)})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["doubled"] != int64(100) {
		t.Errorf("expected doubled=100, got %v", r.Output["doubled"])
	}
}

func TestNewFromValue_WithOptions(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{ name: string }`)

	formatter := func(code ErrorCode, path, detail string) string {
		return "CUSTOM:" + path
	}

	v, err := NewFromValue(schema, WithErrorFormatter(formatter))
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	r := v.Process(map[string]any{})
	if r.Valid {
		t.Fatal("expected invalid — name missing")
	}
	if r.Errors[0].Message != "CUSTOM:name" {
		t.Errorf("expected custom message, got %q", r.Errors[0].Message)
	}
}

func TestNewFromValue_SharedContext(t *testing.T) {
	ctx := cuecontext.New()

	// Create two validators sharing context
	s1 := ctx.CompileString(`{ x: string }`)
	s2 := ctx.CompileString(`{ y: int }`)

	v1, _ := NewFromValue(s1)
	v2, _ := NewFromValue(s2)

	r1 := v1.Process(map[string]any{"x": "hello"})
	r2 := v2.Process(map[string]any{"y": int64(42)})

	if !r1.Valid {
		t.Error("v1 should be valid")
	}
	if !r2.Valid {
		t.Error("v2 should be valid")
	}
}

func TestNewFromValue_ErrorOnInvalidValue(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{ invalid !!!`)

	_, err := NewFromValue(schema)
	if err == nil {
		t.Fatal("expected error for invalid CUE value")
	}
}

// ========== Schema Introspection ==========

func TestFields_SimpleSchema(t *testing.T) {
	v := MustNew(`{
		name:    string
		age:     int
		memo?:   string
		amount:  float
		active:  bool
	}`)

	fields := v.Fields()

	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fields))
	}

	// Verify by building a lookup map
	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	tests := []struct {
		name     string
		typ      string
		optional bool
		hasBlob  bool
		path     string
	}{
		{"name", "string", false, false, "name"},
		{"age", "int", false, false, "age"},
		{"memo", "string", true, false, "memo"},
		{"amount", "float", false, false, "amount"},
		{"active", "bool", false, false, "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := byName[tt.name]
			if !ok {
				t.Fatalf("field %q not found", tt.name)
			}
			if f.Type != tt.typ {
				t.Errorf("Type: got %q, want %q", f.Type, tt.typ)
			}
			if f.Optional != tt.optional {
				t.Errorf("Optional: got %v, want %v", f.Optional, tt.optional)
			}
			if f.HasBlob != tt.hasBlob {
				t.Errorf("HasBlob: got %v, want %v", f.HasBlob, tt.hasBlob)
			}
			if f.Path != tt.path {
				t.Errorf("Path: got %q, want %q", f.Path, tt.path)
			}
			if len(f.Children) != 0 {
				t.Errorf("Children: got %d, want 0", len(f.Children))
			}
		})
	}
}

func TestFields_NestedSchema(t *testing.T) {
	v := MustNew(`{
		id:   string
		address: {
			city:    string
			zip:     int
			country?: string
		}
	}`)

	fields := v.Fields()

	if len(fields) != 2 {
		t.Fatalf("expected 2 top-level fields, got %d", len(fields))
	}

	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	// Check top-level id field
	id := byName["id"]
	if id.Type != "string" {
		t.Errorf("id.Type: got %q, want %q", id.Type, "string")
	}
	if id.Path != "id" {
		t.Errorf("id.Path: got %q, want %q", id.Path, "id")
	}

	// Check nested address field
	addr := byName["address"]
	if addr.Type != "struct" {
		t.Errorf("address.Type: got %q, want %q", addr.Type, "struct")
	}
	if addr.Path != "address" {
		t.Errorf("address.Path: got %q, want %q", addr.Path, "address")
	}
	if len(addr.Children) != 3 {
		t.Fatalf("address.Children: got %d, want 3", len(addr.Children))
	}

	// Check children
	childByName := make(map[string]FieldInfo)
	for _, c := range addr.Children {
		childByName[c.Name] = c
	}

	city := childByName["city"]
	if city.Type != "string" {
		t.Errorf("city.Type: got %q, want %q", city.Type, "string")
	}
	if city.Path != "address.city" {
		t.Errorf("city.Path: got %q, want %q", city.Path, "address.city")
	}
	if city.Optional {
		t.Error("city.Optional: should be false")
	}

	zip := childByName["zip"]
	if zip.Type != "int" {
		t.Errorf("zip.Type: got %q, want %q", zip.Type, "int")
	}
	if zip.Path != "address.zip" {
		t.Errorf("zip.Path: got %q, want %q", zip.Path, "address.zip")
	}

	country := childByName["country"]
	if country.Type != "string" {
		t.Errorf("country.Type: got %q, want %q", country.Type, "string")
	}
	if !country.Optional {
		t.Error("country.Optional: should be true")
	}
}

func TestFields_BlobFields(t *testing.T) {
	v := MustNew(`{
		pan:        =~"^[0-9]{16}$"
		amount:     int & >0
		pan_check:  bool   @blob(this.pan.has_prefix("62"))
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	}`)

	fields := v.Fields()

	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	// Non-blob fields
	if byName["pan"].HasBlob {
		t.Error("pan.HasBlob: should be false")
	}
	if byName["amount"].HasBlob {
		t.Error("amount.HasBlob: should be false")
	}

	// Blob fields
	if !byName["pan_check"].HasBlob {
		t.Error("pan_check.HasBlob: should be true")
	}
	if !byName["card_brand"].HasBlob {
		t.Error("card_brand.HasBlob: should be true")
	}

	// Type should still be correct for blob fields
	if byName["pan_check"].Type != "bool" {
		t.Errorf("pan_check.Type: got %q, want %q", byName["pan_check"].Type, "bool")
	}
	if byName["card_brand"].Type != "string" {
		t.Errorf("card_brand.Type: got %q, want %q", byName["card_brand"].Type, "string")
	}
}

func TestFields_EmptyStruct(t *testing.T) {
	v := MustNew(`{}`)

	fields := v.Fields()

	if fields == nil {
		t.Fatal("Fields() should return non-nil empty slice")
	}
	if len(fields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(fields))
	}
}
