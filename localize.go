package schemix

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Localizer renders a validation error as text for a person to read.
//
// The built-in *Catalog covers the common case — a table of message templates
// per language — but the interface exists because translation infrastructure is
// usually already in place. A team with an ICU or gettext pipeline, or with
// plural rules a template language cannot express, implements this instead of
// moving their translations into a Go map.
//
// Implementations must honour three rules:
//
//   - Never return an empty string. Callers render the result unconditionally,
//     so an empty return shows up as a field error with no explanation.
//   - Do not include the offending value. The value may be a password or a card
//     number, and a message ends up in API responses and logs.
//   - Stay pure. The same error must render the same way every time, and the
//     same error must be renderable in two languages at once.
type Localizer interface {
	Localize(e ValidationError) string
}

// Message is one entry in a Catalog: a template, plus the wording to use when
// the template names something the error does not carry.
//
// The pair exists because a template is not always renderable. "{field} must be
// {bound}" has nothing to say about a value rejected as ±Inf, where no declared
// bound was broken — rendering it anyway would produce "amount must be", a
// sentence that trails off. Fallback is the same statement without the missing
// part.
type Message struct {
	// Template is used when every placeholder it names has a value.
	Template string

	// Fallback is used when any placeholder in Template has no value. It should
	// name fewer placeholders — usually only {field} — or none at all.
	Fallback string
}

// Catalog is the built-in Localizer: a lookup table from error code to wording.
//
// A catalog need not be complete. Fallback chains to another Localizer for
// anything this one does not define, which is what makes overriding a single
// message practical:
//
//	var myCatalog = &schemix.Catalog{
//	    Messages: map[schemix.ErrorCode]schemix.Message{
//	        schemix.CodeRequiredMissing: {Template: "please fill in {field}"},
//	    },
//	    Fallback: schemix.EnUS,
//	}
//
// Copying the whole table instead would go stale: a reworded built-in message
// would never reach the copy, and the drift is silent.
//
// A Catalog is read-only once built. Sharing one across goroutines is safe;
// mutating its maps after a validator is serving traffic is not.
type Catalog struct {
	// Messages maps an error code to its wording. Codes absent here fall through
	// to Default, then to Fallback.
	Messages map[ErrorCode]Message

	// Default renders codes Messages does not cover, including codes added by a
	// future version of this package. Leaving it empty is safe but means an
	// unrecognised code falls through to Fallback.
	Default Message

	// Labels renames field paths for display: a schema calls it "user_email",
	// a form calls it "Email address".
	//
	// Keys are matched against the error path first, then against the path with
	// array indices stripped, so "items[].price" labels every element. The empty
	// key labels errors that carry no path at all.
	Labels map[string]string

	// SuggestionSuffix is appended when the error carries a Suggestion. It is a
	// template like the others and may use {suggestion}, which renders quoted.
	// An empty value inherits the suffix from Fallback, so a catalog overriding
	// one message does not silently drop the suggestion from it.
	SuggestionSuffix string

	// Fallback handles what this catalog does not define. Nil ends the chain.
	Fallback Localizer
}

// Compile-time proof that the built-in implementation satisfies the interface it
// is the default for.
var _ Localizer = (*Catalog)(nil)

// genericFailureMessage is the last resort when a chain defines nothing usable.
// It says as little as possible while still being a sentence, because the
// alternative — an empty string — renders as a blank error in a UI.
const genericFailureMessage = "validation failed"

// maxFallbackDepth bounds chain traversal. A cycle is easy to build by accident
// when catalogs are assembled at startup, and a library must not spin on one.
const maxFallbackDepth = 8

// Localize implements Localizer.
func (c *Catalog) Localize(e ValidationError) string {
	if s, ok := c.resolve(e, 0); ok {
		return s
	}
	return genericFailureMessage
}

// resolve walks the fallback chain, reporting whether anything in it produced
// wording. The boolean is what separates "nobody covers this code" from "the
// wording happens to read like the generic message", which Validate depends on
// to tell a gap from a coincidence.
func (c *Catalog) resolve(e ValidationError, depth int) (string, bool) {
	if c == nil || depth >= maxFallbackDepth {
		return "", false
	}

	// Resolve the label at every level rather than once at the entry point: the
	// wording may come from a fallback catalog while the label comes from this
	// one. e is a copy, so the caller's error is untouched.
	if label, ok := c.lookupLabel(e.Path); ok {
		e.Path = label
	}

	if s := c.render(e, depth); s != "" {
		return s, true
	}

	switch next := c.Fallback.(type) {
	case nil:
		return "", false
	case *Catalog:
		return next.resolve(e, depth+1)
	default:
		// A third-party implementation owns its own chain; call it once and
		// trust it, but still guarantee a non-empty result.
		if s := next.Localize(e); s != "" {
			return s, true
		}
		return "", false
	}
}

