package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/mredencom/schemix"
)

// Pre-compiled validators for each API endpoint (initialized once at startup).
var (
	createUserSchema = schemix.MustNew(`{
		username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,20}$"
		email:    =~"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
		password: =~"^.{8,64}$"
		age?:     int & >=1 & <=150 @meta(optional,omit_empty)
		role:     "admin" | "user" | "guest"
	}`)

	createOrderSchema = schemix.MustNew(`{
		user_id:    =~"^[a-f0-9]{24}$"
		product_id: =~"^SKU-[A-Z0-9]{8}$"
		quantity:   int & >=1 & <=999
		address: {
			country: =~"^[A-Z]{2}$"
			city:    string
			line1:   string
			zip:     =~"^[0-9A-Z -]{3,10}$"
		}

		// Business rule: express shipping only available for quantity <= 10
		shipping: "standard" | "express"
		shipping_check: bool @blob(
			if this.shipping == "express" { this.quantity <= 10 } else { true }
		)
	}`)

	transferSchema = schemix.MustNew(`{
		from_account: =~"^[A-Z0-9]{10,20}$"
		to_account:   =~"^[A-Z0-9]{10,20}$"
		amount:       int & >=1 & <=10000000
		currency:     "USD" | "EUR" | "CNY"
		memo?:        string @meta(optional,omit_empty)

		// Business rule: cannot transfer to self
		self_check: bool @blob(this.from_account != this.to_account)

		// Computed field: fee based on currency
		fee: number @blob(
			if this.currency == "CNY" { 0 }
			else if this.amount > 100000 { (this.amount * 0.001).ceil() }
			else { (this.amount * 0.005).ceil() }
		)
	}`)
)

func apiValidationExample() {
	fmt.Println("  --- POST /users ---")
	simulateRequest(createUserSchema, map[string]any{
		"username": "alice_dev",
		"email":    "alice@example.com",
		"password": "secure123",
		"role":     "user",
	})
	simulateRequest(createUserSchema, map[string]any{
		"username": "ab",          // too short
		"email":    "not-an-email",
		"password": "short",       // less than 8 chars
		"role":     "superadmin",  // invalid enum
	})

	fmt.Println("\n  --- POST /orders ---")
	simulateRequest(createOrderSchema, map[string]any{
		"user_id":    "507f1f77bcf86cd799439011",
		"product_id": "SKU-AB12CD34",
		"quantity":   int64(2),
		"shipping":   "express",
		"address": map[string]any{
			"country": "US", "city": "Seattle", "line1": "123 Main St", "zip": "98101",
		},
	})
	simulateRequest(createOrderSchema, map[string]any{
		"user_id":    "507f1f77bcf86cd799439011",
		"product_id": "SKU-AB12CD34",
		"quantity":   int64(50),
		"shipping":   "express", // express not allowed for qty > 10
		"address": map[string]any{
			"country": "US", "city": "Seattle", "line1": "123 Main St", "zip": "98101",
		},
	})

	fmt.Println("\n  --- POST /transfers ---")
	simulateRequest(transferSchema, map[string]any{
		"from_account": "ACCT0000000001",
		"to_account":   "ACCT0000000002",
		"amount":       int64(500000),
		"currency":     "USD",
	})
	simulateRequest(transferSchema, map[string]any{
		"from_account": "ACCT0000000001",
		"to_account":   "ACCT0000000001", // self transfer
		"amount":       int64(100),
		"currency":     "CNY",
	})

	fmt.Println("\n  --- HTTP handler pattern ---")
	fmt.Println("  (see apiHandler function for production usage)")
}

// simulateRequest demonstrates request validation and response formatting.
func simulateRequest(schema *schemix.Validator, body map[string]any) {
	r := schema.ProcessWithMode(body, schemix.FailAll)
	if r.Valid {
		fmt.Printf("  ✓ 200 OK | output keys: %v\n", mapKeys(r.Output))
	} else {
		resp := formatErrorResponse(r)
		b, _ := json.Marshal(resp)
		fmt.Printf("  ✗ 400 Bad Request | %s\n", b)
	}
}

// formatErrorResponse converts validation result to a standard API error response.
func formatErrorResponse(r schemix.Result) map[string]any {
	details := make([]map[string]any, 0, len(r.Errors))
	for _, e := range r.Errors {
		details = append(details, map[string]any{
			"field":   e.Path,
			"code":    string(e.Code),
			"message": e.Message,
		})
	}
	return map[string]any{
		"error":   "validation_failed",
		"message": fmt.Sprintf("%d field(s) failed validation", len(r.Errors)),
		"details": details,
	}
}

// apiHandler demonstrates how to use schemix in an actual HTTP handler.
// This is the recommended pattern for production use.
func apiHandler(schema *schemix.Validator) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// 1. Decode request body
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "invalid_json",
				"message": err.Error(),
			})
			return
		}

		// 2. Validate with FailFast for gateway scenarios (or FailAll for forms)
		r := schema.ProcessWithMode(body, schemix.FailFast)
		if !r.Valid {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(formatErrorResponse(r))
			return
		}

		// 3. Use r.Output which contains computed fields (fee, etc.)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   r.Output,
		})
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Prevent "unused import" if not running HTTP server.
var _ = log.Println
var _ http.HandlerFunc
