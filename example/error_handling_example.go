package main

import (
	"fmt"

	"github.com/mredencom/schemix"
)

func errorHandlingExample() {
	v := schemix.MustNew(`{
		name:   string
		age:    int & >=0 & <=150
		email:  =~"^.+@.+\\..+$"
		role:   "admin" | "user" | "guest"
	}`)

	// Multiple field errors
	r := v.Process(map[string]any{
		"name": 123, "age": int64(200), "email": "invalid", "role": "superuser",
	})

	// Method 1: Err() — standard Go error interface
	if err := r.Err(); err != nil {
		fmt.Printf("  Err(): %s\n", truncate(err.Error(), 80))
	}

	// Method 2: FirstError() — get the first error
	if first := r.FirstError(); first != nil {
		fmt.Printf("  FirstError(): [%s] %s\n", first.Code, first.Path)
	}

	// Method 3: ErrorsByPath() — filter by field
	ageErrors := r.ErrorsByPath("age")
	fmt.Printf("  ErrorsByPath(age): %d error(s)\n", len(ageErrors))

	// Method 4: ErrorMessages() — formatted output
	fmt.Printf("  ErrorMessages():\n")
	for _, line := range splitLines(r.ErrorMessages()) {
		if line != "" {
			fmt.Printf("    %s\n", line)
		}
	}

	// Method 5: iterate directly (most flexible)
	fmt.Printf("  iterating Errors (total %d):\n", len(r.Errors))
	for _, e := range r.Errors {
		fmt.Printf("    [%s|%s] %s → %s\n", e.Code, e.Type, e.Path, truncate(e.Message, 50))
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
