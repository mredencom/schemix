package main

import (
	"fmt"
	"log"

	"github.com/mredencom/schemix"
)

func nestedExample() {
	v, err := schemix.New(`
	{
		order_id: =~"^ORD-[0-9]+$"
		customer: {
			// Built-in methods: not_blank + len_between for name validation
			name: string @blob(this.customer.name.not_blank() && this.customer.name.len_between(min: 2, max: 50))
			// Built-in method: is_email replaces manual regex
			email: string @blob(this.customer.email.is_email())
		}
		items: [...{
			product: string
			price: number & >0
			qty: int & >=1
		}]
		total: number @blob(this.items.map_each(this.price * this.qty).sum())
	}
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Valid nested data
	r := v.Process(map[string]any{
		"order_id": "ORD-001",
		"customer": map[string]any{"name": "Alice", "email": "alice@test.com"},
		"items": []any{
			map[string]any{"product": "Laptop", "price": 5999.0, "qty": int64(1)},
			map[string]any{"product": "Mouse", "price": 99.0, "qty": int64(2)},
		},
	})
	fmt.Printf("  valid=%v, total=%v\n", r.Valid, r.Output["total"])

	// Invalid: bad email + invalid array elements
	r = v.Process(map[string]any{
		"order_id": "ORD-002",
		"customer": map[string]any{"name": "Bob", "email": "not-an-email"},
		"items": []any{
			map[string]any{"product": "Phone", "price": -100.0, "qty": int64(0)},
		},
	})
	fmt.Printf("  invalid: valid=%v, errors=%d\n", r.Valid, len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("    [%s] %s: %s\n", e.Code, e.Path, truncate(e.Message, 60))
	}
}
