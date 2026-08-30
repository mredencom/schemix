# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `FailMode` now implements `CallOption`, so `Process`, `ProcessContext`, `Validate` and `ValidateContext` accept it per call: `v.Process(data, schemix.FailFast)`. The `Validate` family can select a mode for the first time — it previously hardcoded `FailAll`, so callers wanting fail-fast had to call `Process` and discard the `Output` they paid for. Existing calls are unaffected; the parameter is variadic. When several options are given, the last one wins.
- `Registry.Register` accepts construction options: `reg.Register("payment", src, schemix.WithMethod("is_allowed_bin", fn))`. Previously a schema exposed to a Benthos pipeline could not also use custom Bloblang functions — the two features were mutually exclusive. `WithName` is applied last and cannot be overridden, because the registry key is what labels metrics.
- `Registry.Put(name, v)` files an already-constructed validator, which `Register` could not do because it builds from CUE source. This covers validators from `NewFromValue` with shared definitions, or ones sharing a `FuncMap`. A nil validator is rejected rather than stored. Unlike `Register`, `Put` cannot make the metrics label agree with the registry key, since a `Validator` is immutable once constructed — pass `WithName` yourself if they need to match.
- `Validator.Localizer()` reports the configured default localizer, returning `EnUS` when none was set. `WithLocalizer` was previously write-only, so a service could not log which language a schema defaults to, nor reuse it to render a message outside a `Result`.
- `FieldInfo` reports `@meta()` controls: `Priority`, `RequiredIf`, `SkipIf`, `Conditional`, `OmitEmpty`. `Fields()` is documented for generating forms but exposed none of them, so a form could not tell that `cvv?: string @meta(conditional, required_if=...)` is required for credit payments rather than simply optional. `RequiredIf` and `SkipIf` hold the raw expression text. All five carry `omitempty`, so a schema using no `@meta` serialises exactly as before. A field carrying only `@meta(priority=N)` reports `Priority: 0`, because priority orders rule execution and such a field has no rules to order.

### Deprecated
All of the following are removed in v0.3.0. Each carries a `Deprecated:` marker naming its replacement.

| Deprecated | Use instead |
|------------|-------------|
| `Validator.ProcessWithMode(data, mode)` | `v.Process(data, mode)` |
| `Validator.ProcessWithModeContext(ctx, data, mode)` | `v.ProcessContext(ctx, data, mode)` |
| `Validator.ProcessValue` / `ProcessValueWithMode` | `v.Process(data[, mode])` — accepts every supported input type from v0.3.0 |
| `Validator.ProcessValueContext` / `ProcessValueWithModeContext` | `v.ProcessContext(ctx, data[, mode])` — same |
| `Validator.ValidateValue` / `ValidateValueContext` | `v.Validate(data)` / `v.ValidateContext(ctx, data)` — same |
| `ProcessStruct` / `ProcessStructWithMode` / `ValidateStruct` | `v.Process(data[, mode])` / `v.Validate(data)`. The type parameter constrained nothing — `T` is `any`, so `ProcessStruct(v, 42)` compiled and failed at runtime with `E0C01`. |
| Package-level `Register` / `MustRegister` / `Get` / `MustGet` / `Unregister` / `Has` / `List` / `Len` | A `Registry`: `reg := schemix.NewRegistry()`, then `reg.Put(name, v)`, `reg.Get(name)`, and so on |
| Package-level `ProcessWith` / `ProcessWithMode` / `ValidateWith` | `v, ok := reg.Get(name)`, then call the validator. The package-level `ProcessWithMode` shares a name with the `Validator` method while taking a name where that takes data, so a mixed-up call still compiles. |
| `ValidationError.FriendlyMessage()` | `schemix.EnUS.Localize(e)` — the implementation is exactly that, so the wording is identical |

The package-level store is going away because a process-global registry cannot be scoped to a test, an environment, or a tenant: two components sharing a process share one namespace, and a collision between them is silent.

