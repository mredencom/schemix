package schemix

import (
	"encoding/json"
	"testing"
)

var valueAPISchema = MustNew(`{
	name:   string
	age:    int & >=0
	active: bool
}`)

type testUser struct {
	Name   string `json:"name"`
	Age    int64  `json:"age"`
	Active bool   `json:"active"`
}

type testUserOmit struct {
	Name   string `json:"name"`
	Age    int64  `json:"age"`
	Active bool   `json:"active,omitempty"`
	Hidden string `json:"-"` // should be excluded
}

// processableUser implements the Processable interface.
type processableUser struct {
	name   string
	age    int64
	active bool
}

func (u processableUser) ToMap() map[string]any {
	return map[string]any{
		"name":   u.name,
		"age":    u.age,
		"active": u.active,
	}
}

// ─── toMapAny unit tests ─────────────────────────────────────────────────────

func TestProcess_Map(t *testing.T) {
	r := valueAPISchema.Process(map[string]any{"name": "Alice", "age": int64(30), "active": true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcess_Struct(t *testing.T) {
	r := valueAPISchema.Process(testUser{Name: "Bob", Age: 25, Active: true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["name"] != "Bob" {
		t.Errorf("got name=%v, want Bob", r.Output["name"])
	}
}

func TestProcess_StructPointer(t *testing.T) {
	r := valueAPISchema.Process(&testUser{Name: "Carol", Age: 40, Active: false})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcess_JSONBytes(t *testing.T) {
	data := []byte(`{"name":"Dave","age":50,"active":true}`)
	r := valueAPISchema.Process(data)
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcess_Processable(t *testing.T) {
	r := valueAPISchema.Process(processableUser{name: "Eve", age: 28, active: true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcess_InvalidType(t *testing.T) {
	r := valueAPISchema.Process(42)
	if r.Valid {
		t.Error("expected invalid for unsupported type")
	}
	if len(r.Errors) == 0 || r.Errors[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got: %v", r.Errors)
	}
}

func TestProcess_Nil(t *testing.T) {
	r := valueAPISchema.Process(nil)
	if r.Valid {
		t.Error("expected invalid for nil")
	}
}

func TestProcess_InvalidStruct(t *testing.T) {
	// struct that produces invalid data
	type bad struct {
		Name   string `json:"name"`
		Age    int64  `json:"age"`
		Active string `json:"active"` // wrong type: string instead of bool
	}
	r := valueAPISchema.Process(bad{Name: "X", Age: 10, Active: "yes"})
	if r.Valid {
		t.Error("expected validation failure for type mismatch")
	}
}

// ─── ProcessValueWithMode tests ──────────────────────────────────────────────

func TestProcess_FailFast(t *testing.T) {
	bad := testUser{Name: "X", Age: -1, Active: true}
	// Age is negative, but JSON marshals int64 as number → CUE sees float64
	// This will fail the range constraint
	data, _ := json.Marshal(bad)
	r := valueAPISchema.Process(data, FailFast)
	if r.Valid {
		t.Error("expected invalid")
	}
	if len(r.Errors) > 1 {
		t.Errorf("FailFast should produce at most 1 error, got %d", len(r.Errors))
	}
}

// ─── ValidateValue tests ─────────────────────────────────────────────────────

func TestValidate_FlexibleInput_Valid(t *testing.T) {
	valid, errs := valueAPISchema.Validate(testUser{Name: "Alice", Age: 30, Active: true})
	if !valid {
		t.Errorf("expected valid, got errors: %v", errs)
	}
}

func TestValidate_FlexibleInput_Invalid(t *testing.T) {
	valid, errs := valueAPISchema.Validate([]byte(`{"name":"X","age":-1,"active":true}`))
	if valid {
		t.Error("expected invalid for age < 0")
	}
	if len(errs) == 0 {
		t.Error("expected at least one error")
	}
}

func TestValidate_FlexibleInput_UnsupportedType(t *testing.T) {
	valid, errs := valueAPISchema.Validate("not a struct")
	if valid {
		t.Error("expected invalid")
	}
	if len(errs) == 0 || errs[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got: %v", errs)
	}
}

// ─── struct input via Process ────────────────────────────────────────────────

func TestProcess_StructInput_Valid(t *testing.T) {
	r := valueAPISchema.Process(testUser{Name: "Generic", Age: 20, Active: true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcess_StructInput_Invalid(t *testing.T) {
	r := valueAPISchema.Process(testUser{Name: "", Age: -5, Active: true})
	// Name="" should fail string constraint (CUE treats empty string as valid string though)
	// Age=-5 should fail >=0
	if r.Valid {
		t.Error("expected invalid for age < 0")
	}
}

func TestProcess_StructInput_FailAll(t *testing.T) {
	type multiErr struct {
		Name   int    `json:"name"`   // wrong type
		Age    int64  `json:"age"`    // valid
		Active string `json:"active"` // wrong type
	}
	r := valueAPISchema.Process(multiErr{Name: 0, Age: 5, Active: "yes"}, FailAll)
	if r.Valid {
		t.Error("expected invalid")
	}
	if len(r.Errors) < 2 {
		t.Errorf("FailAll should report multiple errors, got %d", len(r.Errors))
	}
}

// ─── struct input via Validate ───────────────────────────────────────────────

func TestValidate_StructInput_Valid(t *testing.T) {
	valid, errs := valueAPISchema.Validate(testUser{Name: "Gen", Age: 10, Active: false})
	if !valid {
		t.Errorf("expected valid, got errors: %v", errs)
	}
}

func TestValidate_StructInput_Invalid(t *testing.T) {
	valid, _ := valueAPISchema.Validate(testUser{Name: "X", Age: -1, Active: true})
	if valid {
		t.Error("expected invalid for age < 0")
	}
}

// ─── Processable pointer receiver test ───────────────────────────────────────

type processablePtr struct {
	val string
}

func (p *processablePtr) ToMap() map[string]any {
	return map[string]any{"name": p.val, "age": int64(1), "active": true}
}

func TestProcess_ProcessablePointer(t *testing.T) {
	r := valueAPISchema.Process(&processablePtr{val: "PtrImpl"})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["name"] != "PtrImpl" {
		t.Errorf("got name=%v, want PtrImpl", r.Output["name"])
	}
}
