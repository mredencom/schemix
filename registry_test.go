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
	releaseEnv(globalBloblangEnvironment)
	t.Cleanup(func() { releaseEnv(globalBloblangEnvironment) })
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

	// Invalid mode now causes a construction-time error (E0C01 contract)
	env := bloblang.GlobalEnvironment()
	_, err := env.Parse(`root = this.validate_schema(name: "test_schema", mode: "invalid")`)
	if err == nil {
		t.Fatal("expected error from mapping construction with invalid mode, got nil")
	}
}

func TestUnregisteredSchema(t *testing.T) {
	releaseEnv(globalBloblangEnvironment)
	t.Cleanup(func() { releaseEnv(globalBloblangEnvironment) })
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