### Breaking Changes
- Constraints the Go-native fast path cannot represent exactly are no longer silently dropped. A field carrying a conjunct outside the descriptor's vocabulary — a second regex (`=~"^a" & =~"b$"`), a `!=` bound (`string & !=""`, `int & >0 & !=5`), a builtin call (`strings.MinRunes(3)`, `math.MultipleOf(0.5)`), a concrete literal (`n: 5`, `s: "hello"`, `b: true`), or a default marker (`*10 | int & >=0`, where CUE folds the disjunction and hides the `>=0`) — now falls back to CUE and is validated in full. Previously the descriptor kept only the part it understood and reported `Valid=true` with no errors, so such fields accepted invalid data. Verdicts change from accept to reject for input that violates the dropped conjunct.
- Integer values supplied to CUE `float` fields now fail with `E1T01`. CUE keeps `int` and `float` as sibling subtypes of `number`, so `r: float` rejects `50` and accepts `50.0`; the fast path previously accepted either. This affected bare `float`, float ranges, float enums, and lists of floats. Declare the field `number` to accept both, which is what CUE itself does.
- Non-nullable fields receiving `nil` now fail with `E1M01`.
- Go floating-point values supplied to CUE `int` fields now fail with `E1T01`, even when mathematically integral.
- Non-bool `@blob()` results are checked against the field schema; mismatches fail with `E2T01`.
- Invalid results always return `Output == nil`; partial output is no longer exposed.
- Invalid `FailMode` values return an invalid Result with `E0C01` instead of silently using `FailAll`.
- Invalid `@meta()` configuration makes `New()` return an error; runtime condition errors fail closed with `E3X01`.
- `@blob()` / `@meta()` written **inside an array element schema** now makes `New()` return an error. Previously such attributes were silently dropped — the rule never ran and invalid data passed validation. Rules are compiled per field path and an element index is unknown until runtime, so the attribute cannot be honoured where it was written.
- `@blob()` / `@meta()` written **on a definition** (`#Name: int @blob(...)`) now makes `New()` return an error, for the same reason: a definition is a reusable template while the expression binds to an absolute path, so the attribute was silently dropped. Attributes on fields *inside* a struct definition are unaffected.

### Migration
| Change | Required action |
|--------|-----------------|
| Unrepresentable constraint now enforced | None for correct data. Input that violated a previously dropped conjunct now fails, which is the intended verdict; the affected field costs more than a fast-path field because CUE decides it. |
| Integer value on a `float` field (`E1T01`) | Declare the field `number` if it should accept both, or pass a Go float (`50.0`, not `50`). |
| Attribute inside an array element | Move it to the array field itself: `items: [...{qty: int}] @blob(this.items.all(i -> i.qty > 0))`. Computed element fields must be declared optional (`subtotal?: number`) because CUE runs before `@blob`. |
| Attribute on a definition | Move it to the field that references the definition: `pan: #PAN @blob(this.pan.luhn_valid())`. |
| Non-nullable `nil` | Declare nullable fields as `null \| T`; do not use `nil` for non-nullable fields. |
| Float value for an `int` field | Pass a Go integer type such as `int64` instead of `float64`. |
| `@blob()` type mismatch (`E2T01`) | Return a value that satisfies the field's declared CUE schema. |
| Invalid result output | Check `Result.Valid` before reading `Result.Output`; invalid results have no output. |
| Invalid `FailMode` (`E0C01`) | Handle the returned invalid Result; do not rely on silent fallback or panic recovery. |
| Invalid `@meta()` at compile time | Check the error from `New()` and fix unknown parameters, invalid expressions, or priority values. |
| `@meta()` runtime error (`E3X01`) | Fix failing `required_if`/`skip_if` expressions and handle the validation error. |
| Global Registry plugins | Create a `bloblang.Environment` and use `RegisterAllTo`, `RegisterMethodsTo`, or `RegisterFunctionsTo`. |

