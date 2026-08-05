// Package schemixotel provides an OpenTelemetry Metrics implementation of
// [schemix.MetricsRecorder]. It records validation duration, error codes,
// blob execution, layer timing, and fast-path decisions as OTel instruments.
//
// Usage:
//
//	rec, err := schemixotel.New(meterProvider)
//	v, _ := schemix.New(schema, schemix.WithMetricsRecorder(rec))
package schemixotel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mredencom/schemix"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Recorder implements schemix.MetricsRecorder using OpenTelemetry Metrics.
type Recorder struct {
	validationDuration metric.Float64Histogram
	validationTotal    metric.Int64Counter
	errorTotal         metric.Int64Counter
	blobDuration       metric.Float64Histogram
	blobTotal          metric.Int64Counter
	layerDuration      metric.Float64Histogram
	fastpathTotal      metric.Int64Counter
}

// New creates a Recorder backed by the given MeterProvider.
// Returns an error if mp is nil or instrument creation fails.
func New(mp metric.MeterProvider, opts ...Option) (*Recorder, error) {
	if mp == nil {
		return nil, errors.New("schemixotel: nil MeterProvider")
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	meterName := schemix.ScopeName
	if cfg.meterName != "" {
		meterName = cfg.meterName
	}
	meter := mp.Meter(meterName,
		metric.WithInstrumentationVersion(schemix.ScopeVersion),
	)

	r := &Recorder{}
	var err error

	r.validationDuration, err = meter.Float64Histogram("schemix.validation.duration",
		metric.WithDescription("Time spent in Process/Validate calls"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create validation.duration: %w", err)
	}

	r.validationTotal, err = meter.Int64Counter("schemix.validation.total",
		metric.WithDescription("Total number of Process/Validate calls"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create validation.total: %w", err)
	}

	r.errorTotal, err = meter.Int64Counter("schemix.error.total",
		metric.WithDescription("Total validation errors by code"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create error.total: %w", err)
	}

	r.blobDuration, err = meter.Float64Histogram("schemix.blob.duration",
		metric.WithDescription("Time spent executing individual @blob expressions"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create blob.duration: %w", err)
	}

	r.blobTotal, err = meter.Int64Counter("schemix.blob.total",
		metric.WithDescription("Total @blob executions"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create blob.total: %w", err)
	}

	r.layerDuration, err = meter.Float64Histogram("schemix.layer.duration",
		metric.WithDescription("Time spent in each validation layer"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create layer.duration: %w", err)
	}

	r.fastpathTotal, err = meter.Int64Counter("schemix.fastpath.total",
		metric.WithDescription("Fastpath hit/miss decisions"),
	)
	if err != nil {
		return nil, fmt.Errorf("schemixotel: create fastpath.total: %w", err)
	}

	return r, nil
}

func (r *Recorder) ObserveValidation(d time.Duration, valid bool, schemaName string) {
	ctx := context.Background()
	attrs := metric.WithAttributes(
		attribute.String("schema", schemaName),
		attribute.Bool("valid", valid),
	)
	r.validationDuration.Record(ctx, d.Seconds(), attrs)
	r.validationTotal.Add(ctx, 1, attrs)
}

func (r *Recorder) ObserveErrorCode(code schemix.ErrorCode, schemaName string) {
	ctx := context.Background()
	r.errorTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("schema", schemaName),
		attribute.String("code", string(code)),
	))
}

func (r *Recorder) ObserveBlobExecution(fieldPath string, d time.Duration, success bool) {
	ctx := context.Background()
	attrs := metric.WithAttributes(
		attribute.String("field", fieldPath),
		attribute.Bool("success", success),
	)
	r.blobDuration.Record(ctx, d.Seconds(), attrs)
	r.blobTotal.Add(ctx, 1, attrs)
}

func (r *Recorder) ObserveLayerDuration(layer string, d time.Duration, schemaName string) {
	ctx := context.Background()
	r.layerDuration.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("schema", schemaName),
		attribute.String("layer", layer),
	))
}

func (r *Recorder) ObserveFastpathDecision(fieldPath string, hit bool) {
	ctx := context.Background()
	result := "miss"
	if hit {
		result = "hit"
	}
	r.fastpathTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("field", fieldPath),
		attribute.String("result", result),
	))
}

// Compile-time interface check.
var _ schemix.MetricsRecorder = (*Recorder)(nil)
