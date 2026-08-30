package schemix

import (
	"cmp"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestFields_SimpleSchema(t *testing.T) {
	v := MustNew(`{
		name:    string
		age:     int
		memo?:   string
		amount:  float
		active:  bool
	}`)

	fields := v.Fields()

	if len(fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(fields))
	}

	// Verify by building a lookup map
	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	tests := []struct {
		name     string
		typ      string
		optional bool
		hasBlob  bool
		path     string
	}{
		{"name", "string", false, false, "name"},
		{"age", "int", false, false, "age"},
		{"memo", "string", true, false, "memo"},
		{"amount", "float", false, false, "amount"},
		{"active", "bool", false, false, "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := byName[tt.name]
			if !ok {
				t.Fatalf("field %q not found", tt.name)
			}
			if f.Type != tt.typ {
				t.Errorf("Type: got %q, want %q", f.Type, tt.typ)
			}
			if f.Optional != tt.optional {
				t.Errorf("Optional: got %v, want %v", f.Optional, tt.optional)
			}
			if f.HasBlob != tt.hasBlob {
				t.Errorf("HasBlob: got %v, want %v", f.HasBlob, tt.hasBlob)
			}
			if f.Path != tt.path {
				t.Errorf("Path: got %q, want %q", f.Path, tt.path)
			}
			if len(f.Children) != 0 {
				t.Errorf("Children: got %d, want 0", len(f.Children))
			}
		})
	}
}

func TestFields_NestedSchema(t *testing.T) {
	v := MustNew(`{
		id:   string
		address: {
			city:    string
			zip:     int
			country?: string
		}
	}`)

	fields := v.Fields()

	if len(fields) != 2 {
		t.Fatalf("expected 2 top-level fields, got %d", len(fields))
	}

	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	// Check top-level id field
	id := byName["id"]
	if id.Type != "string" {
		t.Errorf("id.Type: got %q, want %q", id.Type, "string")
	}
	if id.Path != "id" {
		t.Errorf("id.Path: got %q, want %q", id.Path, "id")
	}

	// Check nested address field
	addr := byName["address"]
	if addr.Type != "struct" {
		t.Errorf("address.Type: got %q, want %q", addr.Type, "struct")
	}
	if addr.Path != "address" {
		t.Errorf("address.Path: got %q, want %q", addr.Path, "address")
	}
	if len(addr.Children) != 3 {
		t.Fatalf("address.Children: got %d, want 3", len(addr.Children))
	}

	// Check children
	childByName := make(map[string]FieldInfo)
	for _, c := range addr.Children {
		childByName[c.Name] = c
	}

	city := childByName["city"]
	if city.Type != "string" {
		t.Errorf("city.Type: got %q, want %q", city.Type, "string")
	}
	if city.Path != "address.city" {
		t.Errorf("city.Path: got %q, want %q", city.Path, "address.city")
	}
	if city.Optional {
		t.Error("city.Optional: should be false")
	}

	zip := childByName["zip"]
	if zip.Type != "int" {
		t.Errorf("zip.Type: got %q, want %q", zip.Type, "int")
	}
	if zip.Path != "address.zip" {
		t.Errorf("zip.Path: got %q, want %q", zip.Path, "address.zip")
	}

	country := childByName["country"]
	if country.Type != "string" {
		t.Errorf("country.Type: got %q, want %q", country.Type, "string")
	}
	if !country.Optional {
		t.Error("country.Optional: should be true")
	}
}

func TestFields_BlobFields(t *testing.T) {
	v := MustNew(`{
		pan:        =~"^[0-9]{16}$"
		amount:     int & >0
		pan_check:  bool   @blob(this.pan.has_prefix("62"))
		card_brand: string @blob(if this.pan.has_prefix("62") { "UnionPay" } else { "Visa" })
	}`)

	fields := v.Fields()

	byName := make(map[string]FieldInfo)
	for _, f := range fields {
		byName[f.Name] = f
	}

	// Non-blob fields
	if byName["pan"].HasBlob {
		t.Error("pan.HasBlob: should be false")
	}
	if byName["amount"].HasBlob {
		t.Error("amount.HasBlob: should be false")
	}

	// Blob fields
	if !byName["pan_check"].HasBlob {
		t.Error("pan_check.HasBlob: should be true")
	}
	if !byName["card_brand"].HasBlob {
		t.Error("card_brand.HasBlob: should be true")
	}

	// Type should still be correct for blob fields
	if byName["pan_check"].Type != "bool" {
		t.Errorf("pan_check.Type: got %q, want %q", byName["pan_check"].Type, "bool")
	}
	if byName["card_brand"].Type != "string" {
		t.Errorf("card_brand.Type: got %q, want %q", byName["card_brand"].Type, "string")
	}
}

