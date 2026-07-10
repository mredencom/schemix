package main

import (
	"fmt"
	"log"

	"github.com/mredencom/schemix"
)

func metaExample() {
	v, err := schemix.New(`
	{
		mti: =~"^[01][0-9]{3}$" @meta(priority=1,fail_fast)
		pan: =~"^[0-9]{13,19}$" @meta(priority=1,skip_empty)
		amount: int & >0 @meta(priority=2)
		currency: "156" | "840" @meta(priority=1)

		// Conditional required: response messages need auth_code
		auth_code?: =~"^[A-Z0-9]{6}$" @meta(optional,conditional,required_if=this.mti == "0110")

		// Conditional skip + omit from output
		fee?: number @meta(optional,skip_if=this.operation == "query",omit_if_skip) @blob(this.fee >= 0)

		// Omit empty values from output
		memo?: string @meta(optional,omit_empty)
	}
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Request message — auth_code not required
	r := v.Process(map[string]any{
		"mti": "0100", "pan": "6222021234567890", "amount": int64(10000), "currency": "156",
	})
	fmt.Printf("  0100 request: valid=%v\n", r.Valid)

	// Response message — required_if triggered
	r = v.Process(map[string]any{
		"mti": "0110", "pan": "6222021234567890", "amount": int64(10000), "currency": "156",
	})
	fmt.Printf("  0110 response missing auth_code: valid=%v\n", r.Valid)
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s: %s\n", e.Code, e.Path, e.Message)
	}

	// Query transaction — skip_if skips fee, omit_empty skips memo
	r = v.Process(map[string]any{
		"mti": "0100", "pan": "6222021234567890", "amount": int64(10000), "currency": "156",
		"operation": "query", "fee": int64(-100), "memo": "",
	})
	_, hasFee := r.Output["fee"]
	_, hasMemo := r.Output["memo"]
	fmt.Printf("  query transaction: valid=%v, fee_in_output=%v, memo_in_output=%v\n", r.Valid, hasFee, hasMemo)
}
