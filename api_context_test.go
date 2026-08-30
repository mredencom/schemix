package schemix

import (
	"context"
	"testing"
)

func TestProcessContext_SameResultAsProcess(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int & >=0
	}`, WithName("parity"))

	tests := []struct {
		name string
		data map[string]any
	}{
		{
			name: "valid",
			data: map[string]any{"name": "Alice", "age": int64(30)},
		},
		{
			name: "invalid_type",
			data: map[string]any{"name": 123, "age": int64(30)},
		},
		{
			name: "missing_field",
			data: map[string]any{"name": "Bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.ProcessContext(context.Background(), tt.data)
			want := v.Process(tt.data)

			if got.Valid != want.Valid {
				t.Errorf("Valid: got %v, want %v", got.Valid, want.Valid)
			}
			if len(got.Errors) != len(want.Errors) {
				t.Errorf("Errors count: got %d, want %d", len(got.Errors), len(want.Errors))
			}
			for i := range got.Errors {
				if i >= len(want.Errors) {
					break
				}
				if got.Errors[i].Code != want.Errors[i].Code {
					t.Errorf("Error[%d].Code: got %s, want %s", i, got.Errors[i].Code, want.Errors[i].Code)
				}
				if got.Errors[i].Path != want.Errors[i].Path {
					t.Errorf("Error[%d].Path: got %s, want %s", i, got.Errors[i].Path, want.Errors[i].Path)
				}
			}
		})
	}
}

func TestValidateContext_SameResultAsValidate(t *testing.T) {
	v := MustNew(`{
		email: string @blob(this.email.is_email())
	}`, WithName("validate-parity"))

	tests := []struct {
		name string
		data map[string]any
	}{
		{
			name: "valid_email",
			data: map[string]any{"email": "test@example.com"},
		},
		{
			name: "invalid_email",
			data: map[string]any{"email": "not-an-email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValid, gotErrs := v.ValidateContext(context.Background(), tt.data)
			wantValid, wantErrs := v.Validate(tt.data)

			if gotValid != wantValid {
				t.Errorf("Valid: got %v, want %v", gotValid, wantValid)
			}
			if len(gotErrs) != len(wantErrs) {
				t.Errorf("Errors count: got %d, want %d", len(gotErrs), len(wantErrs))
			}
		})
	}
}

func TestProcessContext_Struct(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int & >=0
	}`, WithName("struct-ctx"))

	type Person struct {
		Name string `json:"name"`
		Age  int64  `json:"age"`
	}

	r := v.ProcessContext(context.Background(), Person{Name: "Alice", Age: 25})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcessContext_FailFast(t *testing.T) {
	v := MustNew(`{
		a: string
		b: int
		c: float
	}`, WithName("failfast-ctx"))

	// All fields wrong type
	data := map[string]any{"a": 123, "b": "wrong", "c": "wrong"}
	r := v.ProcessContext(context.Background(), data, FailFast)

	if r.Valid {
		t.Error("expected invalid result")
	}
	if len(r.Errors) != 1 {
		t.Errorf("FailFast should produce 1 error, got %d", len(r.Errors))
	}
}

func TestValidateContext_FlexibleInput(t *testing.T) {
	v := MustNew(`{
		x: int & >0
	}`, WithName("validate-value-ctx"))

	type Data struct {
		X int64 `json:"x"`
	}

	tests := []struct {
		name      string
		data      any
		wantValid bool
	}{
		{"valid_struct", Data{X: 5}, true},
		{"invalid_struct", Data{X: -1}, false},
		{"unsupported_type", 42, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, errs := v.ValidateContext(context.Background(), tt.data)
			if valid != tt.wantValid {
				t.Errorf("Valid: got %v, want %v (errors: %v)", valid, tt.wantValid, errs)
			}
		})
	}
}

func TestProcessContext_UnsupportedType(t *testing.T) {
	v := MustNew(`{ x: int }`)

	r := v.ProcessContext(context.Background(), 42, FailAll)
	if r.Valid {
		t.Error("expected invalid for unsupported type")
	}
	if len(r.Errors) == 0 || r.Errors[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got %v", r.Errors)
	}
}