func TestFields_EmptyStruct(t *testing.T) {
	v := MustNew(`{}`)

	fields := v.Fields()

	if fields == nil {
		t.Fatal("Fields() should return non-nil empty slice")
	}
	if len(fields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(fields))
	}
}

// ========== Result convenience methods (moved from registry_test.go) ==========

// TestMetaCompileReject verifies parsefieldMeta fails on invalid @meta() params (R9).
func TestMetaCompileReject(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string // substring in compile error
	}{
		// Unknown params must error
		{
			name:    "unknown_param_rejects",
			schema:  `{ x: string @meta(foo_bar) }`,
			wantErr: "unknown @meta parameter",
		},
		// priority must be a valid integer
		{
			name:    "priority_non_numeric",
			schema:  `{ x: string @meta(priority=abc) }`,
			wantErr: "priority",
		},
		{
			name:    "priority_overflow",
			schema:  `{ x: string @meta(priority=99999999999999999999) }`,
			wantErr: "priority",
		},
		// Negative priority IS valid (not an error)
		// required_if with empty expression
		{
			name:    "required_if_empty_expr",
			schema:  `{ x?: string @meta(conditional, required_if=) }`,
			wantErr: "required_if",
		},
		// skip_if with empty expression
		{
			name:    "skip_if_empty_expr",
			schema:  `{ x: string @meta(skip_if=) }`,
			wantErr: "skip_if",
		},
		// required_if with invalid bloblang expression (valid CUE syntax)
		{
			name:    "required_if_parse_error",
			schema:  `{ x?: string @meta(conditional, required_if=this.!!!bad) }`,
			wantErr: "required_if",
		},
		// skip_if with invalid bloblang expression (valid CUE syntax)
		{
			name:    "skip_if_parse_error",
			schema:  `{ x: string @meta(skip_if=this.!!!bad) }`,
			wantErr: "skip_if",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)
			if err == nil {
				t.Fatal("expected compile error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestMetaCompileAccept verifies valid @meta() params compile successfully.
func TestMetaCompileAccept(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{"negative_priority", `{ x: string @meta(priority=-1) }`},
		{"flag_optional", `{ x?: string @meta(optional) }`},
		{"flag_conditional", `{ x?: string @meta(conditional) }`},
		{"flag_skip_empty", `{ x: string @meta(skip_empty) }`},
		{"flag_fail_fast", `{ x: string @meta(fail_fast) }`},
		{"flag_omit_if_skip", `{ x: string @meta(omit_if_skip) }`},
		{"flag_omit_empty", `{ x: string @meta(omit_empty) }`},
		{"valid_required_if", `{ x?: string @meta(conditional, required_if=true) }`},
		{"valid_skip_if", `{ x: string @meta(skip_if=true) }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)
			if err != nil {
				t.Fatalf("unexpected compile error: %v", err)
			}
		})
	}
}

// TestCmpCompareSortOverflowSafe verifies that sortBlobRules uses cmp.Compare
// instead of integer subtraction, preventing overflow on extreme priority values.
func TestCmpCompareSortOverflowSafe(t *testing.T) {
	rules := []blobRule{
		{Path: "c", Meta: fieldMeta{Priority: math.MaxInt}},
		{Path: "a", Meta: fieldMeta{Priority: math.MinInt}},
		{Path: "b", Meta: fieldMeta{Priority: 0}},
	}

	sortBlobRules(rules)

	// Expected order: MinInt < 0 < MaxInt
	expected := []string{"a", "b", "c"}
	for i, r := range rules {
		if r.Path != expected[i] {
			t.Errorf("rules[%d].Path = %q, want %q", i, r.Path, expected[i])
		}
	}

	// Verify the old subtraction would have overflowed:
	// math.MinInt - math.MaxInt would wrap positive, corrupting sort.
	// We check that cmp.Compare gives the correct answer:
	if cmp.Compare(math.MinInt, math.MaxInt) != -1 {
		t.Error("cmp.Compare(MinInt, MaxInt) should be -1")
	}
}

func TestNewRejectsAttributesInsideArrayElements(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		// Rejected — attribute sits inside the element schema.
		{
			name:    "blob inside array element",
			schema:  `{ items: [...{qty: int @blob(this.qty > 100)}] }`,
			wantErr: true,
		},
		{
			name:    "meta inside array element",
			schema:  `{ items: [...{memo: string @meta(optional)}] }`,
			wantErr: true,
		},
		{
			name:    "blob deeper inside array element",
			schema:  `{ items: [...{sub: {x: int @blob(this.x > 0)}}] }`,
			wantErr: true,
		},
		{
			name:    "blob inside array nested in a struct",
			schema:  `{ order: {items: [...{qty: int @blob(this.qty > 0)}]} }`,
			wantErr: true,
		},
		{
			name:    "blob inside an array nested in an array element",
			schema:  `{ items: [...{sub: [...{x: int @blob(this.x > 0)}]}] }`,
			wantErr: true,
		},

		// Accepted — the supported form puts the attribute on the array field.
		{
			name:    "blob on the array field itself",
			schema:  `{ items: [...{qty: int}] @blob(this.items.all(i -> i.qty > 100)) }`,
			wantErr: false,
		},
		{
			name:    "meta on the array field itself",
			schema:  `{ a: int, items?: [...int] @meta(optional, omit_empty) }`,
			wantErr: false,
		},
		{
			name:    "plain scalar array",
			schema:  `{ items: [...int] }`,
			wantErr: false,
		},
		{
			name:    "plain struct array without attributes",
			schema:  `{ items: [...{qty: int, name: string}] }`,
			wantErr: false,
		},
		{
			name:    "blob in a nested struct is still allowed",
			schema:  `{ user: {age: int, adult: bool @blob(this.user.age >= 18)} }`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)

			if tt.wantErr {
				if err == nil {
					t.Fatal("New() returned nil error; attributes inside array " +
						"elements are silently ignored and must be rejected")
				}
				// The message has to name the offending path and show the fix,
				// otherwise the user cannot act on it.
				msg := err.Error()
				for _, want := range []string{"items", "array element"} {
					if !strings.Contains(msg, want) {
						t.Errorf("error message missing %q; got: %s", want, msg)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("New() error on a supported schema: %v", err)
			}
		})
	}
}

// TestArrayFieldBlobStillWorks pins the supported form end-to-end, so the new
// rejection cannot regress it.
func TestArrayFieldBlobStillWorks(t *testing.T) {
	v, err := New(`{
		items: [...{qty: int & >=0, subtotal?: number}] @blob(
			this.items.length() > 0,
			this.items.map_each(this.merge({"subtotal": this.qty * 2}))
		)
	}`)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	r := v.Process(map[string]any{"items": []any{
		map[string]any{"qty": int64(5)},
	}})
	if !r.Valid {
		t.Fatalf("Valid = false, want true; errors: %v", r.Errors)
	}

	got, ok := r.Output["items"].([]any)
	if !ok || len(got) != 1 {
		t.Fatalf("Output[items] = %#v, want a 1-element slice", r.Output["items"])
	}
	elem, _ := got[0].(map[string]any)
	if elem["subtotal"] != int64(10) {
		t.Errorf("Output[items][0][subtotal] = %v (%T), want 10", elem["subtotal"], elem["subtotal"])
	}

	// Empty array must fail the length rule.
	if r := v.Process(map[string]any{"items": []any{}}); r.Valid {
		t.Error("empty array accepted, want rejection by the length rule")
	}
}

// ─── Attributes on a definition ─────────────────────────────────────────────
//
// A definition (#Name) is a reusable template, while a @blob expression
// references absolute paths (this.field). The two cannot be combined: the same
// definition may be referenced by several fields, so there is no single field
// the expression could bind to. Such attributes were silently dropped, letting
// invalid data pass, so they must be rejected at construction time.

func TestNewRejectsAttributesOnDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		// Rejected — attribute sits on the definition itself.
		{
			name:    "blob on a scalar definition",
			schema:  `{ #Amount: int @blob(this.amount > 100), amount: #Amount }`,
			wantErr: true,
		},
		{
			name:    "meta on a scalar definition",
			schema:  `{ #Memo: string @meta(optional), memo: #Memo }`,
			wantErr: true,
		},
		{
			name:    "blob on a list definition",
			schema:  `{ #Items: [...int] @blob(this.items.length() > 0), items: #Items }`,
			wantErr: true,
		},
		{
			name:    "blob on a struct definition",
			schema:  `{ #U: {age: int} @blob(this.u.age > 0), u: #U }`,
			wantErr: true,
		},
		{
			name:    "blob on an unreferenced definition",
			schema:  `{ #Unused: int @blob(this.x > 0), plain: int }`,
			wantErr: true,
		},
		{
			name:    "blob on a definition nested in a definition",
			schema:  `{ #Outer: { #Inner: int @blob(this.x > 0), y: int }, o: #Outer }`,
			wantErr: true,
		},

		// Accepted — attribute is on a real field.
		{
			name:    "blob on the field referencing a definition",
			schema:  `{ #PAN: =~"^[0-9]{16}$", pan: #PAN @blob(this.pan.luhn_valid()) }`,
			wantErr: false,
		},
		{
			name:    "blob on a field inside a struct definition",
			schema:  `{ #U: { age: int @blob(this.u.age >= 18) }, u: #U }`,
			wantErr: false,
		},
		{
			name:    "plain definitions without attributes",
			schema:  `{ #PAN: =~"^[0-9]{16}$", #Amt: int & >0, pan: #PAN, amt: #Amt }`,
			wantErr: false,
		},
		{
			name:    "struct definition referenced twice",
			schema:  `{ #Addr: {city: string}, home: #Addr, work: #Addr }`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.schema)

			if tt.wantErr {
				if err == nil {
					t.Fatal("New() returned nil error; an attribute on a definition is " +
						"silently ignored and must be rejected")
				}
				msg := err.Error()
				if !strings.Contains(msg, "definition") {
					t.Errorf("error message should mention 'definition'; got: %s", msg)
				}
				return
			}

			if err != nil {
				t.Fatalf("New() error on a supported schema: %v", err)
			}
		})
	}
}

// TestDefinitionReferenceRuleWorks pins the supported rewrite end-to-end.
func TestDefinitionReferenceRuleWorks(t *testing.T) {
	v, err := New(`{
		#PAN: =~"^[0-9]{16}$"
		pan:  #PAN @blob(this.pan.luhn_valid())
	}`)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if r := v.Process(map[string]any{"pan": "4111111111111111"}); !r.Valid {
		t.Errorf("valid Luhn rejected: %v", r.Errors)
	}

	r := v.Process(map[string]any{"pan": "4111111111111112"})
	if r.Valid {
		t.Fatal("invalid Luhn accepted")
	}
	if got := r.Errors[0].Path; got != "pan" {
		t.Errorf("error path = %q, want %q", got, "pan")
	}
}

// Schema analysis at construction time recurses through structs, array element
// schemas and definitions. CUE permits mutually recursive definitions, so an
// unbounded walk never terminates:
//
//	#A: {bs: [...#B]}
//	#B: {as: [...#A]}
//
// A schema is untrusted input in any system that lets users supply it, so New()
// must terminate on every input. It bounds the walk and reports an error rather
// than silently skipping deeper levels, which would let a hidden @blob/@meta
// attribute slip through unextracted.

// newWithTimeout calls New in a goroutine so a hang surfaces as a test failure
// instead of stalling the whole run.
func newWithTimeout(t *testing.T, schema string, opts ...Option) (err error, timedOut bool) {
	t.Helper()
	type outcome struct {
		err   error
		panic any
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- outcome{panic: p}
			}
		}()
		_, e := New(schema, opts...)
		done <- outcome{err: e}
	}()

	select {
	case o := <-done:
		if o.panic != nil {
			t.Fatalf("New() panicked (likely stack exhaustion): %v", o.panic)
		}
		return o.err, false
	case <-time.After(10 * time.Second):
		return nil, true
	}
}

