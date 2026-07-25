package schemix

import (
	"testing"
)

// TestFailPriorityFullLayerIsolation verifies that FailPriority mode groups errors
// by priority across BOTH CUE and Blob layers: if priority group N fails,
// higher-priority groups (N+1, N+2, ...) are not executed in the Blob layer.
func TestFailPriorityFullLayerIsolation(t *testing.T) {
	// Schema with two priority groups:
	// Priority 1: pan must be 16 digits (CUE regex)
	// Priority 2: luhn check (blob)
	// If priority 1 fails, priority 2 should NOT be executed
	schema := `{
		pan:        =~"^[0-9]{16}$" @meta(priority=1)
		luhn_check: bool @blob(this.pan.luhn_valid()) @meta(priority=2)
	}`

	t.Run("p1_cue_fails_p2_blob_skipped", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// pan is too short — CUE regex fails at priority 1
		data := map[string]any{"pan": "123"}
		r := v.ProcessWithMode(data, FailPriority)

		if r.Valid {
			t.Fatal("expected Valid=false")
		}
		// Should only have priority 1 errors (CUE regex), NOT priority 2 (blob)
		for _, e := range r.Errors {
			if e.Path == "luhn_check" {
				t.Errorf("FailPriority should have skipped priority 2 blob, got error: %v", e)
			}
		}
		if !r.HasCode(CodeFormatMismatch) {
			t.Errorf("expected E1F01 for pan regex failure, got: %v", r.Errors)
		}
	})

	t.Run("p1_passes_p2_executes", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// pan is 16 digits but invalid luhn — priority 1 passes, priority 2 should execute
		data := map[string]any{"pan": "1234567890123456"}
		r := v.ProcessWithMode(data, FailPriority)

		if r.Valid {
			t.Fatal("expected Valid=false (luhn should fail)")
		}
		if !r.HasCode(CodeBizRuleFailed) {
			t.Errorf("expected E2B01 for luhn failure, got: %v", r.Errors)
		}
	})

	t.Run("both_pass_valid", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// Valid Visa card number (passes luhn)
		data := map[string]any{"pan": "4111111111111111"}
		r := v.ProcessWithMode(data, FailPriority)

		if !r.Valid {
			t.Fatalf("expected Valid=true, got errors: %v", r.Errors)
		}
	})

	t.Run("same_priority_cue_and_blob_errors_are_reported", func(t *testing.T) {
		v := MustNew(`{
			name:  string @meta(priority=1)
			adult: bool @blob(false) @meta(priority=1)
		}`)

		r := v.ProcessWithMode(map[string]any{"name": int64(42)}, FailPriority)
		if r.Valid {
			t.Fatal("expected Valid=false")
		}
		if !r.HasCode(CodeTypeMismatch) || !r.HasCode(CodeBizRuleFailed) {
			t.Fatalf("expected same-group E1T01 and E2B01, got %v", r.Errors)
		}
	})
}

// TestFailPriorityCUEGrouping verifies that CUE errors are grouped by the field's
// priority metadata, and only the lowest-failed priority group's CUE errors are reported.
func TestFailPriorityCUEGrouping(t *testing.T) {
	// Two CUE fields at different priorities:
	// priority 1: name must be string
	// priority 2: age must be int
	schema := `{
		name: string @meta(priority=1)
		age:  int    @meta(priority=2)
	}`

	t.Run("p1_cue_fails_p2_cue_not_reported", func(t *testing.T) {
		v, err := New(schema)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}
		// Both fields wrong — only priority 1 should be reported
		data := map[string]any{"name": int64(123), "age": "not-int"}
		r := v.ProcessWithMode(data, FailPriority)

		if r.Valid {
			t.Fatal("expected Valid=false")
		}
		// Should only report name error (priority 1), not age (priority 2)
		for _, e := range r.Errors {
			if e.Path == "age" {
				t.Errorf("FailPriority should not report priority 2 CUE error, got: %v", e)
			}
		}
		if !r.HasErrorsAt("name") {
			t.Errorf("expected error at path 'name', got: %v", r.Errors)
		}
	})
}
