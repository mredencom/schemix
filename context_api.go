package schemix

import "context"

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
