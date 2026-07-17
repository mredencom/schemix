package main

import (
	"fmt"

	"cuelang.org/go/cue/cuecontext"
	"github.com/mredencom/schemix"
)

func compositionExample() {
	ctx := cuecontext.New()

	// Define reusable field constraints as CUE definitions
	// In production, these could come from shared .cue files
	shared := ctx.CompileString(`{
		// Reusable type definitions
		#PAN:      =~"^[0-9]{13,19}$"
		#Amount:   int & >0
		#Currency: "CNY" | "USD" | "EUR" | "GBP" | "JPY"
		#Email:    =~"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
		#Phone:    =~"^\\+?[0-9]{10,15}$"
	}`)
	if shared.Err() != nil {
		fmt.Printf("  compile shared defs error: %v\n", shared.Err())
		return
	}

	// Schema 1: Payment — compose from shared definitions
	paymentSchema := ctx.CompileString(`{
		#PAN:      _
		#Amount:   _
		#Currency: _
		
		pan:      #PAN
		amount:   #Amount
		currency: #Currency
		memo?:    string
	}`)
	paymentFull := shared.Unify(paymentSchema)

	vPayment, err := schemix.NewFromValue(paymentFull)
	if err != nil {
		fmt.Printf("  payment schema error: %v\n", err)
		return
	}

	// Schema 2: User registration — different composition from same base
	userSchema := ctx.CompileString(`{
		#Email: _
		#Phone: _
		
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,20}$"
		email:    #Email
		phone?:   #Phone
		age:      int & >=13 & <=150
	}`)
	userFull := shared.Unify(userSchema)

	vUser, err := schemix.NewFromValue(userFull)
	if err != nil {
		fmt.Printf("  user schema error: %v\n", err)
		return
	}

	// Test payment validation
	fmt.Println("  Payment validation:")
	r := vPayment.Process(map[string]any{
		"pan": "6222021234567890", "amount": int64(5000), "currency": "CNY",
	})
	fmt.Printf("    valid=%v\n", r.Valid)

	r = vPayment.Process(map[string]any{
		"pan": "SHORT", "amount": int64(-1), "currency": "BTC",
	})
	fmt.Printf("    invalid: %d errors\n", len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("      [%s] %s\n", e.Code, e.Path)
	}

	// Test user validation
	fmt.Println("  User validation:")
	r = vUser.Process(map[string]any{
		"username": "alice_dev", "email": "alice@example.com", "age": int64(28),
	})
	fmt.Printf("    valid=%v\n", r.Valid)

	r = vUser.Process(map[string]any{
		"username": "a", "email": "invalid", "age": int64(5),
	})
	fmt.Printf("    invalid: %d errors\n", len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("      [%s] %s\n", e.Code, e.Path)
	}

	// Show that definitions are properly shared
	fmt.Println("  Schema introspection (payment):")
	for _, f := range vPayment.Fields() {
		if !f.HasBlob {
			fmt.Printf("    %s: %s (optional=%v)\n", f.Name, f.Type, f.Optional)
		}
	}
}
