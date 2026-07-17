package schemix

import (
	"fmt"
	"sync"
	"testing"
)

// TestValidator_ConcurrentProcess tests that multiple goroutines can safely
// call Process on the same Validator concurrently.
func TestValidator_ConcurrentProcess(t *testing.T) {
	v := MustNew(`{ name: string, age: int & >=0 & <=150 }`)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := map[string]any{"name": fmt.Sprintf("user%d", n), "age": int64(n % 151)}
			r := v.Process(data)
			if !r.Valid {
				t.Errorf("goroutine %d: unexpected invalid result: %v", n, r.Errors)
			}
		}(i)
	}
	wg.Wait()
}

// TestValidator_ConcurrentValidate tests Validate() concurrency.
func TestValidator_ConcurrentValidate(t *testing.T) {
	v := MustNew(`{ name: string, age: int & >=0 & <=150 }`)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := map[string]any{"name": fmt.Sprintf("user%d", n), "age": int64(n % 151)}
			valid, errs := v.Validate(data)
			if !valid {
				t.Errorf("goroutine %d: unexpected invalid result: %v", n, errs)
			}
		}(i)
	}
	wg.Wait()
}

// TestValidator_ConcurrentMixed tests mixed Process + Validate concurrency.
func TestValidator_ConcurrentMixed(t *testing.T) {
	v := MustNew(`{ name: string, age: int & >=0 & <=150 }`)

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := map[string]any{"name": fmt.Sprintf("user%d", n), "age": int64(n % 151)}
			if n%2 == 0 {
				r := v.Process(data)
				if !r.Valid {
					t.Errorf("goroutine %d (Process): unexpected invalid result: %v", n, r.Errors)
				}
			} else {
				valid, errs := v.Validate(data)
				if !valid {
					t.Errorf("goroutine %d (Validate): unexpected invalid result: %v", n, errs)
				}
			}
		}(i)
	}
	wg.Wait()
}

// TestRegistry_ConcurrentGetProcess tests concurrent Get + Process on Registry.
func TestRegistry_ConcurrentGetProcess(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register("user", `{ name: string, age: int & >=0 & <=150 }`)
	if err != nil {
		t.Fatalf("failed to register schema: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v, ok := reg.Get("user")
			if !ok {
				t.Errorf("goroutine %d: schema not found", n)
				return
			}
			data := map[string]any{"name": fmt.Sprintf("user%d", n), "age": int64(n % 151)}
			r := v.Process(data)
			if !r.Valid {
				t.Errorf("goroutine %d: unexpected invalid result: %v", n, r.Errors)
			}
		}(i)
	}
	wg.Wait()
}

// TestRegistry_ConcurrentRegisterGet tests concurrent Register + Get on Registry.
func TestRegistry_ConcurrentRegisterGet(t *testing.T) {
	reg := NewRegistry()
	// Pre-register a baseline schema so Get always has something to find.
	err := reg.Register("base", `{ id: string }`)
	if err != nil {
		t.Fatalf("failed to register base schema: %v", err)
	}

	var wg sync.WaitGroup

	// Writers: concurrently register new schemas.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("schema_%d", n)
			src := fmt.Sprintf(`{ field%d: string }`, n)
			if err := reg.Register(name, src); err != nil {
				t.Errorf("goroutine %d: register failed: %v", n, err)
			}
		}(i)
	}

	// Readers: concurrently Get + Process the base schema.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v, ok := reg.Get("base")
			if !ok {
				t.Errorf("goroutine %d: base schema not found", n)
				return
			}
			data := map[string]any{"id": fmt.Sprintf("id_%d", n)}
			r := v.Process(data)
			if !r.Valid {
				t.Errorf("goroutine %d: unexpected invalid result: %v", n, r.Errors)
			}
		}(i)
	}

	wg.Wait()
}