// render produces this catalog's own wording, or "" to defer to the chain.
func (c *Catalog) render(e ValidationError, depth int) string {
	msg, ok := c.Messages[e.Code]
	if !ok {
		msg = c.Default
	}

	body := c.expand(msg.Template, e)
	if body == "" {
		body = c.expand(msg.Fallback, e)
	}
	if body == "" {
		return ""
	}

	if e.Suggestion != "" {
		if suffix := c.expand(c.suggestionSuffix(depth), e); suffix != "" {
			body += suffix
		}
	}
	return body
}

// suggestionSuffix walks the chain so that a catalog overriding a single message
// keeps the suffix rather than dropping it by omission.
func (c *Catalog) suggestionSuffix(depth int) string {
	if c.SuggestionSuffix != "" {
		return c.SuggestionSuffix
	}
	if next, ok := c.Fallback.(*Catalog); ok && depth < maxFallbackDepth {
		return next.suggestionSuffix(depth + 1)
	}
	return ""
}

// expand substitutes {placeholder} references, returning "" if any of them has
// no value — that is the signal for the caller to try Message.Fallback rather
// than emit a sentence with a hole in it.
func (c *Catalog) expand(tmpl string, e ValidationError) string {
	if tmpl == "" {
		return ""
	}
	if !strings.ContainsRune(tmpl, '{') {
		return tmpl
	}

	var b strings.Builder
	b.Grow(len(tmpl) + 16)
	rest := tmpl
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			// Unbalanced brace: emit it literally rather than discarding the
			// message over a typo.
			b.WriteString(rest)
			break
		}
		close += open

		b.WriteString(rest[:open])
		value, ok := placeholderValue(rest[open+1:close], e)
		if !ok {
			return ""
		}
		b.WriteString(value)
		rest = rest[close+1:]
	}
	return b.String()
}

// placeholderValue resolves one placeholder. Reporting false means the template
// cannot be rendered, which includes an unknown placeholder name: a catalog
// naming {feild} should fall back to sound wording rather than print the typo.
//
// There is deliberately no {value} placeholder. The rejected value may be a
// password or a card number, and these messages are returned to API clients and
// written to logs.
func placeholderValue(name string, e ValidationError) (string, bool) {
	switch name {
	case "field":
		return e.Path, e.Path != ""
	case "type":
		return e.FieldType, e.FieldType != ""
	case "bound":
		return e.Bound, e.Bound != ""
	case "options":
		opts := enumOptionsText(e)
		return opts, opts != ""
	case "suggestion":
		return strconv.Quote(e.Suggestion), e.Suggestion != ""
	}
	return "", false
}

// lookupLabel resolves a display name for a path, trying the path as given and
// then with array indices collapsed.
func (c *Catalog) lookupLabel(path string) (string, bool) {
	if len(c.Labels) == 0 {
		return "", false
	}
	if label, ok := c.Labels[path]; ok {
		return label, true
	}
	if normalized := NormalizePath(path); normalized != path {
		label, ok := c.Labels[normalized]
		return label, ok
	}
	return "", false
}

// enumOptionsText renders the accepted values as they appear in a message:
// string candidates quoted, numeric ones bare, matching the raw diagnostic.
//
// It falls back to parsing the detail text for errors that predate the
// structured field or were built by hand elsewhere.
func enumOptionsText(e ValidationError) string {
	if len(e.EnumOptions) == 0 {
		return enumOptionsFromDetail(e.Message)
	}

	quoted := !isNumericFieldType(e.FieldType)
	// Sized up front and appended into, the way stringEnumDetail does it:
	// strconv.Quote per candidate would allocate an intermediate string each
	// time, and this runs once per rejected field of a form.
	size := 2 + 2*len(e.EnumOptions)
	for _, opt := range e.EnumOptions {
		size += len(opt) + 2
	}
	buf := make([]byte, 0, size)
	buf = append(buf, '[')
	for i, opt := range e.EnumOptions {
		if i > 0 {
			buf = append(buf, ", "...)
		}
		if quoted {
			buf = strconv.AppendQuote(buf, opt)
		} else {
			buf = append(buf, opt...)
		}
	}
	buf = append(buf, ']')
	return string(buf)
}

