package schemix_test

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mredencom/schemix"
)

// Order is a typical request payload with json tags.
type Order struct {
	OrderID  string `json:"order_id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Memo     string `json:"memo,omitempty"`
}

// Payment implements schemix.Processable for full control over the conversion,
// which avoids the JSON round-trip that plain structs go through.
type Payment struct {
	PAN      string
	Amount   int64
	Currency string
}

func (p Payment) ToMap() map[string]any {
	return map[string]any{"pan": p.PAN, "amount": p.Amount, "currency": p.Currency}
}

// Validate skips deepCopy and Output construction entirely, which is the right
// choice when only a pass/fail verdict is needed.
func ExampleValidator_Validate() {
	v := schemix.MustNew(`{
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,20}$"
		age:      int & >=13 & <=150
	}`)

	valid, errs := v.Validate(map[string]any{"username": "alice_dev", "age": int64(28)})
	fmt.Println("valid:", valid, "errors:", len(errs))

	valid, errs = v.Validate(map[string]any{"username": "x", "age": int64(200)})
	fmt.Println("valid:", valid, "errors:", len(errs))
	// Output:
	// valid: true errors: 0
	// valid: false errors: 2
}

// ProcessValue accepts a struct, a pointer, JSON bytes, a Processable, or a
// plain map — anything convertible to map[string]any.
func ExampleValidator_ProcessValue() {
	v := schemix.MustNew(`{
		order_id: =~"^ORD-[0-9]+$"
		amount:   int & >0
		currency: "CNY" | "USD" | "EUR"
		memo?:    string @meta(optional,omit_empty)
	}`)

	// struct
	fmt.Println("struct:  ", v.ProcessValue(Order{OrderID: "ORD-12345", Amount: 9900, Currency: "CNY"}).Valid)

	// pointer to struct
	fmt.Println("*struct: ", v.ProcessValue(&Order{OrderID: "ORD-99999", Amount: 500, Currency: "USD"}).Valid)

	// raw JSON bytes
	fmt.Println("[]byte:  ", v.ProcessValue([]byte(`{"order_id":"ORD-1","amount":100,"currency":"EUR"}`)).Valid)

	// plain map
	fmt.Println("map:     ", v.ProcessValue(map[string]any{
		"order_id": "ORD-55555", "amount": int64(200), "currency": "CNY",
	}).Valid)

	// An unconvertible input fails with a config-layer error rather than panicking.
	r := v.ProcessValue(42)
	fmt.Println("int:     ", r.Valid, r.Errors[0].Code)
	// Output:
	// struct:   true
	// *struct:  true
	// []byte:   true
	// map:      true
	// int:      false E0C01
}

// Implementing Processable avoids the JSON round-trip and gives exact control
// over field names and types.
func ExampleProcessable() {
	v := schemix.MustNew(`{
		pan:      =~"^[0-9]{16}$"
		amount:   int & >0
		currency: "CNY" | "USD"
	}`)

	r := v.ProcessValue(Payment{PAN: "6222021234567890", Amount: 10000, Currency: "CNY"})
	fmt.Println(r.Valid)
	// Output: true
}

// ProcessStruct is the generic form; it reads better at call sites that already
// have a concrete type.
func ExampleProcessStruct() {
	v := schemix.MustNew(`{
		order_id: =~"^ORD-[0-9]+$"
		amount:   int & >0
		currency: "CNY" | "USD" | "EUR"
	}`)

	r := schemix.ProcessStruct(v, Order{OrderID: "ORD-77777", Amount: 3000, Currency: "USD"})
	fmt.Println("valid:", r.Valid)

	r = schemix.ProcessStructWithMode(v,
		Order{OrderID: "BAD", Amount: -1, Currency: "XXX"}, schemix.FailAll)
	fmt.Println("valid:", r.Valid, "errors:", len(r.Errors))

	valid, _ := schemix.ValidateStruct(v, Order{OrderID: "ORD-88888", Amount: 1, Currency: "EUR"})
	fmt.Println("valid:", valid)
	// Output:
	// valid: true
	// valid: false errors: 3
	// valid: true
}

// A realistic HTTP handler: compile the schema once at startup, then validate
// per request with no compilation cost. Error codes map cleanly onto statuses.
func ExampleValidator_ProcessWithMode_httpHandler() {
	userSchema := schemix.MustNew(`{
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,20}$"
		email:    string @blob(this.email.is_email())
		age:      int    @blob(this.age.between(min: 13, max: 150))
	}`)

	handler := func(body map[string]any) (int, string) {
		r := userSchema.ProcessWithMode(body, schemix.FailAll)
		if !r.Valid {
			if r.HasCode(schemix.CodeRequiredMissing) {
				return http.StatusUnprocessableEntity, "missing required fields"
			}
			return http.StatusBadRequest, "validation_failed"
		}
		return http.StatusOK, "ok"
	}

	var good map[string]any
	_ = json.Unmarshal([]byte(`{"username":"alice_dev","email":"a@example.com","age":28}`), &good)
	// json.Unmarshal yields float64 for numbers; ProcessValue would convert for
	// us, but here the map form is used directly so age is normalised by hand.
	good["age"] = int64(28)

	fmt.Println(handler(good))
	fmt.Println(handler(map[string]any{"username": "alice_dev", "email": "nope", "age": int64(28)}))
	// username is a plain CUE constraint, so its absence is a required-field
	// error. Fields carrying @blob do not report E1M01 — their rule simply
	// fails to evaluate, which is why username is the one omitted here.
	fmt.Println(handler(map[string]any{"email": "a@example.com", "age": int64(28)}))
	// Output:
	// 200 ok
	// 400 validation_failed
	// 422 missing required fields
}