### Added
- Open lists of **scalar** elements are served by the fast path, checked element-by-element in pure Go with no allocation: three such fields went from 22.0 µs / 365 allocs to **72.9 ns / 0 allocs** at one element each, and from 66.2 µs / 776 allocs to **338 ns / 0 allocs** at ten. One error per offending element, indexed as `tags[1]`, matching what CUE reports. Lists whose verdict does not reduce to a per-element scalar check are left to CUE: struct or nested-list elements, fixed arity (`[string, string]`), a fixed head element (`[string, ...int]`), a list-level conjunct (`list.MinItems`), and a disjunction of list types.
- **Localization.** `Localizer` is a one-method interface over the whole error, with `Catalog` as the built-in implementation and `EnUS` / `ZhCN` shipped. `WithLocalizer` sets a validator-wide default; `Result.LocalizedMessages()` renders with it and `Result.LocalizedMessagesWith(loc)` picks a language per call, so one compiled schema serves every language concurrently rather than needing a validator each. `Catalog.Fallback` chains, so overriding one message does not mean copying a table that goes stale when a built-in is reworded. `Catalog.Validate()` reports uncovered codes, unknown placeholders, missing `Message.Fallback`, and fallback cycles — call it at startup, since an uncovered code degrades to generic wording rather than failing. `NormalizePath` is exported for implementations that look labels up by path (`items[3].price` → `items[].price`). There is deliberately no `{value}` placeholder: the rejected value may be a password or a card number.
- `ValidationError.EnumOptions` and `ValidationError.Bound` — the accepted values of an enum and the comparison a number broke, as structured fields instead of text to be recovered from `Message`. Both were already known at the point of failure and discarded during formatting. `Message` is replaceable by an `ErrorFormatter`, so anything parsed out of it disappeared as soon as one was configured; that is also why `FriendlyMessage` used to degrade to generic wording under a formatter, which it no longer does. The scalar fast path is untouched: `fastResult` is unchanged at 56 bytes, since growing it by a slice header measured 6% on every scalar field of every validation. `Process` on valid input is unchanged at 86 allocations; on invalid input it is 103, one more for a copy of the candidate list, which cannot be shared because a descriptor is built once and read by every concurrent `Process`.
- `WithMaxSchemaDepth(n)` — bounds how deep `New()` recurses while analysing the schema (nested structs, array element schemas, definitions). Defaults to 32. Exceeding the limit returns an error rather than silently skipping deeper levels, which could otherwise hide an unextracted `@blob`/`@meta` attribute. A non-positive value is rejected instead of meaning "unlimited".
- `ValidationError.FieldType` — the schema type of the offending field (`string`, `int`, `list`…), resolved at compile time so the error path pays nothing for it.
- `ValidationError.Suggestion` — the closest valid value for enum violations, via case-insensitive edit distance. Deliberately empty for range/regex/type violations, where a guess would mislead.
- `ValidationError.FriendlyMessage()` — a user-facing sentence derived from the structured fields. Available alongside the raw `Message` at all times, so logging the diagnostic and rendering the friendly text need no mode switch. Never returns an empty string.
- Enum violations now name every accepted value: `value "USE" not in enum ["CNY", "USD", "EUR"]` instead of `value "USE" not in enum`. The message is assembled in a single buffer and the edit-distance search uses a stack-resident row, so naming the candidates and suggesting a correction costs no additional allocations.
- A failed disjunction inside an array now yields **one** error instead of a summary plus one per rejected branch, and no longer leaks CUE wording (`2 errors in empty disjunction`, `conflicting values`). Parsing CUE's format is best-effort: an unrecognised shape falls back to the previous per-branch errors rather than inventing a message.
- Scoped Bloblang registration through `RegisterAllTo(env)`, `RegisterMethodsTo(env)`, and `RegisterFunctionsTo(env)` with environment ownership isolation.
- Error codes `E0C01` (configuration), `E2T01` (Blob result type), and `E3X01` (meta runtime).
- Three-state fast-path decisions with safe fallback to CUE when correctness cannot be determined.
- Structured array paths such as `items[0].field`.
- Differential CUE oracle tests, no-recover fuzz tests, and boundary tests for all built-in validators.
- A fail-closed, statistically significant 5% CI benchmark regression gate with pinned `benchstat`.
- A 70% CI coverage gate and weekly Dependabot updates for Go modules and GitHub Actions.
- CUE native constraints: type, regex, enum, range, nested structs, arrays, and nullable `null | T`.
- `@blob()` validation and computed-field expressions.
- `@meta()` field controls for priority, optionality, conditions, skipping, and output omission.
- `FailAll`, `FailFast`, and `FailPriority` execution modes.
- Structured validation errors and Registry management APIs.
- Custom functions and methods, reusable `FuncMap`, schema composition, and introspection APIs.

