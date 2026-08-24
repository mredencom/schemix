package schemix

import (
	"context"
)

// Validate performs validation only and returns (valid, errors).
// Unlike Process, it skips deepCopy and Output construction for better performance.
func (v *Validator) Validate(data map[string]any) (bool, []ValidationError) {
	r := v.withValidationMetrics(func() Result {
		return v.processInternal(context.Background(), data, FailAll, false)
	})
	return r.Valid, r.Errors
}

// Process performs validation and value computation using the default FailAll mode.
func (v *Validator) Process(data map[string]any) Result {
	return v.ProcessWithMode(data, FailAll)
}

// ProcessWithMode performs validation and value computation with the specified FailMode.
func (v *Validator) ProcessWithMode(data map[string]any, mode FailMode) Result {
	if err := validateFailMode(mode); err != nil {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}},
			v:      v,
		}
	}
	return v.withValidationMetrics(func() Result {
		return v.processInternal(context.Background(), data, mode, true)
	})
}

// ─── context Input API ──────────────────────────────────────────────────────

// ProcessContext performs validation and value computation with context
// propagation for distributed tracing. Uses FailAll mode.
func (v *Validator) ProcessContext(ctx context.Context, data map[string]any) Result {
	return v.ProcessWithModeContext(ctx, data, FailAll)
}

// ProcessWithModeContext performs validation with the specified FailMode
// and context propagation for distributed tracing.
func (v *Validator) ProcessWithModeContext(ctx context.Context, data map[string]any, mode FailMode) Result {
	if err := validateFailMode(mode); err != nil {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}},
			v:      v,
		}
	}
	return v.withValidationMetricsCtx(ctx, mode, func(ctx context.Context) Result {
		return v.processInternal(ctx, data, mode, true)
	})
}

// ValidateContext performs validation only (no Output) with context propagation
// for distributed tracing. Uses FailAll mode.
func (v *Validator) ValidateContext(ctx context.Context, data map[string]any) (bool, []ValidationError) {
	r := v.withValidationMetricsCtx(ctx, FailAll, func(ctx context.Context) Result {
		return v.processInternal(ctx, data, FailAll, false)
	})
	return r.Valid, r.Errors
}

// ProcessValueContext validates any supported input type with context propagation.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
func (v *Validator) ProcessValueContext(ctx context.Context, data any) Result {
	return v.ProcessValueWithModeContext(ctx, data, FailAll)
}

// ProcessValueWithModeContext validates any supported input type with the
// specified FailMode and context propagation for distributed tracing.
func (v *Validator) ProcessValueWithModeContext(ctx context.Context, data any, mode FailMode) Result {
	m, err := toMapAny(data)
	if err != nil {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}},
			v:      v,
		}
	}
	return v.ProcessWithModeContext(ctx, m, mode)
}

// ValidateValueContext performs validation (no Output) on any supported input
// type with context propagation for distributed tracing.
func (v *Validator) ValidateValueContext(ctx context.Context, data any) (bool, []ValidationError) {
	m, err := toMapAny(data)
	if err != nil {
		return false, []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}}
	}
	return v.ValidateContext(ctx, m)
}

// ─── Flexible Input API ──────────────────────────────────────────────────────

// ProcessValue validates and processes any supported input type.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
// Returns a config error Result if the input type is unsupported or conversion fails.
func (v *Validator) ProcessValue(data any) Result {
	return v.ProcessValueWithMode(data, FailAll)
}

// ProcessValueWithMode validates and processes any supported input type with the given FailMode.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
func (v *Validator) ProcessValueWithMode(data any, mode FailMode) Result {
	m, err := toMapAny(data)
	if err != nil {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}},
			v:      v,
		}
	}
	return v.ProcessWithMode(m, mode)
}

// ValidateValue performs validation (no Output) on any supported input type.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
func (v *Validator) ValidateValue(data any) (bool, []ValidationError) {
	m, err := toMapAny(data)
	if err != nil {
		return false, []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}}
	}
	return v.Validate(m)
}

// ProcessStruct validates and processes a struct value with compile-time type safety.
// The struct is converted to map[string]any via JSON serialization (respects json tags).
// For hot paths, implement Processable or pass map[string]any directly.
func ProcessStruct[T any](v *Validator, data T) Result {
	return v.ProcessValue(data)
}

// ProcessStructWithMode is like ProcessStruct but accepts a FailMode.
func ProcessStructWithMode[T any](v *Validator, data T, mode FailMode) Result {
	return v.ProcessValueWithMode(data, mode)
}

// ValidateStruct validates a struct value with compile-time type safety (no Output).
func ValidateStruct[T any](v *Validator, data T) (bool, []ValidationError) {
	return v.ValidateValue(data)
}