func TestNewTerminatesOnRecursiveSchemas(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{
			name:    "mutually recursive definitions through arrays",
			schema:  `{ #A: {bs: [...#B]}, #B: {as: [...#A]}, r: #A }`,
			wantErr: true,
		},
		{
			name:    "self-referential definition through an array",
			schema:  `{ #N: {name: string, kids: [...#N]}, r: #N }`,
			wantErr: false, // terminates today; must keep terminating
		},
		{
			name:    "self-referential definition through a struct",
			schema:  `{ #N: {name: string, c?: #N}, r: #N }`,
			wantErr: false,
		},
		{
			name:    "mutually recursive definitions through structs",
			schema:  `{ #A: {b?: #B}, #B: {a?: #A}, r: #A }`,
			wantErr: false,
		},
		{
			name:    "ordinary nested schema is unaffected",
			schema:  `{ a: {b: {c: {d: int}}}, items: [...{x: int}] }`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err, timedOut := newWithTimeout(t, tt.schema)
			if timedOut {
				t.Fatal("New() did not terminate — schema analysis recursed without bound")
			}
			if tt.wantErr && err == nil {
				t.Error("New() returned nil error; a schema exceeding the depth limit " +
					"must be rejected rather than partially analysed")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("New() error on an acceptable schema: %v", err)
			}
		})
	}
}

