package schemix_test

import (
	"fmt"

	"github.com/mredencom/schemix"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// A Registry holds named schemas behind one shared CUE context.
func ExampleRegistry() {
	reg := schemix.NewRegistry()

	if err := reg.Register("user", `{
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,15}$"
		age:      int & >=0 & <=150
	}`); err != nil {
		panic(err)
	}
	if err := reg.Register("product", `{
		sku:   =~"^SKU-[A-Z0-9]{8}$"
		price: number & >0
	}`); err != nil {
		panic(err)
	}

	fmt.Println("count:      ", reg.Len())

	// List returns names sorted lexicographically, so the result can be
	// displayed or compared directly.
	fmt.Println("names:      ", reg.List())

	fmt.Println("has user:   ", reg.Has("user"))
	fmt.Println("has order:  ", reg.Has("order"))

	v, _ := reg.Get("user")
	fmt.Println("validate:   ", v.Process(map[string]any{"username": "alice_dev", "age": int64(28)}).Valid)

	fmt.Println("unregister: ", reg.Unregister("product"), "remaining", reg.Len())
	// Output:
	// count:       2
	// names:       [product user]
	// has user:    true
	// has order:   false
	// validate:    true
	// unregister:  true remaining 1
}

// RegisterMethodsTo exposes every registered schema to a Bloblang environment as
// this.process_schema(name: "...") / this.validate_schema(name: "..."), which is
// how schemix plugs into a Benthos/Redpanda Connect pipeline.
//
// Prefer the scoped *To variants over the deprecated global registration.
func ExampleRegistry_RegisterMethodsTo() {
	reg := schemix.NewRegistry()
	if err := reg.Register("payment", `{
		pan:        =~"^[0-9]{16}$"
		currency:   "156" | "840"
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
		pan_masked: string @blob(this.pan.slice(0, 4) + "****" + this.pan.slice(-4))
	}`); err != nil {
		panic(err)
	}

	env := bloblang.NewEnvironment()
	if err := reg.RegisterMethodsTo(env); err != nil {
		panic(err)
	}

	exec, err := env.Parse(`
		let r = this.process_schema(name: "payment")
		root = if $r.valid {
			{"status": "approved", "card": $r.output.card_brand, "masked": $r.output.pan_masked}
		} else {
			{"status": "rejected", "codes": $r.errors.map_each(this.code)}
		}
	`)
	if err != nil {
		panic(err)
	}

	for _, msg := range []map[string]any{
		{"pan": "6222021234567890", "currency": "156"},
		{"pan": "4111111111111111", "currency": "840"},
		{"pan": "9999000011112222", "currency": "999"},
	} {
		out, _ := exec.Query(msg)
		m := out.(map[string]any)
		if m["status"] == "approved" {
			fmt.Println(m["status"], m["card"], m["masked"])
		} else {
			fmt.Println(m["status"], m["codes"])
		}
	}
	// Output:
	// approved UnionPay 6222****7890
	// approved Visa 4111****1111
	// rejected [E1E01]
}

// validate_schema returns only {valid, errors}; process_schema additionally
// returns the computed output. Pick the cheaper one when output is not needed.
func ExampleRegistry_RegisterFunctionsTo() {
	reg := schemix.NewRegistry()
	if err := reg.Register("amount", `{ amount: int & >0 }`); err != nil {
		panic(err)
	}

	env := bloblang.NewEnvironment()
	if err := reg.RegisterFunctionsTo(env); err != nil {
		panic(err)
	}

	exec, _ := env.Parse(`root = validate_schema(data: this.payload, name: "amount").valid`)

	out, _ := exec.Query(map[string]any{"payload": map[string]any{"amount": int64(10)}})
	fmt.Println(out)

	out, _ = exec.Query(map[string]any{"payload": map[string]any{"amount": int64(-1)}})
	fmt.Println(out)
	// Output:
	// true
	// false
}

// The package-level store is deprecated and removed in v0.3.0.
//
// A process-global registry cannot be scoped to a test, an environment, or a
// tenant: two components sharing this process share one namespace, and a
// collision between them is silent. Use a Registry, whose ownership is
// explicit — reg.Put files an already-built Validator just as this did.
//
// This example remains only to document the behaviour of code still using it.
func ExampleRegister() {
	schemix.Register("currency", schemix.MustNew(`{ code: "CNY" | "USD" | "EUR" }`))
	defer schemix.Unregister("currency")

	fmt.Println("has:  ", schemix.Has("currency"))
	fmt.Println("valid:", schemix.ProcessWith("currency", map[string]any{"code": "USD"}).Valid)
	fmt.Println("valid:", schemix.ProcessWith("currency", map[string]any{"code": "JPY"}).Valid)
	// Output:
	// has:   true
	// valid: true
	// valid: false
}
