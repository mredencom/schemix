package schemix

import (
	"slices"
	"strings"
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
	// List sorts its result, so the order is part of the contract.
	want := []string{"payment", "test_schema"}
	if !slices.Equal(names, want) {
		t.Errorf("List() = %v, want %v", names, want)
	}
}

// TestRegistry_List_sorted pins the ordering guarantee. Before it existed, List
// walked the internal map and returned a different order on different runs,
// which made any caller that displayed or compared the result nondeterministic.
func TestRegistry_List_sorted(t *testing.T) {
	reg := NewRegistry()
	// Register in an order that is neither sorted nor reverse-sorted.
	for _, name := range []string{"user", "product", "zebra", "address", "mango"} {
		if err := reg.Register(name, `{ x: int }`); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	want := []string{"address", "mango", "product", "user", "zebra"}
	// Repeat: a map-order bug would surface only intermittently.
	for i := range 50 {
		if got := reg.List(); !slices.Equal(got, want) {
			t.Fatalf("iteration %d: List() = %v, want %v", i, got, want)
		}
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

// TestRegisterWithOptions covers P0-a: Register accepts construction options.
//
// Before this, Registry.Register had no way to pass a construction Option, so a
// schema registered for a Benthos pipeline could not use custom Bloblang
// functions — the two features were mutually exclusive. That was the only
// outright functional gap in the API, not merely an ergonomic one.
func TestRegisterWithOptions(t *testing.T) {
	t.Run("custom method reaches the Bloblang plugin", func(t *testing.T) {
		reg := NewRegistry()
		// is_allowed_bin returns bool, so it is a validation rule; brand
		// returns a string, so it is a computed field. Exercising both proves
		// the custom method is visible on each path.
		err := reg.Register("payment", `{
			pan:    string
			bin_ok: bool   @blob(this.pan.is_allowed_bin())
			brand:  string @blob(if this.pan.is_allowed_bin() { "UnionPay" } else { "other" })
		}`, WithMethod("is_allowed_bin", func(v any) (any, error) {
			s, _ := v.(string)
			return len(s) > 2 && s[:2] == "62", nil
		}))
		if err != nil {
			t.Fatalf("Register with WithMethod: %v", err)
		}

		env := bloblang.NewEnvironment()
		t.Cleanup(func() { releaseEnv(env) })
		if err := reg.RegisterAllTo(env); err != nil {
			t.Fatalf("RegisterAllTo: %v", err)
		}
		exec, err := env.Parse(`root = this.process_schema(name: "payment")`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}

		query := func(t *testing.T, pan string) map[string]any {
			t.Helper()
			got, err := exec.Query(map[string]any{"pan": pan})
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			result, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("result type = %T, want map[string]any", got)
			}
			return result
		}

		// The negative case is what proves the method actually ran. A skipped
		// rule would also leave valid == true, so the accepting case alone
		// cannot distinguish "method works" from "method never called".
		rejected := query(t, "4111111111111111")
		if rejected["valid"] != false {
			t.Fatalf("valid = %v for a non-62 pan, want false; the custom method did not run",
				rejected["valid"])
		}

		accepted := query(t, "6212345678901234")
		if accepted["valid"] != true {
			t.Fatalf("valid = %v, want true (errors: %v)", accepted["valid"], accepted["errors"])
		}
		output, ok := accepted["output"].(map[string]any)
		if !ok {
			t.Fatalf("output type = %T, want map[string]any", accepted["output"])
		}
		if output["brand"] != "UnionPay" {
			t.Fatalf("output[brand] = %v, want %q", output["brand"], "UnionPay")
		}
	})

	t.Run("registry name wins over WithName", func(t *testing.T) {
		// The registry key must be what labels metrics: a validator filed under
		// "canonical" that reports itself as "impostor" makes the metric
		// impossible to correlate with the schema it came from.
		rec := &fakeRecorder{}
		reg := NewRegistry()
		err := reg.Register("canonical", `{value: string}`,
			WithName("impostor"),
			WithMetricsRecorder(rec),
		)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		v, ok := reg.Get("canonical")
		if !ok {
			t.Fatal("Get(canonical) = false, want true")
		}
		v.Validate(map[string]any{"value": "ok"})

		if len(rec.validationCalls) != 1 {
			t.Fatalf("ObserveValidation calls = %d, want 1", len(rec.validationCalls))
		}
		if got := rec.validationCalls[0].schemaName; got != "canonical" {
			t.Fatalf("schemaName = %q, want %q (the registry key must win)", got, "canonical")
		}
	})

	t.Run("option error is wrapped with the name", func(t *testing.T) {
		reg := NewRegistry()
		// An invalid custom name fails at construction; Register must surface
		// that rather than storing a broken validator.
		err := reg.Register("bad", `{value: string}`,
			WithMethod("NotSnakeCase", func(any) (any, error) { return true, nil }),
		)
		if err == nil {
			t.Fatal("Register with an invalid option = nil error, want failure")
		}
		if !strings.Contains(err.Error(), `register "bad"`) {
			t.Fatalf("error = %q, want it to name the registration", err)
		}
		if reg.Has("bad") {
			t.Fatal(`Has("bad") = true; a failed Register must not store anything`)
		}
	})

	t.Run("does not mutate the caller's option slice", func(t *testing.T) {
		// Register appends WithName internally. Doing that on the caller's
		// backing array would overwrite whatever follows the passed slice —
		// the classic append-aliasing trap.
		opts := make([]Option, 1, 4)
		opts[0] = WithMetricsRecorder(&fakeRecorder{})
		sentinel := WithName("sentinel-must-survive")
		full := opts[:2:4]
		full[1] = sentinel

		reg := NewRegistry()
		if err := reg.Register("aliasing", `{value: string}`, opts...); err != nil {
			t.Fatalf("Register: %v", err)
		}

		// full[1] must still be the sentinel, not Register's internal WithName.
		probe := &validatorConfig{}
		full[1](probe)
		if probe.schemaName != "sentinel-must-survive" {
			t.Fatalf("caller's slice was overwritten: schemaName = %q, want %q",
				probe.schemaName, "sentinel-must-survive")
		}
	})

	t.Run("no options behaves as before", func(t *testing.T) {
		reg := NewRegistry()
		if err := reg.Register("plain", `{value: string}`); err != nil {
			t.Fatalf("Register without options: %v", err)
		}
		v, ok := reg.Get("plain")
		if !ok {
			t.Fatal("Get(plain) = false, want true")
		}
		if valid, errs := v.Validate(map[string]any{"value": "ok"}); !valid {
			t.Fatalf("Validate = false, errs = %v", errs)
		}
	})
}
