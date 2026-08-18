package schemix

import (
	"testing"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

func TestFuncMap_SharedAcrossValidators(t *testing.T) {
	funcs := NewFuncMap(
		Func("double", func(args ...any) (bloblang.Function, error) {
			n := args[0].(int64)
			return func() (any, error) { return n * 2, nil }, nil
		}),
		Method("is_positive", func(v any) (any, error) {
			n, ok := v.(int64)
			if !ok {
				return false, nil
			}
			return n > 0, nil
		}),
	)
	if funcs.Err() != nil {
		t.Fatalf("FuncMap error: %v", funcs.Err())
	}

	// Same FuncMap used by two different validators
	v1, err := New(`{
		amount: int
		doubled: number @blob(double(this.amount))
	}`, WithFuncMap(funcs))
	if err != nil {
		t.Fatalf("v1: %v", err)
	}

	v2, err := New(`{
		score: int
		check: bool @blob(this.score.is_positive())
	}`, WithFuncMap(funcs))
	if err != nil {
		t.Fatalf("v2: %v", err)
	}

	r := v1.Process(map[string]any{"amount": int64(50)})
	if !r.Valid || r.Output["doubled"] != int64(100) {
		t.Errorf("v1: expected doubled=100, got %v", r.Output["doubled"])
	}

	r = v2.Process(map[string]any{"score": int64(10)})
	if !r.Valid {
		t.Errorf("v2: expected valid, got %v", r.Errors)
	}

	r = v2.Process(map[string]any{"score": int64(-1)})
	if r.Valid {
		t.Error("v2: expected invalid for negative score")
	}
}

func TestFuncMap_OptionStyle(t *testing.T) {
	funcs := NewFuncMap(
		Func("add_one", func(args ...any) (bloblang.Function, error) {
			n := args[0].(int64)
			return func() (any, error) { return n + 1, nil }, nil
		}),
		Method("is_even", func(v any) (any, error) {
			n, ok := v.(int64)
			return ok && n%2 == 0, nil
		}),
	)

	v, err := New(`{
		n:     int
		n1:    number @blob(add_one(this.n))
		check: bool   @blob(this.n.is_even())
	}`, WithFuncMap(funcs))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := v.Process(map[string]any{"n": int64(4)})
	if !r.Valid {
		t.Errorf("expected valid, got %v", r.Errors)
	}
	if r.Output["n1"] != int64(5) {
		t.Errorf("expected n1=5, got %v", r.Output["n1"])
	}
}

func TestFuncMap_CombineWithSingleOptions(t *testing.T) {
	funcs := NewFuncMap(
		Method("triple", func(v any) (any, error) {
			n := v.(int64)
			return n * 3, nil
		}),
	)

	// FuncMap + individual WithFunction + WithErrorFormatter — all work together
	v, err := New(`{
		x:       int
		tripled: number @blob(this.x.triple())
		check:   bool   @blob(is_ok(this.x))
	}`,
		WithFuncMap(funcs),
		WithFunction("is_ok", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r := v.Process(map[string]any{"x": int64(7)})
	if !r.Valid {
		t.Errorf("expected valid, got %v", r.Errors)
	}
	if r.Output["tripled"] != int64(21) {
		t.Errorf("expected tripled=21, got %v", r.Output["tripled"])
	}
}

func TestFuncMap_InvalidName(t *testing.T) {
	funcs := NewFuncMap(
		Func("ValidName", func(args ...any) (bloblang.Function, error) { // uppercase — invalid
			return func() (any, error) { return true, nil }, nil
		}),
	)
	if funcs.Err() == nil {
		t.Fatal("expected error for invalid name 'ValidName'")
	}

	// Using invalid FuncMap in New() should return error
	_, err := New(`{ x: string }`, WithFuncMap(funcs))
	if err == nil {
		t.Fatal("expected New to fail with invalid FuncMap")
	}
}

func TestFuncMap_InvalidNameVariants(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"check_blacklist", true},
		{"is_email", true},
		{"luhn_valid", true},
		{"a1b2", true},
		{"x", true},
		{"CheckBlacklist", false},  // uppercase
		{"check-blacklist", false}, // dash
		{"_leading", false},        // leading underscore
		{"check__double", false},   // double underscore
		{"123start", true},         // digits allowed at start
		{"", false},                // empty
	}

	for _, tt := range tests {
		err := validateName(tt.name)
		if tt.valid && err != nil {
			t.Errorf("validateName(%q) should be valid, got: %v", tt.name, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateName(%q) should be invalid", tt.name)
		}
	}
}

// ========== Conflict Detection ==========
