package schemix

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestValidate_NoOutputAllocation verifies that Validate() returns correct
// results for valid data without needing Output allocation.
func TestValidate_NoOutputAllocation(t *testing.T) {
	v := MustNew(`{
		name: string
		age:  int & >=0 & <=150
	}`)

	valid, errs := v.Validate(map[string]any{
		"name": "Alice",
		"age":  int64(30),
	})

	if !valid {
		t.Errorf("expected valid=true, got false")
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

// TestValidate_ReportsErrors verifies that Validate() correctly reports
// validation errors for invalid data.
func TestValidate_ReportsErrors(t *testing.T) {
	v := MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"
	}`)

	valid, errs := v.Validate(map[string]any{
		"pan":      "ABC",
		"amount":   int64(-1),
		"currency": "999",
	})

	if valid {
		t.Errorf("expected valid=false, got true")
	}
	if len(errs) == 0 {
		t.Errorf("expected errors, got none")
	}

	// Verify specific error paths are reported
	pathsSeen := map[string]bool{}
	for _, e := range errs {
		pathsSeen[e.Path] = true
	}
	for _, expected := range []string{"pan", "amount", "currency"} {
		if !pathsSeen[expected] {
			t.Errorf("expected error for path %q, not found in %v", expected, errs)
		}
	}
}

// TestValidate_WithBlob verifies that Validate() correctly handles @blob fields:
//   - bool-returning @blob rules still validate (can fail)
//   - non-bool (computed) @blob rules execute but Output is irrelevant since
//     Validate() doesn't return Output anyway.
func TestValidate_WithBlob(t *testing.T) {
	v := MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"

		pan_check:  bool   @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
		fee:        number @blob(if this.currency == "156" { 0 } else { (this.amount * 0.015).ceil() })
	}`)

	t.Run("valid data with blob", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"pan": "6222021234567890", "amount": int64(10000), "currency": "156",
		})
		if !valid {
			t.Errorf("expected valid=true, got false; errors: %v", errs)
		}
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("blob bool validation fails", func(t *testing.T) {
		// PAN starts with "99" — neither "62" nor "4", so pan_check @blob returns false
		valid, errs := v.Validate(map[string]any{
			"pan": "9900021234567890", "amount": int64(10000), "currency": "156",
		})
		if valid {
			t.Errorf("expected valid=false, got true")
		}
		// Should have an error for pan_check
		found := false
		for _, e := range errs {
			if e.Path == "pan_check" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error for pan_check, got: %v", errs)
		}
	})

	t.Run("CUE validation fails with blob schema", func(t *testing.T) {
		valid, errs := v.Validate(map[string]any{
			"pan": "INVALID", "amount": int64(-1), "currency": "999",
		})
		if valid {
			t.Errorf("expected valid=false, got true")
		}
		if len(errs) == 0 {
			t.Errorf("expected errors, got none")
		}
	})
}

// TestValidate_OriginalDataUnmodified ensures Validate() does not mutate the input.
func TestValidate_OriginalDataUnmodified(t *testing.T) {
	v := MustNew(`{
		name:   string
		upper:  string @blob(this.name.uppercase())
	}`)

	data := map[string]any{"name": "alice"}
	v.Validate(data)

	// Original data should be unchanged
	if data["name"] != "alice" {
		t.Errorf("input data was modified: name=%v", data["name"])
	}
	// No "upper" key should be injected into original data
	if _, exists := data["upper"]; exists {
		t.Errorf("Validate() should not inject computed fields into input data")
	}
}

// p1Removals lists every exported symbol that P1 deletes, as an executable
// checklist for P0-f.
//
// Method keys are "Receiver.Name"; package-level functions are bare names. The
// two ProcessWithMode entries are not a typo — one is a method taking data
// first, the other a package function taking a name first. That collision is
// among the reasons the surface is being reworked.
//
// When P1 removes a symbol, drop its entry here: the test asserts the listed
// names exist, so a stale entry fails rather than silently passing.
var p1Removals = []string{
	// Redundant Validator entry points, folded into Process/Validate.
	"Validator.ProcessValue",
	"Validator.ProcessValueContext",
	"Validator.ProcessValueWithMode",
	"Validator.ProcessValueWithModeContext",
	"Validator.ProcessWithMode",
	"Validator.ProcessWithModeContext",
	"Validator.ValidateValue",
	"Validator.ValidateValueContext",

	// Generic wrappers that promise type safety they cannot deliver.
	"ProcessStruct",
	"ProcessStructWithMode",
	"ValidateStruct",

	// Package-level global store.
	"ProcessWith",
	"ProcessWithMode",
	"ValidateWith",
	"Register",
	"MustRegister",
	"Get",
	"MustGet",
	"Unregister",
	"Has",
	"List",
	"Len",

	// Superseded by EnUS.Localize.
	"ValidationError.FriendlyMessage",
}

