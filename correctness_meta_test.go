package schemix

import (
	"strings"
	"testing"
)

// TestMetaCompileReject verifies parsefieldMeta fails on invalid @meta() params (R9).
func TestMetaCompileReject(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string // substring in compile error
	}{
		// Unknown params must error
		{
			name:    "unknown_param_rejects",
			schema:  `{ x: string @meta(foo_bar) }`,
			wantErr: "unknown @meta parameter",
		},
		// priority must be a valid integer
		{
			name:    "priority_non_numeric",
			schema:  `{ x: string @meta(priority=abc) }`,
			wantErr: "priority",
		},
		{
			name:    "priority_overflow",
			schema:  `{ x: string @meta(priority=99999999999999999999) }`,
			wantErr: "priority",
		},
		// Negative priority IS valid (not an error)
		// required_if with empty expression
		{
			name:    "required_if_empty_expr",
			schema:  `{ x?: string @meta(conditional, required_if=) }`,
			wantErr: "required_if",
		},
		// skip_if with empty expression
		{
			name:    "skip_if_empty_expr",
			schema:  `{ x: string @meta(skip_if=) }`,
			wantErr: "skip_if",
		},
		// required_if with invalid bloblang expression (valid CUE syntax)
		{
			name:    "required_if_parse_error",
			schema:  `{ x?: string @meta(conditional, required_if=this.!!!bad) }`,
			wantErr: "required_if",
		},
		// skip_if with invalid bloblang expression (valid CUE syntax)
		{
			name:    "skip_if_parse_error",
			schema:  `{ x: string @meta(skip_if=this.!!!bad) }`,
			wantErr: "skip_if",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)
			if err == nil {
				t.Fatal("expected compile error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestMetaCompileAccept verifies valid @meta() params compile successfully.
func TestMetaCompileAccept(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{"negative_priority", `{ x: string @meta(priority=-1) }`},
		{"flag_optional", `{ x?: string @meta(optional) }`},
		{"flag_conditional", `{ x?: string @meta(conditional) }`},
		{"flag_skip_empty", `{ x: string @meta(skip_empty) }`},
		{"flag_fail_fast", `{ x: string @meta(fail_fast) }`},
		{"flag_omit_if_skip", `{ x: string @meta(omit_if_skip) }`},
		{"flag_omit_empty", `{ x: string @meta(omit_empty) }`},
		{"valid_required_if", `{ x?: string @meta(conditional, required_if=true) }`},
		{"valid_skip_if", `{ x: string @meta(skip_if=true) }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
		})
	}
}

// TestMetaRuntimeQueryError verifies runtime Query errors in required_if/skip_if
// produce E3X01 (not silent swallow) with the original expression (R10).
func TestMetaRuntimeQueryError(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		data     map[string]any
		mode     FailMode
		wantCode ErrorCode
		wantExpr string // expression must appear in Message
	}{
		// required_if Query error in conditional branch (fieldVal==nil, x absent)
		{
			name:     "conditional_required_if_query_error",
			schema:   `{ x?: string @meta(conditional, required_if=this.a > 0) }`,
			data:     map[string]any{},
			mode:     FailAll,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.a > 0",
		},
		// required_if Query error in optional branch (fieldVal==nil, x absent)
		{
			name:     "optional_required_if_query_error",
			schema:   `{ x?: string @meta(optional, required_if=this.a > 0) }`,
			data:     map[string]any{},
			mode:     FailAll,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.a > 0",
		},
		// skip_if Query error (x present, but skip_if references missing field)
		{
			name:     "skip_if_query_error",
			schema:   `{ x: string @meta(skip_if=this.a > 0) }`,
			data:     map[string]any{"x": "hello"},
			mode:     FailAll,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.a > 0",
		},
		// FailFast returns on first Query error
		{
			name:     "failfast_skip_if_query_error",
			schema:   `{ x: string @meta(skip_if=this.b > 0) }`,
			data:     map[string]any{"x": "hello"},
			mode:     FailFast,
			wantCode: CodeMetaRuntimeError,
			wantExpr: "this.b > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := New(tt.schema)
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
			r := v.ProcessWithMode(tt.data, tt.mode)
			if r.Valid {
				t.Fatal("expected validation failure, got Valid=true (silent swallow)")
			}
			if len(r.Errors) == 0 {
				t.Fatal("expected at least one error")
			}
			found := false
			for _, e := range r.Errors {
				if e.Code == tt.wantCode && strings.Contains(e.Message, tt.wantExpr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no error with code=%s containing expr=%q; got: %v",
					tt.wantCode, tt.wantExpr, r.Errors)
			}
		})
	}
}
