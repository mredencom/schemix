package comparison

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	validator "github.com/go-playground/validator/v10"
	"github.com/mredencom/schemix"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// This file holds the whole cross-library comparison suite, in reading order:
//
//	1. Fixtures        — semantically equivalent rule sets for every engine
//	2. Equivalence     — the correctness gate that must pass before publishing
//	3. Cross-library   — scalar / nested / JSON / compile / parallel benchmarks
//	4. Breakdown       — where schemix's own time goes (container shape, arrays)

// ==========================================================================
// ─── 1. Fixtures ───
// ==========================================================================

// emailPattern is shared verbatim by schemix (CUE regex), JSON Schema
// (pattern) and the raw CUE baseline so the regex engines do equal work.
// The doubled backslash is the escaping both CUE and JSON strings require; the
// regex the engines actually compile is `\.`.
const emailPattern = `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$`

// ─── Input shapes ────────────────────────────────────────────────────────────

// User carries go-playground/validator struct tags.
type User struct {
	PAN      string `json:"pan"      validate:"required,len=16,numeric"`
	Amount   int64  `json:"amount"   validate:"required,gt=0"`
	Currency string `json:"currency" validate:"required,oneof=156 840"`
	Age      int    `json:"age"      validate:"gte=0,lte=150"`
	Email    string `json:"email"    validate:"required,email"`
}

var userValid = User{
	PAN:      "6222021234567890",
	Amount:   10000,
	Currency: "156",
	Age:      30,
	Email:    "alice@example.com",
}

var userInvalid = User{
	PAN:      "ABC",
	Amount:   -1,
	Currency: "999",
	Age:      999,
	Email:    "not-an-email",
}

// mapValid mirrors userValid for the map-driven engines.
var mapValid = map[string]any{
	"pan":      "6222021234567890",
	"amount":   int64(10000),
	"currency": "156",
	"age":      int64(30),
	"email":    "alice@example.com",
}

var mapInvalid = map[string]any{
	"pan":      "ABC",
	"amount":   int64(-1),
	"currency": "999",
	"age":      int64(999),
	"email":    "not-an-email",
}

// jsonValid is the same payload as raw bytes, for the end-to-end
// "JSON in, verdict out" scenario every HTTP handler actually faces.
var jsonValid = []byte(`{"pan":"6222021234567890","amount":10000,"currency":"156","age":30,"email":"alice@example.com"}`)

// ─── schemix ─────────────────────────────────────────────────────────────────

// schemixCUEOnly expresses all five rules as CUE constraints, which lets every
// field take the Go-native fast path (no Bloblang involved).
const schemixCUEOnlySrc = `{
	pan:      =~"^[0-9]{16}$"
	amount:   int & >0
	currency: "156" | "840"
	age:      int & >=0 & <=150
	email:    =~"` + emailPattern + `"
}`

// schemixBlobSrc swaps the email regex for the built-in is_email() validator,
// isolating the cost of one Bloblang rule against the CUE-only variant.
const schemixBlobSrc = `{
	pan:      =~"^[0-9]{16}$"
	amount:   int & >0
	currency: "156" | "840"
	age:      int & >=0 & <=150
	email:    string @blob(this.email.is_email())
}`

var (
	schemixCUEOnly = schemix.MustNew(schemixCUEOnlySrc)
	schemixBlob    = schemix.MustNew(schemixBlobSrc)
)

// ─── go-playground/validator ─────────────────────────────────────────────────

var orderIDRe = regexp.MustCompile(`^ORD-[0-9]+$`)

var goPlayground = func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// validator has no built-in regex tag, so the order_id rule is registered
	// as a custom validation — its documented extension point.
	if err := v.RegisterValidation("orderid", func(fl validator.FieldLevel) bool {
		return orderIDRe.MatchString(fl.Field().String())
	}); err != nil {
		panic(err)
	}
	// Warm the struct cache so the benchmark measures steady state, matching
	// how schemix and JSON Schema are pre-compiled.
	_ = v.Struct(userValid)
	_ = v.Struct(orderValid)
	return v
}()

// ─── ozzo-validation ─────────────────────────────────────────────────────────

var panRe = regexp.MustCompile(`^[0-9]{16}$`)

// validateOzzo applies the equivalent rule set. ozzo builds its rule list per
// call by design, so that construction is part of its honest cost.
func validateOzzo(u *User) error {
	return validation.ValidateStruct(u,
		validation.Field(&u.PAN, validation.Required, validation.Match(panRe)),
		validation.Field(&u.Amount, validation.Required, validation.Min(int64(1))),
		validation.Field(&u.Currency, validation.Required, validation.In("156", "840")),
		validation.Field(&u.Age, validation.Min(0), validation.Max(150)),
		validation.Field(&u.Email, validation.Required, is.EmailFormat),
	)
}

