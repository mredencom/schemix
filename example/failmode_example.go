package main

import (
	"fmt"
	"log"

	"github.com/mredencom/schemix"
)

func failModeExample() {
	v, err := schemix.New(`
	{
		mti: =~"^[01][0-9]{3}$" @meta(priority=1)
		pan: =~"^[0-9]{13,19}$" @meta(priority=1) @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
		amount: int & >0 @meta(priority=1)
		currency: "156" | "840" @meta(priority=2)
		merchant: string @meta(priority=3) @blob(this.merchant.length() >= 2)
	}
	`)
	if err != nil {
		log.Fatal(err)
	}

	bad := map[string]any{
		"mti": "9999", "pan": "ABC", "amount": int64(-1), "currency": "999", "merchant": "X",
	}

	// FailAll: collect all errors (suitable for form validation)
	r1 := v.ProcessWithMode(bad, schemix.FailAll)
	fmt.Printf("  FailAll:      errors=%d\n", len(r1.Errors))

	// FailFast: stop at first error (suitable for gateway scenarios)
	r2 := v.ProcessWithMode(bad, schemix.FailFast)
	fmt.Printf("  FailFast:     errors=%d\n", len(r2.Errors))

	// FailPriority: priority group isolation (p1 failure skips p2/p3)
	r3 := v.ProcessWithMode(bad, schemix.FailPriority)
	fmt.Printf("  FailPriority: errors=%d (p1 failure skips p2/p3)\n", len(r3.Errors))
}
