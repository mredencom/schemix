package schemix_test

import (
	"fmt"

	"github.com/mredencom/schemix"
)

// @meta() controls field behavior: execution order, conditional requirement,
// conditional skipping, and whether a field survives into Output.
func ExampleValidator_Process_meta() {
	v := schemix.MustNew(`{
		mti: =~"^[01][0-9]{3}$" @meta(priority=1,fail_fast)
		pan: =~"^[0-9]{13,19}$" @meta(priority=1)

		// Response messages (0110) must carry an auth code.
		auth_code?: =~"^[A-Z0-9]{6}$" @meta(optional,required_if=this.mti == "0110")

		// Skipped entirely for queries, and dropped from Output when skipped.
		fee?: number @meta(optional,skip_if=this.operation == "query",omit_if_skip) @blob(this.fee >= 0)

		// Dropped from Output when empty.
		memo?: string @meta(optional,omit_empty)
	}`)

	// A 0100 request does not need auth_code.
	r := v.Process(map[string]any{"mti": "0100", "pan": "6222021234567890"})
	fmt.Println("0100 request valid:", r.Valid)

	// A 0110 response without auth_code trips required_if.
	r = v.Process(map[string]any{"mti": "0110", "pan": "6222021234567890"})
	fmt.Println("0110 without auth_code valid:", r.Valid)
	fmt.Println("  error:", r.Errors[0].Code, r.Errors[0].Path)

	// skip_if suppresses the fee rule even though fee is negative;
	// omit_if_skip and omit_empty remove both fields from Output.
	r = v.Process(map[string]any{
		"mti": "0100", "pan": "6222021234567890",
		"operation": "query", "fee": int64(-100), "memo": "",
	})
	_, hasFee := r.Output["fee"]
	_, hasMemo := r.Output["memo"]
	fmt.Println("query valid:", r.Valid, "fee in output:", hasFee, "memo in output:", hasMemo)
	// Output:
	// 0100 request valid: true
	// 0110 without auth_code valid: false
	//   error: E3C01 auth_code
	// query valid: true fee in output: false memo in output: false
}

// The three FailModes trade error completeness against work done.
// FailPriority isolates groups: once a group fails, later groups do not run.
func ExampleValidator_Process_failModes() {
	v := schemix.MustNew(`{
		mti:      =~"^[01][0-9]{3}$" @meta(priority=1)
		amount:   int & >0           @meta(priority=1)
		currency: "156" | "840"      @meta(priority=2)
		merchant: string             @meta(priority=3) @blob(this.merchant.length() >= 2)
	}`)

	bad := map[string]any{
		"mti": "9999", "amount": int64(-1), "currency": "999", "merchant": "X",
	}

	fmt.Println("FailAll:     ", len(v.Process(bad, schemix.FailAll).Errors), "errors")
	fmt.Println("FailFast:    ", len(v.Process(bad, schemix.FailFast).Errors), "errors")
	fmt.Println("FailPriority:", len(v.Process(bad, schemix.FailPriority).Errors), "errors")
	// Output:
	// FailAll:      4 errors
	// FailFast:     1 errors
	// FailPriority: 2 errors
}
