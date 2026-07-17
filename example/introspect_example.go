package main

import (
	"fmt"

	"github.com/mredencom/schemix"
)

func introspectExample() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "CNY" | "USD" | "EUR"
		memo?:    string
		address: {
			city:     string
			country:  =~"^[A-Z]{2}$"
			zip?:     string
		}
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	}`)

	// Fields() returns the schema structure for runtime introspection
	fields := v.Fields()

	fmt.Println("  Schema fields:")
	printFields(fields, "    ")

	// Practical use: auto-generate required field list for API docs
	fmt.Println("\n  Required fields (for API docs):")
	for _, f := range fields {
		if !f.Optional && !f.HasBlob {
			fmt.Printf("    - %s (%s)\n", f.Path, f.Type)
		}
	}

	// Practical use: find all computed (@blob) fields
	fmt.Println("\n  Computed fields (auto-generated):")
	for _, f := range fields {
		if f.HasBlob {
			fmt.Printf("    - %s (%s)\n", f.Path, f.Type)
		}
	}
}

func printFields(fields []schemix.FieldInfo, indent string) {
	for _, f := range fields {
		opt := ""
		if f.Optional {
			opt = " (optional)"
		}
		blob := ""
		if f.HasBlob {
			blob = " [@blob]"
		}
		fmt.Printf("%s%s: %s%s%s\n", indent, f.Name, f.Type, opt, blob)
		if len(f.Children) > 0 {
			printFields(f.Children, indent+"  ")
		}
	}
}

func resultChainExample() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "CNY" | "USD" | "EUR"
		pan_check: bool @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
		cvv?: string @meta(conditional, required_if=this.pan.has_prefix("4"))
	}`)

	// Input with multiple errors
	r := v.Process(map[string]any{
		"pan":      "9999ABCDEFGHIJKL", // regex fail
		"amount":   int64(-1),           // range fail
		"currency": "JPY",              // enum fail
	})

	fmt.Printf("  valid=%v, total errors=%d\n", r.Valid, len(r.Errors))

	// HasCode — quick boolean check
	fmt.Printf("  HasCode(FormatMismatch)=%v\n", r.HasCode(schemix.CodeFormatMismatch))
	fmt.Printf("  HasCode(CondRequired)=%v\n", r.HasCode(schemix.CodeCondRequired))

	// HasErrorsAt — check specific field
	fmt.Printf("  HasErrorsAt(pan)=%v\n", r.HasErrorsAt("pan"))
	fmt.Printf("  HasErrorsAt(memo)=%v\n", r.HasErrorsAt("memo"))

	// ErrorsByCode — filter by error category
	fmt.Println("  CUE layer errors (E1*):")
	for _, e := range r.ErrorsByType("cue") {
		fmt.Printf("    [%s] %s\n", e.Code, e.Path)
	}

	// ErrorsByCode — find all format mismatches
	fmtErrors := r.ErrorsByCode(schemix.CodeFormatMismatch)
	fmt.Printf("  Format mismatches: %d\n", len(fmtErrors))

	// Practical pattern: short-circuit error response building
	if r.HasCode(schemix.CodeRequiredMissing) {
		fmt.Println("  → Missing required fields detected")
	} else if r.HasCode(schemix.CodeTypeMismatch) {
		fmt.Println("  → Type errors detected")
	} else {
		fmt.Println("  → Validation/format errors only")
	}
}
