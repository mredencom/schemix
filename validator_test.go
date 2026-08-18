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

// ========== Schema Introspection ==========

func TestMustNew_Success(t *testing.T) {
	v := MustNew(`{ name: string }`)
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
}

func TestMustNew_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid schema")
		}
	}()
	MustNew(`{ invalid schema !!!`)
}

// ========== NewWithContext ==========

func TestNewWithContext_SharedContext(t *testing.T) {
	// Create two validators sharing the same context
	reg := NewRegistry()
	if err := reg.Register("a", `{ x: string }`); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("b", `{ y: int }`); err != nil {
		t.Fatal(err)
	}
	va, _ := reg.Get("a")
	vb, _ := reg.Get("b")

	// Both should work independently
	r := va.Process(map[string]any{"x": "hello"})
	if !r.Valid {
		t.Error("expected a to be valid")
	}
	r = vb.Process(map[string]any{"y": int64(42)})
	if !r.Valid {
		t.Error("expected b to be valid")
	}
}

// ========== Process preserves int64 in output ==========

// TestNewFromValueNilHandling verifies nil handling through NewFromValue with CUE definitions.
func TestNewFromValueNilHandling(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		data      map[string]any
		wantValid bool
		wantCode  ErrorCode
		wantPath  string
	}{
		{
			name: "definition_ref_nullable_nil_valid",
			schema: `{
				#T: null | string
				name: #T
			}`,
			data:      map[string]any{"name": nil},
			wantValid: true,
		},
		{
			name: "definition_ref_nonnullable_nil_E1M01",
			schema: `{
				#T: string
				name: #T
			}`,
			data:      map[string]any{"name": nil},
			wantValid: false,
			wantCode:  CodeRequiredMissing,
			wantPath:  "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := cuecontext.New()
			val := ctx.CompileString(tt.schema)
			if val.Err() != nil {
				t.Fatalf("compile: %v", val.Err())
			}

			v, err := NewFromValue(val)
			if err != nil {
				t.Fatalf("NewFromValue: %v", err)
			}

			r := v.Process(tt.data)
			if r.Valid != tt.wantValid {
				t.Fatalf("valid=%v, want %v; errors=%v", r.Valid, tt.wantValid, r.Errors)
			}
			if !tt.wantValid {
				if len(r.Errors) == 0 {
					t.Fatal("expected errors but got none")
				}
				if r.Errors[0].Code != tt.wantCode {
					t.Errorf("code=%s, want %s; msg=%s", r.Errors[0].Code, tt.wantCode, r.Errors[0].Message)
				}
				if r.Errors[0].Path != tt.wantPath {
					t.Errorf("path=%s, want %s", r.Errors[0].Path, tt.wantPath)
				}
			}
		})
	}
}

// =============================================================================
// Blob output & type contract tests
// =============================================================================
