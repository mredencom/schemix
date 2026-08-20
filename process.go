package schemix

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"go.opentelemetry.io/otel/trace"
)

// blobLoopState holds the mutable bookkeeping that every error path in the
// @blob/@meta rule loop must update in lockstep: the result being built, the
// set of paths that already failed, whether the current priority group has
// produced an error, and the active FailMode.
//
// It exists because those updates were open-coded at six error sites, where
// forgetting one — priorityHasError in particular — silently breaks
// FailPriority group isolation without failing any type check.
type blobLoopState struct {
	result           *Result
	failedPaths      map[string]bool
	mode             FailMode
	priorityHasError bool
}

// markFailed records a failure at path and marks the current priority group as
// failed, for callers that have already appended their error by another route
// (checkBlobResultType).
//
// Deliberately NOT used for the CUE-layer short circuit at the top of the loop:
// inheriting a CUE failure must mark the path without touching
// priorityHasError, otherwise FailPriority would treat a CUE error as a
// blob-group failure and stop evaluating the rest of the group.
func (st *blobLoopState) markFailed(path string) {
	st.failedPaths[path] = true
	st.priorityHasError = true
}

// recordRuleError appends a rule error at path and marks the path failed. It
// reports whether the caller must stop immediately because mode is FailFast.
func (v *Validator) recordRuleError(st *blobLoopState, code ErrorCode, typ, path, detail string) bool {
	st.result.Valid = false
	st.result.Errors = append(st.result.Errors, ValidationError{
		Code:    code,
		Path:    path,
		Type:    typ,
		Message: v.formatMessage(code, path, detail),
	})
	st.markFailed(path)
	return st.mode == FailFast
}

// processInternal is the unified validation/processing engine.
// When needOutput is false, it skips deepCopy and all Output mutations for performance.
// When ctx is non-nil and v.tracer is set, child spans for CUE and Blob layers are created.
func (v *Validator) processInternal(ctx context.Context, data map[string]any, mode FailMode, needOutput bool) (result Result) {
	result = Result{
		Valid:  true,
		Errors: []ValidationError{},
		loc:    v.localizer,
	}
	defer func() {
		if !result.Valid {
			result.Output = nil
		}
	}()

	if needOutput {
		result.Output = deepCopy(data)
	}

	// Layer 1: CUE validation using pre-compiled field descriptors
	var cueSpan trace.Span
	if v.tracer != nil && trace.SpanFromContext(ctx).IsRecording() {
		_, cueSpan = v.tracer.Start(ctx, "schemix.cue", trace.WithSpanKind(trace.SpanKindInternal))
	}
	var cueStart time.Time
	if v.metrics != nil {
		cueStart = time.Now()
	}
	dataValue := newLazyCUEValue(v.ctx, data)
	v.validateCUEFields(v.cueFields, dataValue, data, &result)
	if v.metrics != nil {
		v.metrics.ObserveLayerDuration(LayerCUE, time.Since(cueStart), v.schemaName)
	}
	if cueSpan != nil {
		cueSpan.End()
	}

	if mode == FailFast && !result.Valid {
		if len(result.Errors) > 1 {
			result.Errors = result.Errors[:1]
		}
		return result
	}

	// FailPriority: determine minFailedPriority from CUE errors and filter
	minFailedPriority := math.MaxInt // math.MaxInt
	if mode == FailPriority && !result.Valid {
		// Find the minimum priority among failed CUE fields
		for _, e := range result.Errors {
			p := v.fieldPriorityByPath(e.Path)
			if p < minFailedPriority {
				minFailedPriority = p
			}
		}
		// Filter: keep only errors from the minimum failed priority group
		filtered := result.Errors[:0]
		for _, e := range result.Errors {
			if v.fieldPriorityByPath(e.Path) == minFailedPriority {
				filtered = append(filtered, e)
			}
		}
		result.Errors = filtered
	}

	// Preserve the CUE-layer failures before Blob errors are appended. A rule on
	// the same field must never execute after its CUE constraint has failed.
	cueErrors := append([]ValidationError(nil), result.Errors...)

	// Layer 2: @blob + @meta rules
	var blobSpan trace.Span
	if v.tracer != nil && trace.SpanFromContext(ctx).IsRecording() {
		_, blobSpan = v.tracer.Start(ctx, "schemix.blob", trace.WithSpanKind(trace.SpanKindInternal))
	}
	var blobStart time.Time
	if v.metrics != nil {
		blobStart = time.Now()
	}
	failedPaths := map[string]bool{}
	currentPriority := -1
	st := blobLoopState{result: &result, failedPaths: failedPaths, mode: mode}

	for _, rule := range v.blobRules {
		meta := rule.Meta

		if hasValidationErrorAtPath(cueErrors, rule.Path) {
			// Mark only — see markFailed's note on why priorityHasError stays.
			failedPaths[rule.Path] = true
			continue
		}

		// FailPriority: check priority group transition
		if mode == FailPriority && meta.Priority > currentPriority {
			if st.priorityHasError {
				break
			}
			currentPriority = meta.Priority
			st.priorityHasError = false
		}

		// blobRules are sorted by priority, so once this boundary is crossed
		// every remaining rule belongs to a later group.
		if mode == FailPriority && minFailedPriority < math.MaxInt && meta.Priority > minFailedPriority {
			break
		}

		// Field-level fail_fast
		if meta.FailFast && failedPaths[rule.Path] {
			continue
		}

		// skip_if
		if meta.SkipIf != nil {
			res, err := meta.SkipIf.Query(data)
			if err != nil {
				detail := fmt.Sprintf("skip_if expression error (%s): %v", meta.SkipIfExpr, err)
				if v.recordRuleError(&st, CodeMetaRuntimeError, TypeMeta, rule.Path, detail) {
					return result
				}
				continue
			}
			if skip, ok := res.(bool); ok && skip {
				if meta.OmitIfSkip && result.Output != nil {
					deleteNestedKey(result.Output, rule.Path)
				}
				continue
			}
		}

		// Get field value
		fieldVal := getNestedValue(data, rule.Path)
		fieldEmpty := isEmpty(fieldVal)

		// skip_empty
		if meta.SkipEmpty && fieldEmpty {
			if (meta.OmitIfSkip || meta.OmitEmpty) && result.Output != nil {
				deleteNestedKey(result.Output, rule.Path)
			}
			continue
		}

		// omit_empty
		if meta.OmitEmpty && fieldEmpty && result.Output != nil {
			deleteNestedKey(result.Output, rule.Path)
		}

		// optional + required_if
		//
		// This branch also handles @meta(conditional): parsefieldMeta sets
		// Optional = true whenever the conditional flag is present, so a
		// conditional field always satisfies the test below and always
		// continues. A separate `meta.Conditional && fieldVal == nil` branch
		// after this one is therefore unreachable — it existed as a duplicate
		// of this block until it was removed. Do not reintroduce it; give
		// Conditional its own non-optional semantics in parsefieldMeta first.
		if meta.Optional && fieldVal == nil {
			if meta.OmitEmpty && result.Output != nil {
				deleteNestedKey(result.Output, rule.Path)
			}
			if meta.RequiredIf != nil {
				res, err := meta.RequiredIf.Query(data)
				if err != nil {
					detail := fmt.Sprintf("required_if expression error (%s): %v", meta.RequiredIfExpr, err)
					if v.recordRuleError(&st, CodeMetaRuntimeError, TypeMeta, rule.Path, detail) {
						return result
					}
				} else if required, ok := res.(bool); ok && required {
					detail := fmt.Sprintf("conditional required (%s)", meta.RequiredIfExpr)
					if v.recordRuleError(&st, CodeCondRequired, TypeMeta, rule.Path, detail) {
						return result
					}
				}
			}
			continue
		}

		// @blob execution
		if rule.Exec != nil {
			var execStart time.Time
			if v.metrics != nil {
				execStart = time.Now()
			}
			res, err := rule.Exec.Query(data)
			if v.metrics != nil {
				v.metrics.ObserveBlobExecution(rule.Path, time.Since(execStart), err == nil)
			}
			if err != nil {
				detail := fmt.Sprintf("expression error: %v", err)
				if v.recordRuleError(&st, CodeExprExecError, TypeBloblang, rule.Path, detail) {
					return result
				}
				continue
			}

			if valid, ok := res.(bool); ok {
				if !valid {
					detail := fmt.Sprintf("failed: %s", rule.Expr)
					if v.recordRuleError(&st, CodeBizRuleFailed, TypeBloblang, rule.Path, detail) {
						return result
					}
				}
			} else {
				// Value mode: write computed result to output only when needed.
				// Strict type contract: verify the computed value matches the CUE field type.
				if !v.checkBlobResultType(rule.Path, res, &result, mode) {
					st.markFailed(rule.Path)
					if mode == FailFast {
						return result
					}
					continue
				}
				if result.Output != nil {
					setNestedValue(result.Output, rule.Path, res)
				}
			}
		}
	}
	if v.metrics != nil {
		v.metrics.ObserveLayerDuration(LayerBlob, time.Since(blobStart), v.schemaName)
	}
	if blobSpan != nil {
		blobSpan.End()
	}

	return result
}

