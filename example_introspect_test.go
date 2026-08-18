package schemix_test

import (
	"fmt"

	"cuelang.org/go/cue/cuecontext"
	"github.com/mredencom/schemix"
)

// Fields exposes the compiled schema structure, which is enough to drive
// generated documentation or UI forms. The order follows the schema source,
// not the alphabet.
func ExampleValidator_Fields() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		memo?:    string
		address: {
			city:    string
			country: =~"^[A-Z]{2}$"
		}
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	}`)

	for _, f := range v.Fields() {
		fmt.Printf("%-11s %-7s optional=%-5v blob=%v\n", f.Name, f.Type, f.Optional, f.HasBlob)
		for _, c := range f.Children {
			fmt.Printf("  %-9s %-7s optional=%-5v blob=%v\n", c.Name, c.Type, c.Optional, c.HasBlob)
		}
	}
	// Output:
	// pan         string  optional=false blob=false
	// amount      int     optional=false blob=false
	// memo        string  optional=true  blob=false
	// address     struct  optional=false blob=false
	//   city      string  optional=false blob=false
	//   country   string  optional=false blob=false
	// card_brand  string  optional=false blob=true
}

// Fields is also the basis for deriving a required-field list: a field is
// required when it is neither optional nor computed by a @blob rule.
func ExampleValidator_Fields_requiredList() {
	v := schemix.MustNew(`{
		sku:    =~"^SKU-[A-Z0-9]{8}$"
		price:  number & >0
		memo?:  string
		total:  number @blob(this.price * 2)
	}`)

	for _, f := range v.Fields() {
		if !f.Optional && !f.HasBlob {
			fmt.Println("required:", f.Path, f.Type)
		}
	}
	// Output:
	// required: sku string
	// required: price number
}

// NewFromValue builds a Validator from a pre-compiled cue.Value, which is how
// schemas are composed from shared definitions. Definitions carry constraints
// only — attributes belong on the fields that reference them.
func ExampleNewFromValue() {
	ctx := cuecontext.New()
	schema := ctx.CompileString(`{
		#Amount: int & >0 & <=1000000
		#SKU:    =~"^SKU-[A-Z0-9]{8}$"

		amount: #Amount
		sku:    #SKU
		qty:    int & >=1
	}`)

	v, err := schemix.NewFromValue(schema)
	if err != nil {
		panic(err)
	}

	fmt.Println(v.Process(map[string]any{
		"amount": int64(500), "sku": "SKU-ABCD1234", "qty": int64(3),
	}).Valid)
	fmt.Println(v.Process(map[string]any{
		"amount": int64(500), "sku": "BAD", "qty": int64(3),
	}).Valid)
	// Output:
	// true
	// false
}

// NewWithContext lets several validators share one CUE context, which saves the
// per-context allocation when many schemas are compiled at startup.
func ExampleNewWithContext() {
	ctx := cuecontext.New()

	v1, _ := schemix.NewWithContext(ctx, `{ x: int & >0 }`)
	v2, _ := schemix.NewWithContext(ctx, `{ y: string }`)

	fmt.Println(v1.Process(map[string]any{"x": int64(42)}).Valid)
	fmt.Println(v2.Process(map[string]any{"y": "hello"}).Valid)
	// Output:
	// true
	// true
}
