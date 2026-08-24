package schemix

import (
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	cueerrors "cuelang.org/go/cue/errors"
)

// lazyCUEValue defers cue.Context.Encode until a field actually needs a
// cue.Value. Encoding the input map costs roughly 1.67µs and 39 allocations, so
// a schema whose every field is served by the Go-native fast path must never
// pay for it.
//
// The zero value is not usable; construct with newLazyCUEValue for a root map or
// encodedCUEValue when a cue.Value is already in hand.
type lazyCUEValue struct {
	ctx     *cue.Context
	raw     map[string]any
	val     cue.Value
	encoded bool
}

// newLazyCUEValue wraps a raw input map, deferring the encode.
func newLazyCUEValue(ctx *cue.Context, raw map[string]any) *lazyCUEValue {
	return &lazyCUEValue{ctx: ctx, raw: raw}
}

// encodedCUEValue wraps a cue.Value that has already been resolved (e.g. a
// nested field obtained via LookupPath), so no further encoding is needed.
func encodedCUEValue(val cue.Value) *lazyCUEValue {
	return &lazyCUEValue{val: val, encoded: true}
}

// value returns the cue.Value, encoding on first use. Subsequent calls reuse it.
func (l *lazyCUEValue) value() cue.Value {
	if !l.encoded {
		l.val = l.ctx.Encode(l.raw)
		l.encoded = true
	}
	return l.val
}

// validateCUEFields validates data against pre-compiled field descriptors.
// This is significantly faster than the old validateCUERecursive because:
//   - Optimization #1: Go map check before CUE LookupPath (fast path for missing fields)
//   - Optimization #2: Field metadata is pre-compiled, no schema.Fields() iteration at runtime
//   - Optimization #5: data is a lazyCUEValue, so cue.Context.Encode is skipped
//     entirely when every field is handled by the Go-native fast path
//   - Correctness: present @blob fields still satisfy their CUE constraints before Blob execution
//
// Each field takes at most three steps, in widening order of cost: is a value
// present, can a Go descriptor decide it, and only then CUE.
//
// The presence check stays inlined in the loop rather than living in a helper.
// It runs for every field of every validation, and moving it behind a call cost
// a measured 4.5% on the scalar path — enough to trip the benchmark gate. Only
// the branches that report a problem, or that need CUE, are functions: those run
// rarely enough that a call is free.
//
// stopEarly is set under FailFast, where the walk ends at the first failure.
// A side effect worth stating: ObserveFastpathDecision then reports only the
// fields actually visited, which is fewer than the schema holds. That is the
// point — the remaining work is not done, so it is not reported.
func (v *Validator) validateCUEFields(fields []cueField, data *lazyCUEValue, rawData map[string]any, result *Result, stopEarly bool) {
	for i := range fields {
		// FailFast keeps only the first error, so walking the remaining fields
		// buys nothing — it formats a Message and copies enum candidates for
		// errors that the truncation in processInternal then throws away.
		if stopEarly && !result.Valid {
			return
		}

		f := &fields[i]

		goVal, exists := rawData[f.name]
		if !exists {
			// A @blob field may be computed rather than supplied, so its absence
			// from the input is not in itself an error.
			if !f.optional && !f.hasBlob {
				v.recordMissingField(f, result)
			}
			continue
		}
		if goVal == nil {
			if !f.nullable {
				v.recordNonNullableNil(f, result)
			}
			continue
		}

		if f.fast != nil && v.checkFast(f, goVal, result) {
			continue
		}
		v.validateViaCUE(f, data, goVal, result, stopEarly)
	}
}

// recordMissingField reports a required field absent from the input.
func (v *Validator) recordMissingField(f *cueField, result *Result) {
	v.recordFieldError(result, f, CodeRequiredMissing, f.path,
		fmt.Sprintf("required field %q is missing", f.name))
}

// recordNonNullableNil reports a field present as nil where the schema declares
// no null alternative.
func (v *Validator) recordNonNullableNil(f *cueField, result *Result) {
	v.recordFieldError(result, f, CodeRequiredMissing, f.path,
		fmt.Sprintf("field %q is nil but not nullable", f.path))
}

// validateViaCUE is the slow path, reached only for fields no descriptor could
// decide. Reading the field forces the lazy encode, so everything that could be
// answered without a cue.Value has already been answered.
func (v *Validator) validateViaCUE(f *cueField, data *lazyCUEValue, goVal any, result *Result, stopEarly bool) {
	fieldData := data.value().LookupPath(f.cuePath)
	if !fieldData.Exists() {
		return
	}

	switch {
	case f.isStruct:
		v.validateStructField(f, fieldData, goVal, result, stopEarly)
	case f.isList:
		v.validateListField(f, fieldData, goVal, result)
	default:
		v.validateScalarField(f, fieldData, result)
	}
}

