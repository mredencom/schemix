package main

import (
	"fmt"
	"log"

	"github.com/mredencom/schemix"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

func functionExample() {
	reg := schemix.NewRegistry()
	if err := reg.Register("tx", `{
		from:   =~"^[A-Z0-9]{10}$"
		to:     =~"^[A-Z0-9]{10}$"
		amount: int & >0
		valid_transfer: bool @blob(this.from != this.to)
	}`); err != nil {
		log.Fatal(err)
	}

	// Register as Function form — validate_schema(data: ..., name: "xxx")
	if err := reg.RegisterFunctions(); err != nil {
		log.Fatal(err)
	}

	// Example 1: validate against this directly
	mapping1 := `root = validate_schema(data: this, name: "tx")`
	exec1, err := bloblang.Parse(mapping1)
	if err != nil {
		log.Fatal(err)
	}

	r1, _ := exec1.Query(map[string]any{
		"from": "ACCT000001", "to": "ACCT000002", "amount": int64(5000),
	})
	fmt.Printf("  validate_schema(this): %v\n", r1.(map[string]any)["valid"])

	// Example 2: validate nested field — dynamic parameter this.payload
	mapping2 := `root = process_schema(data: this.payload, name: "tx")`
	exec2, err := bloblang.Parse(mapping2)
	if err != nil {
		log.Fatal(err)
	}

	r2, _ := exec2.Query(map[string]any{
		"metadata": map[string]any{"source": "gateway"},
		"payload": map[string]any{
			"from": "ACCT000001", "to": "ACCT000001", "amount": int64(1000),
		},
	})
	result := r2.(map[string]any)
	fmt.Printf("  process_schema(this.payload): valid=%v\n", result["valid"])
	if result["valid"] == false {
		errs := result["errors"].([]any)
		for _, e := range errs {
			em := e.(map[string]any)
			fmt.Printf("    [%s] %s: %s\n", em["code"], em["path"], em["message"])
		}
	}

	// Example 3: Function call with mode parameter
	mapping3 := `root = validate_schema(data: this, name: "tx", mode: "fast")`
	exec3, err := bloblang.Parse(mapping3)
	if err != nil {
		log.Fatal(err)
	}

	r3, _ := exec3.Query(map[string]any{
		"from": "bad", "to": "bad", "amount": int64(-1),
	})
	errs := r3.(map[string]any)["errors"].([]any)
	fmt.Printf("  mode=fast: errors=%d (returns only the first)\n", len(errs))

	// Example 4: RegisterAll — method + function both available
	reg2 := schemix.NewRegistry()
	_ = reg2.Register("simple", `{ name: string, age: int & >=0 }`)
	if err := reg2.RegisterAll(); err != nil {
		log.Fatal(err)
	}

	// method form
	exec4, _ := bloblang.Parse(`root = this.validate_schema(name: "simple")`)
	r4, _ := exec4.Query(map[string]any{"name": "test", "age": int64(18)})
	fmt.Printf("  RegisterAll method: valid=%v\n", r4.(map[string]any)["valid"])

	// function form
	exec5, _ := bloblang.Parse(`root = validate_schema(data: this, name: "simple")`)
	r5, _ := exec5.Query(map[string]any{"name": "test", "age": int64(18)})
	fmt.Printf("  RegisterAll function: valid=%v\n", r5.(map[string]any)["valid"])
}