// ─── JSON Schema (santhosh-tekuri/jsonschema v6) ─────────────────────────────

const jsonSchemaSrc = `{
	"type": "object",
	"required": ["pan", "amount", "currency", "email"],
	"properties": {
		"pan":      {"type": "string", "pattern": "^[0-9]{16}$"},
		"amount":   {"type": "integer", "exclusiveMinimum": 0},
		"currency": {"enum": ["156", "840"]},
		"age":      {"type": "integer", "minimum": 0, "maximum": 150},
		"email":    {"type": "string", "pattern": "` + emailPattern + `"}
	}
}`

// compileJSONSchema builds a *jsonschema.Schema from src.
func compileJSONSchema(src string) *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(src))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", doc); err != nil {
		panic(err)
	}
	return c.MustCompile("schema.json")
}

var jsonSchema = compileJSONSchema(jsonSchemaSrc)

// mustUnmarshalJSON decodes an instance the way jsonschema expects
// (json.Number preserved).
func mustUnmarshalJSON(b []byte) any {
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
	if err != nil {
		panic(err)
	}
	return inst
}

var (
	jsonSchemaInstValid   = mustUnmarshalJSON(jsonValid)
	jsonSchemaInstInvalid = mustUnmarshalJSON([]byte(`{"pan":"ABC","amount":-1,"currency":"999","age":999,"email":"not-an-email"}`))
)

// ─── Nested + array scenario ─────────────────────────────────────────────────

// Nested rules, again identical across engines:
//
//	order_id        matches ^ORD-[0-9]+$
//	customer.name   3..50 characters
//	customer.email  email format
//	items[].product 3..50 characters
//	items[].price   number greater than 0
//	items[].qty     integer of at least 1
const schemixNestedSrc = `{
	order_id: =~"^ORD-[0-9]+$"
	customer: {
		name:  =~"^.{3,50}$"
		email: =~"` + emailPattern + `"
	}
	items: [...{
		product: =~"^.{3,50}$"
		price:   number & >0
		qty:     int & >=1
	}]
}`

var schemixNested = schemix.MustNew(schemixNestedSrc)

type Customer struct {
	Name  string `json:"name"  validate:"min=3,max=50"`
	Email string `json:"email" validate:"required,email"`
}

type Item struct {
	Product string  `json:"product" validate:"min=3,max=50"`
	Price   float64 `json:"price"   validate:"gt=0"`
	Qty     int     `json:"qty"     validate:"gte=1"`
}

type Order struct {
	OrderID  string   `json:"order_id" validate:"required,orderid"`
	Customer Customer `json:"customer"`
	Items    []Item   `json:"items"    validate:"dive"`
}

var orderValid = Order{
	OrderID:  "ORD-12345",
	Customer: Customer{Name: "Alice", Email: "alice@example.com"},
	Items: []Item{
		{Product: "Laptop", Price: 5999, Qty: 1},
		{Product: "Mouse", Price: 99, Qty: 2},
		{Product: "Keyboard", Price: 299, Qty: 1},
	},
}

var nestedMapValid = map[string]any{
	"order_id": "ORD-12345",
	"customer": map[string]any{"name": "Alice", "email": "alice@example.com"},
	"items": []any{
		map[string]any{"product": "Laptop", "price": 5999.0, "qty": int64(1)},
		map[string]any{"product": "Mouse", "price": 99.0, "qty": int64(2)},
		map[string]any{"product": "Keyboard", "price": 299.0, "qty": int64(1)},
	},
}

const jsonSchemaNestedSrc = `{
	"type": "object",
	"required": ["order_id", "customer", "items"],
	"properties": {
		"order_id": {"type": "string", "pattern": "^ORD-[0-9]+$"},
		"customer": {
			"type": "object",
			"required": ["name", "email"],
			"properties": {
				"name":  {"type": "string", "minLength": 3, "maxLength": 50},
				"email": {"type": "string", "pattern": "` + emailPattern + `"}
			}
		},
		"items": {
			"type": "array",
			"items": {
				"type": "object",
				"required": ["product", "price", "qty"],
				"properties": {
					"product": {"type": "string", "minLength": 3, "maxLength": 50},
					"price":   {"type": "number", "exclusiveMinimum": 0},
					"qty":     {"type": "integer", "minimum": 1}
				}
			}
		}
	}
}`

