package main

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue/cuecontext"
	"github.com/mredencom/schemix"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// completeExample demonstrates the FULL schemix API surface through an
// e-commerce order processing scenario. Each section covers a distinct API
// area so a reader can understand how to combine them in production.
func completeExample() {
	fmt.Println("  ┌─────────────────────────────────────────────────┐")
	fmt.Println("  │  E-Commerce Order Processing — Full API Demo    │")
	fmt.Println("  └─────────────────────────────────────────────────┘")

	// ─── 1. Schema construction variants ─────────────────────────────
	fmt.Println("\n  ── 1. Construction APIs ──")
	demoConstruction()

	// ─── 2. CUE constraints + @blob + @meta ──────────────────────────
	fmt.Println("\n  ── 2. Schema Syntax (CUE + @blob + @meta) ──")
	demoSchemaFeatures()

	// ─── 3. FailMode comparison ──────────────────────────────────────
	fmt.Println("\n  ── 3. FailMode: FailAll / FailFast / FailPriority ──")
	demoFailModes()

	// ─── 4. Result chain API ─────────────────────────────────────────
	fmt.Println("\n  ── 4. Result Chain API ──")
	demoResultChain()

	// ─── 5. Custom functions & methods (V1 + V2) ─────────────────────
	fmt.Println("\n  ── 5. Custom Functions & Methods ──")
	demoCustomFunctions()

	// ─── 6. FuncMap (reusable collections) ───────────────────────────
	fmt.Println("\n  ── 6. FuncMap (shared across validators) ──")
	demoFuncMap()

	// ─── 7. Override built-in validators ─────────────────────────────
	fmt.Println("\n  ── 7. Override Built-in Validators ──")
	demoOverride()

	// ─── 8. ErrorFormatter (i18n) ────────────────────────────────────
	fmt.Println("\n  ── 8. ErrorFormatter (custom messages) ──")
	demoErrorFormatter()

	// ─── 9. Schema introspection ─────────────────────────────────────
	fmt.Println("\n  ── 9. Schema Introspection (Fields) ──")
	demoIntrospection()

	// ─── 10. Registry + Bloblang pipeline ────────────────────────────
	fmt.Println("\n  ── 10. Registry + Bloblang Pipeline ──")
	demoRegistryPipeline()

	// ─── 11. Schema composition (NewFromValue) ───────────────────────
	fmt.Println("\n  ── 11. Schema Composition (NewFromValue) ──")
	demoComposition()

	// ─── 12. Validate (fast path, no Output) ─────────────────────────
	fmt.Println("\n  ── 12. Validate (fast path) ──")
	demoValidateFastPath()

	fmt.Println("\n  ── Done: all 12 API areas covered ──")
}

// ─── Section 1: Construction APIs ────────────────────────────────────────────

func demoConstruction() {
	// New: standard construction
	v1, err := schemix.New(`{ name: string, age: int & >0 }`)
	if err != nil {
		fmt.Printf("    New error: %v\n", err)
		return
	}
	fmt.Printf("    New:            valid=%v\n", v1.Process(map[string]any{"name": "Alice", "age": int64(30)}).Valid)

	// MustNew: panics on error (for package-level vars)
	v2 := schemix.MustNew(`{ sku: =~"^SKU-[A-Z0-9]{6}$" }`)
	fmt.Printf("    MustNew:        valid=%v\n", v2.Process(map[string]any{"sku": "SKU-AB12CD"}).Valid)

	// NewWithContext: share CUE context across validators
	ctx := cuecontext.New()
	v3, _ := schemix.NewWithContext(ctx, `{ x: int & >0 }`)
	v4, _ := schemix.NewWithContext(ctx, `{ y: string }`)
	fmt.Printf("    NewWithContext:  v3=%v, v4=%v (shared ctx)\n",
		v3.Process(map[string]any{"x": int64(1)}).Valid,
		v4.Process(map[string]any{"y": "hello"}).Valid)

	// NewFromValue: from pre-compiled CUE value (see Section 11 for full demo)
	val := ctx.CompileString(`{ code: =~"^[A-Z]{3}$" }`)
	v5, _ := schemix.NewFromValue(val)
	fmt.Printf("    NewFromValue:   valid=%v\n", v5.Process(map[string]any{"code": "USD"}).Valid)
}

