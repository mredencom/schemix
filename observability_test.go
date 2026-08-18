package schemix

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// fakeRecorder is a test double for MetricsRecorder that records all calls
// it receives, safe for concurrent use per the MetricsRecorder contract.
type fakeRecorder struct {
	mu              sync.Mutex
	validationCalls []validationObservation
	fastpathCalls   []fastpathObservation
	errorCodeCalls  []errorCodeObservation
	blobExecCalls   []blobExecObservation
	layerDurCalls   []layerDurObservation
}

type validationObservation struct {
	duration   time.Duration
	valid      bool
	schemaName string
}

type fastpathObservation struct {
	fieldPath string
	hit       bool
}

type errorCodeObservation struct {
	code       ErrorCode
	schemaName string
}

type blobExecObservation struct {
	fieldPath string
	duration  time.Duration
	success   bool
}

type layerDurObservation struct {
	layer      string
	duration   time.Duration
	schemaName string
}

func (f *fakeRecorder) ObserveValidation(d time.Duration, valid bool, schemaName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validationCalls = append(f.validationCalls, validationObservation{duration: d, valid: valid, schemaName: schemaName})
}

func (f *fakeRecorder) ObserveFastpathDecision(fieldPath string, hit bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fastpathCalls = append(f.fastpathCalls, fastpathObservation{fieldPath: fieldPath, hit: hit})
}

func (f *fakeRecorder) ObserveErrorCode(code ErrorCode, schemaName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errorCodeCalls = append(f.errorCodeCalls, errorCodeObservation{code: code, schemaName: schemaName})
}

func (f *fakeRecorder) ObserveBlobExecution(fieldPath string, d time.Duration, success bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobExecCalls = append(f.blobExecCalls, blobExecObservation{fieldPath: fieldPath, duration: d, success: success})
}

func (f *fakeRecorder) ObserveLayerDuration(layer string, d time.Duration, schemaName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.layerDurCalls = append(f.layerDurCalls, layerDurObservation{layer: layer, duration: d, schemaName: schemaName})
}

func (f *fakeRecorder) validationCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.validationCalls)
}

func (f *fakeRecorder) fastpathCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fastpathCalls)
}

// ---------------------------------------------------------------------------
// ObserveValidation & ObserveFastpathDecision
// ---------------------------------------------------------------------------

func TestMetricsRecorder_NotConfigured_NoCallsRecorded(t *testing.T) {
	// Without WithMetricsRecorder, the Validator must not hold a reference
	// to any recorder, and no observability code path should execute.
	v := MustNew(`{ name: string }`)
	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}
}

func TestMetricsRecorder_ObserveValidation_CalledOnceOnValid(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ name: string }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	if got := rec.validationCallCount(); got != 1 {
		t.Fatalf("expected 1 ObserveValidation call, got %d", got)
	}
	rec.mu.Lock()
	obs := rec.validationCalls[0]
	rec.mu.Unlock()
	if !obs.valid {
		t.Fatalf("expected ObserveValidation to report valid=true")
	}
	if obs.duration < 0 {
		t.Fatalf("expected non-negative duration, got %v", obs.duration)
	}
}

func TestMetricsRecorder_ObserveValidation_CalledOnceOnInvalid(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ age: int & >=0 }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"age": int64(-5)})
	if r.Valid {
		t.Fatalf("expected invalid result")
	}

	if got := rec.validationCallCount(); got != 1 {
		t.Fatalf("expected 1 ObserveValidation call, got %d", got)
	}
	rec.mu.Lock()
	obs := rec.validationCalls[0]
	rec.mu.Unlock()
	if obs.valid {
		t.Fatalf("expected ObserveValidation to report valid=false")
	}
}

func TestMetricsRecorder_ObserveValidation_CalledForValidateToo(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ name: string }`, WithMetricsRecorder(rec))

	valid, _ := v.Validate(map[string]any{"name": "Alice"})
	if !valid {
		t.Fatalf("expected valid")
	}

	if got := rec.validationCallCount(); got != 1 {
		t.Fatalf("expected 1 ObserveValidation call from Validate, got %d", got)
	}
}

func TestMetricsRecorder_ObserveFastpathDecision_HitOnScalarField(t *testing.T) {
	rec := &fakeRecorder{}
	// age has a Go-native fast path (simple int range constraint).
	v := MustNew(`{ age: int & >=0 & <=150 }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"age": int64(30)})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	if got := rec.fastpathCallCount(); got != 1 {
		t.Fatalf("expected 1 ObserveFastpathDecision call, got %d", got)
	}
	rec.mu.Lock()
	obs := rec.fastpathCalls[0]
	rec.mu.Unlock()
	if obs.fieldPath != "age" {
		t.Fatalf("expected fieldPath %q, got %q", "age", obs.fieldPath)
	}
	if !obs.hit {
		t.Fatalf("expected fastpath hit for simple int range constraint")
	}
}

