package schemix

import "time"

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
	ObserveValidation(duration time.Duration, valid bool)

	// ObserveFastpathDecision is called once per field that has a compiled
	// Go-native fast constraint, reporting whether the fast path successfully
	// handled the field (hit=true) or fell back to the CUE path (hit=false).
	ObserveFastpathDecision(fieldPath string, hit bool)
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
