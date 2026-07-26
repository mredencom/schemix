package schemix

import (
	"encoding/json"
	"testing"
)

// ─── Test fixtures ───────────────────────────────────────────────────────────

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

func TestToMapAny_MapDirect(t *testing.T) {
	input := map[string]any{"name": "Alice", "age": int64(30), "active": true}
	m, err := toMapAny(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["name"] != "Alice" {
		t.Errorf("got name=%v, want Alice", m["name"])
	}
}

func TestToMapAny_Processable(t *testing.T) {
	input := processableUser{name: "Bob", age: 25, active: true}
	m, err := toMapAny(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["name"] != "Bob" || m["age"] != int64(25) {
		t.Errorf("unexpected map: %v", m)
	}
}

func TestToMapAny_JSONBytes(t *testing.T) {
	input := []byte(`{"name":"Charlie","age":40,"active":false}`)
	m, err := toMapAny(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["name"] != "Charlie" {
		t.Errorf("got name=%v, want Charlie", m["name"])
	}
	// With UseNumber + conversion, integers become int64
	if m["age"] != int64(40) {
		t.Errorf("got age=%v (%T), want int64(40)", m["age"], m["age"])
	}
}

func TestToMapAny_Struct(t *testing.T) {
	input := testUser{Name: "Diana", Age: 28, Active: true}
	m, err := toMapAny(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["name"] != "Diana" {
		t.Errorf("got name=%v, want Diana", m["name"])
	}
}

func TestToMapAny_StructPointer(t *testing.T) {
	input := &testUser{Name: "Eve", Age: 35, Active: false}
	m, err := toMapAny(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["name"] != "Eve" {
		t.Errorf("got name=%v, want Eve", m["name"])
	}
}

func TestToMapAny_StructRespectsJSONTags(t *testing.T) {
	input := testUserOmit{Name: "Frank", Age: 22, Active: false, Hidden: "secret"}
	m, err := toMapAny(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := m["Hidden"]; ok {
		t.Error("Hidden field should be excluded via json:\"-\"")
	}
	// active is false with omitempty, so it should be absent
	if _, ok := m["active"]; ok {
		t.Error("active=false with omitempty should be absent")
	}
}

func TestToMapAny_Nil(t *testing.T) {
	_, err := toMapAny(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}

func TestToMapAny_NilPointer(t *testing.T) {
	var p *testUser
	_, err := toMapAny(p)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestToMapAny_UnsupportedType(t *testing.T) {
	cases := []any{
		42,
		"string",
		[]string{"a", "b"},
		true,
	}
	for _, c := range cases {
		_, err := toMapAny(c)
		if err == nil {
			t.Errorf("expected error for type %T", c)
		}
	}
}

func TestToMapAny_InvalidJSON(t *testing.T) {
	_, err := toMapAny([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestToMapAny_EmptyJSON(t *testing.T) {
	_, err := toMapAny([]byte(``))
	if err == nil {
		t.Error("expected error for empty JSON")
	}
}

// ─── ProcessValue tests ──────────────────────────────────────────────────────

func TestProcessValue_Map(t *testing.T) {
	r := valueAPISchema.ProcessValue(map[string]any{"name": "Alice", "age": int64(30), "active": true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcessValue_Struct(t *testing.T) {
	r := valueAPISchema.ProcessValue(testUser{Name: "Bob", Age: 25, Active: true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["name"] != "Bob" {
		t.Errorf("got name=%v, want Bob", r.Output["name"])
	}
}

func TestProcessValue_StructPointer(t *testing.T) {
	r := valueAPISchema.ProcessValue(&testUser{Name: "Carol", Age: 40, Active: false})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcessValue_JSONBytes(t *testing.T) {
	data := []byte(`{"name":"Dave","age":50,"active":true}`)
	r := valueAPISchema.ProcessValue(data)
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcessValue_Processable(t *testing.T) {
	r := valueAPISchema.ProcessValue(processableUser{name: "Eve", age: 28, active: true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcessValue_InvalidType(t *testing.T) {
	r := valueAPISchema.ProcessValue(42)
	if r.Valid {
		t.Error("expected invalid for unsupported type")
	}
	if len(r.Errors) == 0 || r.Errors[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got: %v", r.Errors)
	}
}

func TestProcessValue_Nil(t *testing.T) {
	r := valueAPISchema.ProcessValue(nil)
	if r.Valid {
		t.Error("expected invalid for nil")
	}
}

func TestProcessValue_InvalidStruct(t *testing.T) {
	// struct that produces invalid data
	type bad struct {
		Name   string `json:"name"`
		Age    int64  `json:"age"`
		Active string `json:"active"` // wrong type: string instead of bool
	}
	r := valueAPISchema.ProcessValue(bad{Name: "X", Age: 10, Active: "yes"})
	if r.Valid {
		t.Error("expected validation failure for type mismatch")
	}
}

// ─── ProcessValueWithMode tests ──────────────────────────────────────────────

func TestProcessValueWithMode_FailFast(t *testing.T) {
	bad := testUser{Name: "X", Age: -1, Active: true}
	// Age is negative, but JSON marshals int64 as number → CUE sees float64
	// This will fail the range constraint
	data, _ := json.Marshal(bad)
	r := valueAPISchema.ProcessValueWithMode(data, FailFast)
	if r.Valid {
		t.Error("expected invalid")
	}
	if len(r.Errors) > 1 {
		t.Errorf("FailFast should produce at most 1 error, got %d", len(r.Errors))
	}
}

// ─── ValidateValue tests ─────────────────────────────────────────────────────

func TestValidateValue_Valid(t *testing.T) {
	valid, errs := valueAPISchema.ValidateValue(testUser{Name: "Alice", Age: 30, Active: true})
	if !valid {
		t.Errorf("expected valid, got errors: %v", errs)
	}
}

func TestValidateValue_Invalid(t *testing.T) {
	valid, errs := valueAPISchema.ValidateValue([]byte(`{"name":"X","age":-1,"active":true}`))
	if valid {
		t.Error("expected invalid for age < 0")
	}
	if len(errs) == 0 {
		t.Error("expected at least one error")
	}
}

func TestValidateValue_UnsupportedType(t *testing.T) {
	valid, errs := valueAPISchema.ValidateValue("not a struct")
	if valid {
		t.Error("expected invalid")
	}
	if len(errs) == 0 || errs[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got: %v", errs)
	}
}

// ─── ProcessStruct generic function tests ────────────────────────────────────

func TestProcessStruct_Valid(t *testing.T) {
	r := ProcessStruct(valueAPISchema, testUser{Name: "Generic", Age: 20, Active: true})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
}

func TestProcessStruct_Invalid(t *testing.T) {
	r := ProcessStruct(valueAPISchema, testUser{Name: "", Age: -5, Active: true})
	// Name="" should fail string constraint (CUE treats empty string as valid string though)
	// Age=-5 should fail >=0
	if r.Valid {
		t.Error("expected invalid for age < 0")
	}
}

func TestProcessStructWithMode_FailAll(t *testing.T) {
	type multiErr struct {
		Name   int    `json:"name"`   // wrong type
		Age    int64  `json:"age"`    // valid
		Active string `json:"active"` // wrong type
	}
	r := ProcessStructWithMode(valueAPISchema, multiErr{Name: 0, Age: 5, Active: "yes"}, FailAll)
	if r.Valid {
		t.Error("expected invalid")
	}
	if len(r.Errors) < 2 {
		t.Errorf("FailAll should report multiple errors, got %d", len(r.Errors))
	}
}

// ─── ValidateStruct generic function tests ───────────────────────────────────

func TestValidateStruct_Valid(t *testing.T) {
	valid, errs := ValidateStruct(valueAPISchema, testUser{Name: "Gen", Age: 10, Active: false})
	if !valid {
		t.Errorf("expected valid, got errors: %v", errs)
	}
}

func TestValidateStruct_Invalid(t *testing.T) {
	valid, _ := ValidateStruct(valueAPISchema, testUser{Name: "X", Age: -1, Active: true})
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

func TestProcessValue_ProcessablePointer(t *testing.T) {
	r := valueAPISchema.ProcessValue(&processablePtr{val: "PtrImpl"})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["name"] != "PtrImpl" {
		t.Errorf("got name=%v, want PtrImpl", r.Output["name"])
	}
}
