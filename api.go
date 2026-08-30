package schemix

import (
	"context"
)

// CallOption configures a single Process or Validate call.
//
// FailMode implements it, so a mode is passed directly with no wrapper:
//
//	r := v.Process(data, schemix.FailFast)
//
// The interface method is unexported, which makes this a closed set: per-call
// behaviour is the library's contract rather than an extension point. When
// several options are given, the last one wins.
type CallOption interface {
	applyCall(callConfig) callConfig
}

// callConfig is the resolved per-call configuration.
//
// Options take and return it by value. Handing out its address instead — via a
// closure or an interface taking *callConfig — makes the compiler assume the
// pointer may be retained, which moves the struct to the heap and costs an
// allocation on every call, including calls that pass no options at all.
type callConfig struct {
	mode FailMode
}

// applyCall makes FailMode usable as a CallOption.
func (m FailMode) applyCall(c callConfig) callConfig {
	c.mode = m
	return c
}

// resolveCall folds opts onto the default FailAll.
func resolveCall(opts []CallOption) callConfig {
	cfg := callConfig{mode: FailAll}
	for _, o := range opts {
		cfg = o.applyCall(cfg)
	}
	return cfg
}

// ─── primary entry points ────────────────────────────────────────────────────

// Validate performs validation only and returns (valid, errors).
// Unlike Process, it skips deepCopy and Output construction for better performance.
//
// data may be a map[string]any, a struct, a *struct, JSON bytes, or a
// Processable. Anything else fails with CodeConfigError naming the type it got.
//
// The FailMode defaults to FailAll and is selected per call:
//
//	valid, errs := v.Validate(data)                  // collect every error
//	valid, errs := v.Validate(data, schemix.FailFast) // stop at the first
func (v *Validator) Validate(data any, opts ...CallOption) (bool, []ValidationError) {
	m, err := toMapAny(data)
	if err != nil {
		return false, configErrors(err)
	}
	return v.validateMap(m, resolveCall(opts).mode)
}

// Process performs validation and value computation.
//
// data may be a map[string]any, a struct, a *struct, JSON bytes, or a
// Processable. Anything else fails with CodeConfigError naming the type it got.
//
// Prefer handing in raw JSON bytes over a map you decoded yourself:
// json.Unmarshal turns every number into a float64, and CUE keeps int and float
// as sibling types, so an `int` field rejects a decoded 28.
//
// The FailMode defaults to FailAll and is selected per call:
//
//	r := v.Process(data)                  // collect every error
//	r := v.Process(data, schemix.FailFast) // stop at the first
func (v *Validator) Process(data any, opts ...CallOption) Result {
	m, err := toMapAny(data)
	if err != nil {
		return v.configErrorResult(err)
	}
	return v.processMap(m, resolveCall(opts).mode)
}

// ProcessContext performs validation and value computation with context
// propagation for distributed tracing. It accepts the same input types as
// Process.
func (v *Validator) ProcessContext(ctx context.Context, data any, opts ...CallOption) Result {
	m, err := toMapAny(data)
	if err != nil {
		return v.configErrorResult(err)
	}
	return v.processMapCtx(ctx, m, resolveCall(opts).mode)
}

// ValidateContext performs validation only (no Output) with context propagation
// for distributed tracing. It accepts the same input types as Process.
func (v *Validator) ValidateContext(ctx context.Context, data any, opts ...CallOption) (bool, []ValidationError) {
	m, err := toMapAny(data)
	if err != nil {
		return false, configErrors(err)
	}
	return v.validateMapCtx(ctx, m, resolveCall(opts).mode)
}

// ─── unexported implementation ───────────────────────────────────────────────
//
// Every exported entry point routes through these, so the deprecated shells
// below can be deleted without untangling internal callers.

func (v *Validator) processMap(data map[string]any, mode FailMode) Result {
	if err := validateFailMode(mode); err != nil {
		return v.configErrorResult(err)
	}
	return v.withValidationMetrics(func() Result {
		return v.processInternal(context.Background(), data, mode, true)
	})
}

func (v *Validator) processMapCtx(ctx context.Context, data map[string]any, mode FailMode) Result {
	if err := validateFailMode(mode); err != nil {
		return v.configErrorResult(err)
	}
	return v.withValidationMetricsCtx(ctx, mode, func(ctx context.Context) Result {
		return v.processInternal(ctx, data, mode, true)
	})
}

func (v *Validator) validateMap(data map[string]any, mode FailMode) (bool, []ValidationError) {
	if err := validateFailMode(mode); err != nil {
		return false, configErrors(err)
	}
	r := v.withValidationMetrics(func() Result {
		return v.processInternal(context.Background(), data, mode, false)
	})
	return r.Valid, r.Errors
}

func (v *Validator) validateMapCtx(ctx context.Context, data map[string]any, mode FailMode) (bool, []ValidationError) {
	if err := validateFailMode(mode); err != nil {
		return false, configErrors(err)
	}
	r := v.withValidationMetricsCtx(ctx, mode, func(ctx context.Context) Result {
		return v.processInternal(ctx, data, mode, false)
	})
	return r.Valid, r.Errors
}

