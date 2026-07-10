package main

import (
	"fmt"

	"cuelang.org/go/cue/cuecontext"
	"github.com/mredencom/schemix"
)

func convenienceExample() {
	// MustNew: suitable for package-level init, panics on invalid schema
	v := schemix.MustNew(`{
		code: =~"^[A-Z]{3}$"
		name: string
	}`)
	r := v.Process(map[string]any{"code": "USD", "name": "US Dollar"})
	fmt.Printf("  MustNew: valid=%v\n", r.Valid)

	// NewWithContext: multiple Validators share the same CUE context, saving memory
	ctx := cuecontext.New()

	v1, _ := schemix.NewWithContext(ctx, `{ x: int & >0 }`)
	v2, _ := schemix.NewWithContext(ctx, `{ y: string }`)
	v3, _ := schemix.NewWithContext(ctx, `{ z: =~"^[0-9]+$" }`)

	fmt.Printf("  SharedContext v1: valid=%v\n", v1.Process(map[string]any{"x": int64(42)}).Valid)
	fmt.Printf("  SharedContext v2: valid=%v\n", v2.Process(map[string]any{"y": "hello"}).Valid)
	fmt.Printf("  SharedContext v3: valid=%v\n", v3.Process(map[string]any{"z": "12345"}).Valid)

	// Registry internally shares context automatically, no manual management needed
	reg := schemix.NewRegistry()
	_ = reg.Register("a", `{ val: int }`)
	_ = reg.Register("b", `{ val: string }`)
	_ = reg.Register("c", `{ val: bool }`)
	fmt.Printf("  Registry (shared context): %d schemas registered\n", reg.Len())
}
