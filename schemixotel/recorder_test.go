package schemixotel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mredencom/schemix"
	"github.com/mredencom/schemix/schemixotel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// helper: create a ManualReader + MeterProvider pair.
func newTestMeter(t *testing.T) (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	return reader, mp
}

// helper: collect metrics from reader.
func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	return rm
}

// helper: find a metric by name across all scope metrics.
func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

// helper: find scope metrics by scope name.
func findScope(rm metricdata.ResourceMetrics, scopeName string) *metricdata.ScopeMetrics {
	for i := range rm.ScopeMetrics {
		if rm.ScopeMetrics[i].Scope.Name == scopeName {
			return &rm.ScopeMetrics[i]
		}
	}
	return nil
}

func TestNew_NilProvider_Error(t *testing.T) {
	rec, err := schemixotel.New(nil)
	if err == nil {
		t.Fatal("expected error for nil MeterProvider, got nil")
	}
	if rec != nil {
		t.Fatal("expected nil Recorder for nil MeterProvider")
	}
	want := "schemixotel: nil MeterProvider"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestNew_Success(t *testing.T) {
	_, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("New returned nil Recorder")
	}
}

func TestRecorder_ObserveValidation(t *testing.T) {
	reader, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.ObserveValidation(50*time.Millisecond, true, "payment")

	rm := collectMetrics(t, reader)

	// Check schemix.validation.total counter
	m := findMetric(rm, "schemix.validation.total")
	if m == nil {
		t.Fatal("metric schemix.validation.total not found")
	}
	sumData, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64], got %T", m.Data)
	}
	if len(sumData.DataPoints) == 0 {
		t.Fatal("no data points for schemix.validation.total")
	}
	dp := sumData.DataPoints[0]
	if dp.Value != 1 {
		t.Errorf("validation.total value = %d, want 1", dp.Value)
	}
	assertHasAttribute(t, dp.Attributes, attribute.String("schema", "payment"))
	assertHasAttribute(t, dp.Attributes, attribute.Bool("valid", true))

	// Check schemix.validation.duration histogram
	h := findMetric(rm, "schemix.validation.duration")
	if h == nil {
		t.Fatal("metric schemix.validation.duration not found")
	}
	histData, ok := h.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected Histogram[float64], got %T", h.Data)
	}
	if len(histData.DataPoints) == 0 {
		t.Fatal("no data points for schemix.validation.duration")
	}
	hdp := histData.DataPoints[0]
	if hdp.Count != 1 {
		t.Errorf("validation.duration count = %d, want 1", hdp.Count)
	}
	if hdp.Sum < 0.04 || hdp.Sum > 0.06 {
		t.Errorf("validation.duration sum = %f, want ~0.05", hdp.Sum)
	}
}

func TestRecorder_ObserveErrorCode(t *testing.T) {
	reader, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.ObserveErrorCode(schemix.CodeTypeMismatch, "user")

	rm := collectMetrics(t, reader)

	m := findMetric(rm, "schemix.error.total")
	if m == nil {
		t.Fatal("metric schemix.error.total not found")
	}
	sumData, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64], got %T", m.Data)
	}
	if len(sumData.DataPoints) == 0 {
		t.Fatal("no data points for schemix.error.total")
	}
	dp := sumData.DataPoints[0]
	if dp.Value != 1 {
		t.Errorf("error.total value = %d, want 1", dp.Value)
	}
	assertHasAttribute(t, dp.Attributes, attribute.String("schema", "user"))
	assertHasAttribute(t, dp.Attributes, attribute.String("code", string(schemix.CodeTypeMismatch)))
}

func TestRecorder_ObserveBlobExecution(t *testing.T) {
	reader, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.ObserveBlobExecution("pan.luhn_check", 2*time.Millisecond, true)

	rm := collectMetrics(t, reader)

	// Check schemix.blob.total counter
	m := findMetric(rm, "schemix.blob.total")
	if m == nil {
		t.Fatal("metric schemix.blob.total not found")
	}
	sumData, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64], got %T", m.Data)
	}
	if len(sumData.DataPoints) == 0 {
		t.Fatal("no data points for schemix.blob.total")
	}
	dp := sumData.DataPoints[0]
	if dp.Value != 1 {
		t.Errorf("blob.total value = %d, want 1", dp.Value)
	}
	assertHasAttribute(t, dp.Attributes, attribute.String("field", "pan.luhn_check"))
	assertHasAttribute(t, dp.Attributes, attribute.Bool("success", true))

	// Check schemix.blob.duration histogram
	h := findMetric(rm, "schemix.blob.duration")
	if h == nil {
		t.Fatal("metric schemix.blob.duration not found")
	}
	histData, ok := h.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected Histogram[float64], got %T", h.Data)
	}
	if len(histData.DataPoints) == 0 {
		t.Fatal("no data points for schemix.blob.duration")
	}
	hdp := histData.DataPoints[0]
	if hdp.Count != 1 {
		t.Errorf("blob.duration count = %d, want 1", hdp.Count)
	}
	if hdp.Sum < 0.001 || hdp.Sum > 0.003 {
		t.Errorf("blob.duration sum = %f, want ~0.002", hdp.Sum)
	}
}