// configErrors builds the error slice for a rejected configuration.
//
// Kept out of the callers so the cheap validateFailMode check stays inlinable:
// a helper returning a Result cannot be inlined, which would put a real call on
// every successful validation just to decide it had nothing to report.
func configErrors(err error) []ValidationError {
	return []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}}
}

// configErrorResult wraps configErrors in an invalid Result.
func (v *Validator) configErrorResult(err error) Result {
	return Result{Valid: false, Errors: configErrors(err), v: v}
}

// convertOrReject turns any supported input into a map, or reports why it could not.
func (v *Validator) convertOrReject(data any) (map[string]any, Result, bool) {
	m, err := toMapAny(data)
	if err != nil {
		return nil, v.configErrorResult(err), true
	}
	return m, Result{}, false
}

// ─── deprecated: superseded by the four entry points above ───────────────────

// ProcessWithMode performs validation and value computation with the specified FailMode.
//
// Deprecated: Pass the mode to Process instead — v.Process(data, mode). This
// method is removed in v0.3.0.
func (v *Validator) ProcessWithMode(data map[string]any, mode FailMode) Result {
	return v.processMap(data, mode)
}

// ProcessWithModeContext performs validation with the specified FailMode
// and context propagation for distributed tracing.
//
// Deprecated: Pass the mode to ProcessContext instead —
// v.ProcessContext(ctx, data, mode). This method is removed in v0.3.0.
func (v *Validator) ProcessWithModeContext(ctx context.Context, data map[string]any, mode FailMode) Result {
	return v.processMapCtx(ctx, data, mode)
}

// ProcessValue validates and processes any supported input type.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
//
// Deprecated: Process accepts every supported input type from v0.3.0, where
// this method is removed. Until then, keep using it for non-map input.
func (v *Validator) ProcessValue(data any) Result {
	return v.ProcessValueWithMode(data, FailAll)
}

// ProcessValueWithMode validates and processes any supported input type with the given FailMode.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
//
// Deprecated: Process accepts every supported input type and a mode from
// v0.3.0 — v.Process(data, mode) — where this method is removed.
func (v *Validator) ProcessValueWithMode(data any, mode FailMode) Result {
	m, bad, rejected := v.convertOrReject(data)
	if rejected {
		return bad
	}
	return v.processMap(m, mode)
}

// ProcessValueContext validates any supported input type with context propagation.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
//
// Deprecated: ProcessContext accepts every supported input type from v0.3.0,
// where this method is removed.
func (v *Validator) ProcessValueContext(ctx context.Context, data any) Result {
	return v.ProcessValueWithModeContext(ctx, data, FailAll)
}

// ProcessValueWithModeContext validates any supported input type with the
// specified FailMode and context propagation for distributed tracing.
//
// Deprecated: ProcessContext accepts every supported input type and a mode
// from v0.3.0 — v.ProcessContext(ctx, data, mode) — where this method is removed.
func (v *Validator) ProcessValueWithModeContext(ctx context.Context, data any, mode FailMode) Result {
	m, bad, rejected := v.convertOrReject(data)
	if rejected {
		return bad
	}
	return v.processMapCtx(ctx, m, mode)
}

// ValidateValue performs validation (no Output) on any supported input type.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
//
// Deprecated: Validate accepts every supported input type from v0.3.0, where
// this method is removed.
func (v *Validator) ValidateValue(data any) (bool, []ValidationError) {
	m, bad, rejected := v.convertOrReject(data)
	if rejected {
		return false, bad.Errors
	}
	return v.validateMap(m, FailAll)
}

// ValidateValueContext performs validation (no Output) on any supported input
// type with context propagation for distributed tracing.
//
// Deprecated: ValidateContext accepts every supported input type from v0.3.0,
// where this method is removed.
func (v *Validator) ValidateValueContext(ctx context.Context, data any) (bool, []ValidationError) {
	m, bad, rejected := v.convertOrReject(data)
	if rejected {
		return false, bad.Errors
	}
	return v.validateMapCtx(ctx, m, FailAll)
}

// ProcessStruct validates and processes a struct value.
//
// Deprecated: the type parameter constrains nothing — T is any, so
// ProcessStruct(v, 42) compiles and fails at runtime. Call v.Process directly;
// this function is removed in v0.3.0.
func ProcessStruct[T any](v *Validator, data T) Result {
	return v.ProcessValueWithMode(data, FailAll)
}

// ProcessStructWithMode is like ProcessStruct but accepts a FailMode.
//
// Deprecated: the type parameter constrains nothing. Call v.Process directly;
// this function is removed in v0.3.0.
func ProcessStructWithMode[T any](v *Validator, data T, mode FailMode) Result {
	return v.ProcessValueWithMode(data, mode)
}

// ValidateStruct validates a struct value (no Output).
//
// Deprecated: the type parameter constrains nothing. Call v.Validate directly;
// this function is removed in v0.3.0.
func ValidateStruct[T any](v *Validator, data T) (bool, []ValidationError) {
	m, bad, rejected := v.convertOrReject(data)
	if rejected {
		return false, bad.Errors
	}
	return v.validateMap(m, FailAll)
}