func TestMetricsRecorder_MultipleProcessCalls_AccumulateObservations(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ name: string }`, WithMetricsRecorder(rec))

	v.Process(map[string]any{"name": "Alice"})
	v.Process(map[string]any{"name": "Bob"})
	v.Process(map[string]any{"name": "Carol"})

	if got := rec.validationCallCount(); got != 3 {
		t.Fatalf("expected 3 ObserveValidation calls, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// ObserveErrorCode
// ---------------------------------------------------------------------------

func TestMetricsRecorder_ObserveErrorCode_CalledPerError(t *testing.T) {
	rec := &fakeRecorder{}
	// Schema with two required fields — both will fail if missing.
	v := MustNew(`{ name: string, age: int }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{})
	if r.Valid {
		t.Fatalf("expected invalid result")
	}
	if len(r.Errors) < 2 {
		t.Fatalf("expected at least 2 errors, got %d", len(r.Errors))
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errorCodeCalls) != len(r.Errors) {
		t.Fatalf("expected %d ObserveErrorCode calls, got %d", len(r.Errors), len(rec.errorCodeCalls))
	}
	// Verify codes match errors
	for i, call := range rec.errorCodeCalls {
		if call.code != r.Errors[i].Code {
			t.Errorf("error[%d]: expected code %s, got %s", i, r.Errors[i].Code, call.code)
		}
	}
}

func TestMetricsRecorder_ObserveErrorCode_NotCalledWhenValid(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ name: string }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.errorCodeCalls) != 0 {
		t.Fatalf("expected 0 ObserveErrorCode calls when valid, got %d", len(rec.errorCodeCalls))
	}
}

// ---------------------------------------------------------------------------
// ObserveBlobExecution
// ---------------------------------------------------------------------------

func TestMetricsRecorder_ObserveBlobExecution_CalledPerRule(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{
		name: string
		upper: string @blob(this.name.uppercase())
		lower: string @blob(this.name.lowercase())
	}`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.blobExecCalls) != 2 {
		t.Fatalf("expected 2 ObserveBlobExecution calls, got %d", len(rec.blobExecCalls))
	}
	// Both should be successful
	for i, call := range rec.blobExecCalls {
		if !call.success {
			t.Errorf("blobExec[%d]: expected success=true, got false", i)
		}
		if call.duration < 0 {
			t.Errorf("blobExec[%d]: expected non-negative duration, got %v", i, call.duration)
		}
	}
}

func TestMetricsRecorder_ObserveBlobExecution_SuccessFalseOnError(t *testing.T) {
	rec := &fakeRecorder{}
	// Use an expression that will cause a runtime error — accessing a field
	// that doesn't exist in the data will not error in bloblang (returns null),
	// so we use a method on a type mismatch instead.
	v := MustNew(`{
		amount: int
		check: bool @blob(this.amount.contains("x"))
	}`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"amount": int64(42)})
	if r.Valid {
		t.Fatalf("expected invalid result due to blob error")
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.blobExecCalls) == 0 {
		t.Fatalf("expected at least 1 ObserveBlobExecution call")
	}
	// The blob execution that errors should have success=false
	found := false
	for _, call := range rec.blobExecCalls {
		if !call.success {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one ObserveBlobExecution call with success=false")
	}
}

// ---------------------------------------------------------------------------
// ObserveLayerDuration
// ---------------------------------------------------------------------------

func TestMetricsRecorder_ObserveLayerDuration_CUELayer(t *testing.T) {
	rec := &fakeRecorder{}
	// Schema with only CUE constraints, no blob rules
	v := MustNew(`{ name: string, age: int }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice", "age": int64(30)})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	var cueCalls []layerDurObservation
	for _, call := range rec.layerDurCalls {
		if call.layer == LayerCUE {
			cueCalls = append(cueCalls, call)
		}
	}
	if len(cueCalls) != 1 {
		t.Fatalf("expected 1 CUE layer duration call, got %d", len(cueCalls))
	}
	if cueCalls[0].duration < 0 {
		t.Fatalf("expected non-negative CUE duration, got %v", cueCalls[0].duration)
	}
}