// TestP1RemovalsAreDeprecated covers P0-f: everything P1 deletes must carry a
// Deprecated marker first, so downstream sees a warning for one release cycle
// rather than a compile error out of nowhere.
//
// Checked by parsing the package rather than by eye — 23 markers spread over
// four files is exactly the sort of list where one gets missed.
func TestP1RemovalsAreDeprecated(t *testing.T) {
	docs := exportedFuncDocs(t)

	for _, name := range p1Removals {
		doc, ok := docs[name]
		if !ok {
			t.Errorf("%s: not found in the package; if P1 removed it, drop it from p1Removals", name)
			continue
		}
		if !strings.Contains(doc, "Deprecated:") {
			t.Errorf("%s: missing a Deprecated: marker", name)
		}
	}
}

// TestDeprecatedMarkersPointSomewhere guards the quality of those markers: a
// bare "Deprecated:" tells a caller to stop without saying what to use, which
// is worse than no marker at all because it offers no way forward.
func TestDeprecatedMarkersPointSomewhere(t *testing.T) {
	for name, doc := range exportedFuncDocs(t) {
		_, after, found := strings.Cut(doc, "Deprecated:")
		if !found {
			continue
		}
		if len(strings.Fields(after)) < 3 {
			t.Errorf("%s: Deprecated marker has no usable replacement text: %q",
				name, strings.TrimSpace(after))
		}
	}
}

// exportedFuncDocs maps exported function and method names to their doc
// comments. Methods are keyed "Receiver.Name", package functions by bare name.
func exportedFuncDocs(t *testing.T) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	docs := make(map[string]string)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !fn.Name.IsExported() {
					continue
				}
				docs[funcKey(fn)] = fn.Doc.Text()
			}
		}
	}
	if len(docs) == 0 {
		t.Fatal("parsed no exported functions; the parser filter is wrong")
	}
	return docs
}