// TestMaxSchemaDepthIsConfigurable pins the option: a schema rejected at a low
// limit must be accepted once the limit is raised.
func TestMaxSchemaDepthIsConfigurable(t *testing.T) {
	// 6 levels of nesting.
	schema := `{ l0: { l1: { l2: { l3: { l4: { l5: int } } } } } }`

	if err, timedOut := newWithTimeout(t, schema, WithMaxSchemaDepth(3)); timedOut {
		t.Fatal("New() did not terminate")
	} else if err == nil {
		t.Error("New() accepted a schema deeper than the configured limit")
	} else if !strings.Contains(err.Error(), "depth") {
		t.Errorf("error should mention depth; got: %v", err)
	}

	if err, timedOut := newWithTimeout(t, schema, WithMaxSchemaDepth(32)); timedOut {
		t.Fatal("New() did not terminate")
	} else if err != nil {
		t.Errorf("New() rejected a schema within the configured limit: %v", err)
	}
}

// TestMaxSchemaDepthRejectsInvalidValues keeps the option fail-closed: a
// non-positive limit is a configuration mistake, not "unlimited".
func TestMaxSchemaDepthRejectsInvalidValues(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := New(`{ a: int }`, WithMaxSchemaDepth(n)); err == nil {
			t.Errorf("WithMaxSchemaDepth(%d) accepted; want an error", n)
		}
	}
}

