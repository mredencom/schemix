package schemix

import "time"

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
