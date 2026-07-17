package schemix

import (
	"testing"
)

func TestFields_SimpleSchema(t *testing.T) {
	v := MustNew(`{
		name:    string
		age:     int
		memo?:   string
		amount:  float
		active:  bool
	}`)

	fields := v.Fields()

	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fields))
	}

	// Verify by building a lookup map
	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	tests := []struct {
		name     string
		typ      string
		optional bool
		hasBlob  bool
		path     string
	}{
		{"name", "string", false, false, "name"},
		{"age", "int", false, false, "age"},
		{"memo", "string", true, false, "memo"},
		{"amount", "float", false, false, "amount"},
		{"active", "bool", false, false, "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := byName[tt.name]
			if !ok {
				t.Fatalf("field %q not found", tt.name)
			}
			if f.Type != tt.typ {
				t.Errorf("Type: got %q, want %q", f.Type, tt.typ)
			}
			if f.Optional != tt.optional {
				t.Errorf("Optional: got %v, want %v", f.Optional, tt.optional)
			}
			if f.HasBlob != tt.hasBlob {
				t.Errorf("HasBlob: got %v, want %v", f.HasBlob, tt.hasBlob)
			}
			if f.Path != tt.path {
				t.Errorf("Path: got %q, want %q", f.Path, tt.path)
			}
			if len(f.Children) != 0 {
				t.Errorf("Children: got %d, want 0", len(f.Children))
			}
		})
	}
}

func TestFields_NestedSchema(t *testing.T) {
	v := MustNew(`{
		id:   string
		address: {
			city:    string
			zip:     int
			country?: string
		}
	}`)

	fields := v.Fields()

	if len(fields) != 2 {
		t.Fatalf("expected 2 top-level fields, got %d", len(fields))
	}

	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	// Check top-level id field
	id := byName["id"]
	if id.Type != "string" {
		t.Errorf("id.Type: got %q, want %q", id.Type, "string")
	}
	if id.Path != "id" {
		t.Errorf("id.Path: got %q, want %q", id.Path, "id")
	}

	// Check nested address field
	addr := byName["address"]
	if addr.Type != "struct" {
		t.Errorf("address.Type: got %q, want %q", addr.Type, "struct")
	}
	if addr.Path != "address" {
		t.Errorf("address.Path: got %q, want %q", addr.Path, "address")
	}
	if len(addr.Children) != 3 {
		t.Fatalf("address.Children: got %d, want 3", len(addr.Children))
	}

	// Check children
	childByName := make(map[string]FieldInfo)
	for _, c := range addr.Children {
		childByName[c.Name] = c
	}

	city := childByName["city"]
	if city.Type != "string" {
		t.Errorf("city.Type: got %q, want %q", city.Type, "string")
	}
	if city.Path != "address.city" {
		t.Errorf("city.Path: got %q, want %q", city.Path, "address.city")
	}
	if city.Optional {
		t.Error("city.Optional: should be false")
	}

	zip := childByName["zip"]
	if zip.Type != "int" {
		t.Errorf("zip.Type: got %q, want %q", zip.Type, "int")
	}
	if zip.Path != "address.zip" {
		t.Errorf("zip.Path: got %q, want %q", zip.Path, "address.zip")
	}

	country := childByName["country"]
	if country.Type != "string" {
		t.Errorf("country.Type: got %q, want %q", country.Type, "string")
	}
	if !country.Optional {
		t.Error("country.Optional: should be true")
	}
}

func TestFields_BlobFields(t *testing.T) {
	v := MustNew(`{
		pan:        =~"^[0-9]{16}$"
		amount:     int & >0
		pan_check:  bool   @blob(this.pan.has_prefix("62"))
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	}`)

	fields := v.Fields()

	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	// Non-blob fields
	if byName["pan"].HasBlob {
		t.Error("pan.HasBlob: should be false")
	}
	if byName["amount"].HasBlob {
		t.Error("amount.HasBlob: should be false")
	}

	// Blob fields
	if !byName["pan_check"].HasBlob {
		t.Error("pan_check.HasBlob: should be true")
	}
	if !byName["card_brand"].HasBlob {
		t.Error("card_brand.HasBlob: should be true")
	}

	// Type should still be correct for blob fields
	if byName["pan_check"].Type != "bool" {
		t.Errorf("pan_check.Type: got %q, want %q", byName["pan_check"].Type, "bool")
	}
	if byName["card_brand"].Type != "string" {
		t.Errorf("card_brand.Type: got %q, want %q", byName["card_brand"].Type, "string")
	}
}

func TestFields_EmptyStruct(t *testing.T) {
	v := MustNew(`{}`)

	fields := v.Fields()

	if fields == nil {
		t.Fatal("Fields() should return non-nil empty slice")
	}
	if len(fields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(fields))
	}
}
