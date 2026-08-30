package schemix

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cuelang.org/go/cue"
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

// ruleAction is what a stage of the @blob/@meta rule loop tells its caller to
// do next. A bool would not be enough: a stage can decide that this rule is
// finished (ruleSkip) or that the whole validation is over (ruleAbort, FailFast),
// and collapsing those two would silently turn a fail-fast return into a
// continue — losing the guarantee that FailFast yields exactly one error.
//
// The loop's own `break` cases stay in the loop: they depend on currentPriority,
// which is loop-local bookkeeping no stage owns.
type ruleAction uint8

const (
	ruleRun   ruleAction = iota // stage passed, continue with this rule
	ruleSkip                    // this rule is done, move to the next
	ruleAbort                   // stop the whole validation and return
)

// recordRuleError appends a rule error at path and marks the path failed. It
// reports whether the caller must stop immediately because mode is FailFast.
func (v *Validator) recordRuleError(st *blobLoopState, code ErrorCode, typ ErrorType, path, detail string) bool {
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
		v:      v,
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
	//
	// beginLayer/end are guarded rather than called unconditionally: neither can
	// be inlined (cost 298 and 201 against a budget of 80), so four unguarded
	// calls per Process cost ~25ns — measurable against a 143ns scalar-list
	// validation, and a breach of the documented promise that an unobserved
	// Validator pays nothing. Guarded, an unobserved run makes zero calls.
	observed := v.metrics != nil || v.tracer != nil

	var cueScope layerScope
	if observed {
		cueScope = v.beginLayer(ctx, "schemix.cue", LayerCUE)
	}
	dataValue := newLazyCUEValue(v.ctx, data)
	v.validateCUEFields(v.cueFields, dataValue, data, &result, mode == FailFast)
	if observed {
		cueScope.end()
	}

	if mode == FailFast && !result.Valid {
		if len(result.Errors) > 1 {
			result.Errors = result.Errors[:1]
		}
		return result
	}

	// FailPriority: determine minFailedPriority from CUE errors and filter
	minFailedPriority := math.MaxInt
	if mode == FailPriority && !result.Valid {
		minFailedPriority = v.keepLowestFailedPriority(&result)
	}

	// Preserve the CUE-layer failures before Blob errors are appended. A rule on
	// the same field must never execute after its CUE constraint has failed.
	//
	// A prefix length rather than a copy: the blob loop only ever appends, so the
	// first nCUEErrors entries stay the CUE-layer ones even after a reallocation,
	// since append carries the old contents over. Copying them cost one
	// allocation of len(Errors) ValidationError, which is 136 bytes each.
	nCUEErrors := len(result.Errors)

	// Layer 2: @blob + @meta rules
	var blobScope layerScope
	if observed {
		blobScope = v.beginLayer(ctx, "schemix.blob", LayerBlob)
	}
	currentPriority := -1
	st := blobLoopState{result: &result, failedPaths: map[string]bool{}, mode: mode}

	for i := range v.blobRules {
		rule := &v.blobRules[i]
		meta := &rule.Meta

		if hasValidationErrorAtPath(result.Errors[:nCUEErrors], rule.Path) {
			// Mark only — see markFailed's note on why priorityHasError stays.
			st.failedPaths[rule.Path] = true
			continue
		}

		// FailPriority: check priority group transition
		if mode == FailPriority && meta.Priority > currentPriority {
			if st.priorityHasError {
				break
			}
			// No reset of priorityHasError is needed: the break above is the
			// only way to reach a new group with it set, so it is already false.
			currentPriority = meta.Priority
		}

		// blobRules are sorted by priority, so once this boundary is crossed
		// every remaining rule belongs to a later group.
		if mode == FailPriority && minFailedPriority < math.MaxInt && meta.Priority > minFailedPriority {
			break
		}

		// Field-level fail_fast
		if meta.FailFast && st.failedPaths[rule.Path] {
			continue
		}

		// Each stage below is guarded by its own precondition even though the
		// stage repeats the check internally. None of the three can be inlined
		// (cost 320-744 against a budget of 80), so an unguarded call costs a
		// real call/ret for a rule that carries no such @meta flag at all — and
		// most rules carry none. The stages keep their internal check so they
		// stay correct when called directly.
		if meta.SkipIf != nil {
			switch v.applySkipIf(&st, rule, data) {
			case ruleSkip:
				continue
			case ruleAbort:
				return result
			}
		}

		if meta.SkipEmpty || meta.OmitEmpty || meta.Optional {
			switch v.applyPresenceRules(&st, rule, data) {
			case ruleSkip:
				continue
			case ruleAbort:
				return result
			}
		}

		if rule.Exec != nil {
			if v.execBlobRule(&st, rule, data) == ruleAbort {
				return result
			}
		}
	}
	if observed {
		blobScope.end()
	}

	return result
}

// applySkipIf evaluates @meta(skip_if). A truthy expression skips the rule, and
// honours omit_if_skip on the way out.
func (v *Validator) applySkipIf(st *blobLoopState, rule *blobRule, data map[string]any) ruleAction {
	meta := &rule.Meta
	if meta.SkipIf == nil {
		return ruleRun
	}

	res, err := meta.SkipIf.Query(data)
	if err != nil {
		detail := fmt.Sprintf("skip_if expression error (%s): %v", meta.SkipIfExpr, err)
		if v.recordRuleError(st, CodeMetaRuntimeError, TypeMeta, rule.Path, detail) {
			return ruleAbort
		}
		return ruleSkip
	}

	// A non-bool skip_if is not an error — it simply does not skip.
	if skip, ok := res.(bool); !ok || !skip {
		return ruleRun
	}
	if meta.OmitIfSkip && st.result.Output != nil {
		deleteNestedKey(st.result.Output, rule.Path)
	}
	return ruleSkip
}