func hasValidationErrorAtPath(errors []ValidationError, path string) bool {
	for _, validationErr := range errors {
		if validationErr.Path == path || strings.HasPrefix(validationErr.Path, path+".") ||
			strings.HasPrefix(validationErr.Path, path+"[") {
			return true
		}
	}
	return false
}

// checkBlobResultType verifies that a non-bool @blob result matches the
// declared CUE field type. Returns true if type-compatible, false if a
// E2T01 error was emitted (strict type contract).
func (v *Validator) checkBlobResultType(path string, val any, result *Result, mode FailMode) bool {
	// Look up the field's CUE schema
	field := v.findCUEField(v.cueFields, path)
	if field == nil {
		// No CUE field found — cannot type-check (should not happen for well-formed schemas)
		return true
	}

	// Encode the computed value and unify with the field schema
	encoded := v.ctx.Encode(val)
	unified := field.schema.Unify(encoded)
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		detail := fmt.Sprintf("@blob result type mismatch: computed %T, field expects %s",
			val, cueKindToString(field.schema.IncompleteKind()))
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Code:    CodeBlobTypeMismatch,
			Path:    path,
			Type:    TypeBloblang,
			Message: v.formatMessage(CodeBlobTypeMismatch, path, detail),
		})
		return false
	}
	return true
}

// findCUEField searches the pre-compiled field tree for a field at the given dot-path.
func (v *Validator) findCUEField(fields []cueField, path string) *cueField {
	parts := strings.Split(path, ".")
	current := fields
	for i, part := range parts {
		found := false
		for j := range current {
			if current[j].name == part {
				if i == len(parts)-1 {
					return &current[j]
				}
				current = current[j].children
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return nil
}

// fieldPriorityByPath looks up the priority of a field by its dot-path from cueFields.
func (v *Validator) fieldPriorityByPath(path string) int {
	f := v.findCUEField(v.cueFields, path)
	if f != nil {
		return f.priority
	}
	return 0
}
