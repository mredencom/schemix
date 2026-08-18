package schemix_test

import (
	"fmt"

	"github.com/mredencom/schemix"
)

// Result carries every error as a structured value. These accessors cover the
// common ways to interrogate a failed validation.
func ExampleResult_ErrorsByPath() {
	v := schemix.MustNew(`{
		name:  string
		age:   int & >=0 & <=150
		role:  "admin" | "user" | "guest"
	}`)

	r := v.Process(map[string]any{"name": 123, "age": int64(200), "role": "superuser"})

	fmt.Println("valid:            ", r.Valid)
	fmt.Println("total errors:     ", len(r.Errors))
	fmt.Println("errors at age:    ", len(r.ErrorsByPath("age")))
	fmt.Println("has errors at age:", r.HasErrorsAt("age"))
	fmt.Println("has errors at memo:", r.HasErrorsAt("memo"))
	// Output:
	// valid:             false
	// total errors:      3
	// errors at age:     1
	// has errors at age: true
	// has errors at memo: false
}

// HasCode answers "did this category of problem occur" without scanning.
func ExampleResult_HasCode() {
	v := schemix.MustNew(`{
		name: string
		age:  int & >=0 & <=150
	}`)

	r := v.Process(map[string]any{"age": int64(200)}) // name missing, age out of range

	fmt.Println("range violation: ", r.HasCode(schemix.CodeRangeViolation))
	fmt.Println("required missing:", r.HasCode(schemix.CodeRequiredMissing))
	fmt.Println("enum invalid:    ", r.HasCode(schemix.CodeEnumInvalid))
	// Output:
	// range violation:  true
	// required missing: true
	// enum invalid:     false
}

// ErrorsByType filters by the layer that produced the error, which is useful
// when CUE constraint failures and @blob rule failures need different handling.
func ExampleResult_ErrorsByType() {
	v := schemix.MustNew(`{
		amount: int & >0
		memo:   string @blob(this.memo.len_between(min: 5, max: 100))
	}`)

	r := v.ProcessWithMode(map[string]any{"amount": int64(-1), "memo": "hi"}, schemix.FailAll)

	fmt.Println("cue errors:     ", len(r.ErrorsByType(schemix.TypeCUE)))
	fmt.Println("bloblang errors:", len(r.ErrorsByType(schemix.TypeBloblang)))
	// Output:
	// cue errors:      1
	// bloblang errors: 1
}

// FirstError and Err cover the two idiomatic shapes: a structured first error,
// or a standard Go error suitable for returning up the stack.
func ExampleResult_FirstError() {
	v := schemix.MustNew(`{ amount: int & >0 }`)

	r := v.Process(map[string]any{"amount": int64(-5)})

	first := r.FirstError()
	fmt.Println("code:", first.Code)
	fmt.Println("path:", first.Path)
	fmt.Println("is error:", r.Err() != nil)
	// Output:
	// code: E1R01
	// path: amount
	// is error: true
}

// An enum violation names every accepted value and suggests the closest match.
// Suggestion is populated for enums only — a range or regex violation has no
// meaningful value to guess.
func ExampleValidationError_FriendlyMessage() {
	v := schemix.MustNew(`{ currency: "CNY" | "USD" | "EUR" }`)

	r := v.Process(map[string]any{"currency": "USE"})
	e := r.Errors[0]

	fmt.Println("suggestion:", e.Suggestion)
	fmt.Println("friendly:  ", e.FriendlyMessage())
	// Output:
	// suggestion: USD
	// friendly:   currency must be one of ["CNY", "USD", "EUR"] — did you mean "USD"?
}

// WithErrorFormatter replaces Message wholesale, which is how i18n is done.
// The formatter receives the stable error code, the field path, and the default
// detail text.
func ExampleWithErrorFormatter() {
	zh := map[schemix.ErrorCode]string{
		schemix.CodeRequiredMissing: "此字段为必填项",
		schemix.CodeRangeViolation:  "数值超出范围",
		schemix.CodeEnumInvalid:     "值不在允许范围内",
	}

	v := schemix.MustNew(`{
		age:      int & >=0 & <=150
		currency: "CNY" | "USD"
	}`, schemix.WithErrorFormatter(
		func(code schemix.ErrorCode, path, detail string) string {
			if msg, ok := zh[code]; ok {
				return fmt.Sprintf("%s: %s", path, msg)
			}
			return detail
		},
	))

	r := v.ProcessWithMode(map[string]any{"age": int64(200), "currency": "JPY"}, schemix.FailAll)
	for _, e := range r.Errors {
		fmt.Println(e.Message)
	}
	// Output:
	// age: 数值超出范围
	// currency: 值不在允许范围内
}
