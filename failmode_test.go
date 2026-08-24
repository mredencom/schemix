package schemix

import (
	"testing"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

func TestFailModeString(t *testing.T) {
	tests := []struct {
		mode FailMode
		want string
	}{
		{FailAll, "all"},
		{FailFast, "fast"},
		{FailPriority, "priority"},
		{FailMode(99), "unknown"},
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

// TestInvalidFailModeProcess verifies that ProcessWithMode with an undefined
// FailMode value does NOT panic and returns a structured error result.
func TestInvalidFailModeProcess(t *testing.T) {
	v := MustNew(`{ name: string }`)
	data := map[string]any{"name": "Alice"}

	// Must not panic
	r := v.ProcessWithMode(data, FailMode(99))

	if r.Valid {
		t.Fatal("expected Valid=false for invalid FailMode")
	}
	if r.Output != nil {
		t.Fatalf("expected nil Output for invalid FailMode, got %v", r.Output)
	}
	if !r.HasCode(CodeConfigError) {
		t.Fatalf("expected error code E0C01, got errors: %v", r.Errors)
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
	e := r.Errors[0]
	if e.Type != "config" {
		t.Errorf("expected Type=%q, got %q", "config", e.Type)
	}
	if e.Path != "" {
		t.Errorf("expected empty Path for config error, got %q", e.Path)
	}
}

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
			r := v.ProcessWithMode(data, tc.mode)
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
	releaseEnv(globalBloblangEnvironment)
	t.Cleanup(func() { releaseEnv(globalBloblangEnvironment) })
	reg := NewRegistry()
	if err := reg.Register("test", `{ name: string }`); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterAll(); err != nil {
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
			env := bloblang.GlobalEnvironment()
			_, err := env.Parse(tc.mapping)
			if err == nil {
				t.Fatal("expected error from mapping construction with invalid mode, got nil")
			}
		})
	}
}

// TestValidModeBloblangMapping verifies that valid mode strings construct and execute.
func TestValidModeBloblangMapping(t *testing.T) {
	releaseEnv(globalBloblangEnvironment)
	t.Cleanup(func() { releaseEnv(globalBloblangEnvironment) })
	reg := NewRegistry()
	if err := reg.Register("test", `{ name: string }`); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterAll(); err != nil {
		t.Fatal(err)
	}

	validModes := []string{"all", "fast", "priority"}

	for _, mode := range validModes {
		t.Run("method_validate_"+mode, func(t *testing.T) {
			env := bloblang.GlobalEnvironment()
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

			r := v.ProcessWithMode(data, tc.mode)
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
