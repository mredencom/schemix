package schemix

import (
	"testing"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

// helper: compile bloblang mapping and execute against data.
func execMapping(t *testing.T, mapping string, data map[string]any) map[string]any {
	t.Helper()
	exec, err := bloblang.Parse(mapping)
	if err != nil {
		t.Fatalf("parse mapping: %v", err)
	}
	res, err := exec.Query(data)
	if err != nil {
		t.Fatalf("exec mapping: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	return m
}

func setupRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Register("test_schema", `{
		name: string
		age:  int & >=0 & <=150
	}`); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	if err := reg.Register("payment", `{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"

		pan_check: bool @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	}`); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return reg
}

// ---------- Method Tests ----------

func TestMethodValidateSchema_Valid(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	data := map[string]any{"name": "Alice", "age": int64(30)}
	r := execMapping(t, `root = this.validate_schema(name: "test_schema")`, data)

	if r["valid"] != true {
		t.Errorf("expected valid=true, got %v", r["valid"])
	}
}

func TestMethodValidateSchema_Invalid(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	data := map[string]any{"name": "Alice", "age": int64(200)}
	r := execMapping(t, `root = this.validate_schema(name: "test_schema")`, data)

	if r["valid"] != false {
		t.Errorf("expected valid=false, got %v", r["valid"])
	}
	errs, ok := r["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Errorf("expected errors, got %v", r["errors"])
	}
}

func TestMethodValidateSchema_WithMode(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	// Use fast mode — should still work, returning at most 1 error
	data := map[string]any{"name": 123, "age": int64(200)}
	r := execMapping(t, `root = this.validate_schema(name: "test_schema", mode: "fast")`, data)

	if r["valid"] != false {
		t.Errorf("expected valid=false, got %v", r["valid"])
	}
	errs, ok := r["errors"].([]any)
	if !ok {
		t.Fatalf("expected errors slice, got %T", r["errors"])
	}
	if len(errs) != 1 {
		t.Errorf("fast mode should return at most 1 error, got %d", len(errs))
	}
}

func TestMethodProcessSchema_Valid(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	data := map[string]any{
		"pan": "6222021234567890", "amount": int64(10000), "currency": "156",
	}
	r := execMapping(t, `root = this.process_schema(name: "payment")`, data)

	if r["valid"] != true {
		t.Errorf("expected valid=true, got %v", r["valid"])
	}
	output, ok := r["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected output map, got %T", r["output"])
	}
	if output["card_brand"] != "UnionPay" {
		t.Errorf("expected card_brand=UnionPay, got %v", output["card_brand"])
	}
}

func TestMethodProcessSchema_WithMode(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	data := map[string]any{
		"pan": "invalid", "amount": int64(-1), "currency": "999",
	}
	r := execMapping(t, `root = this.process_schema(name: "payment", mode: "fast")`, data)

	if r["valid"] != false {
		t.Errorf("expected valid=false, got %v", r["valid"])
	}
	errs, ok := r["errors"].([]any)
	if !ok {
		t.Fatalf("expected errors slice, got %T", r["errors"])
	}
	if len(errs) != 1 {
		t.Errorf("fast mode should return at most 1 error, got %d", len(errs))
	}
}

// ---------- Function Tests ----------

func TestFunctionValidateSchema_Valid(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterFunctions(); err != nil {
		t.Fatalf("register functions: %v", err)
	}

	data := map[string]any{"name": "Alice", "age": int64(30)}
	r := execMapping(t, `root = validate_schema(data: this, name: "test_schema")`, data)

	if r["valid"] != true {
		t.Errorf("expected valid=true, got %v", r["valid"])
	}
}

func TestFunctionValidateSchema_Invalid(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterFunctions(); err != nil {
		t.Fatalf("register functions: %v", err)
	}

	data := map[string]any{"name": "Alice", "age": int64(200)}
	r := execMapping(t, `root = validate_schema(data: this, name: "test_schema")`, data)

	if r["valid"] != false {
		t.Errorf("expected valid=false, got %v", r["valid"])
	}
}

func TestFunctionValidateSchema_WithMode(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterFunctions(); err != nil {
		t.Fatalf("register functions: %v", err)
	}

	data := map[string]any{"name": 123, "age": int64(200)}
	r := execMapping(t, `root = validate_schema(data: this, name: "test_schema", mode: "fast")`, data)

	if r["valid"] != false {
		t.Errorf("expected valid=false, got %v", r["valid"])
	}
	errs, ok := r["errors"].([]any)
	if !ok {
		t.Fatalf("expected errors slice, got %T", r["errors"])
	}
	if len(errs) != 1 {
		t.Errorf("fast mode should return at most 1 error, got %d", len(errs))
	}
}

func TestFunctionProcessSchema_Valid(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterFunctions(); err != nil {
		t.Fatalf("register functions: %v", err)
	}

	data := map[string]any{
		"pan": "6222021234567890", "amount": int64(10000), "currency": "156",
	}
	r := execMapping(t, `root = process_schema(data: this, name: "payment")`, data)

	if r["valid"] != true {
		t.Errorf("expected valid=true, got %v", r["valid"])
	}
	output, ok := r["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected output map, got %T", r["output"])
	}
	if output["card_brand"] != "UnionPay" {
		t.Errorf("expected card_brand=UnionPay, got %v", output["card_brand"])
	}
}

