// Package schemix provides a schema-driven validation and transformation engine
// powered by CUE constraints and Bloblang dynamic expressions.
//
// It combines CUE's declarative type system with Bloblang's scripting capability
// through three annotation layers:
//
//   - CUE native constraints: types, regex, enums, ranges, nested structs, arrays
//   - @blob() dynamic expressions: Bloblang syntax for validation (bool) and computed fields
//   - @meta() field behavior control: priority, optional, conditional, skip/omit rules
//
// # Quick Start
//
//	v := schemix.MustNew(`{
//	    name:  string
//	    email: string @blob(this.email.is_email())
//	    age:   int    @blob(this.age.between(min: 0, max: 150))
//	    pan:   =~"^[0-9]{16}$"
//	    luhn:  bool   @blob(this.pan.luhn_valid())
//	}`)
//
//	r := v.Process(map[string]any{
//	    "name": "Alice", "email": "alice@test.com", "age": int64(30),
//	    "pan": "4111111111111111",
//	})
//	if r.Valid {
//	    // use r.Output
//	}
//
// # Built-in Validation Methods
//
// Every Validator automatically includes 37+ built-in validation methods callable
// in @blob() expressions, covering common format checks:
//
//   - String format: is_email, is_url, is_full_url, is_uuid/3/4/5, is_ip/v4/v6,
//     is_cidr, is_mac, is_dns_name, is_cn_mobile, is_json, is_base64, is_hex,
//     is_hex_color, is_rgb_color, is_data_uri, is_latitude, is_longitude,
//     is_isbn10, is_isbn13
//   - Character type: is_alpha, is_alpha_num, is_alpha_dash, is_numeric, is_number,
//     is_ascii, is_printable_ascii, is_multibyte
//   - String checks: not_blank, has_whitespace
//   - Length: len_between(min,max), min_len(n), max_len(n), str_len(min,max)
//   - Numeric: between(min,max)
//   - Financial: luhn_valid
//   - Date functions: is_valid_date, is_past_date, is_future_date
//
// Usage in schema:
//
//	email: string @blob(this.email.is_email())
//	pan:   string @blob(this.pan.luhn_valid())
//	age:   int    @blob(this.age.between(min: 0, max: 150))
//	name:  string @blob(this.name.len_between(min: 2, max: 50))
//
// # Fail Modes
//
// Three strategies control error collection behavior:
//
//   - FailAll: collect all errors (default, best for form validation)
//   - FailFast: stop at first error (best for API gateways)
//   - FailPriority: priority-group isolation (p1 failure skips p2+)
//
// # Error Handling
//
// Result provides multiple ways to inspect errors:
//
//	r := v.Process(data)
//	r.Valid                         // bool
//	r.Err()                         // combined error (nil if valid)
//	r.FirstError()                  // *ValidationError
//	r.ErrorsByPath("pan")           // []ValidationError
//	r.ErrorsByCode(CodeTypeMismatch)// []ValidationError
//	r.ErrorsByType("cue")           // []ValidationError — filter by layer
//	r.HasCode(CodeBizRuleFailed)    // bool — quick check
//	r.HasErrorsAt("email")          // bool — field-level check
//
// # Custom Error Messages (i18n)
//
// Provide a custom ErrorFormatter to generate user-facing messages:
//
//	v := schemix.MustNew(schema, schemix.WithErrorFormatter(func(code ErrorCode, path, detail string) string {
//	    return myI18n.Translate("zh-CN", string(code), path)
//	}))
//
// # Custom Functions and Methods
//
// Register custom validation logic using the same API as Bloblang:
//
//	// Function style (called as: my_func(args...))
//	v, _ := schemix.New(schema, schemix.WithFunction("check_blacklist",
//	    func(args ...any) (bloblang.Function, error) {
//	        pan := args[0].(string)
//	        return func() (any, error) { return !isBlocked(pan), nil }, nil
//	    },
//	))
//
//	// Method style (called as: this.field.my_method())
//	v, _ := schemix.New(schema, schemix.WithMethod("is_valid_bin",
//	    func(v any) (any, error) {
//	        return checkBIN(v.(string)), nil
//	    },
//	))
//
//	// V2 style with typed parameters (same as bloblang.RegisterFunctionV2)
//	v, _ := schemix.New(schema, schemix.WithFunctionV2("calc_fee",
//	    bloblang.NewPluginSpec().
//	        Param(bloblang.NewInt64Param("amount")).
//	        Param(bloblang.NewFloat64Param("rate")),
//	    func(args *bloblang.ParsedParams) (bloblang.Function, error) {
//	        amount, _ := args.GetInt64("amount")
//	        rate, _ := args.GetFloat64("rate")
//	        return func() (any, error) { return float64(amount) * rate, nil }, nil
//	    },
//	))
//
// Custom functions are isolated per Validator — they do not leak to other instances.
//
// # Schema Composition
//
// Use NewFromValue to build validators from pre-compiled CUE values, enabling
// schema reuse through CUE definitions:
//
//	ctx := cuecontext.New()
//	schema := ctx.CompileString(`{
//	    #PAN: =~"^[0-9]{16}$"
//	    pan:    #PAN
//	    amount: int & >0
//	}`)
//	v, _ := schemix.NewFromValue(schema)
//
// # Schema Introspection
//
// Inspect schema structure at runtime for documentation or UI generation:
//
//	fields := v.Fields() // []FieldInfo{Name, Path, Type, Optional, HasBlob, Children}
//
// # Performance
//
// Schemix uses a Go-native fast path for simple constraints (type, regex, range,
// enum), bypassing CUE evaluation entirely. Typical Process latency is 2-3µs
// for schemas with scalar fields.
//
// # Bloblang Pipeline Integration
//
// Register schemas into a Registry for use within Benthos/Redpanda Connect pipelines:
//
//	reg := schemix.NewRegistry()
//	reg.Register("payment", cueSrc)
//	reg.RegisterAll() // registers both method and function forms
//
// Then use in Bloblang mappings:
//
//	let r = this.process_schema(name: "payment", mode: "fast")
//	let r = validate_schema(data: this.payload, name: "payment")
//
// # Thread Safety
//
// Validator is safe for concurrent use after construction. Registry uses
// sync.RWMutex for concurrent Register/Get/Unregister operations.
package schemix