var (
	jsonSchemaNested = compileJSONSchema(jsonSchemaNestedSrc)
	nestedInstValid  = mustUnmarshalJSON([]byte(`{"order_id":"ORD-12345","customer":{"name":"Alice","email":"alice@example.com"},"items":[{"product":"Laptop","price":5999,"qty":1},{"product":"Mouse","price":99,"qty":2},{"product":"Keyboard","price":299,"qty":1}]}`))
)

// ─── Raw CUE baseline ────────────────────────────────────────────────────────

// rawCUE is the same constraint set driven straight through the CUE API, which
// is what schemix's fast path is measured against.
var (
	rawCUECtx    = cuecontext.New()
	rawCUESchema = rawCUECtx.CompileString(schemixCUEOnlySrc)
)

func validateRawCUE(data map[string]any) error {
	v := rawCUECtx.Encode(data)
	return rawCUESchema.Unify(v).Validate(cue.Concrete(true))
}

// ==========================================================================
// ─── 2. Equivalence gate ───
// ==========================================================================

// TestRuleSetsAreEquivalent guards the benchmark's honesty: a performance
// comparison is meaningless unless every engine reaches the same verdict on the
// same data. If this test fails, the numbers must not be published.
func TestRuleSetsAreEquivalent(t *testing.T) {
	t.Run("valid payload accepted by every engine", func(t *testing.T) {
		if ok, errs := schemixCUEOnly.Validate(mapValid); !ok {
			t.Errorf("schemix (CUE-only) rejected valid payload: %v", errs)
		}
		if ok, errs := schemixBlob.Validate(mapValid); !ok {
			t.Errorf("schemix (@blob) rejected valid payload: %v", errs)
		}
		if err := goPlayground.Struct(userValid); err != nil {
			t.Errorf("go-playground/validator rejected valid payload: %v", err)
		}
		if err := validateOzzo(&userValid); err != nil {
			t.Errorf("ozzo-validation rejected valid payload: %v", err)
		}
		if err := jsonSchema.Validate(jsonSchemaInstValid); err != nil {
			t.Errorf("jsonschema rejected valid payload: %v", err)
		}
		if err := validateRawCUE(mapValid); err != nil {
			t.Errorf("raw CUE rejected valid payload: %v", err)
		}
	})

	t.Run("invalid payload rejected by every engine", func(t *testing.T) {
		if ok, _ := schemixCUEOnly.Validate(mapInvalid); ok {
			t.Error("schemix (CUE-only) accepted invalid payload")
		}
		if ok, _ := schemixBlob.Validate(mapInvalid); ok {
			t.Error("schemix (@blob) accepted invalid payload")
		}
		if err := goPlayground.Struct(userInvalid); err == nil {
			t.Error("go-playground/validator accepted invalid payload")
		}
		if err := validateOzzo(&userInvalid); err == nil {
			t.Error("ozzo-validation accepted invalid payload")
		}
		if err := jsonSchema.Validate(jsonSchemaInstInvalid); err == nil {
			t.Error("jsonschema accepted invalid payload")
		}
		if err := validateRawCUE(mapInvalid); err == nil {
			t.Error("raw CUE accepted invalid payload")
		}
	})

	t.Run("nested valid payload accepted", func(t *testing.T) {
		if ok, errs := schemixNested.Validate(nestedMapValid); !ok {
			t.Errorf("schemix rejected valid nested payload: %v", errs)
		}
		if err := goPlayground.Struct(orderValid); err != nil {
			t.Errorf("go-playground/validator rejected valid nested payload: %v", err)
		}
		if err := jsonSchemaNested.Validate(nestedInstValid); err != nil {
			t.Errorf("jsonschema rejected valid nested payload: %v", err)
		}
	})
}

// ==========================================================================
// ─── 3. Cross-library benchmarks ───
// ==========================================================================

// ─── Scenario A: five scalar fields, valid payload ───────────────────────────

func BenchmarkScalarValid(b *testing.B) {
	b.Run("schemix_cue", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			schemixCUEOnly.Validate(mapValid)
		}
	})

	b.Run("schemix_blob", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			schemixBlob.Validate(mapValid)
		}
	})

	b.Run("go_playground_validator", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = goPlayground.Struct(userValid)
		}
	})

	b.Run("ozzo_validation", func(b *testing.B) {
		u := userValid
		b.ReportAllocs()
		for b.Loop() {
			_ = validateOzzo(&u)
		}
	})

	b.Run("jsonschema", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = jsonSchema.Validate(jsonSchemaInstValid)
		}
	})

	b.Run("raw_cue", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = validateRawCUE(mapValid)
		}
	})
}