func TestFunctionProcessSchema_WithMode(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterFunctions(); err != nil {
		t.Fatalf("register functions: %v", err)
	}

	data := map[string]any{
		"pan": "invalid", "amount": int64(-1), "currency": "999",
	}
	r := execMapping(t, `root = process_schema(data: this, name: "payment", mode: "fast")`, data)

	if r["valid"] != false {
		t.Errorf("expected valid=false, got %v", r["valid"])
	}
	errs, ok := r["errors"].([]any)
	if !ok {
		t.Fatalf("expected errors slice, got %T", r["errors"])
	}
	if len(errs) != 1 {
		t.Errorf("fast mode should return at most 1 error, got %d", len(errs))
	}
}

// ---------- Function Dynamic Data Tests ----------

func TestFunctionDynamicDataParam(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterFunctions(); err != nil {
		t.Fatalf("register functions: %v", err)
	}

	// Use this.payload as dynamic data — NOT a static value
	data := map[string]any{
		"payload": map[string]any{"name": "Bob", "age": int64(25)},
	}
	r := execMapping(t, `root = validate_schema(data: this.payload, name: "test_schema")`, data)

	if r["valid"] != true {
		t.Errorf("expected valid=true with dynamic data, got %v", r["valid"])
	}
}

// ---------- RegisterAll Tests ----------

func TestRegisterAll(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterAll(); err != nil {
		t.Fatalf("register all: %v", err)
	}

	data := map[string]any{"name": "Alice", "age": int64(30)}

	// Method should work
	r := execMapping(t, `root = this.validate_schema(name: "test_schema")`, data)
	if r["valid"] != true {
		t.Errorf("method: expected valid=true, got %v", r["valid"])
	}

	// Function should work
	r = execMapping(t, `root = validate_schema(data: this, name: "test_schema")`, data)
	if r["valid"] != true {
		t.Errorf("function: expected valid=true, got %v", r["valid"])
	}
}

// ---------- Edge Cases ----------

func TestInvalidMode(t *testing.T) {
	reg := setupRegistry(t)
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	data := map[string]any{"name": "Alice", "age": int64(30)}

	// Invalid mode should still work, defaulting to "all"
	r := execMapping(t, `root = this.validate_schema(name: "test_schema", mode: "invalid")`, data)
	if r["valid"] != true {
		t.Errorf("expected valid=true with invalid mode (defaults to all), got %v", r["valid"])
	}
}

func TestUnregisteredSchema(t *testing.T) {
	reg := NewRegistry()
	if err := reg.RegisterMethods(); err != nil {
		t.Fatalf("register methods: %v", err)
	}

	exec, err := bloblang.Parse(`root = this.validate_schema(name: "nonexistent")`)
	if err == nil {
		// If parse succeeded, the error should come at runtime
		_, err = exec.Query(map[string]any{"x": 1})
		if err == nil {
			t.Error("expected error for unregistered schema")
		}
	}
	// Either parse or exec error is acceptable
}

// ---------- deepCopy Tests ----------

func TestDeepCopy_PreservesInt64(t *testing.T) {
	src := map[string]any{
		"amount": int64(10000),
		"nested": map[string]any{
			"value": int64(42),
		},
	}
	dst := deepCopy(src)

	if v, ok := dst["amount"].(int64); !ok || v != 10000 {
		t.Errorf("expected int64(10000), got %T(%v)", dst["amount"], dst["amount"])
	}
	nested := dst["nested"].(map[string]any)
	if v, ok := nested["value"].(int64); !ok || v != 42 {
		t.Errorf("expected int64(42), got %T(%v)", nested["value"], nested["value"])
	}

	// Mutating dst should not affect src
	dst["amount"] = int64(0)
	if src["amount"] != int64(10000) {
		t.Error("deepCopy did not isolate src from dst")
	}
}

