package schemix

import (
	"fmt"
	"strings"
	"testing"
)

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
