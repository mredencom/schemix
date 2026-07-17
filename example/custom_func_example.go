package main

import (
	"fmt"
	"strings"

	"github.com/mredencom/schemix"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
)

// Simulated external services
var blacklist = map[string]bool{
	"4000000000000000": true,
	"5100000000000000": true,
}

func customFuncExample() {
	// === Style 1: WithFunction (FunctionConstructor) ===
	// Same signature as Bloblang's RegisterFunction.
	// Receives args, returns a Function closure.
	checkBlacklist := func(args ...any) (bloblang.Function, error) {
		pan, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("check_blacklist requires string")
		}
		return func() (any, error) {
			return !blacklist[pan], nil
		}, nil
	}

	// === Style 2: WithFunctionV2 (PluginSpec + ParsedParams) ===
	// Same signature as Bloblang's RegisterFunctionV2.
	// Type-safe parameters, self-documenting.

	// === Style 3: WithMethod (called on target value) ===
	// Same signature as Bloblang's RegisterMethod.
	// Called as: this.field.my_method()
	maskPan := func(v any) (any, error) {
		s, ok := v.(string)
		if !ok {
			return v, nil
		}
		if len(s) < 8 {
			return s, nil
		}
		return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:], nil
	}

	// === Style 4: WithMethodV2 (PluginSpec + ParsedParams) ===
	// Same signature as Bloblang's RegisterMethodV2.

	// Build validator combining all styles
	v, err := schemix.New(`{
		pan:    =~"^[0-9]{16}$"
		amount: int & >0
		
		// Style 1: simple function
		not_blacklisted: bool @blob(check_blacklist(this.pan))
		
		// Style 2: V2 function with typed params
		fee: number @blob(calculate_fee(this.amount, 0.015))
		
		// Style 3: simple method on target value
		pan_display: string @blob(this.pan.mask_pan())
		
		// Style 4: V2 method with params
		amount_check: bool @blob(this.amount.in_range(min: 1, max: 1000000))
	}`,
		// Style 1
		schemix.WithFunction("check_blacklist", checkBlacklist),

		// Style 2 — typed params, self-documenting
		schemix.WithFunctionV2("calculate_fee",
			bloblang.NewPluginSpec().
				Param(bloblang.NewInt64Param("amount")).
				Param(bloblang.NewFloat64Param("rate")),
			func(args *bloblang.ParsedParams) (bloblang.Function, error) {
				amount, _ := args.GetInt64("amount")
				rate, _ := args.GetFloat64("rate")
				return func() (any, error) {
					fee := float64(amount) * rate
					if fee < 1 {
						return int64(1), nil
					}
					return int64(fee), nil
				}, nil
			},
		),

		// Style 3 — method on value
		schemix.WithMethod("mask_pan", maskPan),

		// Style 4 — V2 method with typed params
		schemix.WithMethodV2("in_range",
			bloblang.NewPluginSpec().
				Param(bloblang.NewInt64Param("min")).
				Param(bloblang.NewInt64Param("max")),
			func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				min, _ := args.GetInt64("min")
				max, _ := args.GetInt64("max")
				return func(v any) (any, error) {
					n, ok := v.(int64)
					if !ok {
						return false, nil
					}
					return n >= min && n <= max, nil
				}, nil
			},
		),
	)
	if err != nil {
		fmt.Printf("  error: %v\n", err)
		return
	}

	// Test: valid card
	fmt.Println("  Valid card (all 4 styles):")
	r := v.Process(map[string]any{"pan": "6222021234567890", "amount": int64(10000)})
	fmt.Printf("    valid=%v, pan_display=%v, fee=%v\n",
		r.Valid, r.Output["pan_display"], r.Output["fee"])

	// Test: blacklisted card
	fmt.Println("  Blacklisted card:")
	r = v.Process(map[string]any{"pan": "4000000000000000", "amount": int64(5000)})
	fmt.Printf("    valid=%v\n", r.Valid)
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s\n", e.Code, e.Path)
	}

	// Test: amount out of range (V2 method)
	fmt.Println("  Amount out of range (V2 method):")
	r = v.Process(map[string]any{"pan": "6222021234567890", "amount": int64(2000000)})
	fmt.Printf("    valid=%v\n", r.Valid)
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s\n", e.Code, e.Path)
	}

	// Environment isolation
	fmt.Println("  Function isolation:")
	_, err = schemix.New(`{ x: bool @blob(check_blacklist("test")) }`)
	if err != nil {
		fmt.Printf("    correctly fails: %v\n", truncate(err.Error(), 60))
	}
}
