package schemixprom_test

import (
	"sync"
	"testing"
	"time"

	"github.com/mredencom/schemix"
	"github.com/mredencom/schemix/schemixprom"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- Test helpers ---

func findMetricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, f := range families {
		if f.GetName() == name {
			return f
		}
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}

func findCounterValue(t *testing.T, family *dto.MetricFamily, labels map[string]string) float64 {
	t.Helper()
	for _, m := range family.GetMetric() {
		if matchLabels(m.GetLabel(), labels) {
			return m.GetCounter().GetValue()
		}
	}
	t.Fatalf("metric with labels %v not found in %s", labels, family.GetName())
	return 0
}

func findHistogramCount(t *testing.T, family *dto.MetricFamily, labels map[string]string) uint64 {
	t.Helper()
	for _, m := range family.GetMetric() {
		if matchLabels(m.GetLabel(), labels) {
			return m.GetHistogram().GetSampleCount()
		}
	}
	t.Fatalf("histogram metric with labels %v not found in %s", labels, family.GetName())
	return 0
}

func matchLabels(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	for _, p := range pairs {
		if want[p.GetName()] != p.GetValue() {
			return false
		}
	}
	return true
}

// --- Tests ---

func TestNew_NilRegisterer_Error(t *testing.T) {
	rec, err := schemixprom.New(nil)
	if err == nil {
		t.Fatal("expected error for nil Registerer, got nil")
	}
	if rec != nil {
		t.Fatal("expected nil Recorder for nil Registerer")
	}
	if err.Error() != "schemixprom: nil Registerer" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestNew_Success(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil Recorder")
	}
}

func TestRecorder_ObserveValidation(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveValidation(5*time.Millisecond, true, "payment")
	rec.ObserveValidation(10*time.Millisecond, false, "payment")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	// Check histogram
	hf := findMetricFamily(t, families, "schemix_validation_duration_seconds")
	count := findHistogramCount(t, hf, map[string]string{"schema": "payment", "valid": "true"})
	if count != 1 {
		t.Fatalf("expected 1 histogram sample for valid=true, got %d", count)
	}
	count = findHistogramCount(t, hf, map[string]string{"schema": "payment", "valid": "false"})
	if count != 1 {
		t.Fatalf("expected 1 histogram sample for valid=false, got %d", count)
	}

	// Check counter
	cf := findMetricFamily(t, families, "schemix_validations_total")
	val := findCounterValue(t, cf, map[string]string{"schema": "payment", "valid": "true"})
	if val != 1 {
		t.Fatalf("expected counter 1 for valid=true, got %f", val)
	}
	val = findCounterValue(t, cf, map[string]string{"schema": "payment", "valid": "false"})
	if val != 1 {
		t.Fatalf("expected counter 1 for valid=false, got %f", val)
	}
}

func TestRecorder_ObserveErrorCode(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveErrorCode(schemix.CodeTypeMismatch, "user")
	rec.ObserveErrorCode(schemix.CodeTypeMismatch, "user")
	rec.ObserveErrorCode(schemix.CodeEnumInvalid, "user")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	cf := findMetricFamily(t, families, "schemix_errors_total")

	val := findCounterValue(t, cf, map[string]string{"schema": "user", "code": string(schemix.CodeTypeMismatch)})
	if val != 2 {
		t.Fatalf("expected 2 for CodeTypeMismatch, got %f", val)
	}

	val = findCounterValue(t, cf, map[string]string{"schema": "user", "code": string(schemix.CodeEnumInvalid)})
	if val != 1 {
		t.Fatalf("expected 1 for CodeEnumInvalid, got %f", val)
	}
}

func TestRecorder_ObserveBlobExecution(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveBlobExecution("pan.luhn_check", 100*time.Microsecond, true)
	rec.ObserveBlobExecution("pan.luhn_check", 200*time.Microsecond, false)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	// Histogram
	hf := findMetricFamily(t, families, "schemix_blob_duration_seconds")
	count := findHistogramCount(t, hf, map[string]string{"field": "pan.luhn_check", "success": "true"})
	if count != 1 {
		t.Fatalf("expected 1 histogram sample for success=true, got %d", count)
	}
	count = findHistogramCount(t, hf, map[string]string{"field": "pan.luhn_check", "success": "false"})
	if count != 1 {
		t.Fatalf("expected 1 histogram sample for success=false, got %d", count)
	}

	// Counter
	cf := findMetricFamily(t, families, "schemix_blob_executions_total")
	val := findCounterValue(t, cf, map[string]string{"field": "pan.luhn_check", "success": "true"})
	if val != 1 {
		t.Fatalf("expected counter 1 for success=true, got %f", val)
	}
	val = findCounterValue(t, cf, map[string]string{"field": "pan.luhn_check", "success": "false"})
	if val != 1 {
		t.Fatalf("expected counter 1 for success=false, got %f", val)
	}
}

func TestRecorder_ObserveLayerDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveLayerDuration("cue", 3*time.Millisecond, "payment")
	rec.ObserveLayerDuration("blob", 2*time.Millisecond, "payment")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	hf := findMetricFamily(t, families, "schemix_layer_duration_seconds")
	count := findHistogramCount(t, hf, map[string]string{"schema": "payment", "layer": "cue"})
	if count != 1 {
		t.Fatalf("expected 1 histogram sample for layer=cue, got %d", count)
	}
	count = findHistogramCount(t, hf, map[string]string{"schema": "payment", "layer": "blob"})
	if count != 1 {
		t.Fatalf("expected 1 histogram sample for layer=blob, got %d", count)
	}
}

