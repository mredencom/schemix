package schemix

import (
	"fmt"
	"math"
	"slices"
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

// --- Structured enum and bound fields ---
//
// EnumOptions and Bound lift data the validator already holds at the point of
// failure into the error itself, so that rendering a message never requires
// parsing Message. Every case below asserts the structured field, not the
// wording, because the wording is what these fields exist to decouple from.

func TestValidationErrorEnumOptions(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		data   map[string]any
		path   string
		want   []string
	}{
		{
			name:   "fastpath string enum",
			schema: `{currency: "CNY" | "USD" | "EUR"}`,
			data:   map[string]any{"currency": "USE"},
			path:   "currency",
			want:   []string{"CNY", "USD", "EUR"},
		},
		{
			name:   "fastpath int enum",
			schema: `{level: 1 | 2 | 3}`,
			data:   map[string]any{"level": int64(9)},
			path:   "level",
			want:   []string{"1", "2", "3"},
		},
		{
			name:   "fastpath float enum",
			schema: `{rate: 1.5 | 2.5}`,
			data:   map[string]any{"rate": 9.5},
			path:   "rate",
			want:   []string{"1.5", "2.5"},
		},
		{
			name:   "scalar list element",
			schema: `{tags: [..."a" | "b"]}`,
			data:   map[string]any{"tags": []any{"a", "zzz"}},
			path:   "tags[1]",
			want:   []string{"a", "b"},
		},
		{
			// A struct field has no fast descriptor, so this exercises the CUE
			// path, where candidates arrive quoted and must be unquoted.
			name:   "cue path inside struct",
			schema: `{cfg: {currency: "CNY" | "USD"}}`,
			data:   map[string]any{"cfg": map[string]any{"currency": "XXX"}},
			path:   "cfg.currency",
			want:   []string{"CNY", "USD"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := MustNew(tt.schema).Process(tt.data)
			if r.Valid {
				t.Fatal("expected validation to fail")
			}
			errs := r.ErrorsByPath(tt.path)
			if len(errs) == 0 {
				t.Fatalf("no error at %q; got %v", tt.path, r.Errors)
			}
			e := errs[0]
			if e.Code != CodeEnumInvalid {
				t.Fatalf("expected %s at %q, got %s (%s)", CodeEnumInvalid, tt.path, e.Code, e.Message)
			}
			if !slices.Equal(e.EnumOptions, tt.want) {
				t.Errorf("EnumOptions = %q, want %q", e.EnumOptions, tt.want)
			}
		})
	}
}

func TestValidationErrorBound(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		data   map[string]any
		path   string
		want   string
	}{
		{
			name:   "fastpath int upper bound",
			schema: `{age: int & >=13 & <=150}`,
			data:   map[string]any{"age": int64(200)},
			path:   "age",
			want:   "<=150",
		},
		{
			name:   "fastpath int lower bound",
			schema: `{age: int & >=13 & <=150}`,
			data:   map[string]any{"age": int64(5)},
			path:   "age",
			want:   ">=13",
		},
		{
			name:   "fastpath float exclusive bound",
			schema: `{amount: float & >0.0}`,
			data:   map[string]any{"amount": 0.0},
			path:   "amount",
			want:   ">0",
		},
		{
			name:   "cue path inside struct",
			schema: `{cfg: {age: int & <=150}}`,
			data:   map[string]any{"cfg": map[string]any{"age": int64(200)}},
			path:   "cfg.age",
			want:   "<=150",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := MustNew(tt.schema).Process(tt.data)
			if r.Valid {
				t.Fatal("expected validation to fail")
			}
			errs := r.ErrorsByPath(tt.path)
			if len(errs) == 0 {
				t.Fatalf("no error at %q; got %v", tt.path, r.Errors)
			}
			e := errs[0]
			if e.Code != CodeRangeViolation {
				t.Fatalf("expected %s at %q, got %s (%s)", CodeRangeViolation, tt.path, e.Code, e.Message)
			}
			if e.Bound != tt.want {
				t.Errorf("Bound = %q, want %q", e.Bound, tt.want)
			}
		})
	}
}