func isNumericFieldType(t string) bool {
	switch t {
	case "int", "float", "number":
		return true
	}
	return false
}

// NormalizePath collapses array indices so that one label covers every element:
// "items[0].price" and "items[7].price" both become "items[].price".
//
// It is exported because any Localizer implementation needs it to look labels up
// the same way Catalog does; keeping it private would force every implementation
// to reimplement it.
func NormalizePath(path string) string {
	if !strings.ContainsRune(path, '[') {
		return path
	}

	var b strings.Builder
	b.Grow(len(path))
	rest := path
	for {
		open := strings.IndexByte(rest, '[')
		if open < 0 {
			b.WriteString(rest)
			break
		}
		close := strings.IndexByte(rest[open:], ']')
		if close < 0 {
			b.WriteString(rest)
			break
		}
		close += open

		b.WriteString(rest[:open])
		if isAllDigits(rest[open+1 : close]) {
			b.WriteString("[]")
		} else {
			// Not an index. Leave it alone rather than guess what it means.
			b.WriteString(rest[open : close+1])
		}
		rest = rest[close+1:]
	}
	return b.String()
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// EnUS is the built-in English catalog, and the wording every other catalog
// falls back to.
//
// It is also what FriendlyMessage renders, so its templates must keep producing
// exactly the sentences that method has always returned —
// TestEnUSMatchesFriendlyMessage holds that line.
var EnUS = &Catalog{
	Messages: map[ErrorCode]Message{
		CodeRequiredMissing: {Template: "{field} is required"},
		CodeCondRequired:    {Template: "{field} is required for this request"},
		CodeTypeMismatch: {
			Template: "{field} must be of type {type}",
			Fallback: "{field} has the wrong type",
		},
		CodeEnumInvalid: {
			Template: "{field} must be one of {options}",
			Fallback: "{field} is not one of the allowed values",
		},
		CodeRangeViolation: {
			Template: "{field} must be {bound}",
			Fallback: "{field} is out of the allowed range",
		},
		CodeFormatMismatch:   {Template: "{field} has an invalid format"},
		CodeArrayElement:     {Template: "{field} contains an invalid item"},
		CodeBizRuleFailed:    {Template: "{field} does not satisfy a validation rule"},
		CodeBlobTypeMismatch: {Template: "{field} produced a value of the wrong type"},
		CodeExprExecError:    {Template: "{field} could not be evaluated"},
		CodeMetaRuntimeError: {Template: "{field} could not be evaluated"},
		CodeCUEOther:         {Template: "{field} is invalid"},
		// A configuration error is not about a field, so it names none.
		CodeConfigError: {Template: "the validation configuration is invalid"},
	},
	// Covers codes this version does not know, including any added later.
	Default: Message{Template: "{field} is invalid"},
	Labels: map[string]string{
		// Errors that carry no path still need a subject.
		"": "value",
	},
	SuggestionSuffix: " — did you mean {suggestion}?",
}

// LocalizedMessages renders every error with the Localizer configured via
// WithLocalizer, or in English when none was configured.
//
// This is the counterpart to ErrorMessages, which returns the raw diagnostics
// for logs. These are the strings to return to a caller:
//
//	if !r.Valid {
//	    respond(w, 422, map[string]any{"errors": r.LocalizedMessages()})
//	}
//
// Returns nil for a valid result, so len() is a safe test either way.
func (r Result) LocalizedMessages() []string {
	var loc Localizer
	if r.v != nil {
		loc = r.v.localizer
	}
	return r.LocalizedMessagesWith(loc)
}

// LocalizedMessagesWith renders every error with l, ignoring whatever
// WithLocalizer configured. This is how one validator serves several languages:
//
//	msgs := r.LocalizedMessagesWith(catalogFor(req.Header.Get("Accept-Language")))
//
// A nil l falls back to English rather than panicking, since the argument is
// often the result of a lookup that may miss.
func (r Result) LocalizedMessagesWith(l Localizer) []string {
	if len(r.Errors) == 0 {
		return nil
	}
	if l == nil {
		l = EnUS
	}
	out := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		out[i] = l.Localize(e)
	}
	return out
}