func TestRecorder_ObserveLayerDuration(t *testing.T) {
	reader, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.ObserveLayerDuration("cue", 10*time.Millisecond, "payment")

	rm := collectMetrics(t, reader)

	m := findMetric(rm, "schemix.layer.duration")
	if m == nil {
		t.Fatal("metric schemix.layer.duration not found")
	}
	histData, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected Histogram[float64], got %T", m.Data)
	}
	if len(histData.DataPoints) == 0 {
		t.Fatal("no data points for schemix.layer.duration")
	}
	hdp := histData.DataPoints[0]
	if hdp.Count != 1 {
		t.Errorf("layer.duration count = %d, want 1", hdp.Count)
	}
	assertHasAttribute(t, hdp.Attributes, attribute.String("schema", "payment"))
	assertHasAttribute(t, hdp.Attributes, attribute.String("layer", "cue"))
}

func TestRecorder_ObserveFastpathDecision(t *testing.T) {
	reader, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.ObserveFastpathDecision("amount", true)
	rec.ObserveFastpathDecision("currency", false)

	rm := collectMetrics(t, reader)

	m := findMetric(rm, "schemix.fastpath.total")
	if m == nil {
		t.Fatal("metric schemix.fastpath.total not found")
	}
	sumData, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64], got %T", m.Data)
	}
	if len(sumData.DataPoints) != 2 {
		t.Fatalf("expected 2 data points (hit+miss), got %d", len(sumData.DataPoints))
	}

	// Verify both hit and miss data points exist
	var foundHit, foundMiss bool
	for _, dp := range sumData.DataPoints {
		if dp.Value != 1 {
			t.Errorf("fastpath.total value = %d, want 1", dp.Value)
		}
		// Check result attribute
		for _, kv := range attrSlice(dp.Attributes) {
			if kv.Key == "result" {
				switch kv.Value.AsString() {
				case "hit":
					foundHit = true
				case "miss":
					foundMiss = true
				}
			}
		}
	}
	if !foundHit {
		t.Error("no data point with result=hit")
	}
	if !foundMiss {
		t.Error("no data point with result=miss")
	}
}

func TestRecorder_InterfaceSatisfaction(t *testing.T) {
	// This is tested at compile time via:
	//   var _ schemix.MetricsRecorder = (*Recorder)(nil)
	// But we add a runtime check too for clarity.
	reader, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = collectMetrics(t, reader)

	var iface schemix.MetricsRecorder = rec
	if iface == nil {
		t.Fatal("Recorder does not satisfy MetricsRecorder interface")
	}
}

func TestRecorder_Concurrent(t *testing.T) {
	_, mp := newTestMeter(t)
	rec, err := schemixotel.New(mp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			rec.ObserveValidation(time.Millisecond, n%2 == 0, "concurrent")
			rec.ObserveFastpathDecision("field", n%3 == 0)
			rec.ObserveErrorCode(schemix.CodeTypeMismatch, "concurrent")
			rec.ObserveBlobExecution("expr", time.Microsecond, true)
			rec.ObserveLayerDuration("blob", time.Millisecond, "concurrent")
		}(i)
	}

	wg.Wait()
	// If we reach here without a race detector panic, the test passes.
}

func TestRecorder_WithMeterName(t *testing.T) {
	reader, mp := newTestMeter(t)
	customName := "my.custom.meter"
	rec, err := schemixotel.New(mp, schemixotel.WithMeterName(customName))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.ObserveValidation(time.Millisecond, true, "test")

	rm := collectMetrics(t, reader)

	scope := findScope(rm, customName)
	if scope == nil {
		t.Fatalf("scope %q not found in metrics", customName)
	}

	// Verify the default scope is not used
	defaultScope := findScope(rm, schemix.ScopeName)
	if defaultScope != nil {
		t.Error("default scope should not be present when custom meter name is set")
	}
}

// assertHasAttribute checks that the attribute set contains the expected key-value.
func assertHasAttribute(t *testing.T, attrs attribute.Set, expected attribute.KeyValue) {
	t.Helper()
	v, ok := attrs.Value(expected.Key)
	if !ok {
		t.Errorf("attribute %q not found", expected.Key)
		return
	}
	if v != expected.Value {
		t.Errorf("attribute %q = %v, want %v", expected.Key, v, expected.Value)
	}
}

// attrSlice converts an attribute.Set to a slice for iteration.
func attrSlice(attrs attribute.Set) []attribute.KeyValue {
	var result []attribute.KeyValue
	iter := attrs.Iter()
	for iter.Next() {
		result = append(result, iter.Attribute())
	}
	return result
}