func TestDeepCopy_PreservesSlice(t *testing.T) {
	src := map[string]any{
		"items": []any{
			map[string]any{"id": int64(1)},
			map[string]any{"id": int64(2)},
		},
	}
	dst := deepCopy(src)

	items := dst["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	item0 := items[0].(map[string]any)
	if v, ok := item0["id"].(int64); !ok || v != 1 {
		t.Errorf("expected int64(1), got %T(%v)", item0["id"], item0["id"])
	}

	// Mutating dst slice should not affect src
	items[0].(map[string]any)["id"] = int64(99)
	srcItems := src["items"].([]any)
	if srcItems[0].(map[string]any)["id"] != int64(1) {
		t.Error("deepCopy did not isolate slice elements")
	}
}

func TestDeepCopy_NilMap(t *testing.T) {
	if got := deepCopy(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// ---------- isEmpty Tests ----------

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected bool
	}{
		{"nil", nil, true},
		{"empty string", "", true},
		{"non-empty string", "hello", false},
		{"int zero", int(0), true},
		{"int non-zero", int(42), false},
		{"int64 zero", int64(0), true},
		{"int64 non-zero", int64(100), false},
		{"float64 zero", float64(0), true},
		{"float64 non-zero", float64(3.14), false},
		{"bool false", false, true},
		{"bool true", true, false},
		{"empty slice", []any{}, true},
		{"non-empty slice", []any{1}, false},
		{"empty map", map[string]any{}, true},
		{"non-empty map", map[string]any{"k": "v"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEmpty(tt.input)
			if got != tt.expected {
				t.Errorf("isEmpty(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------- Result convenience methods ----------

func TestResult_Err_Valid(t *testing.T) {
	r := Result{Valid: true, Errors: []ValidationError{}}
	if err := r.Err(); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestResult_Err_Invalid(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeTypeMismatch, Path: "name", Type: TypeCUE, Message: "type error"},
			{Code: CodeRangeViolation, Path: "age", Type: TypeCUE, Message: "out of range"},
		},
	}
	err := r.Err()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	s := err.Error()
	if !contains(s, "name") || !contains(s, "age") {
		t.Errorf("error should mention both fields, got: %s", s)
	}
}

func TestResult_FirstError(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Code: CodeTypeMismatch, Path: "x", Type: TypeCUE, Message: "first"},
			{Code: CodeRangeViolation, Path: "y", Type: TypeCUE, Message: "second"},
		},
	}
	first := r.FirstError()
	if first == nil || first.Message != "first" {
		t.Errorf("expected first error, got %v", first)
	}
}

func TestResult_FirstError_Empty(t *testing.T) {
	r := Result{Valid: true, Errors: []ValidationError{}}
	if r.FirstError() != nil {
		t.Error("expected nil for valid result")
	}
}

func TestResult_ErrorsByPath(t *testing.T) {
	r := Result{
		Valid: false,
		Errors: []ValidationError{
			{Path: "a"}, {Path: "b"}, {Path: "a"},
		},
	}
	got := r.ErrorsByPath("a")
	if len(got) != 2 {
		t.Errorf("expected 2 errors for path 'a', got %d", len(got))
	}
}

func TestResult_Errors_NotNil(t *testing.T) {
	v := MustNew(`{ name: string }`)
	r := v.Process(map[string]any{"name": "ok"})
	if r.Errors == nil {
		t.Error("Errors should be empty slice, not nil")
	}
	if len(r.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(r.Errors))
	}
}

// ---------- ValidationError implements error ----------

func TestValidationError_Error(t *testing.T) {
	e := ValidationError{
		Code: CodeTypeMismatch, Path: "name", Type: TypeCUE, Message: "conflicting values",
	}
	s := e.Error()
	if s != "[E1T01] name: conflicting values" {
		t.Errorf("unexpected error string: %s", s)
	}
}

// ---------- MustNew ----------

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

// ---------- Registry management ----------

func TestRegistry_Has(t *testing.T) {
	reg := setupRegistry(t)
	if !reg.Has("test_schema") {
		t.Error("expected Has(test_schema) = true")
	}
	if reg.Has("nonexistent") {
		t.Error("expected Has(nonexistent) = false")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := setupRegistry(t)
	if !reg.Unregister("test_schema") {
		t.Error("expected Unregister to return true")
	}
	if reg.Has("test_schema") {
		t.Error("expected schema to be removed")
	}
	if reg.Unregister("test_schema") {
		t.Error("expected second Unregister to return false")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := setupRegistry(t)
	names := reg.List()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
	// Check both exist (order not guaranteed)
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["test_schema"] || !found["payment"] {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestRegistry_Len(t *testing.T) {
	reg := setupRegistry(t)
	if reg.Len() != 2 {
		t.Errorf("expected Len()=2, got %d", reg.Len())
	}
	reg.Unregister("test_schema")
	if reg.Len() != 1 {
		t.Errorf("expected Len()=1 after unregister, got %d", reg.Len())
	}
}

// ---------- NewWithContext ----------

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

// ---------- Process preserves int64 in output ----------

func TestProcess_PreservesInt64InOutput(t *testing.T) {
	v := MustNew(`{
		amount: int & >0
		doubled: number @blob(this.amount * 2)
	}`)
	r := v.Process(map[string]any{"amount": int64(500)})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}
	// Original field should still be int64
	if _, ok := r.Output["amount"].(int64); !ok {
		t.Errorf("expected output.amount to be int64, got %T", r.Output["amount"])
	}
}

// ---------- helpers ----------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