// ─── Scenario A': same fields, invalid payload (error path) ───────────────────

func BenchmarkScalarInvalid(b *testing.B) {
	b.Run("schemix_cue", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			schemixCUEOnly.Validate(mapInvalid)
		}
	})

	b.Run("schemix_cue_failfast", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			schemixCUEOnly.ProcessWithMode(mapInvalid, schemix.FailFast)
		}
	})

	b.Run("go_playground_validator", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = goPlayground.Struct(userInvalid)
		}
	})

	b.Run("ozzo_validation", func(b *testing.B) {
		u := userInvalid
		b.ReportAllocs()
		for b.Loop() {
			_ = validateOzzo(&u)
		}
	})

	b.Run("jsonschema", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = jsonSchema.Validate(jsonSchemaInstInvalid)
		}
	})

	b.Run("raw_cue", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = validateRawCUE(mapInvalid)
		}
	})
}

// ─── Scenario B: nested struct + array of 3 items ─────────────────────────────

func BenchmarkNestedValid(b *testing.B) {
	b.Run("schemix", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			schemixNested.Validate(nestedMapValid)
		}
	})

	b.Run("go_playground_validator", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = goPlayground.Struct(orderValid)
		}
	})

	b.Run("jsonschema", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = jsonSchemaNested.Validate(nestedInstValid)
		}
	})
}

// ─── Scenario C: end-to-end JSON bytes in, verdict out ───────────────────────
//
// This is the shape an HTTP handler actually deals with, so decoding cost is
// charged to every engine.

func BenchmarkJSONEndToEnd(b *testing.B) {
	b.Run("schemix", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			schemixCUEOnly.ValidateValue(jsonValid)
		}
	})

	b.Run("go_playground_validator", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var u User
			if err := json.Unmarshal(jsonValid, &u); err != nil {
				b.Fatal(err)
			}
			_ = goPlayground.Struct(u)
		}
	})

	b.Run("ozzo_validation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var u User
			if err := json.Unmarshal(jsonValid, &u); err != nil {
				b.Fatal(err)
			}
			_ = validateOzzo(&u)
		}
	})

	b.Run("jsonschema", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(jsonValid)))
			if err != nil {
				b.Fatal(err)
			}
			_ = jsonSchema.Validate(inst)
		}
	})
}

// ─── Scenario D: schema construction / compile cost ──────────────────────────
//
// Paid once at startup for every engine here.

func BenchmarkCompile(b *testing.B) {
	b.Run("schemix", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := schemix.New(schemixCUEOnlySrc); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("go_playground_validator", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v := validator.New(validator.WithRequiredStructEnabled())
			_ = v.Struct(userValid) // first call builds the struct cache
		}
	})

	b.Run("jsonschema", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			compileJSONSchema(jsonSchemaSrc)
		}
	})
}

// ─── Scenario E: concurrency ─────────────────────────────────────────────────

func BenchmarkScalarValidParallel(b *testing.B) {
	b.Run("schemix_cue", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				schemixCUEOnly.Validate(mapValid)
			}
		})
	})

	b.Run("go_playground_validator", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = goPlayground.Struct(userValid)
			}
		})
	})

	b.Run("jsonschema", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = jsonSchema.Validate(jsonSchemaInstValid)
			}
		})
	})
}

// ==========================================================================
// ─── 4. schemix breakdown ───
// ==========================================================================

// This file isolates ONE variable: whether the three scalar constraints sit
// inside a nested struct or inside an array element. Everything else — the
// constraint set, the values, the field count per element — is identical.
//
// It exists to quantify exactly what the fast path does and does not cover:
// compileCUEFields recurses into struct fields and assigns each scalar child
// its own fastConstraint, but a list field gets no descriptor at all
// (extractFastConstraint returns nil for cue.ListKind), so the whole list —
// every element, every scalar inside it — goes through cue.Value.Unify.

// elementRules is the shared 3-constraint payload.
const elementRules = `{
		name: =~"^.{3,50}$"
		qty:  int & >0
		tag:  "A" | "B"
	}`

// scalarOnly is the baseline: the same three constraints at top level, where
// every field is served by the fast path.
var scalarOnly = schemix.MustNew(`{
	name: =~"^.{3,50}$"
	qty:  int & >0
	tag:  "A" | "B"
}`)

var scalarOnlyData = map[string]any{
	"name": "Laptop", "qty": int64(2), "tag": "A",
}

// structNested wraps the same three constraints in a nested struct. The fast
// path still applies to each scalar child via recursion.
var structNested = schemix.MustNew(`{
	item: ` + elementRules + `
}`)

