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
// Errors serve three audiences, and Result has a set of methods for each.
//
// For a person — localized text, ready to return to a caller:
//
//	r.LocalizedMessages()               // []string, in the configured language
//	r.LocalizedMessagesWith(loc)        // []string, in a language chosen now
//	loc.Localize(e)                     // one error
//
// For a log — the raw diagnostic, carrying CUE/Bloblang wording:
//
//	r.ErrorMessages()                   // one per line, with error codes
//	e.Message                           // the diagnostic for one error
//
// For code — structured fields to branch on:
//
//	r.Valid                          // bool
//	r.Err()                          // combined error (nil if valid)
//	r.FirstError()                   // *ValidationError
//	r.ErrorsByPath("pan")            // []ValidationError
//	r.ErrorsByCode(CodeTypeMismatch) // []ValidationError
//	r.ErrorsByType("cue")            // []ValidationError — filter by layer
//	r.HasCode(CodeBizRuleFailed)     // bool — quick check
//	r.HasErrorsAt("email")           // bool — field-level check
//
// Returning e.Message to a caller is the mistake to avoid: it leaks schema
// internals and is never translated.
//
// # Localization
//
// A Localizer renders an error as text for a person. Set one for the whole
// validator when the service speaks one language:
//
//	v := schemix.MustNew(schema, schemix.WithLocalizer(schemix.ZhCN))
//	msgs := v.Process(data).LocalizedMessages()
//
// Or choose per request when it speaks several — one validator, concurrently:
//
//	msgs := r.LocalizedMessagesWith(catalogFor(req.Header.Get("Accept-Language")))
//
// EnUS and ZhCN are built in. Override individual messages by chaining, rather
// than copying the table, so reworded built-ins keep reaching you:
//
//	var myCatalog = &schemix.Catalog{
//	    Messages: map[schemix.ErrorCode]schemix.Message{
//	        schemix.CodeRequiredMissing: {Template: "please fill in {field}"},
//	    },
//	    Labels:   map[string]string{"user_email": "Email address"},
//	    Fallback: schemix.EnUS,
//	}
//
// Call Catalog.Validate at startup to be told about gaps — an uncovered code
// renders as generic wording rather than failing, so nothing else will tell you.
//
// Localizer is an interface, so an existing translation pipeline can be used
// directly instead of moving its catalogue into Go.
//
// Two things it does not affect: ValidationError.FriendlyMessage is always
// English, and the Validate family carries no default because it returns errors
// without a Result. Localize those explicitly.
//
// # Custom Error Messages
//
// ErrorFormatter replaces ValidationError.Message, which is the diagnostic meant
// for logs:
//
//	v := schemix.MustNew(schema, schemix.WithErrorFormatter(
//	    func(code ErrorCode, path, detail string) string {
//	        return fmt.Sprintf("%s[%s]: %s", path, code, detail)
//	    },
//	))
//
// It is not the way to translate: it receives three strings rather than the whole
// error, and it overwrites the text a developer needs when debugging. Use a
// Localizer for user-facing wording. The two are independent and can be set
// together.
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
