# Cross-library benchmarks

Detailed measurements behind the summary table in the main
[README](../../README.md#comparison). This suite is a **separate module** so
competitor libraries never enter schemix's dependency tree.

```bash
go test -run TestRuleSetsAreEquivalent -v   # verify the rule sets are equivalent
go test -bench=. -benchmem -count=10        # reproduce every number below
```

Environment: Apple M4, Go 1.25.11, darwin/arm64, `benchstat` medians over 10
runs. Versions: go-playground/validator v10.28.0 · ozzo-validation v4.3.0 ·
santhosh-tekuri/jsonschema v6.0.2 · cuelang.org/go v0.17.1.

## Methodology

Every engine validates the **same five constraints** — `pan` (16 digits),
`amount` (int > 0), `currency` (enum), `age` (int in 0..150), `email` (address
format) — each expressed in that library's own idiomatic best form.

A performance comparison is meaningless unless every engine reaches the same
verdict, so [`TestRuleSetsAreEquivalent`](comparison_test.go) asserts all six agree on
identical payloads. The numbers are only published when it passes.

Input models differ by design and each library gets the shape it was built for:
struct tags for go-playground/validator and ozzo, decoded JSON for jsonschema,
`map[string]any` for schemix and raw CUE.

Two notes on fairness:

- go-playground/validator has no built-in regex tag, so `pan` uses
  `len=16,numeric` and `order_id` is a registered custom validation — its
  documented extension point.
- `email` uses each library's native format check (`email` tag, `is.EmailFormat`,
  a shared regex for schemix/jsonschema/CUE).

## Full results

### Scalar — 5 fields, valid payload

| Library | Time | Memory | Allocs |
|---------|------|--------|--------|
| **schemix — `Validate`, CUE-only rules** | **382 ns** | **0 B** | **0** |
| go-playground/validator | 784 ns | 163 B | 6 |
| ozzo-validation | 1.72 µs | 2.30 KiB | 37 |
| jsonschema v6 | 1.87 µs | 2.06 KiB | 56 |
| schemix — `Validate`, one `@blob()` rule | 6.44 µs | 10.74 KiB | 127 |
| raw `cuelang.org/go` API | 12.86 µs | 24.87 KiB | 186 |

### Scalar — invalid payload (error path)

| Library | Time | Memory | Allocs |
|---------|------|--------|--------|
| go-playground/validator | 733 ns | 1.23 KiB | 25 |
| **schemix — `Validate` (FailAll)** | **1.06 µs** | 2.33 KiB | 15 |
| schemix — `ProcessWithMode(FailFast)` | 1.23 µs | 2.19 KiB | 16 |
| ozzo-validation | 2.05 µs | 3.51 KiB | 49 |
| jsonschema v6 | 2.13 µs | 2.99 KiB | 81 |
| raw `cuelang.org/go` API | 16.30 µs | 29.85 KiB | 301 |

`FailFast` is not a shortcut to lower latency — it also builds `Output`, unlike
`Validate`. Choose it to cap error volume, not to save time.

Naming every accepted value and searching for the closest match costs no extra
allocations: the message is assembled in one buffer and the edit-distance row
lives on the stack. Only the message itself is larger than before.

### Nested struct + 3-element array

| Library | Time | Memory | Allocs |
|---------|------|--------|--------|
| go-playground/validator | 1.05 µs | 270 B | 10 |
| jsonschema v6 | 4.35 µs | 5.43 KiB | 133 |
| schemix | 27.35 µs | 37.63 KiB | 432 |

### End-to-end, JSON bytes in

Decoding is charged to every engine — the shape an HTTP handler actually faces.

| Library | Time | Memory | Allocs |
|---------|------|--------|--------|
| go-playground/validator (`json.Unmarshal` + `Struct`) | 1.50 µs | 499 B | 14 |
| **schemix — `ValidateValue([]byte)`** | 1.69 µs | 1.54 KiB | 31 |
| ozzo-validation | 2.52 µs | 2.65 KiB | 45 |
| jsonschema v6 (`UnmarshalJSON` + `Validate`) | 2.82 µs | 3.57 KiB | 80 |

Decoding dominates: schemix's own validation is 382 ns of that 1.69 µs.

### Startup cost, paid once

| Library | Time | Memory | Allocs |
|---------|------|--------|--------|
| go-playground/validator (first struct, cache warm-up) | 10.11 µs | 17.18 KiB | 246 |
| schemix — `New` | 43.97 µs | 90.06 KiB | 765 |
| jsonschema v6 — compile | 65.97 µs | 76.98 KiB | 1101 |

### Parallel, 10 cores

