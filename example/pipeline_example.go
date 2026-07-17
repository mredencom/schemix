package main

import (
	"fmt"
	"log"

	"github.com/mredencom/schemix"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

func pipelineExample() {
	reg := schemix.NewRegistry()
	if err := reg.Register("payment", `
	{
		pan: =~"^[0-9]{16}$" @blob(this.pan.has_prefix("62") || this.pan.has_prefix("4"))
		amount: int & >0
		currency: "156" | "840"
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
		pan_masked: string @blob(this.pan.slice(0, 4) + "****" + this.pan.slice(-4))
		fee: number @blob(if this.currency == "156" { 0 } else { (this.amount * 0.015).ceil() })
	}
	`); err != nil {
		log.Fatal(err)
	}

	// Register as Bloblang method — this.process_schema(name: "xxx")
	if err := reg.RegisterMethods(); err != nil {
		log.Fatal(err)
	}

	mapping := `
		let r = this.process_schema(name: "payment")
		root = if $r.valid {
			{
				"status": "approved",
				"card": $r.output.card_brand,
				"masked": $r.output.pan_masked,
				"fee": $r.output.fee
			}
		} else {
			{"status": "rejected", "errors": $r.errors.map_each("[" + this.code + "] " + this.message)}
		}
	`
	exec, err := bloblang.Parse(mapping)
	if err != nil {
		log.Fatal(err)
	}

	cases := []map[string]any{
		{"pan": "6222021234567890", "amount": int64(10000), "currency": "156"},
		{"pan": "4111111111111111", "amount": int64(50000), "currency": "840"},
		{"pan": "9999000011112222", "amount": int64(-1), "currency": "999"},
	}

	for _, c := range cases {
		out, _ := exec.Query(c)
		result := out.(map[string]any)
		if result["status"] == "approved" {
			fmt.Printf("  ✓ %s | %s | fee=%v\n", result["masked"], result["card"], result["fee"])
		} else {
			fmt.Printf("  ✗ %s | %v\n", c["pan"], result["errors"])
		}
	}
}
