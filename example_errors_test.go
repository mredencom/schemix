package schemix_test

import (
	"fmt"
	"strings"

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

	r := v.Process(map[string]any{"amount": int64(-1), "memo": "hi"})

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
func ExampleLocalizer_enumSuggestion() {
	v := schemix.MustNew(`{ currency: "CNY" | "USD" | "EUR" }`)

	r := v.Process(map[string]any{"currency": "USE"})
	e := r.Errors[0]

	fmt.Println("suggestion:", e.Suggestion)
	fmt.Println("localized: ", schemix.EnUS.Localize(e))
	// Output:
	// suggestion: USD
	// localized:  currency must be one of ["CNY", "USD", "EUR"] — did you mean "USD"?
}

// WithErrorFormatter rewrites Message, the diagnostic meant for logs. Use it to
// match a log format — prefixing the code and path, say, or emitting a single
// line a parser downstream expects.
//
// It is not the way to translate. Message is what a developer reads while
// debugging, and replacing it with user-facing text leaves nothing to debug from;
// the formatter also sees only three strings, so it cannot name the accepted
// values of an enum or the bound a number broke. See ExampleWithLocalizer.
func ExampleWithErrorFormatter() {
	v := schemix.MustNew(`{
		age:      int & >=0 & <=150
		currency: "CNY" | "USD"
	}`, schemix.WithErrorFormatter(
		func(code schemix.ErrorCode, path, detail string) string {
			return fmt.Sprintf("[%s] %s: %s", code, path, detail)
		},
	))

	r := v.Process(map[string]any{"age": int64(200), "currency": "JPY"})
	for _, e := range r.Errors {
		fmt.Println(e.Message)
	}
	// Output:
	// [E1R01] age: value 200 out of bound <=150
	// [E1E01] currency: value "JPY" not in enum ["CNY", "USD"]
}

// WithLocalizer sets the language for user-facing text. ZhCN and EnUS ship with
// the package.
//
// Note that e.Message keeps the raw diagnostic: one is for the caller, the other
// for the log, and both are available at once.
func ExampleWithLocalizer() {
	v := schemix.MustNew(`{
		age:      int & >=0 & <=150
		currency: "CNY" | "USD"
	}`, schemix.WithLocalizer(schemix.ZhCN))

	r := v.Process(map[string]any{"age": int64(200), "currency": "USE"})
	for _, msg := range r.LocalizedMessages() {
		fmt.Println(msg)
	}
	// Output:
	// age必须满足 <=150
	// currency必须是 ["CNY", "USD"] 中的一个，您是否想输入 "USD"？
}

// A service answering several languages picks one per request instead of per
// validator. The same validator serves all of them, concurrently.
func ExampleResult_LocalizedMessagesWith() {
	v := schemix.MustNew(`{age: int & <=150}`)
	r := v.Process(map[string]any{"age": int64(200)})

	for _, lang := range []string{"en", "zh"} {
		loc := schemix.Localizer(schemix.EnUS)
		if lang == "zh" {
			loc = schemix.ZhCN
		}
		fmt.Printf("%s: %s\n", lang, r.LocalizedMessagesWith(loc)[0])
	}
	// Output:
	// en: age must be <=150
	// zh: age必须满足 <=150
}

// Catalog covers what most services need: override the messages that matter,
// rename fields for display, and chain the rest to a built-in catalog so that
// reworded defaults keep arriving.
//
// Labels ignore array indices, so one entry covers every element.
func ExampleCatalog() {
	forms := &schemix.Catalog{
		Messages: map[schemix.ErrorCode]schemix.Message{
			schemix.CodeRequiredMissing: {Template: "Please provide {field}."},
			schemix.CodeRangeViolation: {
				Template: "{field} must be {bound}.",
				Fallback: "{field} is out of range.",
			},
		},
		Labels: map[string]string{
			"contact_email": "Your email address",
			"items[].price": "Item price",
		},
		Fallback: schemix.EnUS,
	}
	// Worth doing at startup: an uncovered code degrades to generic wording
	// rather than failing, so nothing else reports it.
	if err := forms.Validate(); err != nil {
		fmt.Println("catalog problem:", err)
		return
	}

	v := schemix.MustNew(`{
		contact_email: string
		items: [...{price: number & >0}]
	}`, schemix.WithLocalizer(forms))

	r := v.Process(map[string]any{
		"items": []any{
			map[string]any{"price": 10.0},
			map[string]any{"price": -5.0},
		},
	})
	for _, msg := range r.LocalizedMessages() {
		fmt.Println(msg)
	}
	// Output:
	// Please provide Your email address.
	// Item price must be >0.
}

// Localizer is an interface because translation infrastructure usually already
// exists. This one delegates to whatever the surrounding application uses,
// keeping the structured fields available for interpolation.
func ExampleLocalizer() {
	v := schemix.MustNew(`{currency: "CNY" | "USD"}`,
		schemix.WithLocalizer(translator{lang: "fr"}))

	r := v.Process(map[string]any{"currency": "USE"})
	fmt.Println(r.LocalizedMessages()[0])
	// Output:
	// [fr] currency n'accepte que ["CNY", "USD"]
}

// translator stands in for an existing i18n pipeline — gettext, ICU, a database
// of strings. NormalizePath is exported so an implementation can look keys up the
// way Catalog does, collapsing items[3].price to items[].price.
type translator struct{ lang string }

func (t translator) Localize(e schemix.ValidationError) string {
	key := schemix.NormalizePath(e.Path)
	switch e.Code {
	case schemix.CodeEnumInvalid:
		return fmt.Sprintf("[%s] %s n'accepte que [%s]", t.lang, key,
			`"`+strings.Join(e.EnumOptions, `", "`)+`"`)
	case schemix.CodeRangeViolation:
		return fmt.Sprintf("[%s] %s doit être %s", t.lang, key, e.Bound)
	}
	// Never empty: callers render the result unconditionally.
	return fmt.Sprintf("[%s] %s est invalide", t.lang, key)
}
