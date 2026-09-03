package schemix

import (
	"testing"
)

// TestFailAllIsTheZeroValue pins a contract the schemixtest package depends
// on: TestCase.Mode is a plain field, so an unset one must mean FailAll.
func TestFailAllIsTheZeroValue(t *testing.T) {
	var zero FailMode
	if zero != FailAll {
		t.Fatalf("zero FailMode = %v, want FailAll", zero)
	}
	if failModeString(zero) != ModeAll {
		t.Fatalf("failModeString(zero) = %q, want %q", failModeString(zero), ModeAll)
	}
}

// TestFailModeIsNotConstructableOutsidePackage documents what the struct buys.
//
// FailMode's only field is unexported, so no caller can write FailMode(99) or
// FailMode{n: 99} — the compiler rejects both. That removes a whole class of
// bug at the type level rather than reporting it at runtime: a bad mode used to
// come back as an invalid Result with E0C01, which a handler mapping invalid
// results to 422 would report as the user's fault.
//
// Inside the package the value is still constructable, which is what lets the
// defensive paths below stay tested.
func TestFailModeIsNotConstructableOutsidePackage(t *testing.T) {
	unknown := FailMode{n: 99}

	t.Run("labels as unknown", func(t *testing.T) {
		if got := failModeString(unknown); got != "unknown" {
			t.Fatalf("failModeString = %q, want %q", got, "unknown")
		}
	})

	t.Run("degrades to collecting everything", func(t *testing.T) {
		// Every mode check is an equality test against FailFast or
		// FailPriority, so an unrecognised value behaves as FailAll — the most
		// conservative outcome. It cannot skip validation.
		v := MustNew(`{
			a: int & >0
			b: int & >0
		}`)
		bad := map[string]any{"a": int64(-1), "b": int64(-1)}

		r := v.processMap(bad, unknown)
		if r.Valid {
			t.Fatal("valid = true; an unrecognised mode must not skip validation")
		}
		if len(r.Errors) != 2 {
			t.Fatalf("errors = %d, want 2 (same as FailAll): %v", len(r.Errors), r.Errors)
		}
	})
}

func TestFailModeString(t *testing.T) {
	tests := []struct {
		mode FailMode
		want string
	}{
		{FailAll, "all"},
		{FailFast, "fast"},
		{FailPriority, "priority"},
		{FailMode{n: 99}, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := failModeString(tt.mode)
			if got != tt.want {
				t.Errorf("failModeString(%d) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// TestValidFailModesStillWork ensures FailAll, FailFast, FailPriority execute normally.
func TestValidFailModesStillWork(t *testing.T) {
	v := MustNew(`{ name: string, age: int }`)
	data := map[string]any{"name": "Bob", "age": int64(25)}

	modes := []struct {
		name string
		mode FailMode
	}{
		{"FailAll", FailAll},
		{"FailFast", FailFast},
		{"FailPriority", FailPriority},
	}

	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			r := v.Process(data, tc.mode)
			if !r.Valid {
				t.Errorf("expected Valid=true for %s, got errors: %v", tc.name, r.Errors)
			}
			if r.Output == nil {
				t.Errorf("expected non-nil Output for %s", tc.name)
			}
		})
	}
}

// TestInvalidFailModeBloblangMapping verifies that Bloblang plugins reject
// invalid mode strings at mapping construction time (not at execution time).
func TestInvalidFailModeBloblangMapping(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("test", `{ name: string }`); err != nil {
		t.Fatal(err)
	}
	env := newTestEnv(t)
	if err := reg.RegisterAllTo(env); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		mapping string
	}{
		{"method_validate", `root = this.validate_schema(name: "test", mode: "invalid")`},
		{"method_process", `root = this.process_schema(name: "test", mode: "invalid")`},
		{"func_validate", `root = validate_schema(data: this, name: "test", mode: "invalid")`},
		{"func_process", `root = process_schema(data: this, name: "test", mode: "invalid")`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.Parse(tc.mapping)
			if err == nil {
				t.Fatal("expected error from mapping construction with invalid mode, got nil")
			}
		})
	}
}

// TestValidModeBloblangMapping verifies that valid mode strings construct and execute.
func TestValidModeBloblangMapping(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("test", `{ name: string }`); err != nil {
		t.Fatal(err)
	}
	env := newTestEnv(t)
	if err := reg.RegisterAllTo(env); err != nil {
		t.Fatal(err)
	}

	validModes := []string{"all", "fast", "priority"}

	for _, mode := range validModes {
		t.Run("method_validate_"+mode, func(t *testing.T) {
			exe, err := env.Parse(`root = this.validate_schema(name: "test", mode: "` + mode + `")`)
			if err != nil {
				t.Fatalf("expected no error for mode %q, got: %v", mode, err)
			}
			res, err := exe.Query(map[string]any{"name": "Alice"})
			if err != nil {
				t.Fatalf("execution failed: %v", err)
			}
			m, ok := res.(map[string]any)
			if !ok {
				t.Fatalf("expected map result, got %T", res)
			}
			if m["valid"] != true {
				t.Errorf("expected valid=true for mode %q, got %v", mode, m["valid"])
			}
		})
	}
}

// =============================================================================
// Fast path tests (three-state, precision, types, arrays)
// =============================================================================

// TestFailFastStopsWalkingFields pins that FailFast stops visiting fields at the
// first failure rather than collecting every error and truncating to one. The
// two are indistinguishable from Result — both yield exactly one error — so the
// evidence has to come from elsewhere: ObserveFastpathDecision fires once per
// field carrying a fast descriptor, so its call count reveals how far the walk
// got.
//
// FailAll is asserted alongside as the control. Without it, an early return
// leaking into the other modes would silently drop errors and this test would
// still pass.
func TestFailFastStopsWalkingFields(t *testing.T) {
	const schema = `{
		a: int & >=0
		b: int & >=0
		c: int & >=0
	}`
	data := map[string]any{"a": int64(-1), "b": int64(-2), "c": int64(-3)}

	tests := []struct {
		mode          FailMode
		wantErrors    int
		wantFieldsHit int
	}{
		{FailFast, 1, 1},
		{FailAll, 3, 3},
	}

	for _, tc := range tests {
		t.Run(failModeString(tc.mode), func(t *testing.T) {
			rec := &fakeRecorder{}
			v, err := New(schema, WithMetricsRecorder(rec))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			r := v.Process(data, tc.mode)
			if r.Valid {
				t.Fatal("expected an invalid result")
			}
			if got := len(r.Errors); got != tc.wantErrors {
				t.Errorf("errors = %d, want %d", got, tc.wantErrors)
			}
			if got := len(rec.fastpathCalls); got != tc.wantFieldsHit {
				t.Errorf("fields visited = %d, want %d — FailFast must not walk past the first failure",
					got, tc.wantFieldsHit)
			}
		})
	}
}
