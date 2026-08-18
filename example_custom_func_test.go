package schemix_test

import (
	"fmt"
	"strings"

	"github.com/mredencom/schemix"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// WithMethod registers a method callable on a value: this.field.my_method().
// The registration is scoped to this Validator — it does not leak into the
// global Bloblang environment.
func ExampleWithMethod() {
	maskPAN := func(v any) (any, error) {
		s, ok := v.(string)
		if !ok || len(s) < 8 {
			return v, nil
		}
		return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:], nil
	}

	v := schemix.MustNew(`{
		pan:         =~"^[0-9]{16}$"
		pan_display: string @blob(this.pan.mask_pan())
	}`, schemix.WithMethod("mask_pan", maskPAN))

	r := v.Process(map[string]any{"pan": "6222021234567890"})
	fmt.Println(r.Output["pan_display"])

	// The method is not visible to other validators.
	_, err := schemix.New(`{ x: string @blob(this.x.mask_pan()) }`)
	fmt.Println("leaked:", err == nil)
	// Output:
	// 6222********7890
	// leaked: false
}

// WithFunction registers a free function: my_func(args...).
func ExampleWithFunction() {
	blacklist := map[string]bool{"4000000000000000": true}

	checkBlacklist := func(args ...any) (bloblang.Function, error) {
		pan, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("check_blacklist requires a string")
		}
		return func() (any, error) { return !blacklist[pan], nil }, nil
	}

	v := schemix.MustNew(`{
		pan:             =~"^[0-9]{16}$"
		not_blacklisted: bool @blob(check_blacklist(this.pan))
	}`, schemix.WithFunction("check_blacklist", checkBlacklist))

	fmt.Println(v.Process(map[string]any{"pan": "6222021234567890"}).Valid)

	r := v.Process(map[string]any{"pan": "4000000000000000"})
	fmt.Println(r.Valid, r.Errors[0].Code, r.Errors[0].Path)
	// Output:
	// true
	// false E2B01 not_blacklisted
}

// The V2 forms take a PluginSpec, which gives named and typed parameters.
func ExampleWithMethodV2() {
	v := schemix.MustNew(`{
		amount:       int & >0
		amount_check: bool @blob(this.amount.in_range(min: 1, max: 1000000))
	}`, schemix.WithMethodV2("in_range",
		bloblang.NewPluginSpec().
			Param(bloblang.NewInt64Param("min")).
			Param(bloblang.NewInt64Param("max")),
		func(args *bloblang.ParsedParams) (bloblang.Method, error) {
			low, _ := args.GetInt64("min")
			high, _ := args.GetInt64("max")
			return func(v any) (any, error) {
				n, ok := v.(int64)
				if !ok {
					return false, nil
				}
				return n >= low && n <= high, nil
			}, nil
		},
	))

	fmt.Println(v.Process(map[string]any{"amount": int64(10000)}).Valid)
	fmt.Println(v.Process(map[string]any{"amount": int64(2000000)}).Valid)
	// Output:
	// true
	// false
}

// WithFunctionV2 is the function-shaped counterpart of WithMethodV2.
func ExampleWithFunctionV2() {
	v := schemix.MustNew(`{
		amount: int & >0
		fee:    number @blob(calculate_fee(this.amount, 0.015))
	}`, schemix.WithFunctionV2("calculate_fee",
		bloblang.NewPluginSpec().
			Param(bloblang.NewInt64Param("amount")).
			Param(bloblang.NewFloat64Param("rate")),
		func(args *bloblang.ParsedParams) (bloblang.Function, error) {
			amount, _ := args.GetInt64("amount")
			rate, _ := args.GetFloat64("rate")
			return func() (any, error) {
				if fee := float64(amount) * rate; fee >= 1 {
					return int64(fee), nil
				}
				return int64(1), nil
			}, nil
		},
	))

	r := v.Process(map[string]any{"amount": int64(10000)})
	fmt.Println(r.Output["fee"])
	// Output: 150
}

// NewFuncMap builds a reusable collection once and shares it across validators.
// Names must be snake_case; FuncMap.Err reports the first violation.
func ExampleNewFuncMap() {
	funcs := schemix.NewFuncMap(
		schemix.Method("mask_pan", func(v any) (any, error) {
			s, _ := v.(string)
			if len(s) < 8 {
				return s, nil
			}
			return s[:4] + "****" + s[len(s)-4:], nil
		}),
		schemix.Method("is_unionpay", func(v any) (any, error) {
			s, _ := v.(string)
			return strings.HasPrefix(s, "62"), nil
		}),
	)
	if err := funcs.Err(); err != nil {
		panic(err)
	}

	v1 := schemix.MustNew(`{
		pan:     =~"^[0-9]{16}$"
		display: string @blob(this.pan.mask_pan())
	}`, schemix.WithFuncMap(funcs))

	v2 := schemix.MustNew(`{
		pan:       =~"^[0-9]{16}$"
		unionpay:  bool @blob(this.pan.is_unionpay())
	}`, schemix.WithFuncMap(funcs))

	// mask_pan returns a string, so it becomes a computed Output field.
	fmt.Println(v1.Process(map[string]any{"pan": "6222021234567890"}).Output["display"])

	// is_unionpay returns a bool, so it acts as a validation rule instead —
	// the value is not written to Output.
	fmt.Println(v2.Process(map[string]any{"pan": "6222021234567890"}).Valid)
	fmt.Println(v2.Process(map[string]any{"pan": "4111111111111111"}).Valid)
	// Output:
	// 6222****7890
	// true
	// false
}

// Built-in names are protected: registering over one fails unless the override
// is requested explicitly.
func ExampleWithOverrideMethod() {
	strict := func(v any) (any, error) {
		s, _ := v.(string)
		return strings.HasSuffix(s, "@example.com"), nil
	}

	// Without the override option the registration is rejected.
	_, err := schemix.New(`{ email: string @blob(this.email.is_email()) }`,
		schemix.WithMethod("is_email", strict))
	fmt.Println("rejected without override:", err != nil)

	v := schemix.MustNew(`{ email: string @blob(this.email.is_email()) }`,
		schemix.WithOverrideMethod("is_email"),
		schemix.WithMethod("is_email", strict),
	)
	fmt.Println(v.Process(map[string]any{"email": "a@example.com"}).Valid)
	fmt.Println(v.Process(map[string]any{"email": "a@other.com"}).Valid)
	// Output:
	// rejected without override: true
	// true
	// false
}
