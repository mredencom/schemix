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
//	    email: =~"^.+@.+\\..+$"
//	    age:   int & >=0 & <=150
//	}`)
//
//	r := v.Process(map[string]any{"name": "Alice", "email": "alice@test.com", "age": 30})
//	if r.Valid {
//	    // use r.Output
//	}
//
// # Fail Modes
//
// Three strategies control error collection behavior:
//
//   - FailAll: collect all errors (default, best for form validation)
//   - FailFast: stop at first error (best for API gateways)
//   - FailPriority: priority-group isolation (p1 failure skips p2+)
//
// # Bloblang Integration
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
package schemix