// ─── Section 2: Schema Syntax ────────────────────────────────────────────────

func demoSchemaFeatures() {
	// Showcases: types, regex, enum, range, nullable, optional, nested, array,
	// @blob (bool validation + computed), @meta (priority, conditional, skip, omit)
	v := schemix.MustNew(`{
		// CUE: type + regex + range
		order_id: =~"^ORD-[0-9]{8}$"
		amount:   int & >=1 & <=10000000

		// CUE: enum
		currency: "CNY" | "USD" | "EUR"

		// CUE: nullable + optional
		memo?:    null | string @meta(optional,omit_empty)

		// CUE: nested struct
		customer: {
			name:  string
			email: =~"^.+@.+$"
			vip:   bool
		}

		// CUE: array of struct
		items: [...{
			sku:   =~"^SKU-[A-Z0-9]{6}$"
			price: number & >0
			qty:   int & >=1
		}]

		// @blob: bool → validation
		amount_check: bool @blob(this.amount >= 100)

		// @blob: non-bool → computed field (must reference input data, not other blobs)
		total: number @blob(this.items.map_each(this.price * this.qty).sum())
		vip_label: string @blob(if this.customer.vip { "VIP Customer" } else { "Regular" })

		// @meta: priority groups + conditional required
		payment_type: "card" | "wallet"
		card_number?: string @meta(conditional, priority=2, required_if=this.payment_type == "card")

		// @meta: skip_if + omit_if_skip
		wallet_id?: string @meta(conditional, priority=2, required_if=this.payment_type == "wallet", skip_if=this.payment_type == "card", omit_if_skip)
	}`)

	order := map[string]any{
		"order_id": "ORD-20260725",
		"amount":   int64(15000),
		"currency": "CNY",
		"customer": map[string]any{"name": "张三", "email": "zhang@shop.cn", "vip": true},
		"items": []any{
			map[string]any{"sku": "SKU-LAP001", "price": 5999.0, "qty": int64(2)},
			map[string]any{"sku": "SKU-MOU001", "price": 99.0, "qty": int64(3)},
		},
		"payment_type": "card",
		"card_number":  "6222021234567890",
	}

	r := v.Process(order)
	fmt.Printf("    valid=%v\n", r.Valid)
	fmt.Printf("    total=%.0f, vip_label=%v\n", r.Output["total"], r.Output["vip_label"])
	fmt.Printf("    wallet_id in output: %v (omit_if_skip works)\n", r.Output["wallet_id"] != nil)
}

// ─── Section 3: FailMode ─────────────────────────────────────────────────────

