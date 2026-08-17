# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking Changes
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
- `cue.Context.Encode` is now **lazy**: the input map is converted into a `cue.Value` only when a field actually needs one. Schemas whose fields are all served by the Go-native fast path validate with **zero allocations** (`Validate` on five scalar fields: 2.94µs / 59 allocs → 382ns / 0 allocs). The encode still runs for `@blob()` rules on present fields, nested structs, arrays, constraints too complex for a descriptor, and `uint*` values on `int` constraints. Verdicts are unchanged and pinned by the differential CUE oracle tests.
- `FailPriority` now collects CUE and Blob errors from the first failing priority group and skips all higher groups.
- `@meta(priority=N)` accepts negative values and uses overflow-safe ordering.
- Present optional fields and present `@blob()` fields receive full CUE validation before Blob execution.
- Invalid Bloblang mode strings fail mapping construction instead of silently selecting another mode.

### Deprecated
- `Registry.RegisterAll()`; use `RegisterAllTo(env)`.
- `Registry.RegisterMethods()`; use `RegisterMethodsTo(env)`.
- `Registry.RegisterFunctions()`; use `RegisterFunctionsTo(env)`.

### Fixed
- `New()` no longer hangs on mutually recursive definitions reached through arrays (`#A: {bs: [...#B]}` with `#B: {as: [...#A]}`). The array-element attribute scan recursed without a bound; every construction-time walk now carries the `WithMaxSchemaDepth` limit. Relevant wherever schemas come from untrusted input.
- Exact `int64` range checks above 2^53 and conservative CUE fallback for unsigned or unrepresentable values.
- Float enums, signed integer aliases, and non-finite numeric classification.
- Conditional meta validation no longer fails open at compile time or runtime.
- CUE validation now runs before Blob rules for present input fields.
- Invalid results no longer leak partial output.
- Array element errors retain their complete indexed path.
- Multiple Registry instances can no longer silently overwrite plugins in the same Bloblang environment.
