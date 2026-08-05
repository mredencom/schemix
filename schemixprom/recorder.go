// Package schemixprom provides a Prometheus implementation of
// [schemix.MetricsRecorder]. It registers histograms and counters for
// validation duration, error codes, blob execution, layer timing, and
// fast-path decisions using prometheus/client_golang.
//
// Usage:
//
//	rec, err := schemixprom.New(prometheus.DefaultRegisterer)
//	v, _ := schemix.New(schema, schemix.WithMetricsRecorder(rec))
package schemixprom

import (
	"errors"
	"fmt"
	"time"

	"github.com/mredencom/schemix"
	"github.com/prometheus/client_golang/prometheus"
)

// Recorder implements schemix.MetricsRecorder using Prometheus metrics.
type Recorder struct {
	validationDuration *prometheus.HistogramVec
	validationTotal    *prometheus.CounterVec
	errorTotal         *prometheus.CounterVec
	blobDuration       *prometheus.HistogramVec
	blobTotal          *prometheus.CounterVec
	layerDuration      *prometheus.HistogramVec
	fastpathTotal      *prometheus.CounterVec
}

// New creates a Recorder and registers all metrics with the given Registerer.
// Returns an error if reg is nil or metric registration fails.
func New(reg prometheus.Registerer, opts ...Option) (*Recorder, error) {
	if reg == nil {
		return nil, errors.New("schemixprom: nil Registerer")
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	ns := cfg.namespace
	sub := "schemix"

	r := &Recorder{
		validationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "validation_duration_seconds",
			Help:      "Time spent in Process/Validate calls.",
			Buckets:   cfg.durationBuckets,
		}, []string{"schema", "valid"}),

		validationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "validations_total",
			Help:      "Total number of Process/Validate calls.",
		}, []string{"schema", "valid"}),

		errorTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "errors_total",
			Help:      "Total validation errors by code.",
		}, []string{"schema", "code"}),

		blobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "blob_duration_seconds",
			Help:      "Time spent executing individual @blob expressions.",
			Buckets:   cfg.blobBuckets,
		}, []string{"field", "success"}),

		blobTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "blob_executions_total",
			Help:      "Total @blob executions.",
		}, []string{"field", "success"}),

		layerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "layer_duration_seconds",
			Help:      "Time spent in each validation layer.",
			Buckets:   cfg.durationBuckets,
		}, []string{"schema", "layer"}),

		fastpathTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "fastpath_decisions_total",
			Help:      "Fastpath hit/miss decisions.",
		}, []string{"field", "result"}),
	}

	collectors := []prometheus.Collector{
		r.validationDuration, r.validationTotal, r.errorTotal,
		r.blobDuration, r.blobTotal, r.layerDuration, r.fastpathTotal,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("schemixprom: register metric: %w", err)
		}
	}

	return r, nil
}

func (r *Recorder) ObserveValidation(d time.Duration, valid bool, schemaName string) {
	v := "false"
	if valid {
		v = "true"
	}
	r.validationDuration.WithLabelValues(schemaName, v).Observe(d.Seconds())
	r.validationTotal.WithLabelValues(schemaName, v).Inc()
}

func (r *Recorder) ObserveErrorCode(code schemix.ErrorCode, schemaName string) {
	r.errorTotal.WithLabelValues(schemaName, string(code)).Inc()
}

func (r *Recorder) ObserveBlobExecution(fieldPath string, d time.Duration, success bool) {
	s := "false"
	if success {
		s = "true"
	}
	r.blobDuration.WithLabelValues(fieldPath, s).Observe(d.Seconds())
	r.blobTotal.WithLabelValues(fieldPath, s).Inc()
}

func (r *Recorder) ObserveLayerDuration(layer string, d time.Duration, schemaName string) {
	r.layerDuration.WithLabelValues(schemaName, layer).Observe(d.Seconds())
}

func (r *Recorder) ObserveFastpathDecision(fieldPath string, hit bool) {
	result := "miss"
	if hit {
		result = "hit"
	}
	r.fastpathTotal.WithLabelValues(fieldPath, result).Inc()
}

// Compile-time interface assertion.
var _ schemix.MetricsRecorder = (*Recorder)(nil)