func demoFailModes() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$" @meta(priority=1)
		amount:   int & >0         @meta(priority=1)
		currency: "CNY" | "USD"    @meta(priority=2)
		luhn:     bool @blob(this.pan.luhn_valid()) @meta(priority=2)
	}`)

	bad := map[string]any{"pan": "ABC", "amount": int64(-1), "currency": "XXX"}

	// FailAll: collect every error
	r := v.ProcessWithMode(bad, schemix.FailAll)
	fmt.Printf("    FailAll:      %d errors\n", len(r.Errors))

	// FailFast: stop at first
	r = v.ProcessWithMode(bad, schemix.FailFast)
	fmt.Printf("    FailFast:     %d error(s)\n", len(r.Errors))

	// FailPriority: only lowest failing group
	r = v.ProcessWithMode(bad, schemix.FailPriority)
	fmt.Printf("    FailPriority: %d errors (priority=1 only, group 2 skipped)\n", len(r.Errors))
}

// ─── Section 4: Result Chain API ─────────────────────────────────────────────

func demoResultChain() {
	v := schemix.MustNew(`{
		email:  =~"^.+@.+$"
		age:    int & >=18
		role:   "admin" | "user"
		active: bool @blob(this.age >= 18)
	}`)

	r := v.ProcessWithMode(map[string]any{
		"email": "bad", "age": int64(10), "role": "hacker",
	}, schemix.FailAll)

	// .Valid, .Err()
	fmt.Printf("    Valid=%v, Err()=%v\n", r.Valid, r.Err() != nil)

	// .FirstError()
	if first := r.FirstError(); first != nil {
		fmt.Printf("    FirstError: [%s] %s\n", first.Code, first.Path)
	}

	// .HasCode()
	fmt.Printf("    HasCode(FormatMismatch)=%v\n", r.HasCode(schemix.CodeFormatMismatch))
	fmt.Printf("    HasCode(BizRuleFailed)=%v\n", r.HasCode(schemix.CodeBizRuleFailed))

	// .HasErrorsAt()
	fmt.Printf("    HasErrorsAt(\"email\")=%v, HasErrorsAt(\"role\")=%v\n",
		r.HasErrorsAt("email"), r.HasErrorsAt("role"))

	// .ErrorsByCode()
	fmt.Printf("    ErrorsByCode(EnumInvalid): %d\n", len(r.ErrorsByCode(schemix.CodeEnumInvalid)))

	// .ErrorsByPath()
	fmt.Printf("    ErrorsByPath(\"age\"): %d\n", len(r.ErrorsByPath("age")))

	// .ErrorsByType()
	fmt.Printf("    ErrorsByType(\"cue\"): %d, ErrorsByType(\"blob\"): %d\n",
		len(r.ErrorsByType("cue")), len(r.ErrorsByType("blob")))

	// .ErrorMessages()
	lines := strings.Split(r.ErrorMessages(), "\n")
	fmt.Printf("    ErrorMessages: %d lines\n", len(lines))
}

// ─── Section 5: Custom Functions & Methods ───────────────────────────────────

func demoCustomFunctions() {
	// V1 Function style: check_inventory(this.sku, this.qty)
	// V1 Method style: this.sku.is_available()
	// V2 Function with PluginSpec: calc_shipping(weight: N, zone: S)
	// V2 Method with PluginSpec: this.price.apply_tax(rate: 0.13)

	schema := `{
		sku:      string
		qty:      int & >=1
		price:    number & >0
		weight:   number & >0
		zone:     string

		// bool @blob: validation only (true=pass, false=fail)
		in_stock:  bool   @blob(check_inventory(this.sku, this.qty))
		available: bool   @blob(this.sku.is_available())

		// non-bool @blob: computed value → Output
		shipping:  number @blob(calc_shipping(weight: this.weight, zone: this.zone))
		with_tax:  number @blob(this.price.apply_tax(rate: 0.13))
	}`

	inventory := map[string]int{"SKU-LAPTOP": 50, "SKU-PHONE": 0}

	v, _ := schemix.New(schema,
		// V1 Function
		schemix.WithFunction("check_inventory", func(args ...any) (bloblang.Function, error) {
			sku := args[0].(string)
			qty := args[1].(int64)
			return func() (any, error) {
				stock, ok := inventory[sku]
				if !ok {
					return false, nil
				}
				return int64(stock) >= qty, nil
			}, nil
		}),

		// V1 Method
		schemix.WithMethod("is_available", func(v any) (any, error) {
			_, ok := inventory[v.(string)]
			return ok, nil
		}),

		// V2 Function with typed params
		schemix.WithFunctionV2("calc_shipping",
			bloblang.NewPluginSpec().
				Param(bloblang.NewFloat64Param("weight")).
				Param(bloblang.NewStringParam("zone")),
			func(args *bloblang.ParsedParams) (bloblang.Function, error) {
				weight, _ := args.GetFloat64("weight")
				zone, _ := args.GetString("zone")
				return func() (any, error) {
					base := 10.0
					if zone == "international" {
						base = 30.0
					}
					return base + weight*2.5, nil
				}, nil
			},
		),

		// V2 Method with typed params
		schemix.WithMethodV2("apply_tax",
			bloblang.NewPluginSpec().
				Param(bloblang.NewFloat64Param("rate")),
			func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				rate, _ := args.GetFloat64("rate")
				return func(v any) (any, error) {
					price := v.(float64)
					return price * (1 + rate), nil
				}, nil
			},
		),
	)

	// Valid case: SKU-LAPTOP has stock=50, qty=2 passes
	r := v.Process(map[string]any{
		"sku": "SKU-LAPTOP", "qty": int64(2), "price": 5999.0, "weight": 3.5, "zone": "domestic",
	})
	fmt.Printf("    valid=%v (in_stock + available pass as bool validations)\n", r.Valid)
	fmt.Printf("    shipping=%.1f, with_tax=%.2f\n", r.Output["shipping"], r.Output["with_tax"])

	// Invalid case: SKU-PHONE has stock=0, fails in_stock check
	r = v.Process(map[string]any{
		"sku": "SKU-PHONE", "qty": int64(1), "price": 2999.0, "weight": 0.5, "zone": "international",
	})
	fmt.Printf("    out-of-stock: valid=%v, errors=%d\n", r.Valid, len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("      [%s] %s\n", e.Code, e.Path)
	}
}

// ─── Section 6: FuncMap ──────────────────────────────────────────────────────

func demoFuncMap() {
	// Build once, share across many validators
	funcs := schemix.NewFuncMap(
		// Function style: classify_day(day) → "business" or "weekend"
		schemix.Func("classify_day", func(args ...any) (bloblang.Function, error) {
			day := args[0].(string)
			return func() (any, error) {
				weekdays := map[string]bool{"Mon": true, "Tue": true, "Wed": true, "Thu": true, "Fri": true}
				if weekdays[day] {
					return "business", nil
				}
				return "weekend", nil
			}, nil
		}),
		schemix.Method("mask_email", func(v any) (any, error) {
			email := v.(string)
			parts := strings.SplitN(email, "@", 2)
			if len(parts) != 2 {
				return email, nil
			}
			name := parts[0]
			if len(name) > 2 {
				name = name[:2] + "***"
			}
			return name + "@" + parts[1], nil
		}),
		schemix.MethodV2("truncate_to",
			bloblang.NewPluginSpec().Param(bloblang.NewInt64Param("max")),
			func(args *bloblang.ParsedParams) (bloblang.Method, error) {
				max, _ := args.GetInt64("max")
				return func(v any) (any, error) {
					s := v.(string)
					if int64(len(s)) > max {
						return s[:max] + "...", nil
					}
					return s, nil
				}, nil
			},
		),
	)

	// Check FuncMap construction errors
	if err := funcs.Err(); err != nil {
		fmt.Printf("    FuncMap error: %v\n", err)
		return
	}

	// Share across two validators
	v1 := schemix.MustNew(`{
		day:   string
		email: string
		day_type:     string @blob(classify_day(this.day))
		masked_email: string @blob(this.email.mask_email())
	}`, schemix.WithFuncMap(funcs))

	v2 := schemix.MustNew(`{
		title:     string
		short:     string @blob(this.title.truncate_to(max: 20))
	}`, schemix.WithFuncMap(funcs))

	r1 := v1.Process(map[string]any{"day": "Mon", "email": "alice.wang@company.com"})
	fmt.Printf("    v1: day_type=%v, masked=%v\n", r1.Output["day_type"], r1.Output["masked_email"])

	r2 := v2.Process(map[string]any{"title": "This is a very long product title that needs truncation"})
	fmt.Printf("    v2: short=%q\n", r2.Output["short"])
}

// ─── Section 7: Override Built-in ────────────────────────────────────────────

func demoOverride() {
	// Override a specific built-in method
	v := schemix.MustNew(`{
		email: string @blob(this.email.is_email())
	}`,
		schemix.WithOverrideMethod("is_email"),
		schemix.WithMethod("is_email", func(v any) (any, error) {
			s := v.(string)
			// Stricter: must end with company domain
			return strings.HasSuffix(s, "@mycompany.com"), nil
		}),
	)

	r := v.Process(map[string]any{"email": "alice@mycompany.com"})
	fmt.Printf("    override is_email: company domain → valid=%v\n", r.Valid)

	r = v.Process(map[string]any{"email": "alice@gmail.com"})
	fmt.Printf("    override is_email: external domain → valid=%v\n", r.Valid)

	// WithOverrideAll: disable all conflict checks
	vAll := schemix.MustNew(`{
		url: string @blob(this.url.is_url())
	}`,
		schemix.WithOverrideAll(),
		schemix.WithMethod("is_url", func(v any) (any, error) {
			return strings.HasPrefix(v.(string), "https://"), nil
		}),
	)
	r = vAll.Process(map[string]any{"url": "https://secure.site"})
	fmt.Printf("    override all (https only): valid=%v\n", r.Valid)
	r = vAll.Process(map[string]any{"url": "http://insecure.site"})
	fmt.Printf("    override all (http rejected): valid=%v\n", r.Valid)
}

// ─── Section 8: ErrorFormatter ───────────────────────────────────────────────

func demoErrorFormatter() {
	// Chinese i18n formatter
	zhMessages := map[schemix.ErrorCode]string{
		schemix.CodeFormatMismatch:  "格式不正确",
		schemix.CodeTypeMismatch:    "数据类型错误",
		schemix.CodeEnumInvalid:     "值不在允许范围内",
		schemix.CodeRangeViolation:  "数值超出范围",
		schemix.CodeRequiredMissing: "必填字段缺失",
		schemix.CodeBizRuleFailed:   "业务规则校验失败",
	}

	v := schemix.MustNew(`{
		phone: =~"^1[3-9][0-9]{9}$"
		age:   int & >=18 & <=65
		level: "bronze" | "silver" | "gold"
	}`, schemix.WithErrorFormatter(func(code schemix.ErrorCode, path, _ string) string {
		if msg, ok := zhMessages[code]; ok {
			return fmt.Sprintf("[%s] %s", path, msg)
		}
		return fmt.Sprintf("[%s] 验证失败", path)
	}))

	r := v.ProcessWithMode(map[string]any{
		"phone": "12345", "age": int64(200), "level": "diamond",
	}, schemix.FailAll)

	fmt.Println("    Chinese error messages:")
	for _, e := range r.Errors {
		fmt.Printf("      %s\n", e.Message)
	}
}

// ─── Section 9: Introspection ────────────────────────────────────────────────

func demoIntrospection() {
	v := schemix.MustNew(`{
		order_id:   =~"^ORD-[0-9]+$"
		amount:     int & >0
		currency:   "CNY" | "USD"
		memo?:      string
		customer: {
			name:  string
			email: string
		}
		fee: number @blob(this.amount * 0.01)
	}`)

	fields := v.Fields()
	fmt.Printf("    Total fields: %d\n", len(fields))

	// Required input fields (not optional, not computed)
	fmt.Print("    Required inputs: ")
	var required []string
	for _, f := range fields {
		if !f.Optional && !f.HasBlob {
			required = append(required, f.Name)
		}
	}
	fmt.Println(strings.Join(required, ", "))

	// Computed fields
	fmt.Print("    Computed (@blob): ")
	var computed []string
	for _, f := range fields {
		if f.HasBlob {
			computed = append(computed, f.Name)
		}
	}
	fmt.Println(strings.Join(computed, ", "))

	// Nested children
	for _, f := range fields {
		if len(f.Children) > 0 {
			fmt.Printf("    Nested '%s' has %d children\n", f.Name, len(f.Children))
		}
	}
}

// ─── Section 10: Registry + Pipeline ─────────────────────────────────────────

func demoRegistryPipeline() {
	reg := schemix.NewRegistry()

	// Register multiple schemas
	_ = reg.Register("product", `{
		sku:   =~"^SKU-[A-Z0-9]{6}$"
		name:  string
		price: number & >0
	}`)
	_ = reg.Register("coupon", `{
		code:     =~"^CPN-[A-Z0-9]{6}$"
		discount: number & >0 & <=1
		label:    string @blob(if this.discount > 0.3 { "big-sale" } else { "normal" })
	}`)

	fmt.Printf("    Registry: %v (len=%d)\n", reg.List(), reg.Len())
	fmt.Printf("    Has(product)=%v, Has(order)=%v\n", reg.Has("product"), reg.Has("order"))

	// Get and use directly
	pv, _ := reg.Get("product")
	r := pv.Process(map[string]any{"sku": "SKU-LAP001", "name": "Laptop", "price": 5999.0})
	fmt.Printf("    Direct use: valid=%v\n", r.Valid)

	// Scoped Bloblang integration — RegisterAllTo
	env := bloblang.NewEnvironment()
	if err := reg.RegisterAllTo(env); err != nil {
		fmt.Printf("    RegisterAllTo error: %v\n", err)
		return
	}

	// Method form: this.validate_schema(name: "product")
	mapping := `
		let r = this.validate_schema(name: "product")
		root.valid = $r.valid
		root.error_count = $r.errors.length()
	`
	exec, err := env.Parse(mapping)
	if err != nil {
		fmt.Printf("    Parse error: %v\n", err)
		return
	}
	out, _ := exec.Query(map[string]any{"sku": "SKU-BAD!!!", "name": "X", "price": -1.0})
	result := out.(map[string]any)
	fmt.Printf("    Pipeline validate_schema: valid=%v, errors=%v\n", result["valid"], result["error_count"])

	// process_schema: includes computed output
	mapping2 := `
		let r = this.process_schema(name: "coupon")
		root.valid = $r.valid
		root.label = if $r.valid { $r.output.label } else { "N/A" }
	`
	exec2, _ := env.Parse(mapping2)
	out2, _ := exec2.Query(map[string]any{"code": "CPN-SAVE20", "discount": 0.2})
	result2 := out2.(map[string]any)
	fmt.Printf("    Pipeline process_schema: valid=%v, label=%v\n", result2["valid"], result2["label"])

	// Unregister
	reg.Unregister("coupon")
	fmt.Printf("    After unregister: %v\n", reg.List())
}

// ─── Section 11: Composition ─────────────────────────────────────────────────

func demoComposition() {
	ctx := cuecontext.New()

	// Shared definitions — reuse across schemas
	defs := ctx.CompileString(`{
		#Money:    int & >0 & <=99999999
		#Currency: "CNY" | "USD" | "EUR" | "GBP"
		#SKU:      =~"^SKU-[A-Z0-9]{6}$"

		amount:   #Money
		currency: #Currency
		sku:      #SKU
	}`)

	v, err := schemix.NewFromValue(defs)
	if err != nil {
		fmt.Printf("    NewFromValue error: %v\n", err)
		return
	}

	r := v.Process(map[string]any{"amount": int64(9999), "currency": "GBP", "sku": "SKU-ABC123"})
	fmt.Printf("    Composed schema: valid=%v\n", r.Valid)

	r = v.Process(map[string]any{"amount": int64(-1), "currency": "JPY", "sku": "bad"})
	fmt.Printf("    Composed invalid: %d errors\n", len(r.Errors))

	// NewFromValue with @blob options
	blobDefs := ctx.CompileString(`{
		price: number & >0
		qty:   int & >=1
		total: number @blob(this.price * this.qty)
	}`)
	v2, _ := schemix.NewFromValue(blobDefs)
	r = v2.Process(map[string]any{"price": 29.9, "qty": int64(3)})
	fmt.Printf("    Composed with @blob: total=%.1f\n", r.Output["total"])
}

// ─── Section 12: Validate (fast path) ────────────────────────────────────────

func demoValidateFastPath() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "CNY" | "USD"
		luhn:     bool @blob(this.pan.luhn_valid())
	}`)

	// Validate(): no Output, no deepCopy — faster for gateway checks
	valid, errs := v.Validate(map[string]any{
		"pan": "4111111111111111", "amount": int64(100), "currency": "CNY",
	})
	fmt.Printf("    Validate (valid): valid=%v, errors=%d\n", valid, len(errs))

	valid, errs = v.Validate(map[string]any{
		"pan": "INVALID", "amount": int64(-1), "currency": "XXX",
	})
	fmt.Printf("    Validate (invalid): valid=%v, errors=%d\n", valid, len(errs))

	// Compare: Process returns Output, Validate does not
	r := v.Process(map[string]any{
		"pan": "4111111111111111", "amount": int64(100), "currency": "CNY",
	})
	fmt.Printf("    Process: Output has %d keys (includes computed fields)\n", len(r.Output))
	fmt.Printf("    Validate: use when you only need pass/fail + error details\n")
}