// TestDeepRecursionStillRejectsHiddenAttributes guards the reason the limit
// reports an error instead of silently truncating: an attribute buried below the
// limit must never be quietly dropped.
func TestDeepRecursionStillRejectsHiddenAttributes(t *testing.T) {
	// An @blob inside an array element, nested deeper than the limit allows.
	schema := `{ a: { b: { c: { items: [...{qty: int @blob(this.qty > 0)}] } } } }`

	err, timedOut := newWithTimeout(t, schema, WithMaxSchemaDepth(2))
	if timedOut {
		t.Fatal("New() did not terminate")
	}
	if err == nil {
		t.Error("New() accepted a schema whose deeper levels were not analysed; " +
			"the hidden @blob would be silently ignored")
	}
}

// TestFields_MetaInfo covers P0-e: exposing @meta() through Fields().
//
// Fields() is documented for "generating documentation, API specs, or UI forms",
// but reported nothing from @meta(). A form built from it could not tell that
// `cvv?: string @meta(conditional, required_if=...)` is required for credit
// payments — which is the entire point of that annotation.
func TestFields_MetaInfo(t *testing.T) {
	v := MustNew(`{
		payment_type: "credit" | "debit"
		pan:          =~"^[0-9]{16}$"       @meta(priority=1)
		luhn:         bool   @blob(this.pan.luhn_valid()) @meta(priority=2)
		cvv?:         string @meta(conditional, required_if=this.payment_type == "credit")
		fee?:         number @blob(this.amount * 0.01) @meta(skip_if=this.payment_type == "debit")
		memo?:        string @meta(optional, omit_empty)
	}`)

	byName := make(map[string]FieldInfo)
	for _, f := range v.Fields() {
		byName[f.Name] = f
	}

	t.Run("priority", func(t *testing.T) {
		if got := byName["luhn"].Priority; got != 2 {
			t.Errorf("luhn.Priority = %d, want 2", got)
		}
	})

	t.Run("required_if carries the raw expression", func(t *testing.T) {
		got := byName["cvv"].RequiredIf
		if got == "" {
			t.Fatal("cvv.RequiredIf is empty, want the raw expression")
		}
		if !strings.Contains(got, "payment_type") {
			t.Errorf("cvv.RequiredIf = %q, want it to mention payment_type", got)
		}
	})

	t.Run("skip_if carries the raw expression", func(t *testing.T) {
		got := byName["fee"].SkipIf
		if got == "" {
			t.Fatal("fee.SkipIf is empty, want the raw expression")
		}
		if !strings.Contains(got, "debit") {
			t.Errorf("fee.SkipIf = %q, want it to mention debit", got)
		}
	})

	t.Run("conditional", func(t *testing.T) {
		if !byName["cvv"].Conditional {
			t.Error("cvv.Conditional = false, want true")
		}
		if byName["memo"].Conditional {
			t.Error("memo.Conditional = true; memo declares optional, not conditional")
		}
	})

	t.Run("omit_empty", func(t *testing.T) {
		if !byName["memo"].OmitEmpty {
			t.Error("memo.OmitEmpty = false, want true")
		}
		if byName["cvv"].OmitEmpty {
			t.Error("cvv.OmitEmpty = true, want false")
		}
	})

	t.Run("fields without meta are untouched", func(t *testing.T) {
		f := byName["payment_type"]
		if f.Priority != 0 || f.RequiredIf != "" || f.SkipIf != "" || f.Conditional || f.OmitEmpty {
			t.Errorf("payment_type carries meta it never declared: %+v", f)
		}
	})
}

