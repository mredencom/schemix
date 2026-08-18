package schemix

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// withValidationMetrics runs fn and, if a MetricsRecorder is configured,
// times the call and reports (duration, valid, schemaName) via ObserveValidation.
// It also reports each error code via ObserveErrorCode.
// When no recorder is configured, fn runs with zero added overhead — no
// timer is started and no interface call is made.
func (v *Validator) withValidationMetrics(fn func() Result) Result {
	if v.metrics == nil {
		return fn()
	}
	start := time.Now()
	result := fn()
	v.metrics.ObserveValidation(time.Since(start), result.Valid, v.schemaName)
	for i := range result.Errors {
		v.metrics.ObserveErrorCode(result.Errors[i].Code, v.schemaName)
	}
	return result
}

// withValidationMetricsCtx is the context-aware counterpart of withValidationMetrics.
// It handles BOTH metrics recording AND root span creation/finalization for context-aware methods.
// When neither metrics nor tracer is configured, fn runs with zero added overhead.
func (v *Validator) withValidationMetricsCtx(ctx context.Context, mode FailMode, fn func(context.Context) Result) Result {
	if v.metrics == nil && v.tracer == nil {
		return fn(ctx)
	}

	// Start root span if tracer is configured.
	var span trace.Span
	if v.tracer != nil {
		ctx, span = v.tracer.Start(ctx, "schemix.process", trace.WithSpanKind(trace.SpanKindInternal))
	}

	// Start timer if metrics are configured.
	var start time.Time
	if v.metrics != nil {
		start = time.Now()
	}

	result := fn(ctx)

	// Report metrics.
	if v.metrics != nil {
		v.metrics.ObserveValidation(time.Since(start), result.Valid, v.schemaName)
		for i := range result.Errors {
			v.metrics.ObserveErrorCode(result.Errors[i].Code, v.schemaName)
		}
	}

	// Report tracing.
	if span != nil {
		recordSpanResult(span, &result, v.schemaName, mode, len(v.cueFields))
		span.End()
	}

	return result
}
