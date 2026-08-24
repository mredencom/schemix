package schemix

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

// friendlyMessageCases pins the exact wording of every branch FriendlyMessage
// can take: one per error code, plus one per conditional split inside a code.
//
// It exists because FriendlyMessage is about to be reimplemented on top of a
// message catalog, and the only tests covering its wording used
// strings.Contains on four cases — enough to catch a missing field name, not
// enough to notice a rephrased sentence, a dropped suggestion, or a swapped
// dash. Every string below was captured from the implementation that predates
// the catalog. Changing one means changing what users read, so a diff here
// should be deliberate rather than incidental.
//
// Errors are built by hand rather than by running a validator: that is the only
// way to reach combinations a real schema cannot easily produce, such as an enum
// violation whose detail carries no candidate list.
var friendlyMessageCases = []struct {
	name string
	err  ValidationError
	want string
}{
	{
		name: "required missing",
		err:  ValidationError{Code: CodeRequiredMissing, Path: "age"},
		want: "age is required",
	},
	{
		name: "empty path falls back to a generic subject",
		err:  ValidationError{Code: CodeRequiredMissing, Path: ""},
		want: "value is required",
	},
	{
		name: "conditional required",
		err:  ValidationError{Code: CodeCondRequired, Path: "cvv"},
		want: "cvv is required for this request",
	},
	{
		name: "type mismatch names the expected type",
		err:  ValidationError{Code: CodeTypeMismatch, Path: "age", FieldType: "int"},
		want: "age must be of type int",
	},
	{
		name: "type mismatch without a known type",
		err:  ValidationError{Code: CodeTypeMismatch, Path: "age"},
		want: "age has the wrong type",
	},
	{
		name: "enum lists candidates and suggests the closest",
		err: ValidationError{
			Code: CodeEnumInvalid, Path: "currency",
			Message:     `value "USE" not in enum ["CNY", "USD"]`,
			EnumOptions: []string{"CNY", "USD"},
			Suggestion:  "USD",
		},
		want: `currency must be one of ["CNY", "USD"] — did you mean "USD"?`,
	},
	{
		name: "enum lists candidates with nothing close enough to suggest",
		err: ValidationError{
			Code: CodeEnumInvalid, Path: "currency",
			Message:     `value "ZZZZZZ" not in enum ["CNY", "USD"]`,
			EnumOptions: []string{"CNY", "USD"},
		},
		want: `currency must be one of ["CNY", "USD"]`,
	},
	{
		name: "numeric enum candidates are not quoted",
		err: ValidationError{
			Code: CodeEnumInvalid, Path: "level", FieldType: "int",
			Message:     `value 9 not in enum [1, 2, 3]`,
			EnumOptions: []string{"1", "2", "3"},
		},
		want: "level must be one of [1, 2, 3]",
	},
	{
		name: "enum with no candidates available at all",
		err:  ValidationError{Code: CodeEnumInvalid, Path: "currency", Message: "opaque"},
		want: "currency is not one of the allowed values",
	},
	{
		name: "enum with a suggestion but no candidates",
		err: ValidationError{
			Code: CodeEnumInvalid, Path: "currency",
			Message:    "opaque",
			Suggestion: "USD",
		},
		want: `currency is not one of the allowed values — did you mean "USD"?`,
	},
	{
		name: "range names the bound that was broken",
		err: ValidationError{
			Code: CodeRangeViolation, Path: "age",
			Message: "value 999 out of bound <=150",
			Bound:   "<=150",
		},
		want: "age must be <=150",
	},
	{
		name: "range with no bound to name",
		err:  ValidationError{Code: CodeRangeViolation, Path: "age", Message: "opaque"},
		want: "age is out of the allowed range",
	},
	{
		name: "format mismatch",
		err:  ValidationError{Code: CodeFormatMismatch, Path: "pan"},
		want: "pan has an invalid format",
	},
	{
		name: "array element",
		err:  ValidationError{Code: CodeArrayElement, Path: "items"},
		want: "items contains an invalid item",
	},
	{
		name: "business rule",
		err:  ValidationError{Code: CodeBizRuleFailed, Path: "luhn_check"},
		want: "luhn_check does not satisfy a validation rule",
	},
	{
		name: "blob type contract",
		err:  ValidationError{Code: CodeBlobTypeMismatch, Path: "fee"},
		want: "fee produced a value of the wrong type",
	},
	{
		name: "expression execution error",
		err:  ValidationError{Code: CodeExprExecError, Path: "fee"},
		want: "fee could not be evaluated",
	},
	{
		name: "meta runtime error",
		err:  ValidationError{Code: CodeMetaRuntimeError, Path: "cvv"},
		want: "cvv could not be evaluated",
	},
	{
		name: "config error names no field",
		err:  ValidationError{Code: CodeConfigError, Path: "whatever"},
		want: "the validation configuration is invalid",
	},
	{
		name: "other CUE error",
		err:  ValidationError{Code: CodeCUEOther, Path: "x"},
		want: "x is invalid",
	},
	{
		name: "unknown code still renders",
		err:  ValidationError{Code: ErrorCode("E9Z99"), Path: "x"},
		want: "x is invalid",
	},
}