// TestFields_MetaInfoNested checks the join reaches nested fields, since the two
// data sources are keyed by dot-path and recursion is where that goes wrong.
func TestFields_MetaInfoNested(t *testing.T) {
	v := MustNew(`{
		order: {
			total: number @blob(this.order.total > 0) @meta(priority=3)
			note?: string @meta(optional, omit_empty)
		}
	}`)

	var order FieldInfo
	for _, f := range v.Fields() {
		if f.Name == "order" {
			order = f
		}
	}
	if len(order.Children) == 0 {
		t.Fatal("order has no children")
	}

	byName := make(map[string]FieldInfo)
	for _, c := range order.Children {
		byName[c.Name] = c
	}
	if got := byName["total"].Priority; got != 3 {
		t.Errorf("order.total.Priority = %d, want 3 (path-keyed join must reach children)", got)
	}
	if !byName["note"].OmitEmpty {
		t.Error("order.note.OmitEmpty = false, want true")
	}
}

// TestFields_MetaInfoLimits pins a boundary rather than pretending it away.
//
// A field carrying only @meta(priority=N) — no @blob, no required_if/skip_if, no
// omit control — is never recorded as a rule node (see the meta-only branch in
// extractRules), so Fields() cannot report its priority. That is acceptable,
// because priority orders rule execution and such a field has no rules to
// order; but it must not be mistaken for a join bug.
func TestFields_MetaInfoLimits(t *testing.T) {
	v := MustNew(`{
		lonely: string @meta(priority=7)
	}`)

	var f FieldInfo
	for _, got := range v.Fields() {
		if got.Name == "lonely" {
			f = got
		}
	}
	if f.Name == "" {
		t.Fatal("lonely field missing from Fields()")
	}
	if f.Priority != 0 {
		t.Errorf("lonely.Priority = %d, want 0: a priority-only field has no rule node, "+
			"so the value is unavailable by design", f.Priority)
	}
}

// TestFieldInfoJSONCompatibility locks the serialised shape. The new fields all
// carry omitempty, so a schema that uses no @meta must marshal exactly as it did
// before they existed.
func TestFieldInfoJSONCompatibility(t *testing.T) {
	v := MustNew(`{
		name: string
		tags: [...string]
		addr: {city: string}
	}`)

	got, err := json.Marshal(v.Fields())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `[` +
		`{"name":"name","path":"name","type":"string","optional":false,"has_blob":false},` +
		`{"name":"tags","path":"tags","type":"list","optional":false,"has_blob":false},` +
		`{"name":"addr","path":"addr","type":"struct","optional":false,"has_blob":false,` +
		`"children":[{"name":"city","path":"addr.city","type":"string","optional":false,"has_blob":false}]}` +
		`]`
	if string(got) != want {
		t.Errorf("FieldInfo JSON changed:\n got  %s\n want %s", got, want)
	}
}
