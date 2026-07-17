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

	// NewFromValue: compose schemas from pre-compiled CUE values (with definitions)
	combined := ctx.CompileString(`{
		#Amount: int & >0 & <=1000000
		#SKU:    =~"^SKU-[A-Z0-9]{8}$"

		amount:  #Amount
		sku:     #SKU
		qty:     int & >=1
	}`)
	vComposed, _ := schemix.NewFromValue(combined)
	r = vComposed.Process(map[string]any{"amount": int64(500), "sku": "SKU-ABCD1234", "qty": int64(3)})
	fmt.Printf("  NewFromValue (composed): valid=%v\n", r.Valid)

	// Fields(): runtime introspection of schema structure
	fields := vComposed.Fields()
	fmt.Printf("  Fields() → %d fields: ", len(fields))
	for i, f := range fields {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%s(%s)", f.Name, f.Type)
	}
	fmt.Println()

	// Validate: fast path — skips Output construction
	valid, errs := v1.Validate(map[string]any{"x": int64(0)})
	fmt.Printf("  Validate (fast): valid=%v, errors=%d\n", valid, len(errs))

	// Registry internally shares context automatically, no manual management needed
	reg := schemix.NewRegistry()
	_ = reg.Register("a", `{ val: int }`)
	_ = reg.Register("b", `{ val: string }`)
	_ = reg.Register("c", `{ val: bool }`)
	fmt.Printf("  Registry (shared context): %d schemas registered\n", reg.Len())
}
