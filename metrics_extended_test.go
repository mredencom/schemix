package schemix

import (
	"testing"
)

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
