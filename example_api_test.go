package schemix_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

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

// Process accepts a struct, a pointer, JSON bytes, a Processable, or a plain
// map — anything convertible to map[string]any. Anything else fails with E0C01
// naming the type it received.
func ExampleValidator_Process_flexibleInput() {
	v := schemix.MustNew(`{
		order_id: =~"^ORD-[0-9]+$"
		amount:   int & >0
		currency: "CNY" | "USD" | "EUR"
		memo?:    string @meta(optional,omit_empty)
	}`)

	// struct
	fmt.Println("struct:  ", v.Process(Order{OrderID: "ORD-12345", Amount: 9900, Currency: "CNY"}).Valid)

	// pointer to struct
	fmt.Println("*struct: ", v.Process(&Order{OrderID: "ORD-99999", Amount: 500, Currency: "USD"}).Valid)

	// raw JSON bytes
	fmt.Println("[]byte:  ", v.Process([]byte(`{"order_id":"ORD-1","amount":100,"currency":"EUR"}`)).Valid)

	// plain map
	fmt.Println("map:     ", v.Process(map[string]any{
		"order_id": "ORD-55555", "amount": int64(200), "currency": "CNY",
	}).Valid)

	// An unconvertible input fails with a config-layer error rather than panicking.
	r := v.Process(42)
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

	r := v.Process(Payment{PAN: "6222021234567890", Amount: 10000, Currency: "CNY"})
	fmt.Println(r.Valid)
	// Output: true
}

// A realistic HTTP handler, mirroring the README's API Validation section so
// that section cannot drift from working code.
//
// Three choices in here are deliberate:
//
//   - Constraints CUE can state are written in CUE. They keep their fast-path
//     descriptor, and a violation names the bound it broke (E1R01, "must be
//     <=150") instead of the shrug @blob gives (E2B01, "does not satisfy a
//     validation rule").
//   - Process receives the raw bytes. Decoding into map[string]any first turns
//     every JSON number into a float64, which CUE's `int` rejects.
//   - A body that is not JSON fails at the conversion layer with E0C01, which is
//     what separates a 400 from a 422.
func ExampleValidator_Process_httpHandler() {
	userSchema := schemix.MustNew(`{
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,20}$"
		age:      int & >=13 & <=150
		role:     "admin" | "user" | "guest"

		email:    string @blob(this.email.is_email())
		password: string @blob(this.password.str_len(min: 8, max: 64))
	}`)

	// catalogFor stands in for whatever maps a request to a language. Returning
	// the interface rather than *Catalog is what lets an application swap in its
	// own i18n pipeline later without touching the handler.
	catalogFor := func(header string) schemix.Localizer {
		if strings.HasPrefix(header, "zh") {
			return schemix.ZhCN
		}
		return schemix.EnUS
	}

	respond := func(w http.ResponseWriter, status int, body any) {
		// Before WriteHeader — afterwards the header is silently discarded.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	handler := func(w http.ResponseWriter, req *http.Request) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]any{"error": "unreadable_body"})
			return
		}

		r := userSchema.Process(raw)
		if !r.Valid {
			if r.HasCode(schemix.CodeConfigError) {
				respond(w, http.StatusBadRequest, map[string]any{"error": "malformed_json"})
				return
			}
			// Localize per request rather than per validator, so one compiled
			// schema serves every language. e.Message holds raw CUE/Bloblang
			// wording — log it, don't return it.
			loc := catalogFor(req.Header.Get("Accept-Language"))
			details := make([]map[string]string, len(r.Errors))
			for i, e := range r.Errors {
				details[i] = map[string]string{
					"field":   e.Path,
					"code":    string(e.Code),
					"message": loc.Localize(e),
				}
			}
			respond(w, http.StatusUnprocessableEntity, map[string]any{
				"error":   "validation_failed",
				"details": details,
			})
			return
		}
		respond(w, http.StatusCreated, map[string]any{"username": r.Output["username"]})
	}

	post := func(label, lang, payload string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(payload))
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		handler(rec, req)
		fmt.Printf("%-11s %d %s | %s", label,
			rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	const badAge = `{"username":"alice_dev","age":200,"role":"user",
		"email":"alice@example.com","password":"correct-horse"}`

	post("valid:", "", `{"username":"alice_dev","age":28,"role":"user",
		"email":"alice@example.com","password":"correct-horse"}`)
	post("age:", "", badAge)
	post("age zh:", "zh-CN", badAge)
	post("not json:", "", `<html>`)

	// Output:
	// valid:      201 application/json | {"username":"alice_dev"}
	// age:        422 application/json | {"details":[{"code":"E1R01","field":"age","message":"age must be \u003c=150"}],"error":"validation_failed"}
	// age zh:     422 application/json | {"details":[{"code":"E1R01","field":"age","message":"age必须满足 \u003c=150"}],"error":"validation_failed"}
	// not json:   400 application/json | {"error":"malformed_json"}
}

// Counting bytes where runes were meant is the kind of mistake a validator
// should not make quietly: three CJK characters occupy nine bytes, so a
// byte-based minimum of eight lets them through.
func ExampleValidator_Process_runeLength() {
	byteLen := schemix.MustNew(`{ password: string @blob(this.password.len_between(min: 8, max: 64)) }`)
	runeLen := schemix.MustNew(`{ password: string @blob(this.password.str_len(min: 8, max: 64)) }`)

	short := map[string]any{"password": "密码密"} // 3 runes, 9 bytes

	fmt.Println("len_between:", byteLen.Process(short).Valid)
	fmt.Println("str_len:    ", runeLen.Process(short).Valid)
	// Output:
	// len_between: true
	// str_len:     false
}
