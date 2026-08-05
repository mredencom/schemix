package schemix

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTel instrumentation scope identifiers.
const (
	ScopeName    = "github.com/mredencom/schemix"
	ScopeVersion = "0.2.0"
)

// maxSpanEvents caps validation_error events per root span.
const maxSpanEvents = 20

// WithTracerProvider configures distributed tracing. Context-aware methods
// (ProcessContext, ValidateContext, etc.) will create spans.
// When nil or unset, v.tracer remains nil — zero overhead on non-context paths.
// Users who want global OTel fallback should pass otel.GetTracerProvider() explicitly.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(cfg *validatorConfig) {
		cfg.tracerProvider = tp
	}
}

// buildTracer creates a Tracer from provider. Returns nil if provider is nil.
func buildTracer(tp trace.TracerProvider) trace.Tracer {
	if tp == nil {
		return nil
	}
	return tp.Tracer(ScopeName, trace.WithInstrumentationVersion(ScopeVersion))
}

// failModeString converts FailMode to a label string.
func failModeString(mode FailMode) string {
	switch mode {
	case FailAll:
		return "all"
	case FailFast:
		return "fast"
	case FailPriority:
		return "priority"
	default:
		return "unknown"
	}
}

// recordSpanResult sets root span attributes and events after processing.
func recordSpanResult(span trace.Span, result *Result, schemaName string, mode FailMode, fieldCount int) {
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(
		attribute.String("schemix.schema_name", schemaName),
		attribute.String("schemix.fail_mode", failModeString(mode)),
		attribute.Bool("schemix.valid", result.Valid),
		attribute.Int("schemix.error_count", len(result.Errors)),
		attribute.Int("schemix.field_count", fieldCount),
	)
	if !result.Valid {
		span.SetStatus(codes.Error, "validation failed")
		limit := len(result.Errors)
		if limit > maxSpanEvents {
			limit = maxSpanEvents
		}
		for i := 0; i < limit; i++ {
			e := &result.Errors[i]
			span.AddEvent("validation_error", trace.WithAttributes(
				attribute.String("schemix.error.code", string(e.Code)),
				attribute.String("schemix.error.path", e.Path),
				attribute.String("schemix.error.type", e.Type),
			))
		}
	}
}