func TestMetricsRecorder_ObserveLayerDuration_BlobLayer(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{
		name: string
		upper: string @blob(this.name.uppercase())
	}`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	var blobCalls []layerDurObservation
	for _, call := range rec.layerDurCalls {
		if call.layer == LayerBlob {
			blobCalls = append(blobCalls, call)
		}
	}
	if len(blobCalls) != 1 {
		t.Fatalf("expected 1 Blob layer duration call, got %d", len(blobCalls))
	}
	if blobCalls[0].duration < 0 {
		t.Fatalf("expected non-negative Blob duration, got %v", blobCalls[0].duration)
	}
}

func TestMetricsRecorder_ObserveLayerDuration_BothLayers(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{
		name: string
		upper: string @blob(this.name.uppercase())
	}`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	layersSeen := make(map[string]bool)
	for _, call := range rec.layerDurCalls {
		layersSeen[call.layer] = true
	}
	if !layersSeen[LayerCUE] {
		t.Fatalf("expected CUE layer duration to be reported")
	}
	if !layersSeen[LayerBlob] {
		t.Fatalf("expected Blob layer duration to be reported")
	}
}

// ---------------------------------------------------------------------------
// WithName propagation
// ---------------------------------------------------------------------------

func TestMetricsRecorder_WithName_PropagatesSchemaName(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ name: string }`, WithMetricsRecorder(rec), WithName("user_schema"))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.validationCalls) != 1 {
		t.Fatalf("expected 1 ObserveValidation call, got %d", len(rec.validationCalls))
	}
	if rec.validationCalls[0].schemaName != "user_schema" {
		t.Fatalf("expected schemaName %q, got %q", "user_schema", rec.validationCalls[0].schemaName)
	}
	// Also check layer duration calls carry the name
	for _, call := range rec.layerDurCalls {
		if call.schemaName != "user_schema" {
			t.Fatalf("expected layer schemaName %q, got %q", "user_schema", call.schemaName)
		}
	}
}

func TestMetricsRecorder_WithName_DefaultEmpty(t *testing.T) {
	rec := &fakeRecorder{}
	v := MustNew(`{ name: string }`, WithMetricsRecorder(rec))

	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.validationCalls) != 1 {
		t.Fatalf("expected 1 ObserveValidation call, got %d", len(rec.validationCalls))
	}
	if rec.validationCalls[0].schemaName != "" {
		t.Fatalf("expected empty schemaName by default, got %q", rec.validationCalls[0].schemaName)
	}
}

func TestMetricsRecorder_Registry_AutoSetsName(t *testing.T) {
	rec := &fakeRecorder{}
	reg := NewRegistry()
	err := reg.Register("payment", `{ amount: int & >0 }`)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	v, ok := reg.Get("payment")
	if !ok {
		t.Fatalf("expected to get validator 'payment'")
	}
	// The registry sets the name, but the recorder is not attached to the
	// registry validator. We need to verify the schemaName field directly.
	if v.schemaName != "payment" {
		t.Fatalf("expected schemaName %q from Registry.Register, got %q", "payment", v.schemaName)
	}

	// Also verify it flows through to metrics when a recorder is attached
	v2, err := New(`{ amount: int & >0 }`, WithMetricsRecorder(rec), WithName("payment"))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	v2.Process(map[string]any{"amount": int64(100)})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.validationCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.validationCalls))
	}
	if rec.validationCalls[0].schemaName != "payment" {
		t.Fatalf("expected schemaName %q, got %q", "payment", rec.validationCalls[0].schemaName)
	}
}

// newTestProvider creates a TracerProvider with an in-memory exporter for testing.
func newTestProvider() (*tracetest.InMemoryExporter, trace.TracerProvider) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	return exp, tp
}

func TestWithTracerProvider_SpanCreated(t *testing.T) {
	exp, tp := newTestProvider()

	v := MustNew(`{
		name: string
	}`, WithTracerProvider(tp), WithName("test-schema"))

	data := map[string]any{"name": "Alice"}
	r := v.ProcessContext(context.Background(), data)
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	// Find root span
	var rootFound bool
	for _, s := range spans {
		if s.Name == "schemix.process" {
			rootFound = true
			break
		}
	}
	if !rootFound {
		t.Errorf("expected root span 'schemix.process', got spans: %v", spanNames(spans))
	}
}

func TestProcessContext_SpanAttributes(t *testing.T) {
	exp, tp := newTestProvider()

	v := MustNew(`{
		name: string
		age:  int & >=0
	}`, WithTracerProvider(tp), WithName("attr-test"))

	data := map[string]any{"name": "Bob", "age": int64(25)}
	r := v.ProcessContext(context.Background(), data)
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	spans := exp.GetSpans()
	root := findSpan(spans, "schemix.process")
	if root == nil {
		t.Fatal("root span not found")
	}

	assertAttr(t, root.Attributes, "schemix.schema_name", "attr-test")
	assertAttr(t, root.Attributes, "schemix.fail_mode", "all")
	assertAttrBool(t, root.Attributes, "schemix.valid", true)
	assertAttrInt(t, root.Attributes, "schemix.error_count", 0)
	assertAttrInt(t, root.Attributes, "schemix.field_count", 2)
}

func TestProcessContext_SpanEvents_OnError(t *testing.T) {
	exp, tp := newTestProvider()

	v := MustNew(`{
		name: string
		age:  int & >=0
	}`, WithTracerProvider(tp), WithName("error-events"))

	// Invalid: name is wrong type, age is missing
	data := map[string]any{"name": 123}
	r := v.ProcessContext(context.Background(), data)
	if r.Valid {
		t.Fatal("expected invalid result")
	}

	spans := exp.GetSpans()
	root := findSpan(spans, "schemix.process")
	if root == nil {
		t.Fatal("root span not found")
	}

	// Check span status is Error
	if root.Status.Code != codes.Error {
		t.Errorf("expected span status Error, got %v", root.Status.Code)
	}

	// Check events exist
	if len(root.Events) == 0 {
		t.Fatal("expected validation_error events, got none")
	}

	for _, ev := range root.Events {
		if ev.Name != "validation_error" {
			t.Errorf("expected event name 'validation_error', got %q", ev.Name)
		}
		// Each event should have code, path, type attributes
		hasCode := hasAttrKey(ev.Attributes, "schemix.error.code")
		hasPath := hasAttrKey(ev.Attributes, "schemix.error.path")
		hasType := hasAttrKey(ev.Attributes, "schemix.error.type")
		if !hasCode || !hasPath || !hasType {
			t.Errorf("event missing required attributes: code=%v path=%v type=%v", hasCode, hasPath, hasType)
		}
	}
}

func TestProcessContext_SpanEvents_Capped(t *testing.T) {
	// Create a schema with many required fields to generate >20 errors
	schema := `{
		f01: string
		f02: string
		f03: string
		f04: string
		f05: string
		f06: string
		f07: string
		f08: string
		f09: string
		f10: string
		f11: string
		f12: string
		f13: string
		f14: string
		f15: string
		f16: string
		f17: string
		f18: string
		f19: string
		f20: string
		f21: string
		f22: string
		f23: string
		f24: string
		f25: string
	}`

	exp, tp := newTestProvider()
	v := MustNew(schema, WithTracerProvider(tp), WithName("capped"))

	// Empty data → all 25 fields missing
	data := map[string]any{}
	r := v.ProcessContext(context.Background(), data)
	if r.Valid {
		t.Fatal("expected invalid result")
	}
	if len(r.Errors) <= maxSpanEvents {
		t.Fatalf("expected >%d errors to test cap, got %d", maxSpanEvents, len(r.Errors))
	}

	spans := exp.GetSpans()
	root := findSpan(spans, "schemix.process")
	if root == nil {
		t.Fatal("root span not found")
	}

	if len(root.Events) > maxSpanEvents {
		t.Errorf("expected at most %d events, got %d", maxSpanEvents, len(root.Events))
	}
	if len(root.Events) != maxSpanEvents {
		t.Errorf("expected exactly %d events (capped), got %d", maxSpanEvents, len(root.Events))
	}
}

func TestProcessContext_LayerSpans(t *testing.T) {
	exp, tp := newTestProvider()

	v := MustNew(`{
		name: string
		ok:   bool @blob(this.name != "")
	}`, WithTracerProvider(tp), WithName("layers"))

	data := map[string]any{"name": "test"}
	r := v.ProcessContext(context.Background(), data)
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	spans := exp.GetSpans()

	cueSpan := findSpan(spans, "schemix.cue")
	if cueSpan == nil {
		t.Errorf("expected 'schemix.cue' child span, got spans: %v", spanNames(spans))
	}

	blobSpan := findSpan(spans, "schemix.blob")
	if blobSpan == nil {
		t.Errorf("expected 'schemix.blob' child span, got spans: %v", spanNames(spans))
	}

	// Verify parent-child relationship: both should have same trace ID as root
	root := findSpan(spans, "schemix.process")
	if root == nil {
		t.Fatal("root span not found")
	}
	if cueSpan != nil && cueSpan.SpanContext.TraceID() != root.SpanContext.TraceID() {
		t.Error("cue span has different trace ID than root")
	}
	if blobSpan != nil && blobSpan.SpanContext.TraceID() != root.SpanContext.TraceID() {
		t.Error("blob span has different trace ID than root")
	}
}

func TestProcessContext_NoTracer_ZeroOverhead(t *testing.T) {
	// No WithTracerProvider — tracer is nil
	v := MustNew(`{
		name: string
		age:  int
	}`, WithName("no-tracer"))

	data := map[string]any{"name": "Alice", "age": int64(30)}
	r := v.ProcessContext(context.Background(), data)
	if !r.Valid {
		t.Errorf("expected valid, got errors: %v", r.Errors)
	}

	// Also test invalid path
	bad := map[string]any{"name": 123}
	r2 := v.ProcessContext(context.Background(), bad)
	if r2.Valid {
		t.Error("expected invalid result")
	}
}

func TestProcess_NoSpans_EvenWithTracer(t *testing.T) {
	exp, tp := newTestProvider()

	v := MustNew(`{
		name: string
	}`, WithTracerProvider(tp), WithName("no-span-test"))

	// Use non-context Process — should NOT create spans
	data := map[string]any{"name": "Alice"}
	r := v.Process(data)
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}

	spans := exp.GetSpans()
	if len(spans) != 0 {
		t.Errorf("Process (non-context) should not create spans, got %d: %v", len(spans), spanNames(spans))
	}
}

func TestWithTracerProvider_NilProvider(t *testing.T) {
	// Passing nil should not panic
	v := MustNew(`{ x: int }`, WithTracerProvider(nil), WithName("nil-tp"))

	r := v.ProcessContext(context.Background(), map[string]any{"x": int64(1)})
	if !r.Valid {
		t.Errorf("expected valid, got: %v", r.Errors)
	}
}

func TestProcessWithModeContext_FailFast_SpanAttributes(t *testing.T) {
	exp, tp := newTestProvider()

	v := MustNew(`{
		a: string
		b: int
	}`, WithTracerProvider(tp), WithName("failfast-span"))

	data := map[string]any{"a": 123, "b": "wrong"}
	r := v.ProcessWithModeContext(context.Background(), data, FailFast)
	if r.Valid {
		t.Fatal("expected invalid")
	}

	spans := exp.GetSpans()
	root := findSpan(spans, "schemix.process")
	if root == nil {
		t.Fatal("root span not found")
	}

	assertAttr(t, root.Attributes, "schemix.fail_mode", "fast")
}

func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name
	}
	return names
}

func assertAttr(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			got := a.Value.AsString()
			if got != want {
				t.Errorf("attribute %q: got %q, want %q", key, got, want)
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertAttrBool(t *testing.T, attrs []attribute.KeyValue, key string, want bool) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			got := a.Value.AsBool()
			if got != want {
				t.Errorf("attribute %q: got %v, want %v", key, got, want)
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertAttrInt(t *testing.T, attrs []attribute.KeyValue, key string, want int) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			got := a.Value.AsInt64()
			if got != int64(want) {
				t.Errorf("attribute %q: got %d, want %d", key, got, want)
			}
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func hasAttrKey(attrs []attribute.KeyValue, key string) bool {
	for _, a := range attrs {
		if string(a.Key) == key {
			return true
		}
	}
	return false
}
