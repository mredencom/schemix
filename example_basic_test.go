package schemix_test

import (
	"fmt"

	"github.com/mredencom/schemix"
)

// New compiles a CUE schema once; the returned Validator is immutable and safe
// for concurrent use.
func ExampleNew() {
	v, err := schemix.New(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"
	}`)
	if err != nil {
		panic(err)
	}

	r := v.Process(map[string]any{
		"pan": "4111111111111111", "amount": int64(10000), "currency": "840",
	})
	fmt.Println(r.Valid)
	// Output: true
}

// MustNew panics instead of returning an error, which suits package-level
// variables initialised at startup.
func ExampleMustNew() {
	v := schemix.MustNew(`{ age: int & >=0 & <=150 }`)

	fmt.Println(v.Process(map[string]any{"age": int64(30)}).Valid)
	fmt.Println(v.Process(map[string]any{"age": int64(200)}).Valid)
	// Output:
	// true
	// false
}

// A @blob() expression returning a non-bool writes a computed field into
// Output. Returning a bool makes it a validation rule instead.
func ExampleValidator_Process_computedFields() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"

		luhn_check: bool   @blob(this.pan.luhn_valid())
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
		pan_masked: string @blob(this.pan.slice(0, 4) + "****" + this.pan.slice(-4))
		fee:        number @blob(if this.currency == "156" { 0 } else { (this.amount * 0.015).ceil() })
	}`)

	r := v.Process(map[string]any{
		"pan": "4111111111111111", "amount": int64(10000), "currency": "840",
	})

	fmt.Println("valid:     ", r.Valid)
	fmt.Println("card_brand:", r.Output["card_brand"])
	fmt.Println("pan_masked:", r.Output["pan_masked"])
	fmt.Println("fee:       ", r.Output["fee"])
	// Output:
	// valid:      true
	// card_brand: Visa
	// pan_masked: 4111****1111
	// fee:        150
}

// Errors carry a stable code, the field path, and the layer that produced them.
func ExampleValidator_Process_invalid() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "156" | "840"
	}`)

	r := v.Process(map[string]any{
		"pan": "9999000011112222", "amount": int64(-1), "currency": "999",
	})

	fmt.Println("valid:", r.Valid)
	for _, e := range r.Errors {
		fmt.Printf("%s %s (%s)\n", e.Code, e.Path, e.Type)
	}
	// Output:
	// valid: false
	// E1R01 amount (cue)
	// E1E01 currency (cue)
}

// Nested structs are expressed with CUE; a @blob() rule on a nested field
// addresses it by absolute path from the root (this.customer.email).
func ExampleValidator_Process_nested() {
	v := schemix.MustNew(`{
		order_id: =~"^ORD-[0-9]+$"
		customer: {
			name:  string @blob(this.customer.name.len_between(min: 2, max: 50))
			email: string @blob(this.customer.email.is_email())
		}
	}`)

	r := v.Process(map[string]any{
		"order_id": "ORD-001",
		"customer": map[string]any{"name": "Alice", "email": "alice@example.com"},
	})
	fmt.Println("valid:", r.Valid)

	r = v.Process(map[string]any{
		"order_id": "ORD-002",
		"customer": map[string]any{"name": "Bob", "email": "not-an-email"},
	})
	fmt.Println("valid:", r.Valid)
	fmt.Println("error:", r.Errors[0].Code, r.Errors[0].Path, "("+r.Errors[0].Type+")")
	// Output:
	// valid: true
	// valid: false
	// error: E2B01 customer.email (bloblang)
}

// Element structure goes in CUE; cross-element rules and per-element
// computation go on the array field itself. Only CUE reports the element index.
func ExampleValidator_Process_array() {
	v := schemix.MustNew(`{
		items: [...{
			product:   string
			price:     number & >0
			qty:       int & >=1
			subtotal?: number
		}] @blob(
			this.items.length() > 0,
			this.items.map_each(this.merge({"subtotal": this.price * this.qty}))
		)
		total: number @blob(this.items.map_each(this.price * this.qty).sum())
	}`)

	r := v.Process(map[string]any{
		"items": []any{
			map[string]any{"product": "Laptop", "price": 5999.0, "qty": int64(1)},
			map[string]any{"product": "Mouse", "price": 99.0, "qty": int64(2)},
		},
	})
	fmt.Println("valid:", r.Valid)
	fmt.Println("total:", r.Output["total"])

	// A CUE constraint violation names the offending element index.
	r = v.Process(map[string]any{
		"items": []any{
			map[string]any{"product": "Phone", "price": -100.0, "qty": int64(1)},
		},
	})
	fmt.Println("valid:", r.Valid)
	fmt.Println("error:", r.Errors[0].Code, r.Errors[0].Path, "("+r.Errors[0].Type+")")
	// Output:
	// valid: true
	// total: 6197
	// valid: false
	// error: E1R01 items[0].price (cue)
}
