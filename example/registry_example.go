package main

import (
	"fmt"
	"log"

	"github.com/mredencom/schemix"
)

func registryExample() {
	reg := schemix.NewRegistry()

	// Register multiple schemas
	schemas := map[string]string{
		"user": `{
			username: =~"^[a-zA-Z][a-zA-Z0-9_]{2,15}$"
			email:    =~"^.+@.+\\..+$"
			age:      int & >=0 & <=150
		}`,
		"address": `{
			country: =~"^[A-Z]{2}$"
			city:    string
			zip:     =~"^[0-9]{5,6}$"
		}`,
		"product": `{
			sku:   =~"^SKU-[A-Z0-9]{8}$"
			name:  string
			price: number & >0
			stock: int & >=0
		}`,
	}

	for name, src := range schemas {
		if err := reg.Register(name, src); err != nil {
			log.Fatal(err)
		}
	}

	// List: view all registered schemas
	fmt.Printf("  registered: %v (total %d)\n", reg.List(), reg.Len())

	// Has: check existence
	fmt.Printf("  Has(user)=%v, Has(order)=%v\n", reg.Has("user"), reg.Has("order"))

	// Get + use
	v, _ := reg.Get("user")
	r := v.Process(map[string]any{
		"username": "alice_dev", "email": "alice@example.com", "age": int64(28),
	})
	fmt.Printf("  validate user: valid=%v\n", r.Valid)

	// Unregister: remove
	removed := reg.Unregister("product")
	fmt.Printf("  removed product: %v, remaining %d\n", removed, reg.Len())
}
