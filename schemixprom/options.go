package schemixprom

import "github.com/prometheus/client_golang/prometheus"

type config struct {
	namespace       string
	durationBuckets []float64
	blobBuckets     []float64
}

// Option configures the Recorder.
type Option func(*config)

func defaultConfig() *config {
	return &config{
		durationBuckets: prometheus.DefBuckets,
		blobBuckets:     []float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025},
	}
}

// WithNamespace sets the Prometheus metric namespace prefix.
func WithNamespace(ns string) Option {
	return func(cfg *config) {
		cfg.namespace = ns
	}
}

// WithDurationBuckets sets custom histogram buckets for validation/layer duration.
func WithDurationBuckets(b []float64) Option {
	return func(cfg *config) {
		cfg.durationBuckets = b
	}
}

// WithBlobBuckets sets custom histogram buckets for blob execution duration.
func WithBlobBuckets(b []float64) Option {
	return func(cfg *config) {
		cfg.blobBuckets = b
	}
}