// validateStructField recurses into a nested struct's own descriptors, which is
// what keeps its scalar leaves on the fast path.
func (v *Validator) validateStructField(f *cueField, fieldData cue.Value, goVal any, result *Result, stopEarly bool) {
	if fieldData.IncompleteKind() != cue.StructKind {
		v.recordFieldError(result, f, CodeTypeMismatch, f.path,
			fmt.Sprintf("field %q expects struct, got %T", f.path, goVal))
		return
	}

	nestedRaw, _ := goVal.(map[string]any)
	if nestedRaw == nil || len(f.children) == 0 {
		return
	}
	v.validateCUEFields(f.children, encodedCUEValue(fieldData), nestedRaw, result, stopEarly)
}

// validateListField handles the lists no element descriptor covers — struct
// elements, fixed arity, list-level constraints — by unifying the whole value.
func (v *Validator) validateListField(f *cueField, fieldData cue.Value, goVal any, result *Result) {
	if fieldData.IncompleteKind() != cue.ListKind {
		v.recordFieldError(result, f, CodeTypeMismatch, f.path,
			fmt.Sprintf("field %q expects list, got %T", f.path, goVal))
		return
	}

	err := f.schema.Unify(fieldData).Validate(cue.Concrete(true))
	if err == nil {
		return
	}
	result.Valid = false
	result.Errors = append(result.Errors, v.listErrors(f, err)...)
}

// listErrors maps CUE's diagnostics for a list onto one error per offending
// element, with the index in the path.
func (v *Validator) listErrors(f *cueField, err error) []ValidationError {
	cueErrs := cueerrors.Errors(err)
	collected := make([]ValidationError, 0, len(cueErrs))
	for _, e := range cueErrs {
		code := classifyCUEErrorStructured(e)
		if code == CodeCUEOther {
			code = CodeArrayElement
		}
		collected = append(collected, ValidationError{
			Code:    code,
			Path:    formatCUEErrorPath(f.path, e),
			Type:    TypeCUE,
			Message: e.Error(),
		})
	}

	// CUE emits one error per rejected disjunct plus a summary line; collapse
	// those into a single enum error before formatting, so the caller sees one
	// error per offending field.
	collected = collapseDisjunctionErrors(collected)
	for i := range collected {
		// Message still holds the raw CUE text at this point; lift what a
		// localizer needs before a formatter is given the chance to replace it.
		attachStructuredFields(&collected[i], nil, collected[i].Message)
		collected[i].Message = v.formatMessage(
			collected[i].Code, collected[i].Path, collected[i].Message)
	}
	return collected
}

// validateScalarField covers the scalars a descriptor refused: a constraint
// outside its vocabulary, or a value it could not decide such as a uint.
func (v *Validator) validateScalarField(f *cueField, fieldData cue.Value, result *Result) {
	err := f.schema.Unify(fieldData).Validate(cue.Concrete(true))
	if err == nil {
		return
	}
	for _, e := range cueerrors.Errors(err) {
		v.recordFieldError(result, f, classifyCUEErrorStructured(e), f.path, e.Error())
	}
}

// recordFieldError appends one error against f and marks the result invalid.
//
// The four steps it replaces — flip Valid, build the struct, format the message,
// append — appeared ten times in this file. The fast-path helpers below build
// their own because they also carry a Suggestion.
func (v *Validator) recordFieldError(result *Result, f *cueField, code ErrorCode, path, detail string) {
	result.Valid = false
	e := ValidationError{
		Code:      code,
		Path:      path,
		Type:      TypeCUE,
		FieldType: f.fieldType,
	}
	attachStructuredFields(&e, f.fast, detail)
	e.Message = v.formatMessage(code, path, detail)
	result.Errors = append(result.Errors, e)
}

// checkFast runs the Go-native descriptor for one field and records any
// violation. It reports whether the descriptor decided the field; false means the
// caller must fall through to CUE Unify.
//
// Lists take a separate route because they can reject several elements at once,
// which one fastResult cannot carry.
func (v *Validator) checkFast(f *cueField, goVal any, result *Result) bool {
	if f.fast.kind == constraintList {
		return v.checkFastList(f, goVal, result)
	}

	fr := validateFast(f.fast, goVal)
	if v.metrics != nil {
		v.metrics.ObserveFastpathDecision(f.path, fr.Handled)
	}
	if !fr.Handled {
		return false
	}
	if !fr.Valid {
		result.Valid = false
		e := ValidationError{
			Code:       fr.Code,
			Path:       f.path,
			Type:       TypeCUE,
			FieldType:  f.fieldType,
			Suggestion: fr.Suggestion,
		}
		attachStructuredFields(&e, f.fast, fr.Detail)
		e.Message = v.formatMessage(fr.Code, f.path, fr.Detail)
		result.Errors = append(result.Errors, e)
	}
	return true
}