// FriendlyMessage renders a user-facing sentence for the error.
//
// Message and FriendlyMessage are both always available, which is deliberate:
// a service typically logs the raw diagnostic and renders the friendly one, and
// needing both at once is the common case rather than a mode to switch between.
//
//	log.Warn(e.Message)              // raw CUE/Bloblang wording
//	json.Encode(e.FriendlyMessage()) // user-facing text
//
// The text is always English. It is EnUS.Localize(e), and there is no way to
// change that through this method — an error carries no locale, and adding one
// would put a translation decision inside a serialised DTO. For any other
// language, render the error with a Localizer instead:
//
//	msg := myCatalog.Localize(e)          // one error
//	msgs := result.LocalizedMessages()    // whole result
//
// A custom ErrorFormatter replaces Message entirely; FriendlyMessage is derived
// from the structured fields (Code, Path, FieldType, EnumOptions, Bound,
// Suggestion) and therefore stays stable regardless of formatter configuration.
//
// Deprecated: Call schemix.EnUS.Localize(e) instead — the implementation is
// exactly that, so the wording is identical. The method occupies an awkward
// middle ground: a log line wants Message, which carries the error code, and
// anything user-facing wants a Localizer, which can be any language. Removed in
// v0.3.0.
func (e ValidationError) FriendlyMessage() string {
	return EnUS.Localize(e)
}

// enumOptionsFromDetail lifts the candidate list out of an enum detail such as
// `value "USE" not in enum ["CNY", "USD"]`, returning `["CNY", "USD"]`.
//
// This is a fallback for errors whose EnumOptions field is empty. It reads text
// that is not part of any contract, which is exactly why the structured field
// exists — but an error built by hand, or one produced before that field was
// filled, still has to render.
func enumOptionsFromDetail(detail string) string {
	open := strings.LastIndex(detail, "[")
	if open < 0 || !strings.HasSuffix(detail, "]") {
		return ""
	}
	return detail[open:]
}

// boundFromDetail lifts the comparison out of a range detail such as
// `value 999 out of bound <=150`, returning `<=150`.
//
// Unlike the enum equivalent this is the primary source for Bound rather than a
// fallback: a descriptor holding both a minimum and a maximum cannot say which
// one a value broke without comparing again, while the detail text — generated
// by this package, not by CUE — already names the side that failed.
//
// One path does report CUE's own wording, and CUE parenthesises it:
// `items.0.price: invalid value -5 (out of bound >0)`. A comparison never
// contains a closing paren, so cutting at one separates the bound from the
// punctuation around it. Without this, the sentence read "must be >0)".
func boundFromDetail(detail string) string {
	const marker = "out of bound "
	i := strings.Index(detail, marker)
	if i < 0 {
		return ""
	}
	bound := detail[i+len(marker):]
	if end := strings.IndexByte(bound, ')'); end >= 0 {
		bound = bound[:end]
	}
	return strings.TrimSpace(bound)
}

// ErrorFormatter customizes the raw diagnostic in ValidationError.Message.
//
// It replaces Message, which is the text meant for logs. For user-facing
// wording — including translations — use a Localizer instead: it receives the
// whole error rather than three strings, and it does not overwrite the
// diagnostic a developer needs when debugging.
//
// Example:
//
//	func myFormatter(code ErrorCode, path, detail string) string {
//	    return fmt.Sprintf("%s[%s]: %s", path, code, detail)
//	}
type ErrorFormatter func(code ErrorCode, path string, detail string) string

// ZhCN is the built-in Simplified Chinese catalog.
//
// Latin runs embedded in Chinese text are spaced on both sides, per common
// Chinese typography — "年龄的类型必须是 int", not "年龄的类型必须是int". Field
// paths are not spaced, because a path is a schema identifier and reads as one;
// set Labels to give a field a Chinese name where that matters:
//
//	loc := &schemix.Catalog{
//	    Labels:   map[string]string{"age": "年龄", "items[].price": "单价"},
//	    Fallback: schemix.ZhCN,
//	}
var ZhCN = &Catalog{
	Messages: map[ErrorCode]Message{
		CodeRequiredMissing: {Template: "{field}为必填项"},
		CodeCondRequired:    {Template: "当前请求必须提供{field}"},
		CodeTypeMismatch: {
			Template: "{field}的类型必须是 {type}",
			Fallback: "{field}的类型不正确",
		},
		CodeEnumInvalid: {
			Template: "{field}必须是 {options} 中的一个",
			Fallback: "{field}不在允许的取值范围内",
		},
		CodeRangeViolation: {
			Template: "{field}必须满足 {bound}",
			Fallback: "{field}超出允许的范围",
		},
		CodeFormatMismatch:   {Template: "{field}的格式不正确"},
		CodeArrayElement:     {Template: "{field}中包含无效的元素"},
		CodeBizRuleFailed:    {Template: "{field}未通过校验规则"},
		CodeBlobTypeMismatch: {Template: "{field}计算出的值类型不正确"},
		CodeExprExecError:    {Template: "{field}无法完成计算"},
		CodeMetaRuntimeError: {Template: "{field}无法完成计算"},
		CodeCUEOther:         {Template: "{field}无效"},
		CodeConfigError:      {Template: "校验配置无效"},
	},
	Default: Message{Template: "{field}无效"},
	Labels: map[string]string{
		"": "该值",
	},
	SuggestionSuffix: "，您是否想输入 {suggestion}？",
}

