package schemix

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

func TestNewFromValue_Basic(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{ name: string, age: int & >=0 }`)

	v, err := NewFromValue(schema)
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	r := v.Process(map[string]any{"name": "Alice", "age": int64(30)})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}

	r = v.Process(map[string]any{"name": "Alice", "age": int64(-1)})
	if r.Valid {
		t.Error("expected invalid for negative age")
	}
}

func TestNewFromValue_WithDefinitions(t *testing.T) {
	ctx := cuecontext.New()

	// Definitions and schema in a single compilation unit (CUE standard approach)
	combined := ctx.CompileString(`{
		#PAN:      =~"^[0-9]{16}$"
		#Amount:   int & >0
		#Currency: "156" | "840"

		pan:      #PAN
		amount:   #Amount
		currency: #Currency
	}`)
	if combined.Err() != nil {
		t.Fatalf("compile: %v", combined.Err())
	}

	v, err := NewFromValue(combined)
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	// Valid data
	r := v.Process(map[string]any{
		"pan": "6222021234567890", "amount": int64(100), "currency": "156",
	})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}

	// Invalid pan
	r = v.Process(map[string]any{
		"pan": "ABC", "amount": int64(100), "currency": "156",
	})
	if r.Valid {
		t.Error("expected invalid for bad PAN")
	}
}

func TestNewFromValue_WithBlob(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{
		amount:  int & >0
		doubled: number @blob(this.amount * 2)
	}`)

	v, err := NewFromValue(schema)
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	r := v.Process(map[string]any{"amount": int64(50)})
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}
	if r.Output["doubled"] != int64(100) {
		t.Errorf("expected doubled=100, got %v", r.Output["doubled"])
	}
}

func TestNewFromValue_WithOptions(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{ name: string }`)

	formatter := func(code ErrorCode, path, detail string) string {
		return "CUSTOM:" + path
	}

	v, err := NewFromValue(schema, WithErrorFormatter(formatter))
	if err != nil {
		t.Fatalf("NewFromValue: %v", err)
	}

	r := v.Process(map[string]any{})
	if r.Valid {
		t.Fatal("expected invalid — name missing")
	}
	if r.Errors[0].Message != "CUSTOM:name" {
		t.Errorf("expected custom message, got %q", r.Errors[0].Message)
	}
}

func TestNewFromValue_SharedContext(t *testing.T) {
	ctx := cuecontext.New()

	// Create two validators sharing context
	s1 := ctx.CompileString(`{ x: string }`)
	s2 := ctx.CompileString(`{ y: int }`)

	v1, _ := NewFromValue(s1)
	v2, _ := NewFromValue(s2)

	r1 := v1.Process(map[string]any{"x": "hello"})
	r2 := v2.Process(map[string]any{"y": int64(42)})

	if !r1.Valid {
		t.Error("v1 should be valid")
	}
	if !r2.Valid {
		t.Error("v2 should be valid")
	}
}

func TestNewFromValue_ErrorOnInvalidValue(t *testing.T) {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{ invalid !!!`)

	_, err := NewFromValue(schema)
	if err == nil {
		t.Fatal("expected error for invalid CUE value")
	}
}
