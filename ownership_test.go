package schemix

import (
	"strings"
	"testing"

	"github.com/warpstreamlabs/bento/public/bloblang"
)

func registryWithSchema(t *testing.T, name string) *Registry {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Register(name, `{ value: string }`); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return reg
}

func TestRegisterAllToScopedEnvironment(t *testing.T) {
	reg := registryWithSchema(t, "test")
	env := bloblang.NewEnvironment()
	t.Cleanup(func() { releaseEnv(env) })

	if err := reg.RegisterAllTo(env); err != nil {
		t.Fatalf("RegisterAllTo: %v", err)
	}

	exec, err := env.Parse(`root = this.validate_schema(name: "test")`)
	if err != nil {
		t.Fatalf("parse scoped method: %v", err)
	}
	got, err := exec.Query(map[string]any{"value": "ok"})
	if err != nil {
		t.Fatalf("query scoped method: %v", err)
	}
	result, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", got)
	}
	if result["valid"] != true {
		t.Fatalf("valid = %v, want true", result["valid"])
	}
}

func TestRegisterComponentsToScopedEnvironment(t *testing.T) {
	t.Run("methods only", func(t *testing.T) {
		reg := registryWithSchema(t, "test")
		env := bloblang.NewEmptyEnvironment()
		t.Cleanup(func() { releaseEnv(env) })

		if err := reg.RegisterMethodsTo(env); err != nil {
			t.Fatalf("RegisterMethodsTo: %v", err)
		}
		if _, err := env.Parse(`root = this.validate_schema(name: "test")`); err != nil {
			t.Fatalf("method unavailable: %v", err)
		}
		if _, err := env.Parse(`root = validate_schema(data: this, name: "test")`); err == nil {
			t.Fatal("function unexpectedly available in methods-only environment")
		}
	})

	t.Run("functions only", func(t *testing.T) {
		reg := registryWithSchema(t, "test")
		env := bloblang.NewEmptyEnvironment()
		t.Cleanup(func() { releaseEnv(env) })

		if err := reg.RegisterFunctionsTo(env); err != nil {
			t.Fatalf("RegisterFunctionsTo: %v", err)
		}
		if _, err := env.Parse(`root = validate_schema(data: this, name: "test")`); err != nil {
			t.Fatalf("function unavailable: %v", err)
		}
		if _, err := env.Parse(`root = this.validate_schema(name: "test")`); err == nil {
			t.Fatal("method unexpectedly available in functions-only environment")
		}
	})
}

func TestEnvironmentOwnership(t *testing.T) {
	t.Run("same registry can add other component", func(t *testing.T) {
		reg := registryWithSchema(t, "test")
		env := bloblang.NewEnvironment()
		t.Cleanup(func() { releaseEnv(env) })

		if err := reg.RegisterMethodsTo(env); err != nil {
			t.Fatalf("RegisterMethodsTo: %v", err)
		}
		if err := reg.RegisterFunctionsTo(env); err != nil {
			t.Fatalf("RegisterFunctionsTo after methods: %v", err)
		}
	})

	t.Run("same component cannot be registered twice", func(t *testing.T) {
		reg := registryWithSchema(t, "test")
		env := bloblang.NewEnvironment()
		t.Cleanup(func() { releaseEnv(env) })

		if err := reg.RegisterMethodsTo(env); err != nil {
			t.Fatalf("first RegisterMethodsTo: %v", err)
		}
		err := reg.RegisterMethodsTo(env)
		if err == nil || !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("second RegisterMethodsTo error = %v, want already registered", err)
		}
	})

	t.Run("different registry cannot own same environment", func(t *testing.T) {
		regA := registryWithSchema(t, "a")
		regB := registryWithSchema(t, "b")
		env := bloblang.NewEnvironment()
		t.Cleanup(func() { releaseEnv(env) })

		if err := regA.RegisterAllTo(env); err != nil {
			t.Fatalf("regA.RegisterAllTo: %v", err)
		}
		err := regB.RegisterAllTo(env)
		if err == nil || !strings.Contains(err.Error(), "already owned") {
			t.Fatalf("regB.RegisterAllTo error = %v, want already owned", err)
		}
	})
}

func TestRegisterToRejectsNilEnvironment(t *testing.T) {
	reg := registryWithSchema(t, "test")
	for name, register := range map[string]func(*bloblang.Environment) error{
		"all":       reg.RegisterAllTo,
		"methods":   reg.RegisterMethodsTo,
		"functions": reg.RegisterFunctionsTo,
	} {
		t.Run(name, func(t *testing.T) {
			if err := register(nil); err == nil {
				t.Fatal("nil environment returned nil error")
			}
		})
	}
}

func TestConcurrentRegistrationClaimsEnvironmentOnce(t *testing.T) {
	reg := registryWithSchema(t, "test")
	env := bloblang.NewEnvironment()
	t.Cleanup(func() { releaseEnv(env) })

	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- reg.RegisterAllTo(env) }()
	}

	var successes, failures int
	for range 2 {
		if err := <-errs; err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes=%d failures=%d, want 1 and 1", successes, failures)
	}
}