// placeholderNamesInTemplate is the closed set expand recognises. A name outside
// it makes a template unrenderable, which is a silent degradation at runtime and
// a reported problem at Validate time.
var knownPlaceholderNames = map[string]bool{
	"field": true, "type": true, "bound": true, "options": true, "suggestion": true,
}

// Validate reports problems that would otherwise show up as degraded wording in
// production: a code nothing covers, a template naming a placeholder that does
// not exist, a message with no Fallback for the case its placeholders are empty,
// or a fallback chain that loops.
//
// Nothing calls this automatically. A Localizer stays pure and silent — it has no
// logger and must not write to stderr from inside a library — so the way to find
// out that a translation is incomplete is to ask at startup:
//
//	func init() {
//	    if err := myCatalog.Validate(); err != nil {
//	        log.Fatalf("message catalog: %v", err)
//	    }
//	}
//
// A catalog with a Fallback is judged on what the whole chain produces, so
// defining one message and inheriting the rest passes.
func (c *Catalog) Validate() error {
	if c == nil {
		return errors.New("schemix: catalog is nil")
	}

	// Cycles first: every check below renders through the chain, and the results
	// are not meaningful while it loops.
	if err := c.detectFallbackCycle(); err != nil {
		return err
	}

	problems := c.checkPlaceholders()

	for _, code := range builtinErrorCodes {
		// Every placeholder has a value, so Template must be renderable.
		furnished := ValidationError{
			Code: code, Path: "field", FieldType: "int",
			Bound: ">0", EnumOptions: []string{"a"}, Suggestion: "a",
		}
		if _, ok := c.resolve(furnished, 0); !ok {
			problems = append(problems,
				fmt.Errorf("schemix: no message covers %s", code))
			continue
		}
		// Nothing but the code and the path, which is what an error looks like
		// when the schema declared no type and broke no bound.
		bare := ValidationError{Code: code, Path: "field"}
		if _, ok := c.resolve(bare, 0); !ok {
			problems = append(problems,
				fmt.Errorf("schemix: %s renders only when its placeholders have values; "+
					"add Message.Fallback so an error missing them still reads as a sentence", code))
		}
	}

	return errors.Join(problems...)
}

func (c *Catalog) detectFallbackCycle() error {
	seen := make(map[*Catalog]bool)
	for cur := c; cur != nil; {
		if seen[cur] {
			return errors.New("schemix: catalog fallback chain contains a cycle")
		}
		seen[cur] = true
		next, ok := cur.Fallback.(*Catalog)
		if !ok {
			return nil
		}
		cur = next
	}
	return nil
}

func (c *Catalog) checkPlaceholders() []error {
	var problems []error
	check := func(where, tmpl string) {
		for _, name := range placeholderNames(tmpl) {
			if !knownPlaceholderNames[name] {
				problems = append(problems,
					fmt.Errorf("schemix: %s uses unknown placeholder {%s}", where, name))
			}
		}
	}

	// Sorted so that a report is stable across runs; map order is not.
	codes := make([]string, 0, len(c.Messages))
	for code := range c.Messages {
		codes = append(codes, string(code))
	}
	slices.Sort(codes)
	for _, code := range codes {
		msg := c.Messages[ErrorCode(code)]
		check("template for "+code, msg.Template)
		check("fallback for "+code, msg.Fallback)
	}

	check("default template", c.Default.Template)
	check("default fallback", c.Default.Fallback)
	check("suggestion suffix", c.SuggestionSuffix)
	return problems
}

// placeholderNames lists the placeholder names a template references, including
// repeats, in the order they appear.
func placeholderNames(tmpl string) []string {
	if !strings.ContainsRune(tmpl, '{') {
		return nil
	}
	var names []string
	rest := tmpl
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			return names
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			return names
		}
		close += open
		names = append(names, rest[open+1:close])
		rest = rest[close+1:]
	}
}
