package schemix

import "testing"

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