| Library | Time | Memory | Allocs |
|---------|------|--------|--------|
| **schemix** | **~100 ns** | **0 B** | **0** |
| go-playground/validator | 247 ns | 163 B | 6 |
| jsonschema v6 | 785 ns | 2.04 KiB | 56 |

Allocation-free validation never touches the allocator or the GC, which is why
the parallel gap is wider than the single-core one.

## Where the schemix time goes

`cue.Context.Encode` — turning the input map into a `cue.Value` — costs
**~1.67 µs and 39 allocations**. It is **lazy**: it fires only when a field
actually needs a `cue.Value`, so a schema of pure scalar constraints never
triggers it. That is exactly the 39 allocations missing from the scalar rows
above compared to earlier releases (`Process` 125 → 86, `Validate` 121 → 82).

The encode still happens, unavoidably, when any of these appear:

| Trigger | Why |
|---------|-----|
| `@blob()` rule on a **present** field | the field's CUE constraint is verified before the rule runs |
| Nested struct | container navigation uses `LookupPath` |
| Array | the whole list goes to `cue.Value.Unify` |
| Constraint too complex for a descriptor | no `fastConstraint` extracted |
| `uint*` value on an `int` constraint | the fast path returns `Handled=false` and falls back to CUE for exactness |

With the encode gone, pure constraint checking is what remains: `ValidateFields`
runs in **149.0 ns with zero allocations**.

## Arrays: it depends on the element

An array field's cost is decided by what its elements are.

**Elements that are scalars** — `[...string]`, `[...int & >0]`,
`[..."A" | "B"]` — get an element descriptor, and `validateFastElements` applies
it per element in pure Go. No `cue.Value` is built at all:

| Three scalar-element list fields | Time | Allocs |
|----------------------------------|------|--------|
| 1 element each, before the descriptor existed | 22.0 µs | 365 |
| 1 element each | **72.9 ns** | **0** |
| 10 elements each, before | 66.2 µs | 776 |
| 10 elements each | **338 ns** | **0** |

**Elements that are structs** — `[...{…}]` — have no such descriptor, because a
per-element check cannot express a struct's field set. The **entire list** —
every element, and every scalar constraint inside it — is handed to
`cue.Value.Unify`. Struct *fields* are different again: `compileCUEFields`
recurses and gives each scalar child its own descriptor, so nested leaves stay on
the fast path and only the container navigation touches CUE.

Isolating that one variable — the same three constraints (`=~regex`, `int & >0`,
enum), only the container changes:

| Container | Time | Allocs | vs. fast path |
|-----------|------|--------|---------------|
| Scalars at top level (fully fast-pathed) | **105 ns** | **0** | 1x |
| Same scalars inside a nested struct | 3.90 µs | 90 | 37x |
| Same scalars inside an array of **one** struct element | 13.46 µs | 225 | **128x** |

Struct-element array cost is strictly linear in element count, at roughly
**6.5 µs and 78 allocations per element**:

| Elements | 1 | 3 | 10 | 50 | 100 |
|----------|---|---|----|----|-----|
| Time | 13.5 µs | 27.1 µs | 74.2 µs | 342 µs | 639 µs |
| Allocs | 225 | 382 | 926 | 4021 | 7899 |

Struct nesting, by contrast, grows at about **2.1 µs per level** (depth 1 / 3 /
10 → 2.91 / 7.14 / 22.14 µs) — the leaves are fast, the navigation is not.

### Mitigation

This applies to **struct-element** arrays only; scalar-element lists already take
the fast path. Validate large collections element-by-element against a
per-element `Validator` so every element takes the scalar fast path. Measured,
and guarded by [`TestArrayWorkaroundIsEquivalent`](comparison_test.go):

| Elements | Whole list | Per-element `Validator` | Speedup |
|----------|------------|-------------------------|---------|
| 3 | 26.70 µs / 382 allocs | **327 ns / 0 allocs** | **82x** |
| 10 | 72.46 µs / 926 allocs | **1.16 µs / 0 allocs** | **62x** |
| 50 | 358.1 µs / 4021 allocs | **5.77 µs / 0 allocs** | **62x** |

Per-element validation is allocation-free at every size. The tradeoff: you lose
list-level constraints, and per-element error paths are no longer prefixed with
the array index.

Reproduce the breakdown with:

```bash
go test -bench='ContainerShape|ArrayScaling|ArrayWorkaround|StructDepthScaling' \
  -benchmem -run='^$' -count=8 -benchtime=2s
```

## A note on measurement noise

Time values require a quiet machine. On a loaded laptop (load average 26,
browser at 66% CPU) `benchstat` variance reached ±83% and those samples were
discarded. The published time values come from low-load windows and are
cross-checked across runs; `allocs/op` and `B/op` are deterministic (±0%) and
reproducible under any load.
