package schemix

import (
	"testing"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

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