// TestEnumOptionsIsCopy guards the one way this feature could corrupt an
// otherwise correct validator: fastConstraint is built once in New() and shared
// by every concurrent Process, so handing its slice to the caller would let one
// caller's sort or append rewrite the schema for everybody else.
func TestEnumOptionsIsCopy(t *testing.T) {
	v := MustNew(`{currency: "CNY" | "USD" | "EUR"}`)
	data := map[string]any{"currency": "USE"}

	first := v.Process(data)
	opts := first.Errors[0].EnumOptions
	if len(opts) != 3 {
		t.Fatalf("expected 3 options, got %q", opts)
	}
	// Whatever a caller does to the slice it was handed must not be observable.
	opts[0] = "POLLUTED"
	slices.Reverse(opts)

	second := v.Process(data)
	if got := second.Errors[0].EnumOptions; !slices.Equal(got, []string{"CNY", "USD", "EUR"}) {
		t.Errorf("second call saw mutated options %q — internal state was shared", got)
	}
	// The message is rendered from the same source slice, so it must be intact too.
	if want := `value "USE" not in enum ["CNY", "USD", "EUR"]`; second.Errors[0].Message != want {
		t.Errorf("Message = %q, want %q", second.Errors[0].Message, want)
	}
}

// TestStructuredFieldsSurviveErrorFormatter pins the reason these fields are
// filled from the raw detail rather than from Message: a custom ErrorFormatter
// replaces Message with arbitrary text, and anything parsed out of Message would
// vanish the moment a formatter is configured.
func TestStructuredFieldsSurviveErrorFormatter(t *testing.T) {
	opaque := WithErrorFormatter(func(code ErrorCode, path, detail string) string {
		return "opaque"
	})

	enum := MustNew(`{currency: "CNY" | "USD"}`, opaque).Process(map[string]any{"currency": "XXX"})
	e := enum.Errors[0]
	if e.Message != "opaque" {
		t.Fatalf("formatter did not take effect: Message = %q", e.Message)
	}
	if !slices.Equal(e.EnumOptions, []string{"CNY", "USD"}) {
		t.Errorf("EnumOptions = %q, want [CNY USD] despite the formatter", e.EnumOptions)
	}

	rng := MustNew(`{age: int & <=150}`, opaque).Process(map[string]any{"age": int64(200)})
	if got := rng.Errors[0].Bound; got != "<=150" {
		t.Errorf("Bound = %q, want %q despite the formatter", got, "<=150")
	}
}

// TestNonEnumErrorsHaveNoEnumOptions keeps the fields honest: a localizer
// branches on whether they are populated, so a stray value on an unrelated code
// would produce a nonsensical sentence.
func TestNonEnumErrorsHaveNoEnumOptions(t *testing.T) {
	r := MustNew(`{pan: =~"^[0-9]{16}$", age: int & <=150}`).Process(map[string]any{
		"pan": "abc",
		"age": int64(200),
	})
	for _, e := range r.Errors {
		switch e.Code {
		case CodeRangeViolation:
			if e.EnumOptions != nil {
				t.Errorf("%s carries EnumOptions %q", e.Code, e.EnumOptions)
			}
		case CodeFormatMismatch:
			if e.EnumOptions != nil || e.Bound != "" {
				t.Errorf("%s carries EnumOptions %q / Bound %q", e.Code, e.EnumOptions, e.Bound)
			}
		}
	}
}

// TestBoundEmptyForOverflowRangeErrors distinguishes the two things that share
// CodeRangeViolation: breaking a bound the schema declared, and being a value no
// finite bound can describe (±Inf, NaN). The latter has no bound to report, so
// Bound stays empty and a renderer must fall back to generic wording rather than
// emitting a sentence with a hole in it.
func TestBoundEmptyForOverflowRangeErrors(t *testing.T) {
	r := MustNew(`{amount: float & >0.0}`).Process(map[string]any{
		"amount": math.Inf(1),
	})
	if r.Valid {
		t.Fatal("expected validation to fail")
	}
	e := r.Errors[0]
	if e.Code != CodeRangeViolation {
		t.Fatalf("expected %s, got %s (%s)", CodeRangeViolation, e.Code, e.Message)
	}
	if !strings.Contains(e.Message, "finite range") {
		t.Fatalf("expected a finite-range diagnostic, got %q", e.Message)
	}
	if e.Bound != "" {
		t.Errorf("Bound = %q, want empty — no declared bound was broken", e.Bound)
	}
}
