package schemix

import "testing"

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