// funcKey renders "Receiver.Name" for methods and "Name" for functions.
func funcKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if ident, ok := typ.(*ast.Ident); ok {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// TestCallOption covers the per-call option added ahead of the P1 reshape.
//
// It lands in P0 because the Deprecated markers have to point at something that
// actually exists: without it there is no non-deprecated way to pass a FailMode,
// and schemix/testing — a separate package — cannot reach an unexported helper.
// Adding a variadic parameter keeps every existing call compiling.
func TestCallOption(t *testing.T) {
	const schema = `{
		a: int & >0
		b: int & >0
	}`
	bothBad := map[string]any{"a": int64(-1), "b": int64(-1)}

	t.Run("existing calls keep working", func(t *testing.T) {
		v := MustNew(schema)
		if r := v.Process(map[string]any{"a": int64(1), "b": int64(1)}); !r.Valid {
			t.Fatalf("Process without options: valid = false, errors = %v", r.Errors)
		}
		if valid, errs := v.Validate(map[string]any{"a": int64(1), "b": int64(1)}); !valid {
			t.Fatalf("Validate without options: valid = false, errors = %v", errs)
		}
	})

	t.Run("FailMode is itself a CallOption", func(t *testing.T) {
		// No wrapper function: the mode constant is passed directly.
		v := MustNew(schema)
		r := v.Process(bothBad, FailFast)
		if len(r.Errors) != 1 {
			t.Fatalf("errors = %d, want 1 under FailFast: %v", len(r.Errors), r.Errors)
		}
	})

	t.Run("Validate accepts a mode", func(t *testing.T) {
		// Previously impossible: the Validate family hardcoded FailAll, so
		// callers wanting fail-fast had to pay for Output they discarded.
		v := MustNew(schema)
		_, errs := v.Validate(bothBad, FailFast)
		if len(errs) != 1 {
			t.Fatalf("errors = %d, want 1 under FailFast: %v", len(errs), errs)
		}
		_, all := v.Validate(bothBad, FailAll)
		if len(all) != 2 {
			t.Fatalf("errors = %d, want 2 under FailAll: %v", len(all), all)
		}
	})

	t.Run("context variants accept a mode", func(t *testing.T) {
		v := MustNew(schema)
		if r := v.ProcessContext(context.Background(), bothBad, FailFast); len(r.Errors) != 1 {
			t.Fatalf("ProcessContext errors = %d, want 1", len(r.Errors))
		}
		if _, errs := v.ValidateContext(context.Background(), bothBad, FailFast); len(errs) != 1 {
			t.Fatalf("ValidateContext errors = %d, want 1", len(errs))
		}
	})

	t.Run("the last option wins", func(t *testing.T) {
		// Standard functional-option semantics, worth pinning because a mode is
		// a scalar and callers may reasonably expect the first to stick.
		v := MustNew(schema)
		r := v.Process(bothBad, FailAll, FailFast)
		if len(r.Errors) != 1 {
			t.Fatalf("errors = %d, want 1: the last option must win", len(r.Errors))
		}
	})

	t.Run("an undefined mode still reports a config error", func(t *testing.T) {
		// Unchanged behaviour: making FailMode unconstructable is a P1 concern.
		v := MustNew(schema)
		r := v.Process(map[string]any{"a": int64(1), "b": int64(1)}, FailMode(99))
		if r.Valid {
			t.Fatal("valid = true, want false for an undefined mode")
		}
		if !r.HasCode(CodeConfigError) {
			t.Fatalf("codes = %v, want %s", r.Errors, CodeConfigError)
		}
	})
}

// TestProcessAcceptsEveryInputType covers P1-A: the four entry points take any
// supported input, which is what lets the eight ProcessValue/ValidateValue
// variants go away.
func TestProcessAcceptsEveryInputType(t *testing.T) {
	v := MustNew(`{
		order_id: =~"^ORD-[0-9]+$"
		amount:   int & >0
	}`)

	cases := []struct {
		name string
		in   any
	}{
		{"map", map[string]any{"order_id": "ORD-1", "amount": int64(100)}},
		{"struct", valueOrder{OrderID: "ORD-2", Amount: 200}},
		{"struct pointer", &valueOrder{OrderID: "ORD-3", Amount: 300}},
		{"JSON bytes", []byte(`{"order_id":"ORD-4","amount":400}`)},
		{"Processable", processableOrder{id: "ORD-5", amount: 500}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if r := v.Process(tc.in); !r.Valid {
				t.Fatalf("Process: valid = false, errors = %v", r.Errors)
			}
			if valid, errs := v.Validate(tc.in); !valid {
				t.Fatalf("Validate: valid = false, errors = %v", errs)
			}
			ctx := context.Background()
			if r := v.ProcessContext(ctx, tc.in); !r.Valid {
				t.Fatalf("ProcessContext: valid = false, errors = %v", r.Errors)
			}
			if valid, errs := v.ValidateContext(ctx, tc.in); !valid {
				t.Fatalf("ValidateContext: valid = false, errors = %v", errs)
			}
		})
	}

	t.Run("with a CallOption", func(t *testing.T) {
		bad := []byte(`{"order_id":"NOPE","amount":-1}`)
		r := v.Process(bad, FailFast)
		if r.Valid {
			t.Fatal("valid = true, want false")
		}
		if len(r.Errors) != 1 {
			t.Fatalf("errors = %d, want 1 under FailFast: %v", len(r.Errors), r.Errors)
		}
	})

	t.Run("an unsupported type names what it got and what it wanted", func(t *testing.T) {
		// This is the cost of accepting any: a type error that the compiler used
		// to catch now surfaces here. The message has to carry its weight.
		for _, in := range []any{42, "str", []int{1}, 3.14, true} {
			r := v.Process(in)
			if r.Valid {
				t.Fatalf("%T: valid = true, want false", in)
			}
			if !r.HasCode(CodeConfigError) {
				t.Fatalf("%T: codes = %v, want %s", in, r.Errors, CodeConfigError)
			}
			msg := r.Errors[0].Message
			if !strings.Contains(msg, "unsupported input type") {
				t.Errorf("%T: message = %q, want it to say the type is unsupported", in, msg)
			}
			if !strings.Contains(msg, "map[string]any") {
				t.Errorf("%T: message = %q, want it to list the accepted types", in, msg)
			}
		}
	})

	t.Run("nil is rejected", func(t *testing.T) {
		r := v.Process(nil)
		if r.Valid {
			t.Fatal("valid = true, want false")
		}
		if !r.HasCode(CodeConfigError) {
			t.Fatalf("codes = %v, want %s", r.Errors, CodeConfigError)
		}
	})
}

// valueOrder and processableOrder back the input-type table above.
type valueOrder struct {
	OrderID string `json:"order_id"`
	Amount  int64  `json:"amount"`
}

type processableOrder struct {
	id     string
	amount int64
}

func (p processableOrder) ToMap() map[string]any {
	return map[string]any{"order_id": p.id, "amount": p.amount}
}