var structNestedData = map[string]any{
	"item": map[string]any{"name": "Laptop", "qty": int64(2), "tag": "A"},
}

// arraySchema wraps the identical three constraints in a list. No fast path.
var arraySchema = schemix.MustNew(`{
	items: [...` + elementRules + `]
}`)

// makeArrayData builds a payload of n structurally identical elements.
func makeArrayData(n int) map[string]any {
	items := make([]any, n)
	for i := range items {
		items[i] = map[string]any{
			"name": "Item" + strconv.Itoa(i), "qty": int64(i + 1), "tag": "A",
		}
	}
	return map[string]any{"items": items}
}

// structChainSchema nests n struct levels, each carrying one scalar, to show
// that struct depth stays on the fast path while array width does not.
func structChainSchema(depth int) *schemix.Validator {
	var sb strings.Builder
	for i := 0; i < depth; i++ {
		sb.WriteString("lvl" + strconv.Itoa(i) + ": {")
	}
	sb.WriteString("qty: int & >0")
	sb.WriteString(strings.Repeat("}", depth))
	return schemix.MustNew("{" + sb.String() + "}")
}

func structChainData(depth int) map[string]any {
	inner := map[string]any{"qty": int64(1)}
	for i := depth - 1; i >= 0; i-- {
		inner = map[string]any{"lvl" + strconv.Itoa(i): inner}
	}
	return inner
}

// BenchmarkContainerShape compares identical constraints in three containers.
func BenchmarkContainerShape(b *testing.B) {
	b.Run("scalar_top_level", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			scalarOnly.Validate(scalarOnlyData)
		}
	})

	b.Run("inside_nested_struct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			structNested.Validate(structNestedData)
		}
	})

	b.Run("inside_array_1_element", func(b *testing.B) {
		data := makeArrayData(1)
		b.ReportAllocs()
		for b.Loop() {
			arraySchema.Validate(data)
		}
	})
}

// BenchmarkArrayScaling measures how array validation cost grows with element
// count — the shape of the fast path's blind spot.
func BenchmarkArrayScaling(b *testing.B) {
	for _, n := range []int{1, 3, 10, 50, 100} {
		data := makeArrayData(n)
		b.Run("elements_"+strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				arraySchema.Validate(data)
			}
		})
	}
}

// BenchmarkStructDepthScaling is the control: struct nesting depth grows the
// same way array width does, but stays on the fast path.
func BenchmarkStructDepthScaling(b *testing.B) {
	for _, d := range []int{1, 3, 10} {
		v := structChainSchema(d)
		data := structChainData(d)
		b.Run("depth_"+strconv.Itoa(d), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v.Validate(data)
			}
		})
	}
}

// elementValidator validates a single element on its own, so all three scalar
// constraints take the fast path.
var elementValidator = schemix.MustNew(elementRules)

// BenchmarkArrayWorkaround checks the mitigation advertised in the README:
// validating a collection element-by-element against a per-element Validator
// must actually beat handing the whole list to CUE. If this ever regresses,
// the README advice is wrong and must be removed.
func BenchmarkArrayWorkaround(b *testing.B) {
	for _, n := range []int{3, 10, 50} {
		data := makeArrayData(n)
		items := data["items"].([]any)

		b.Run("whole_list_"+strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				arraySchema.Validate(data)
			}
		})

		b.Run("per_element_"+strconv.Itoa(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for _, it := range items {
					elementValidator.Validate(it.(map[string]any))
				}
			}
		})
	}
}

// TestArrayWorkaroundIsEquivalent guards that the per-element mitigation
// reaches the same verdict as whole-list validation, so the README advice is
// not merely faster but also correct.
func TestArrayWorkaroundIsEquivalent(t *testing.T) {
	good := makeArrayData(3)
	if ok, errs := arraySchema.Validate(good); !ok {
		t.Fatalf("whole-list rejected valid data: %v", errs)
	}
	for _, it := range good["items"].([]any) {
		if ok, errs := elementValidator.Validate(it.(map[string]any)); !ok {
			t.Fatalf("per-element rejected valid element: %v", errs)
		}
	}

	bad := makeArrayData(3)
	bad["items"].([]any)[1].(map[string]any)["qty"] = int64(0) // violates >0

	if ok, _ := arraySchema.Validate(bad); ok {
		t.Error("whole-list accepted invalid element")
	}
	anyRejected := false
	for _, it := range bad["items"].([]any) {
		if ok, _ := elementValidator.Validate(it.(map[string]any)); !ok {
			anyRejected = true
		}
	}
	if !anyRejected {
		t.Error("per-element accepted invalid element")
	}
}