func TestFriendlyMessageExactWording(t *testing.T) {
	for _, tt := range friendlyMessageCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.FriendlyMessage(); got != tt.want {
				t.Errorf("FriendlyMessage()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// TestFriendlyMessageExactWordingFromRealValidation checks the same wording
// against errors a validator actually produced, so the hand-built cases above
// cannot drift away from the shapes that occur in practice.
func TestFriendlyMessageExactWordingFromRealValidation(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		data   map[string]any
		path   string
		want   string
	}{
		{
			name:   "enum on the fast path",
			schema: `{currency: "CNY" | "USD" | "EUR"}`,
			data:   map[string]any{"currency": "USE"},
			path:   "currency",
			want:   `currency must be one of ["CNY", "USD", "EUR"] — did you mean "USD"?`,
		},
		{
			name:   "int enum on the fast path",
			schema: `{level: 1 | 2 | 3}`,
			data:   map[string]any{"level": int64(9)},
			path:   "level",
			want:   "level must be one of [1, 2, 3]",
		},
		{
			name:   "range on the fast path",
			schema: `{age: int & >=0 & <=150}`,
			data:   map[string]any{"age": int64(999)},
			path:   "age",
			want:   "age must be <=150",
		},
		{
			name:   "required missing",
			schema: `{age: int}`,
			data:   map[string]any{},
			path:   "age",
			want:   "age is required",
		},
		{
			name:   "type mismatch",
			schema: `{age: int}`,
			data:   map[string]any{"age": "old"},
			path:   "age",
			want:   "age must be of type int",
		},
		{
			name:   "list element enum",
			schema: `{tags: [..."a" | "b"]}`,
			data:   map[string]any{"tags": []any{"a", "zzz"}},
			path:   "tags[1]",
			want:   `tags[1] must be one of ["a", "b"]`,
		},
		{
			// CUE's own range wording is parenthesised, and the paren must not
			// end up inside the sentence: "must be >0)" is what a user saw.
			name:   "struct list element range",
			schema: `{items: [...{price: number & >0}]}`,
			data:   map[string]any{"items": []any{map[string]any{"price": -5.0}}},
			path:   "items[0].price",
			want:   "items[0].price must be >0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := MustNew(tt.schema).Process(tt.data)
			errs := r.ErrorsByPath(tt.path)
			if len(errs) == 0 {
				t.Fatalf("no error at %q; got %v", tt.path, r.Errors)
			}
			if got := errs[0].FriendlyMessage(); got != tt.want {
				t.Errorf("FriendlyMessage()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// --- Localizer and Catalog ---

// TestEnUSMatchesFriendlyMessage is the reason FriendlyMessage can be
// reimplemented on top of a catalog at all: the two must agree on every branch,
// byte for byte, or existing callers would see their wording shift under them.
func TestEnUSMatchesFriendlyMessage(t *testing.T) {
	for _, tt := range friendlyMessageCases {
		t.Run(tt.name, func(t *testing.T) {
			got := EnUS.Localize(tt.err)
			if got != tt.want {
				t.Errorf("EnUS.Localize()\n got: %q\nwant: %q", got, tt.want)
			}
			if friendly := tt.err.FriendlyMessage(); got != friendly {
				t.Errorf("EnUS.Localize() = %q, FriendlyMessage() = %q — they must not diverge", got, friendly)
			}
		})
	}
}

func TestCatalogSatisfiesLocalizer(t *testing.T) {
	var _ Localizer = EnUS
	var _ Localizer = &Catalog{}
}

// TestCatalogFallbackChain covers the case a catalog is built for: overriding one
// message without restating the rest. Cloning the whole table instead would go
// stale the moment a built-in message is reworded.
func TestCatalogFallbackChain(t *testing.T) {
	partial := &Catalog{
		Messages: map[ErrorCode]Message{
			CodeRequiredMissing: {Template: "please fill in {field}"},
		},
		Fallback: EnUS,
	}

	if got, want := partial.Localize(ValidationError{Code: CodeRequiredMissing, Path: "age"}), "please fill in age"; got != want {
		t.Errorf("overridden code: got %q, want %q", got, want)
	}
	// Not overridden — must come through unchanged from the fallback.
	if got, want := partial.Localize(ValidationError{Code: CodeFormatMismatch, Path: "pan"}), "pan has an invalid format"; got != want {
		t.Errorf("inherited code: got %q, want %q", got, want)
	}
}

// TestCatalogNeverReturnsEmpty holds the contract a UI depends on: the return
// value is rendered unconditionally, so an empty string would show up as a blank
// field error with no explanation.
func TestCatalogNeverReturnsEmpty(t *testing.T) {
	codes := []ErrorCode{
		CodeConfigError, CodeFormatMismatch, CodeTypeMismatch, CodeEnumInvalid,
		CodeRangeViolation, CodeRequiredMissing, CodeArrayElement, CodeCUEOther,
		CodeBizRuleFailed, CodeExprExecError, CodeBlobTypeMismatch,
		CodeCondRequired, CodeMetaRuntimeError, ErrorCode("E9Z99"), ErrorCode(""),
	}
	catalogs := map[string]*Catalog{
		"EnUS":              EnUS,
		"empty catalog":     {},
		"empty with EnUS":   {Fallback: EnUS},
		"nil messages only": {SuggestionSuffix: " (x)"},
	}
	for name, c := range catalogs {
		for _, code := range codes {
			for _, path := range []string{"field", ""} {
				if got := c.Localize(ValidationError{Code: code, Path: path}); got == "" {
					t.Errorf("%s: code %q path %q produced an empty string", name, code, path)
				}
			}
		}
	}
}

// TestCatalogFallbackCycleTerminates guards against a hang rather than a wrong
// answer. A user assembling catalogs at startup can easily point one at another
// that points back, and a library must not spin on it.
func TestCatalogFallbackCycleTerminates(t *testing.T) {
	self := &Catalog{}
	self.Fallback = self

	a, b := &Catalog{}, &Catalog{}
	a.Fallback, b.Fallback = b, a

	done := make(chan string, 2)
	go func() { done <- self.Localize(ValidationError{Code: CodeCUEOther, Path: "x"}) }()
	go func() { done <- a.Localize(ValidationError{Code: CodeCUEOther, Path: "x"}) }()

	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if got == "" {
				t.Error("a cycle must still yield renderable text")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Localize did not terminate on a fallback cycle")
		}
	}
}

// TestMessageFallbackWhenPlaceholderHasNoValue is why Message carries two
// strings. A template naming a value the error does not have would render
// "age must be of type " — a sentence that trails off mid-thought.
func TestMessageFallbackWhenPlaceholderHasNoValue(t *testing.T) {
	c := &Catalog{
		Messages: map[ErrorCode]Message{
			CodeTypeMismatch: {
				Template: "{field} must be of type {type}",
				Fallback: "{field} has the wrong type",
			},
		},
	}
	withType := ValidationError{Code: CodeTypeMismatch, Path: "age", FieldType: "int"}
	if got, want := c.Localize(withType), "age must be of type int"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	withoutType := ValidationError{Code: CodeTypeMismatch, Path: "age"}
	if got, want := c.Localize(withoutType), "age has the wrong type"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCatalogLabelsRenameFields covers the half of localization that is not the
// sentence: "user_email" is a schema identifier, not something to show a person.
func TestCatalogLabelsRenameFields(t *testing.T) {
	c := &Catalog{
		Labels:   map[string]string{"user_email": "Email address"},
		Fallback: EnUS,
	}
	if got, want := c.Localize(ValidationError{Code: CodeFormatMismatch, Path: "user_email"}), "Email address has an invalid format"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A label must reach an inherited message too, not just this catalog's own.
	if got, want := c.Localize(ValidationError{Code: CodeRequiredMissing, Path: "user_email"}), "Email address is required"; got != want {
		t.Errorf("inherited message with label: got %q, want %q", got, want)
	}
}

// TestCatalogLabelsIgnoreArrayIndices keeps a label usable across elements: a
// schema declares items[].price once, but errors arrive as items[0].price,
// items[7].price, and labelling each index separately is not feasible.
func TestCatalogLabelsIgnoreArrayIndices(t *testing.T) {
	c := &Catalog{
		Labels:   map[string]string{"items[].price": "Unit price"},
		Fallback: EnUS,
	}
	for _, path := range []string{"items[0].price", "items[7].price", "items[123].price"} {
		got := c.Localize(ValidationError{Code: CodeRangeViolation, Path: path, Bound: ">0"})
		if want := "Unit price must be >0"; got != want {
			t.Errorf("path %q: got %q, want %q", path, got, want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"price", "price"},
		{"items[0]", "items[]"},
		{"items[0].price", "items[].price"},
		{"items[123].price", "items[].price"},
		{"a[0].b[1].c", "a[].b[].c"},
		{"items[].price", "items[].price"},
		// Not an index — leave alone rather than guess.
		{"items[x].price", "items[x].price"},
		{"weird[", "weird["},
		{"weird]", "weird]"},
	}
	for _, tt := range tests {
		if got := NormalizePath(tt.in); got != tt.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestLocalizeDoesNotMutateError pins Localize as a pure read of the error: a
// caller rendering the same error in two languages must get two translations of
// the same facts.
func TestLocalizeDoesNotMutateError(t *testing.T) {
	e := ValidationError{
		Code: CodeEnumInvalid, Path: "currency",
		EnumOptions: []string{"CNY", "USD"},
		Suggestion:  "USD",
		Message:     "raw",
	}
	before := e
	beforeOptions := slices.Clone(e.EnumOptions)

	EnUS.Localize(e)

	if e.Code != before.Code || e.Path != before.Path || e.Message != before.Message ||
		e.Suggestion != before.Suggestion || e.Bound != before.Bound {
		t.Error("Localize mutated a scalar field of the error")
	}
	if !slices.Equal(e.EnumOptions, beforeOptions) {
		t.Errorf("Localize mutated EnumOptions: %q, want %q", e.EnumOptions, beforeOptions)
	}
}

// TestSuggestionSuffixInheritedFromFallback covers a way partial overriding
// could quietly lose information: a catalog restating one enum message, but not
// the suffix, would drop "did you mean" from it by omission rather than by
// choice.
func TestSuggestionSuffixInheritedFromFallback(t *testing.T) {
	partial := &Catalog{
		Messages: map[ErrorCode]Message{
			CodeEnumInvalid: {Template: "pick one of {options} for {field}"},
		},
		Fallback: EnUS,
	}
	e := ValidationError{
		Code: CodeEnumInvalid, Path: "currency",
		EnumOptions: []string{"CNY", "USD"},
		Suggestion:  "USD",
	}
	want := `pick one of ["CNY", "USD"] for currency — did you mean "USD"?`
	if got := partial.Localize(e); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	// An explicit suffix must win over the inherited one.
	own := &Catalog{
		Messages:         partial.Messages,
		SuggestionSuffix: " (try {suggestion})",
		Fallback:         EnUS,
	}
	want = `pick one of ["CNY", "USD"] for currency (try "USD")`
	if got := own.Localize(e); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestUnknownPlaceholderFallsBack keeps a typo in a catalog from reaching a user
// as literal text. The template is discarded and the chain continues, so the
// worst case is wording that is correct but less specific.
func TestUnknownPlaceholderFallsBack(t *testing.T) {
	typo := &Catalog{
		Messages: map[ErrorCode]Message{
			CodeFormatMismatch: {Template: "{feild} looks wrong"},
		},
		Fallback: EnUS,
	}
	got := typo.Localize(ValidationError{Code: CodeFormatMismatch, Path: "pan"})
	if want := "pan has an invalid format"; got != want {
		t.Errorf("got %q, want %q — a bad placeholder must not be printed", got, want)
	}

	// With no fallback there is still no leaking of the raw template.
	alone := &Catalog{
		Messages: map[ErrorCode]Message{
			CodeFormatMismatch: {Template: "{feild} looks wrong"},
		},
	}
	if got := alone.Localize(ValidationError{Code: CodeFormatMismatch, Path: "pan"}); got != genericFailureMessage {
		t.Errorf("got %q, want %q", got, genericFailureMessage)
	}
}

// --- ZhCN and startup validation ---

// TestBuiltinCatalogsValidate is the check a user is expected to copy: a catalog
// assembled at startup should be proven complete before it serves traffic, and
// the built-in ones must pass their own bar.
func TestBuiltinCatalogsValidate(t *testing.T) {
	for name, c := range map[string]*Catalog{"EnUS": EnUS, "ZhCN": ZhCN} {
		if err := c.Validate(); err != nil {
			t.Errorf("%s.Validate() = %v, want nil", name, err)
		}
	}
}

// TestZhCNTranslatesEveryCode catches the failure mode a coverage check cannot:
// a catalog that is structurally complete because a code was copied from English
// and never translated.
func TestZhCNTranslatesEveryCode(t *testing.T) {
	for _, code := range builtinErrorCodes {
		e := ValidationError{
			Code: code, Path: "字段", FieldType: "int",
			Bound: ">0", EnumOptions: []string{"a", "b"}, Suggestion: "a",
		}
		zh, en := ZhCN.Localize(e), EnUS.Localize(e)
		if zh == en {
			t.Errorf("code %s renders identically in both catalogs: %q", code, zh)
		}
		if zh == genericFailureMessage {
			t.Errorf("code %s fell through to the generic message", code)
		}
		if !hasHanCharacter(zh) {
			t.Errorf("code %s produced no Chinese text: %q", code, zh)
		}
	}
}

// TestZhCNWording pins a few sentences so that a reword is a deliberate edit
// rather than a side effect. Chinese typography puts a space around embedded
// Latin runs, which is easy to lose when editing templates.
func TestZhCNWording(t *testing.T) {
	tests := []struct {
		name string
		err  ValidationError
		want string
	}{
		{
			name: "required",
			err:  ValidationError{Code: CodeRequiredMissing, Path: "年龄"},
			want: "年龄为必填项",
		},
		{
			name: "no path",
			err:  ValidationError{Code: CodeRequiredMissing},
			want: "该值为必填项",
		},
		{
			name: "type",
			err:  ValidationError{Code: CodeTypeMismatch, Path: "年龄", FieldType: "int"},
			want: "年龄的类型必须是 int",
		},
		{
			name: "type without a known type",
			err:  ValidationError{Code: CodeTypeMismatch, Path: "年龄"},
			want: "年龄的类型不正确",
		},
		{
			name: "enum with suggestion",
			err: ValidationError{
				Code: CodeEnumInvalid, Path: "币种",
				EnumOptions: []string{"CNY", "USD"},
				Suggestion:  "USD",
			},
			want: `币种必须是 ["CNY", "USD"] 中的一个，您是否想输入 "USD"？`,
		},
		{
			name: "range",
			err:  ValidationError{Code: CodeRangeViolation, Path: "年龄", Bound: "<=150"},
			want: "年龄必须满足 <=150",
		},
		{
			name: "range without a bound",
			err:  ValidationError{Code: CodeRangeViolation, Path: "年龄"},
			want: "年龄超出允许的范围",
		},
		{
			name: "config error names no field",
			err:  ValidationError{Code: CodeConfigError, Path: "无关"},
			want: "校验配置无效",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ZhCN.Localize(tt.err); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestCatalogValidateReportsProblems(t *testing.T) {
	cycle := &Catalog{Messages: EnUS.Messages}
	cycle.Fallback = cycle

	a, b := &Catalog{Messages: EnUS.Messages}, &Catalog{Messages: EnUS.Messages}
	a.Fallback, b.Fallback = b, a

	tests := []struct {
		name    string
		catalog *Catalog
		want    string
	}{
		{
			name:    "no messages at all",
			catalog: &Catalog{},
			want:    "cover",
		},
		{
			name: "one code missing with no fallback",
			catalog: &Catalog{
				Messages: withoutCode(EnUS.Messages, CodeRangeViolation),
			},
			want: string(CodeRangeViolation),
		},
		{
			name: "unknown placeholder",
			catalog: &Catalog{
				Messages: map[ErrorCode]Message{CodeCUEOther: {Template: "{feild} is off"}},
				Fallback: EnUS,
			},
			want: "feild",
		},
		{
			name:    "self-referential fallback",
			catalog: cycle,
			want:    "cycle",
		},
		{
			name:    "two-catalog cycle",
			catalog: a,
			want:    "cycle",
		},
		{
			name: "template renders but fallback does not",
			catalog: &Catalog{
				Messages: map[ErrorCode]Message{
					CodeTypeMismatch: {Template: "{field} must be {type}"},
				},
			},
			want: string(CodeTypeMismatch),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.catalog.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want a problem report")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestCatalogValidateAcceptsPartialWithFallback keeps Validate from punishing the
// pattern catalogs exist for: define one message, inherit the rest.
func TestCatalogValidateAcceptsPartialWithFallback(t *testing.T) {
	partial := &Catalog{
		Messages: map[ErrorCode]Message{
			CodeRequiredMissing: {Template: "please fill in {field}"},
		},
		Fallback: EnUS,
	}
	if err := partial.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for a catalog backed by EnUS", err)
	}
}

// TestCatalogConcurrentLocalize backs the documented promise that a catalog is
// safe to share. It is read-only after construction, but "should be" is not
// evidence — this runs under -race in CI.
func TestCatalogConcurrentLocalize(t *testing.T) {
	chained := &Catalog{
		Labels:   map[string]string{"items[].price": "单价"},
		Fallback: ZhCN,
	}
	errs := []ValidationError{
		{Code: CodeRequiredMissing, Path: "年龄"},
		{Code: CodeEnumInvalid, Path: "币种", EnumOptions: []string{"CNY", "USD"}, Suggestion: "USD"},
		{Code: CodeRangeViolation, Path: "items[2].price", Bound: ">0"},
		{Code: ErrorCode("E9Z99"), Path: ""},
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				for _, e := range errs {
					for _, loc := range []Localizer{EnUS, ZhCN, chained} {
						if loc.Localize(e) == "" {
							t.Error("Localize returned an empty string")
							return
						}
					}
				}
			}
		}()
	}
	wg.Wait()
}

func withoutCode(src map[ErrorCode]Message, drop ErrorCode) map[ErrorCode]Message {
	out := make(map[ErrorCode]Message, len(src))
	for code, msg := range src {
		if code != drop {
			out[code] = msg
		}
	}
	return out
}

func hasHanCharacter(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// --- WithLocalizer and Result convenience ---

const localizerTestSchema = `{currency: "CNY" | "USD", age: int & <=150}`

// "USE" is one edit from "USD", so it produces a suggestion; a more distant
// value would exceed maxSuggestionDistance and leave the suffix off, which would
// silently drop that half of the rendering from these tests.
var localizerTestData = map[string]any{"currency": "USE", "age": int64(200)}

func TestLocalizedMessagesDefaultsToEnglish(t *testing.T) {
	r := MustNew(localizerTestSchema).Process(localizerTestData)
	got := r.LocalizedMessages()
	want := []string{
		`currency must be one of ["CNY", "USD"] — did you mean "USD"?`,
		"age must be <=150",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestWithLocalizerSetsTheResultDefault(t *testing.T) {
	v := MustNew(localizerTestSchema, WithLocalizer(ZhCN))
	got := v.Process(localizerTestData).LocalizedMessages()
	want := []string{
		`currency必须是 ["CNY", "USD"] 中的一个，您是否想输入 "USD"？`,
		"age必须满足 <=150",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestLocalizedMessagesWithOverridesTheDefault covers the case the whole design
// exists for: one validator, one request per language, chosen at render time.
// Binding a locale at construction would need a validator per language.
func TestLocalizedMessagesWithOverridesTheDefault(t *testing.T) {
	v := MustNew(localizerTestSchema, WithLocalizer(ZhCN))
	r := v.Process(localizerTestData)

	if got := r.LocalizedMessagesWith(EnUS)[1]; got != "age must be <=150" {
		t.Errorf("render-time override ignored: %q", got)
	}
	// The default must survive being overridden for one call.
	if got := r.LocalizedMessages()[1]; got != "age必须满足 <=150" {
		t.Errorf("default was clobbered by the override: %q", got)
	}
}

func TestLocalizedMessagesEmptyWhenValid(t *testing.T) {
	r := MustNew(localizerTestSchema).Process(map[string]any{"currency": "CNY", "age": int64(30)})
	if !r.Valid {
		t.Fatalf("expected a valid result, got %v", r.Errors)
	}
	if got := r.LocalizedMessages(); len(got) != 0 {
		t.Errorf("LocalizedMessages() = %q, want empty", got)
	}
}

func TestLocalizedMessagesWithNilFallsBackToEnglish(t *testing.T) {
	r := MustNew(localizerTestSchema, WithLocalizer(ZhCN)).Process(localizerTestData)
	if got := r.LocalizedMessagesWith(nil)[1]; got != "age must be <=150" {
		t.Errorf("got %q, want the English default", got)
	}
}

func TestWithLocalizerNilIsHarmless(t *testing.T) {
	v := MustNew(localizerTestSchema, WithLocalizer(nil))
	if got := v.Process(localizerTestData).LocalizedMessages()[1]; got != "age must be <=150" {
		t.Errorf("got %q, want the English default", got)
	}
}

// TestResultJSONExcludesLocalizer guards the reason the default is held on Result
// rather than on ValidationError: the error is a DTO with json tags on every
// field, and a Localizer must never reach an API response.
func TestResultJSONExcludesLocalizer(t *testing.T) {
	r := MustNew(localizerTestSchema, WithLocalizer(ZhCN)).Process(localizerTestData)

	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for key := range decoded {
		switch key {
		case "valid", "errors", "output":
		default:
			t.Errorf("unexpected key %q in encoded Result: %s", key, encoded)
		}
	}
	// The same for the errors themselves.
	var errs []map[string]json.RawMessage
	if err := json.Unmarshal(decoded["errors"], &errs); err != nil {
		t.Fatalf("decode errors: %v", err)
	}
	allowed := map[string]bool{
		"code": true, "path": true, "type": true, "field_type": true,
		"message": true, "suggestion": true, "enum_options": true, "bound": true,
	}
	for _, e := range errs {
		for key := range e {
			if !allowed[key] {
				t.Errorf("unexpected key %q in encoded ValidationError", key)
			}
		}
	}
}

// TestWithLocalizerDoesNotAffectFriendlyMessage records a rough edge rather than
// a feature. FriendlyMessage is a method on the error, the error carries no
// locale, and giving it one would put a translation decision inside a serialised
// DTO. So a validator configured for Chinese still answers English here.
//
// The test exists so that a later reader treats this as decided rather than
// broken, and fixes the call site instead of the method.
func TestWithLocalizerDoesNotAffectFriendlyMessage(t *testing.T) {
	r := MustNew(localizerTestSchema, WithLocalizer(ZhCN)).Process(localizerTestData)

	localized := r.LocalizedMessages()[1]
	if localized != "age必须满足 <=150" {
		t.Fatalf("LocalizedMessages() = %q, want Chinese", localized)
	}
	if friendly := r.Errors[1].FriendlyMessage(); friendly != "age must be <=150" {
		t.Errorf("FriendlyMessage() = %q; it is always English by design", friendly)
	}
}

// TestValidateFamilyHasNoDefaultLocalizer pins an asymmetry. Validate returns
// (bool, []ValidationError) with no Result to carry a default, so a configured
// localizer cannot reach it. Changing that signature is a breaking change, and
// the explicit call is one line, so the gap stays — documented, not silent.
func TestValidateFamilyHasNoDefaultLocalizer(t *testing.T) {
	v := MustNew(localizerTestSchema, WithLocalizer(ZhCN))

	valid, errs := v.Validate(localizerTestData)
	if valid || len(errs) == 0 {
		t.Fatal("expected validation to fail")
	}
	// English, because nothing carried the configured localizer here.
	if got := errs[1].FriendlyMessage(); got != "age must be <=150" {
		t.Errorf("FriendlyMessage() = %q", got)
	}
	// The supported way to localize on this path is to say so.
	if got := ZhCN.Localize(errs[1]); got != "age必须满足 <=150" {
		t.Errorf("ZhCN.Localize() = %q", got)
	}
}

// TestLocalizerAndErrorFormatterAreOrthogonal keeps the two hooks from being
// confused for alternatives: the formatter owns Message, which is for logs, and
// the localizer owns what a person reads. Configuring both must not have one
// overwrite the other.
func TestLocalizerAndErrorFormatterAreOrthogonal(t *testing.T) {
	v := MustNew(localizerTestSchema,
		WithLocalizer(ZhCN),
		WithErrorFormatter(func(code ErrorCode, path, detail string) string {
			return "log:" + string(code) + ":" + path
		}),
	)
	r := v.Process(localizerTestData)

	if got, want := r.Errors[1].Message, "log:"+string(CodeRangeViolation)+":age"; got != want {
		t.Errorf("Message = %q, want %q", got, want)
	}
	if got := r.LocalizedMessages()[1]; got != "age必须满足 <=150" {
		t.Errorf("LocalizedMessages() = %q — the formatter must not reach it", got)
	}
}
