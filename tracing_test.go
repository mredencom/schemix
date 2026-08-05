package schemix

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

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

func TestFailModeString(t *testing.T) {
	tests := []struct {
		mode FailMode
		want string
	}{
		{FailAll, "all"},
		{FailFast, "fast"},
		{FailPriority, "priority"},
		{FailMode(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := failModeString(tt.mode)
			if got != tt.want {
				t.Errorf("failModeString(%d) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

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
