package schemix

import (
	"testing"
)

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

// ─── conversion via Process ──────────────────────────────────────────────────