### Changed
- `FailFast` now **stops walking fields at the first failure** instead of collecting every error and truncating to one. The Result was always a single error, so nothing observable there changes, but the discarded errors were fully built first — one `Message` formatted and one enum candidate list copied per error that was then thrown away. `ProcessWithMode(FailFast)` on a three-error input: 1185 → **528 B**, 11 → **5 allocs**, 641 → **264 ns**. One side effect is observable: `ObserveFastpathDecision` now reports only the fields actually visited, so counting those calls to infer a schema's field count works under `FailAll` and `FailPriority` but not under `FailFast`.
- The CUE-layer error snapshot taken before Blob rules run is a slice prefix rather than a copy. The blob loop only ever appends, so the first N entries stay the CUE-layer ones even across a reallocation; copying them cost one allocation of N `ValidationError`.
- Field lookup walks the dot-path with `strings.IndexByte` instead of `strings.Split`, which allocated a slice on every call. `fieldPriorityByPath` calls it twice per error under `FailPriority`, so this was measurable: `FailPriority` 31 → **23 allocs**. `Process` (valid) 86 → **81 allocs**, `Validate` (valid) 82 → **77**, since computed `@blob` fields reach the same lookup on the success path.
- `ValidationError.FriendlyMessage()` is now `EnUS.Localize(e)`. Wording is byte-for-byte identical on every branch, pinned by 27 cases captured from the previous implementation. It remains always English by design — an error carries no locale, and giving it one would put a translation decision inside a struct that gets serialised into API responses. `WithLocalizer` therefore does not change it; use `LocalizedMessages()` for anything else.
- The presentation layer moved from `errors.go` to a new `localize.go`, splitting what an error *is* (codes, structs, queries) from how it is *shown*. Internal reorganisation: no import path or exported API changed.
- `ErrorFormatter` is documented as rewriting the diagnostic for logs, not as the way to translate. It sees three strings rather than the whole error, so it cannot name an enum's accepted values or the bound a number broke, and it overwrites the text a developer needs while debugging. The two hooks are independent and can be set together.
- Field lookup paths are built once at schema compile time (`cue.MakePath`) instead of parsed on every call (`cue.ParsePath`), which ran the CUE expression parser at 1385 ns and 36 allocations per navigated field per validation. `Process` on a nested schema: 492 → **420 allocs**, 46331 → 42895 B, 30381 → 26382 ns. The scalar fast path never reaches the lookup and is unaffected.
- Enums of `enumSetThreshold` (8) candidates or more are checked against a membership set built at compile time rather than scanned. Measured end to end: 60 candidates 75.05 → **42.39 ns**, 180 candidates 150.4 → **42.40 ns**, both allocation-free. Smaller enums keep the scan, which is genuinely faster below the threshold (3.1 ns vs 5.5 ns at three candidates), and the ordered candidate list remains the only source for error messages so their wording still follows declaration order.
- `Registry.List()` and the package-level `List()` now return names **sorted lexicographically**. Both previously returned whatever order the backing store produced — a Go map for `Registry`, `sync.Map.Range` for the global store — which varied between calls on the same contents (measured: three distinct orderings across 200 runs of one registration sequence). No documented behaviour changes, since no order was ever guaranteed, but callers that displayed, diffed, or asserted on the result were nondeterministic.
- `cue.Context.Encode` is now **lazy**: the input map is converted into a `cue.Value` only when a field actually needs one. Schemas whose fields are all served by the Go-native fast path validate with **zero allocations** (`Validate` on five scalar fields: 2.94µs / 59 allocs → 382ns / 0 allocs). The encode still runs for `@blob()` rules on present fields, nested structs, lists of struct elements, constraints too complex for a descriptor, and `uint*` values on `int` constraints. Verdicts are unchanged and pinned by the differential CUE oracle tests.
- `FailPriority` now collects CUE and Blob errors from the first failing priority group and skips all higher groups.
- `@meta(priority=N)` accepts negative values and uses overflow-safe ordering.
- Present optional fields and present `@blob()` fields receive full CUE validation before Blob execution.
- Invalid Bloblang mode strings fail mapping construction instead of silently selecting another mode.

### Deprecated
- `Registry.RegisterAll()`; use `RegisterAllTo(env)`.
- `Registry.RegisterMethods()`; use `RegisterMethodsTo(env)`.
- `Registry.RegisterFunctions()`; use `RegisterFunctionsTo(env)`.

### Fixed
- A struct element's range violation no longer reads `items[0].price must be >0)`. Four of the five paths producing `E1R01` carry wording this package generates, where the bound runs to the end of the string; a struct inside a list has no fast-path descriptor and reports CUE's own text, which parenthesises it (`invalid value -5 (out of bound >0)`). The bound is now cut at the closing paren.
- Four doc comments split across files by an earlier file split are rejoined. `errors.go` and `classify.go` each carried a dangling half-sentence attached to no symbol, describing `maxSuggestionDistance` and `suggestClosest` in `suggest.go`; godoc rendered neither.
- `New()` no longer hangs on mutually recursive definitions reached through arrays (`#A: {bs: [...#B]}` with `#B: {as: [...#A]}`). The array-element attribute scan recursed without a bound; every construction-time walk now carries the `WithMaxSchemaDepth` limit. Relevant wherever schemas come from untrusted input.
- Exact `int64` range checks above 2^53 and conservative CUE fallback for unsigned or unrepresentable values.
- Float enums, signed integer aliases, and non-finite numeric classification.
- Conditional meta validation no longer fails open at compile time or runtime.
- CUE validation now runs before Blob rules for present input fields.
- Invalid results no longer leak partial output.
- Array element errors retain their complete indexed path.
- Multiple Registry instances can no longer silently overwrite plugins in the same Bloblang environment.
