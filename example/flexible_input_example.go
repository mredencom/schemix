package main

import (
	"fmt"

	"github.com/mredencom/schemix"
)

// ─── Flexible Input API Example ──────────────────────────────────────────────
// Demonstrates ProcessValue, ValidateValue, and generic ProcessStruct/ValidateStruct.
// These APIs accept struct, *struct, []byte (JSON), Processable, or map[string]any.

// Order is a typical request struct with json tags.
type Order struct {
	OrderID  string `json:"order_id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Memo     string `json:"memo,omitempty"`
}

// Payment implements schemix.Processable for full control over map conversion.
type Payment struct {
	PAN      string
	Amount   int64
	Currency string
}

func (p Payment) ToMap() map[string]any {
	return map[string]any{
		"pan":      p.PAN,
		"amount":   p.Amount,
		"currency": p.Currency,
	}
}

var flexSchema = schemix.MustNew(`{
	order_id: =~"^ORD-[0-9]+$"
	amount:   int & >0
	currency: "CNY" | "USD" | "EUR"
	memo?:    string @meta(optional,omit_empty)
}`)

var paymentSchema = schemix.MustNew(`{
	pan:      =~"^[0-9]{16}$"
	amount:   int & >0
	currency: "CNY" | "USD"
}`)

func flexibleInputExample() {
	fmt.Println("  ── ProcessValue: accept any supported type ──")

	// 1. struct (json round-trip, int64 preserved)
	r := flexSchema.ProcessValue(Order{
		OrderID: "ORD-12345", Amount: 9900, Currency: "CNY",
	})
	fmt.Printf("    struct:      valid=%v\n", r.Valid)

	// 2. *struct (pointer)
	r = flexSchema.ProcessValue(&Order{
		OrderID: "ORD-99999", Amount: 500, Currency: "USD", Memo: "rush",
	})
	fmt.Printf("    *struct:     valid=%v, memo=%v\n", r.Valid, r.Output["memo"])

	// 3. []byte (JSON)
	jsonData := []byte(`{"order_id":"ORD-00001","amount":100,"currency":"EUR"}`)
	r = flexSchema.ProcessValue(jsonData)
	fmt.Printf("    []byte JSON: valid=%v\n", r.Valid)

	// 4. Processable interface
	r = paymentSchema.ProcessValue(Payment{PAN: "6222021234567890", Amount: 10000, Currency: "CNY"})
	fmt.Printf("    Processable: valid=%v\n", r.Valid)

	// 5. map[string]any (still works, zero overhead)
	r = flexSchema.ProcessValue(map[string]any{
		"order_id": "ORD-55555", "amount": int64(200), "currency": "CNY",
	})
	fmt.Printf("    map:         valid=%v\n", r.Valid)

	// 6. Invalid type → graceful error
	r = flexSchema.ProcessValue(42)
	fmt.Printf("    invalid:     valid=%v, error=%s\n", r.Valid, r.Errors[0].Code)

	// ── ValidateValue: fast path (no Output) ──
	fmt.Println("\n  ── ValidateValue: validation only ──")
	valid, errs := flexSchema.ValidateValue(Order{OrderID: "ORD-1", Amount: 50, Currency: "CNY"})
	fmt.Printf("    valid=%v, errors=%d\n", valid, len(errs))

	// ── ProcessStruct: compile-time type safety via generics ──
	fmt.Println("\n  ── ProcessStruct[T]: generic convenience ──")
	r = schemix.ProcessStruct(flexSchema, Order{OrderID: "ORD-77777", Amount: 3000, Currency: "USD"})
	fmt.Printf("    ProcessStruct:  valid=%v\n", r.Valid)

	r = schemix.ProcessStructWithMode(flexSchema, Order{OrderID: "BAD", Amount: -1, Currency: "XXX"}, schemix.FailAll)
	fmt.Printf("    invalid struct: valid=%v, errors=%d\n", r.Valid, len(r.Errors))

	// ── ValidateStruct: generic + fast path ──
	valid, _ = schemix.ValidateStruct(flexSchema, Order{OrderID: "ORD-88888", Amount: 1, Currency: "EUR"})
	fmt.Printf("    ValidateStruct: valid=%v\n", valid)

	// ── ProcessValueWithMode ──
	fmt.Println("\n  ── ProcessValueWithMode: FailFast with struct ──")
	r = flexSchema.ProcessValueWithMode(Order{OrderID: "X", Amount: -1, Currency: "BAD"}, schemix.FailFast)
	fmt.Printf("    FailFast: errors=%d (stops at first)\n", len(r.Errors))
}