func TestRecorder_ObserveFastpathDecision(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveFastpathDecision("name", true)
	rec.ObserveFastpathDecision("name", true)
	rec.ObserveFastpathDecision("age", false)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	cf := findMetricFamily(t, families, "schemix_fastpath_decisions_total")

	val := findCounterValue(t, cf, map[string]string{"field": "name", "result": "hit"})
	if val != 2 {
		t.Fatalf("expected 2 for name/hit, got %f", val)
	}

	val = findCounterValue(t, cf, map[string]string{"field": "age", "result": "miss"})
	if val != 1 {
		t.Fatalf("expected 1 for age/miss, got %f", val)
	}
}

func TestRecorder_InterfaceSatisfaction(t *testing.T) {
	// This is primarily a compile-time check (var _ in recorder.go).
	// This test additionally verifies at runtime.
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var iface schemix.MetricsRecorder = rec
	if iface == nil {
		t.Fatal("Recorder does not satisfy MetricsRecorder interface")
	}
}

func TestRecorder_Concurrent(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rec.ObserveValidation(time.Duration(n)*time.Microsecond, n%2 == 0, "concurrent")
			rec.ObserveFastpathDecision("field", n%3 == 0)
			rec.ObserveErrorCode(schemix.CodeTypeMismatch, "concurrent")
			rec.ObserveBlobExecution("f", time.Duration(n)*time.Microsecond, n%2 == 0)
			rec.ObserveLayerDuration("cue", time.Duration(n)*time.Microsecond, "concurrent")
		}(i)
	}
	wg.Wait()

	// Verify no panics occurred and metrics were recorded.
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("expected metric families after concurrent writes")
	}
}

func TestRecorder_DuplicateRegistration_Error(t *testing.T) {
	reg := prometheus.NewRegistry()
	_, err := schemixprom.New(reg)
	if err != nil {
		t.Fatalf("first New: unexpected error: %v", err)
	}

	_, err = schemixprom.New(reg)
	if err == nil {
		t.Fatal("expected error on duplicate registration, got nil")
	}
}

func TestRecorder_WithNamespace(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := schemixprom.New(reg, schemixprom.WithNamespace("myapp"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveValidation(time.Millisecond, true, "test")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	// With namespace "myapp", the metric should be "myapp_schemix_validation_duration_seconds"
	findMetricFamily(t, families, "myapp_schemix_validation_duration_seconds")
	findMetricFamily(t, families, "myapp_schemix_validations_total")
}

func TestRecorder_WithDurationBuckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	customBuckets := []float64{0.001, 0.01, 0.1, 1.0}
	rec, err := schemixprom.New(reg, schemixprom.WithDurationBuckets(customBuckets))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec.ObserveValidation(5*time.Millisecond, true, "buckets")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather error: %v", err)
	}

	hf := findMetricFamily(t, families, "schemix_validation_duration_seconds")
	for _, m := range hf.GetMetric() {
		if matchLabels(m.GetLabel(), map[string]string{"schema": "buckets", "valid": "true"}) {
			h := m.GetHistogram()
			// Prometheus represents user-defined boundaries as bucket entries.
			// The +Inf bucket is implicit in sample_count, not in GetBucket().
			bucketCount := len(h.GetBucket())
			if bucketCount != len(customBuckets) {
				t.Fatalf("expected %d buckets, got %d", len(customBuckets), bucketCount)
			}
			// Verify first bucket boundary matches our custom value
			firstBound := h.GetBucket()[0].GetUpperBound()
			if firstBound != customBuckets[0] {
				t.Fatalf("expected first bucket bound %f, got %f", customBuckets[0], firstBound)
			}
			return
		}
	}
	t.Fatal("could not find histogram metric with expected labels")
}
