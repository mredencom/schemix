package schemix

import (
	"fmt"
	"strings"
	"testing"
)

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

func TestResult_Err_Valid(t *testing.T) {
	r := Result{Valid: true, Errors: []ValidationError{}}
	if err := r.Err(); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestResult_Err_Invalid(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeTypeMismatch, Path: "name", Type: TypeCUE, Message: "type error"},
			{Code: CodeRangeViolation, Path: "age", Type: TypeCUE, Message: "out of range"},
		},
	}
	err := r.Err()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	s := err.Error()
	if !strings.Contains(s, "name") || !strings.Contains(s, "age") {
		t.Errorf("error should mention both fields, got: %s", s)
	}
}

func TestResult_FirstError(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeTypeMismatch, Path: "x", Type: TypeCUE, Message: "first"},
			{Code: CodeRangeViolation, Path: "y", Type: TypeCUE, Message: "second"},
		},
	}
	first := r.FirstError()
	if first == nil || first.Message != "first" {
		t.Errorf("expected first error, got %v", first)
	}
}

func TestResult_FirstError_Empty(t *testing.T) {
	r := Result{Valid: true, Errors: []ValidationError{}}
	if r.FirstError() != nil {
		t.Error("expected nil for valid result")
	}
}

func TestResult_ErrorsByPath(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Path: "a"}, {Path: "b"}, {Path: "a"},
		},
	}
	got := r.ErrorsByPath("a")
	if len(got) != 2 {
		t.Errorf("expected 2 errors for path 'a', got %d", len(got))
	}
}

func TestResult_Errors_NotNil(t *testing.T) {
	v := MustNew(`{ name: string }`)
	r := v.Process(map[string]any{"name": "ok"})
	if r.Errors == nil {
		t.Error("Errors should be empty slice, not nil")
	}
	if len(r.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(r.Errors))
	}
}

// ========== ValidationError implements error ==========

func TestValidationError_Error(t *testing.T) {
	e := ValidationError{
		Code: CodeTypeMismatch, Path: "name", Type: TypeCUE, Message: "conflicting values",
	}
	s := e.Error()
	if s != "[E1T01] name: conflicting values" {
		t.Errorf("unexpected error string: %s", s)
	}
}

// ========== MustNew ==========

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
