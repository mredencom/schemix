package main

import (
	"fmt"
	"strings"

	"github.com/mredencom/schemix"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

func funcMapExample() {
	// === Build a reusable FuncMap (option pattern) ===
	// Define once, share across all validators.
	funcs := schemix.NewFuncMap(
		// Function style: check_blacklist(this.pan)
		schemix.Func("check_blacklist", func(args ...any) (bloblang.Function, error) {
			pan := args[0].(string)
			blacklist := map[string]bool{"4000000000000000": true}
			return func() (any, error) {
				return !blacklist[pan], nil
			}, nil
		}),

		// Method style: this.pan.mask()
		schemix.Method("mask", func(v any) (any, error) {
			s := v.(string)
			if len(s) < 8 {
				return s, nil
			}
			return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:], nil
		}),

		// V2 Method with typed params: this.amount.fee_rate(rate: 0.015)
		schemix.MethodV2("fee_rate",
			bloblang.NewPluginSpec().
				Param(bloblang.NewFloat64Param("rate")),
			func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				rate, _ := args.GetFloat64("rate")
				return func(v any) (any, error) {
					n, _ := v.(int64)
					return int64(float64(n) * rate), nil
				}, nil
			},
		),
	)

	// Check FuncMap for errors (invalid names caught here)
	if err := funcs.Err(); err != nil {
		fmt.Printf("  FuncMap error: %v\n", err)
		return
	}

	// === Share FuncMap across multiple validators ===
	fmt.Println("  Shared FuncMap across validators:")

	v1, _ := schemix.New(`{
		pan:     =~"^[0-9]{16}$"
		amount:  int & >0
		safe:    bool   @blob(check_blacklist(this.pan))
		masked:  string @blob(this.pan.mask())
		fee:     number @blob(this.amount.fee_rate(rate: 0.015))
	}`, schemix.WithFuncMap(funcs))

	r := v1.Process(map[string]any{"pan": "6222021234567890", "amount": int64(10000)})
	fmt.Printf("    v1: valid=%v, masked=%v, fee=%v\n", r.Valid, r.Output["masked"], r.Output["fee"])

	v2, _ := schemix.New(`{
		card:   =~"^[0-9]{16}$"
		display: string @blob(this.card.mask())
	}`, schemix.WithFuncMap(funcs))

	r = v2.Process(map[string]any{"card": "4111111111111111"})
	fmt.Printf("    v2: masked=%v\n", r.Output["display"])

	// === Combine FuncMap with individual Options ===
	fmt.Println("\n  FuncMap + extra options:")

	v3, _ := schemix.New(`{
		pan:    =~"^[0-9]{16}$"
		safe:   bool @blob(check_blacklist(this.pan))
		extra:  bool @blob(extra_check(this.pan))
	}`,
		schemix.WithFuncMap(funcs),
		schemix.WithFunction("extra_check", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)

	r = v3.Process(map[string]any{"pan": "6222021234567890"})
	fmt.Printf("    v3: valid=%v\n", r.Valid)

	// === Invalid name detection ===
	fmt.Println("\n  Name validation:")

	badFuncs := schemix.NewFuncMap(
		schemix.Func("InvalidName", func(args ...any) (bloblang.Function, error) {
			return func() (any, error) { return true, nil }, nil
		}),
	)
	fmt.Printf("    invalid name error: %v\n", badFuncs.Err())
}

func overrideExample() {
	// === Override a specific built-in method ===
	fmt.Println("  Override built-in is_email (company-only):")

	v, _ := schemix.New(`{
		email: string
		check: bool @blob(this.email.is_email())
	}`,
		schemix.WithOverrideMethod("is_email"),
		schemix.WithMethod("is_email", func(v any) (any, error) {
			s, _ := v.(string)
			return strings.HasSuffix(s, "@company.com"), nil
		}),
	)

	r := v.Process(map[string]any{"email": "alice@company.com"})
	fmt.Printf("    alice@company.com → valid=%v\n", r.Valid)

	r = v.Process(map[string]any{"email": "alice@gmail.com"})
	fmt.Printf("    alice@gmail.com → valid=%v (rejected by custom rule)\n", r.Valid)

	// === Override all — full control ===
	fmt.Println("\n  WithOverrideAll (full custom control):")

	v2, _ := schemix.New(`{
		date:  string
		valid: bool @blob(is_valid_date(this.date))
	}`,
		schemix.WithOverrideAll(),
		schemix.WithFunction("is_valid_date", func(args ...any) (bloblang.Function, error) {
			s := args[0].(string)
			return func() (any, error) {
				// Only accept YYYY-MM-DD format
				return len(s) == 10 && s[4] == '-' && s[7] == '-', nil
			}, nil
		}),
	)

	r = v2.Process(map[string]any{"date": "2024-01-15"})
	fmt.Printf("    2024-01-15 → valid=%v\n", r.Valid)

	r = v2.Process(map[string]any{"date": "Jan 15, 2024"})
	fmt.Printf("    Jan 15, 2024 → valid=%v\n", r.Valid)

	// Non-overridden builtins still work
	fmt.Println("\n  Non-overridden builtins still available:")
	v3, _ := schemix.New(`{
		email: string
		url:   string
		e_ok:  bool @blob(this.email.is_email())
		u_ok:  bool @blob(this.url.is_url())
	}`,
		schemix.WithOverrideMethod("is_email"),
		schemix.WithMethod("is_email", func(v any) (any, error) {
			return strings.Contains(v.(string), "@"), nil
		}),
	)

	r = v3.Process(map[string]any{"email": "x@y", "url": "https://example.com"})
	fmt.Printf("    is_email (custom)=%v, is_url (builtin)=%v\n",
		!r.HasErrorsAt("e_ok"), !r.HasErrorsAt("u_ok"))
}