// checkFastList records one error per rejected element, indexed the way CUE does
// it: items[1], not items.
func (v *Validator) checkFastList(f *cueField, goVal any, result *Result) bool {
	failures, handled := validateFastElements(f.fast, goVal)
	if v.metrics != nil {
		v.metrics.ObserveFastpathDecision(f.path, handled)
	}
	if !handled {
		return false
	}
	if len(failures) > 0 {
		result.Valid = false
		for _, ef := range failures {
			ePath := indexedPath(f.path, ef.Index)
			e := ValidationError{
				Code:       ef.Code,
				Path:       ePath,
				Type:       TypeCUE,
				FieldType:  f.fieldType,
				Suggestion: ef.Suggestion,
			}
			// Candidates live on the element descriptor, not the list's own.
			attachStructuredFields(&e, f.fast.elem, ef.Detail)
			e.Message = v.formatMessage(ef.Code, ePath, ef.Detail)
			result.Errors = append(result.Errors, e)
		}
	}
	return true
}

// validateCUERecursive is the pre-descriptor validation path, walking the schema
// with schema.Fields() on every call instead of using compiled descriptors.
//
// It is not dead code and not merely "kept for reference": BenchmarkCUE_
// ValidateRecursive_Legacy measures it, and that measurement is the denominator
// behind the ~172x fast-path figure in the README. Deleting it would remove the
// evidence for a published claim.
func (v *Validator) validateCUERecursive(schema, data cue.Value, prefix string, result *Result) {
	if schema.IncompleteKind() != cue.StructKind {
		return
	}

	iter, err := schema.Fields(cue.Optional(true))
	if err != nil {
		return
	}

	for iter.Next() {
		fieldName := strings.TrimSuffix(iter.Selector().String(), "?")
		fieldSchema := iter.Value()
		isOptional := iter.IsOptional()

		fullPath := fieldName
		if prefix != "" {
			fullPath = prefix + "." + fieldName
		}

		fieldData := data.LookupPath(cue.ParsePath(fieldName))
		if !fieldData.Exists() {
			continue
		}

		// Skip computed fields (have @blob)
		blobAttr := fieldSchema.Attribute(attrBlob)
		hasBlob := blobAttr.Err() == nil

		unified := fieldSchema.Unify(fieldData)
		if err := unified.Validate(cue.Concrete(true)); err != nil {
			if hasBlob {
				continue
			}
			if !isOptional {
				cueErrs := cueerrors.Errors(err)
				for _, e := range cueErrs {
					code := classifyCUEErrorStructured(e)
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    code,
						Path:    fullPath,
						Type:    TypeCUE,
						Message: v.formatMessage(code, fullPath, e.Error()),
					})
				}
			}
			continue
		}

		// Recurse into nested struct
		if fieldSchema.IncompleteKind() == cue.StructKind && fieldData.IncompleteKind() == cue.StructKind {
			v.validateCUERecursive(fieldSchema, fieldData, fullPath, result)
		}

		// Array validation
		if fieldSchema.IncompleteKind() == cue.ListKind && fieldData.IncompleteKind() == cue.ListKind {
			listUnified := fieldSchema.Unify(fieldData)
			if err := listUnified.Validate(cue.Concrete(true)); err != nil {
				cueErrs := cueerrors.Errors(err)
				for _, e := range cueErrs {
					code := classifyCUEErrorStructured(e)
					if code == CodeCUEOther {
						code = CodeArrayElement
					}
					ePath := formatCUEErrorPath(fullPath, e)
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    code,
						Path:    ePath,
						Type:    TypeCUE,
						Message: v.formatMessage(code, ePath, e.Error()),
					})
				}
			}
		}
	}
}

// ─── Error path formatting ───────────────────────────────────────────────────

// indexedPath renders the path of a list element as parent[i], matching the
// form formatCUEErrorPath produces for errors that come back from CUE.
func indexedPath(parent string, index int) string {
	buf := make([]byte, 0, len(parent)+12)
	buf = append(buf, parent...)
	buf = append(buf, '[')
	buf = strconv.AppendInt(buf, int64(index), 10)
	buf = append(buf, ']')
	return string(buf)
}

// formatCUEErrorPath formats a structured CUE error path using Go-style array indices.
func formatCUEErrorPath(parent string, err error) string {
	segments := cueerrors.Path(err)
	if len(segments) == 0 {
		return parent
	}

	var path strings.Builder
	for _, segment := range segments {
		if _, parseErr := strconv.ParseUint(segment, 10, 64); parseErr == nil {
			path.WriteByte('[')
			path.WriteString(segment)
			path.WriteByte(']')
			continue
		}
		if path.Len() > 0 {
			path.WriteByte('.')
		}
		path.WriteString(segment)
	}

	formatted := path.String()
	if parent == "" || formatted == parent || strings.HasPrefix(formatted, parent+".") ||
		strings.HasPrefix(formatted, parent+"[") {
		return formatted
	}
	return parent + "." + formatted
}
