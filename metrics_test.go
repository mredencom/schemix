package schemix

import (
	"sync"
	"testing"
	"time"
)

// fakeRecorder is a test double for MetricsRecorder that records all calls
// it receives, safe for concurrent use per the MetricsRecorder contract.
type fakeRecorder struct {
	mu              sync.Mutex
	validationCalls []validationObservation
	fastpathCalls   []fastpathObservation
}

type validationObservation struct {
	duration time.Duration
	valid    bool
}

type fastpathObservation struct {
	fieldPath string
	hit       bool
}

func (f *fakeRecorder) ObserveValidation(d time.Duration, valid bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validationCalls = append(f.validationCalls, validationObservation{duration: d, valid: valid})
}

func (f *fakeRecorder) ObserveFastpathDecision(fieldPath string, hit bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fastpathCalls = append(f.fastpathCalls, fastpathObservation{fieldPath: fieldPath, hit: hit})
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

func TestMetricsRecorder_NotConfigured_NoCallsRecorded(t *testing.T) {
	// Without WithMetricsRecorder, the Validator must not hold a reference
	// to any recorder, and no observability code path should execute.
	v := MustNew(`{ name: string }`)
	r := v.Process(map[string]any{"name": "Alice"})
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %v", r.Errors)
	}
	// No recorder attached — nothing to assert on calls, but this documents
	// baseline behavior: Process must succeed identically with or without
	// the metrics feature compiled in.
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
