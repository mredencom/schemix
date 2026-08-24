package schemix

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

// layerScope carries the span and timer for one validation layer. Whichever of
// the two is not configured costs nothing: span stays nil and start stays the
// zero Time, and end() checks both before touching them.
//
// It is a value type closed by an explicit end() rather than a deferred
// closure, because a closure capturing *Validator escapes to the heap and both
// layers open on every Process call.
type layerScope struct {
	v     *Validator
	layer string
	span  trace.Span
	start time.Time
}

// beginLayer opens the observability scope for a layer. spanName is the OTel
// span name, layer the metrics label — LayerCUE or LayerBlob.
func (v *Validator) beginLayer(ctx context.Context, spanName, layer string) layerScope {
	s := layerScope{v: v, layer: layer}
	// Order matters: with no tracer configured, ctx is never inspected. That is
	// what keeps a ProcessContext(nil, …) call from panicking in SpanFromContext
	// on the untraced path, and it preserves the original short-circuit.
	if v.tracer != nil && trace.SpanFromContext(ctx).IsRecording() {
		_, s.span = v.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	}
	if v.metrics != nil {
		s.start = time.Now()
	}
	return s
}

// end reports the layer duration and closes the span.
func (s layerScope) end() {
	if s.v.metrics != nil {
		s.v.metrics.ObserveLayerDuration(s.layer, time.Since(s.start), s.v.schemaName)
	}
	if s.span != nil {
		s.span.End()
	}
}

// Layer constants identify validation layers in ObserveLayerDuration calls.
const (
	LayerCUE  = "cue"
	LayerBlob = "blob"
)

// MetricsRecorder receives observability events from a Validator during
// Process/Validate execution. Implementations must be safe for concurrent
// use, since a single Validator may be shared across goroutines after
// construction.
//
// All methods MUST return quickly (non-blocking). Slow implementations
// (e.g. synchronous network calls) will directly add latency to every
// Process/Validate call. Buffer and batch asynchronously if needed.
//
// When no MetricsRecorder is configured (the default), Validator does not
// invoke any of these methods and incurs zero related overhead — Process
// and Validate behave exactly as they did before this feature existed.
type MetricsRecorder interface {
	// ObserveValidation is called once per Process/Validate call, after the
	// result is finalized. duration excludes recorder overhead itself.
	// schemaName is the name set via WithName (empty if unset).
	ObserveValidation(duration time.Duration, valid bool, schemaName string)

	// ObserveFastpathDecision is called once per field that has a compiled
	// Go-native fast constraint, reporting whether the fast path successfully
	// handled the field (hit=true) or fell back to the CUE path (hit=false).
	//
	// FailFast is the exception: the field walk ends at the first failure, so
	// only the fields actually visited are reported. Counting these calls to
	// infer a schema's field count is therefore safe under FailAll and
	// FailPriority but not under FailFast.
	ObserveFastpathDecision(fieldPath string, hit bool)

	// ObserveErrorCode is called once per validation error in the result,
	// allowing implementations to maintain per-code counters.
	ObserveErrorCode(code ErrorCode, schemaName string)

	// ObserveBlobExecution is called once per @blob rule execution, reporting
	// the field path, execution duration, and whether the expression succeeded.
	ObserveBlobExecution(fieldPath string, duration time.Duration, success bool)

	// ObserveLayerDuration is called once per validation layer (CUE, Blob),
	// reporting the total time spent in that layer for a single Process call.
	ObserveLayerDuration(layer string, duration time.Duration, schemaName string)
}

// WithMetricsRecorder attaches a MetricsRecorder to the Validator. When unset
// (default), all observability code paths are skipped entirely — Process and
// Validate incur zero additional overhead.
//
// Example:
//
//	v, _ := schemix.New(schema, schemix.WithMetricsRecorder(myPromRecorder))
func WithMetricsRecorder(r MetricsRecorder) Option {
	return func(cfg *validatorConfig) {
		cfg.metricsRecorder = r
	}
}

// WithName sets a schema name for observability labels. This name is passed
// to MetricsRecorder methods (e.g. ObserveValidation, ObserveLayerDuration)
// as the schemaName parameter, enabling per-schema metric slicing.
//
// Example:
//
//	v, _ := schemix.New(schema, schemix.WithName("payment"))
func WithName(name string) Option {
	return func(cfg *validatorConfig) {
		cfg.schemaName = name
	}
}

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
