package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/mredencom/schemix"
)

func basicExample() {
	v, err := schemix.New(`
	{
		// CUE static constraints
		pan: =~"^[0-9]{16}$"
		amount: int & >0
		currency: "156" | "840"

		// @blob dynamic validation (returns bool)
		pan_check: bool @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))

		// @blob computed fields (returns non-bool → written to Output)
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
		pan_masked: string @blob(this.pan.slice(0, 4) + "****" + this.pan.slice(-4))
		fee: number @blob(if this.currency == "156" { 0 } else { (this.amount * 0.015).ceil() })
	}
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Valid data
	r := v.Process(map[string]any{
		"pan": "6222021234567890", "amount": int64(10000), "currency": "156",
	})
	fmt.Printf("  valid=%v, card_brand=%v, fee=%v, pan_masked=%v\n",
		r.Valid, r.Output["card_brand"], r.Output["fee"], r.Output["pan_masked"])

	// Invalid data
	r = v.Process(map[string]any{
		"pan": "9999000011112222", "amount": int64(-1), "currency": "999",
	})
	fmt.Printf("  valid=%v, errors=%d\n", r.Valid, len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s: %s\n", e.Code, e.Path, truncate(e.Message, 60))
	}
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