// applyPresenceRules handles the @meta flags that depend on whether the field
// carries a value: skip_empty, omit_empty, and optional paired with required_if.
func (v *Validator) applyPresenceRules(st *blobLoopState, rule *blobRule, data map[string]any) ruleAction {
	meta := &rule.Meta
	fieldVal := getNestedValue(data, rule.Path)

	if isEmpty(fieldVal) {
		if meta.SkipEmpty {
			if (meta.OmitIfSkip || meta.OmitEmpty) && st.result.Output != nil {
				deleteNestedKey(st.result.Output, rule.Path)
			}
			return ruleSkip
		}
		if meta.OmitEmpty && st.result.Output != nil {
			deleteNestedKey(st.result.Output, rule.Path)
		}
	}

	// The test below also covers @meta(conditional): parsefieldMeta sets
	// Optional = true whenever the conditional flag is present, so a conditional
	// field always satisfies it and always returns ruleSkip. A separate
	// `meta.Conditional && fieldVal == nil` branch after this one is therefore
	// unreachable — it existed as a duplicate of this block until it was
	// removed. Do not reintroduce it; give Conditional its own non-optional
	// semantics in parsefieldMeta first.
	if !meta.Optional || fieldVal != nil {
		return ruleRun
	}
	if meta.OmitEmpty && st.result.Output != nil {
		deleteNestedKey(st.result.Output, rule.Path)
	}
	if meta.RequiredIf == nil {
		return ruleSkip
	}

	// An absent field is still absent whichever way required_if goes, so every
	// path from here reports the rule as finished rather than runnable.
	res, err := meta.RequiredIf.Query(data)
	if err != nil {
		detail := fmt.Sprintf("required_if expression error (%s): %v", meta.RequiredIfExpr, err)
		if v.recordRuleError(st, CodeMetaRuntimeError, TypeMeta, rule.Path, detail) {
			return ruleAbort
		}
		return ruleSkip
	}
	if required, ok := res.(bool); ok && required {
		detail := fmt.Sprintf("conditional required (%s)", meta.RequiredIfExpr)
		if v.recordRuleError(st, CodeCondRequired, TypeMeta, rule.Path, detail) {
			return ruleAbort
		}
	}
	return ruleSkip
}

// execBlobRule runs the compiled @blob expression. A bool return validates the
// field; any other type is a computed value written to Output after it has been
// checked against the field's declared CUE type.
func (v *Validator) execBlobRule(st *blobLoopState, rule *blobRule, data map[string]any) ruleAction {
	if rule.Exec == nil {
		return ruleRun
	}

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
		if v.recordRuleError(st, CodeExprExecError, TypeBloblang, rule.Path, detail) {
			return ruleAbort
		}
		return ruleRun
	}

	if valid, ok := res.(bool); ok {
		if valid {
			return ruleRun
		}
		detail := fmt.Sprintf("failed: %s", rule.Expr)
		if v.recordRuleError(st, CodeBizRuleFailed, TypeBloblang, rule.Path, detail) {
			return ruleAbort
		}
		return ruleRun
	}

	// Value mode: the computed value must satisfy the field schema before it is
	// allowed into Output, otherwise a rule could widen the schema it validates.
	if !v.checkBlobResultType(rule.Path, res, st.result) {
		st.markFailed(rule.Path)
		if st.mode == FailFast {
			return ruleAbort
		}
		return ruleRun
	}
	if st.result.Output != nil {
		setNestedValue(st.result.Output, rule.Path, res)
	}
	return ruleRun
}

// keepLowestFailedPriority narrows result.Errors to the failing priority group
// with the lowest number and reports that priority, so the blob loop can stop
// once it reaches a later group.
//
// Filtering in place over result.Errors[:0] is safe here because the read index
// always leads the write index.
func (v *Validator) keepLowestFailedPriority(result *Result) int {
	lowest := math.MaxInt
	for _, e := range result.Errors {
		if p := v.fieldPriorityByPath(e.Path); p < lowest {
			lowest = p
		}
	}
	filtered := result.Errors[:0]
	for _, e := range result.Errors {
		if v.fieldPriorityByPath(e.Path) == lowest {
			filtered = append(filtered, e)
		}
	}
	result.Errors = filtered
	return lowest
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
func (v *Validator) checkBlobResultType(path string, val any, result *Result) bool {
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
//
// The path is walked segment by segment rather than through strings.Split,
// which would allocate a slice on every call. fieldPriorityByPath calls this
// twice per error under FailPriority, so those allocations showed up as a
// measurable share of a failing validation.
func (v *Validator) findCUEField(fields []cueField, path string) *cueField {
	current := fields
	for {
		part, rest := path, ""
		if i := strings.IndexByte(path, '.'); i >= 0 {
			part, rest = path[:i], path[i+1:]
		}
		idx := -1
		for j := range current {
			if current[j].name == part {
				idx = j
				break
			}
		}
		if idx < 0 {
			return nil
		}
		if rest == "" {
			return &current[idx]
		}
		current = current[idx].children
		path = rest
	}
}

// fieldPriorityByPath looks up the priority of a field by its dot-path from cueFields.
func (v *Validator) fieldPriorityByPath(path string) int {
	f := v.findCUEField(v.cueFields, path)
	if f != nil {
		return f.priority
	}
	return 0
}
