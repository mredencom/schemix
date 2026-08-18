package schemix

import (
	"slices"
	"testing"
)

func TestGlobalStore_RegisterAndGet(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	v := MustNew(`{ name: string, age: int & >=0 }`)
	Register("user", v)

	got, ok := Get("user")
	if !ok || got == nil {
		t.Fatal("Get returned not found")
	}

	r := got.Process(map[string]any{"name": "Alice", "age": int64(30)})
	if !r.Valid {
		t.Errorf("expected valid, got: %v", r.Errors)
	}
}

func TestGlobalStore_RegisterOverwrites(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("item", MustNew(`{ x: int }`))
	// Overwrite with different schema
	Register("item", MustNew(`{ y: string }`))

	v, _ := Get("item")
	// Should work with new schema (y: string), not old (x: int)
	r := v.Process(map[string]any{"y": "hello"})
	if !r.Valid {
		t.Errorf("expected valid after overwrite, got: %v", r.Errors)
	}
}

func TestGlobalStore_MustRegister_Success(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	// Should not panic
	MustRegister("order", MustNew(`{ id: string }`))

	if !Has("order") {
		t.Error("expected Has(order) == true")
	}
}

func TestGlobalStore_MustRegister_Conflict(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	MustRegister("payment", MustNew(`{ pan: string }`))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate MustRegister")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if msg == "" {
			t.Error("expected non-empty panic message")
		}
	}()
	MustRegister("payment", MustNew(`{ pan: string }`))
}

func TestGlobalStore_MustGet_Success(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("found", MustNew(`{ x: int }`))
	v := MustGet("found")
	if v == nil {
		t.Error("expected non-nil validator")
	}
}

func TestGlobalStore_MustGet_NotFound(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for MustGet on missing name")
		}
	}()
	MustGet("nonexistent")
}

func TestGlobalStore_Get_NotFound(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	v, ok := Get("missing")
	if ok || v != nil {
		t.Error("expected not found")
	}
}

func TestGlobalStore_Has(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	if Has("x") {
		t.Error("expected Has == false before register")
	}
	Register("x", MustNew(`{ v: bool }`))
	if !Has("x") {
		t.Error("expected Has == true after register")
	}
}

func TestGlobalStore_Unregister(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("temp", MustNew(`{ v: string }`))
	if !Unregister("temp") {
		t.Error("expected Unregister to return true")
	}
	if Has("temp") {
		t.Error("expected Has == false after unregister")
	}
	if Unregister("temp") {
		t.Error("expected Unregister to return false for missing")
	}
}

func TestGlobalStore_ListAndLen(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("a", MustNew(`{ x: int }`))
	Register("b", MustNew(`{ y: string }`))
	Register("c", MustNew(`{ z: bool }`))

	if Len() != 3 {
		t.Errorf("expected Len()==3, got %d", Len())
	}
	names := List()
	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}
	// List sorts its result, so the order is part of the contract.
	if want := []string{"a", "b", "c"}; !slices.Equal(names, want) {
		t.Errorf("List() = %v, want %v", names, want)
	}
}

// TestGlobalStore_List_sorted pins the ordering guarantee for the package-level
// store, which is backed by sync.Map and therefore had no inherent order.
func TestGlobalStore_List_sorted(t *testing.T) {
	t.Cleanup(resetGlobalStore)
	resetGlobalStore()

	for _, name := range []string{"user", "product", "zebra", "address", "mango"} {
		Register(name, MustNew(`{ x: int }`))
	}

	want := []string{"address", "mango", "product", "user", "zebra"}
	for i := range 50 {
		if got := List(); !slices.Equal(got, want) {
			t.Fatalf("iteration %d: List() = %v, want %v", i, got, want)
		}
	}
}

func TestGlobalStore_ProcessWith(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("calc", MustNew(`{
		price: number & >0
		qty:   int & >=1
		total: number @blob(this.price * this.qty)
	}`))

	// Valid — with struct
	type item struct {
		Price float64 `json:"price"`
		Qty   int64   `json:"qty"`
	}
	r := ProcessWith("calc", item{Price: 29.9, Qty: 3})
	if !r.Valid {
		t.Errorf("expected valid, got: %v", r.Errors)
	}
	total, _ := r.Output["total"].(float64)
	if total < 89.6 || total > 89.8 {
		t.Errorf("expected total≈89.7, got %v", total)
	}

	// Invalid data
	r = ProcessWith("calc", item{Price: -1, Qty: 0})
	if r.Valid {
		t.Error("expected invalid")
	}

	// Unknown name
	r = ProcessWith("unknown", map[string]any{})
	if r.Valid {
		t.Error("expected invalid for unknown name")
	}
	if r.Errors[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got %s", r.Errors[0].Code)
	}
}

func TestGlobalStore_ValidateWith(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("check", MustNew(`{ code: =~"^[A-Z]{3}$" }`))

	valid, errs := ValidateWith("check", map[string]any{"code": "USD"})
	if !valid {
		t.Errorf("expected valid, got: %v", errs)
	}

	valid, _ = ValidateWith("check", map[string]any{"code": "bad"})
	if valid {
		t.Error("expected invalid")
	}

	// Unknown name
	valid, errs = ValidateWith("nope", map[string]any{})
	if valid {
		t.Error("expected invalid for unknown name")
	}
	if errs[0].Code != CodeConfigError {
		t.Errorf("expected CodeConfigError, got %s", errs[0].Code)
	}
}

func TestGlobalStore_ProcessWithMode_FailFast(t *testing.T) {
	t.Cleanup(resetGlobalStore)

	Register("strict", MustNew(`{
		a: int & >0
		b: string
		c: =~"^[0-9]+$"
	}`))

	r := ProcessWithMode("strict", map[string]any{"a": int64(-1), "b": int64(0), "c": "abc"}, FailFast)
	if r.Valid {
		t.Error("expected invalid")
	}
	if len(r.Errors) > 1 {
		t.Errorf("FailFast should produce 1 error, got %d", len(r.Errors))
	}
}
