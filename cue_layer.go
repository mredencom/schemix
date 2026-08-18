package schemix

import (
	"fmt"
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
func (v *Validator) validateCUEFields(fields []cueField, data *lazyCUEValue, rawData map[string]any, result *Result) {
	for i := range fields {
		f := &fields[i]

		// Fast Go-level existence check before touching CUE.
		// Use field name (not full path) since rawData is the current level map
		goVal, exists := rawData[f.name]
		if !exists {
			// Field is truly missing from input data
			if !f.optional && !f.hasBlob {
				detail := fmt.Sprintf("required field %q is missing", f.name)
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:      CodeRequiredMissing,
					Path:      f.path,
					Type:      TypeCUE,
					Message:   v.formatMessage(CodeRequiredMissing, f.path, detail),
					FieldType: f.fieldType,
				})
			}
			continue
		}
		if goVal == nil {
			// Field exists but value is nil — check if schema allows null
			if f.nullable {
				continue
			}
			// Non-nullable field with nil value → required missing
			detail := fmt.Sprintf("field %q is nil but not nullable", f.path)
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Code:      CodeRequiredMissing,
				Path:      f.path,
				Type:      TypeCUE,
				Message:   v.formatMessage(CodeRequiredMissing, f.path, detail),
				FieldType: f.fieldType,
			})
			continue
		}

		// Optimization #4: Go-native fast path — skip CUE Encode+Unify for simple constraints
		if f.fast != nil {
			fr := validateFast(f.fast, goVal)
			if v.metrics != nil {
				v.metrics.ObserveFastpathDecision(f.path, fr.Handled)
			}
			if fr.Handled {
				if !fr.Valid {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:       fr.Code,
						Path:       f.path,
						Type:       TypeCUE,
						Message:    v.formatMessage(fr.Code, f.path, fr.Detail),
						FieldType:  f.fieldType,
						Suggestion: fr.Suggestion,
					})
				}
				continue
			}
			// fr.Handled=false: fall through to CUE Unify
		}

		// Only now do we touch CUE for actual constraint validation, which is
		// also the point where the lazy encode is forced.
		fieldData := data.value().LookupPath(cue.ParsePath(f.name))
		if !fieldData.Exists() {
			continue
		}

		// Struct validation: recurse into children
		if f.isStruct && fieldData.IncompleteKind() == cue.StructKind {
			nestedRaw, _ := goVal.(map[string]any)
			if nestedRaw != nil && len(f.children) > 0 {
				v.validateCUEFields(f.children, encodedCUEValue(fieldData), nestedRaw, result)
			}
			continue
		}

		// Struct field with wrong type (e.g. int instead of struct)
		if f.isStruct {
			detail := fmt.Sprintf("field %q expects struct, got %T", f.path, goVal)
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Code:      CodeTypeMismatch,
				Path:      f.path,
				Type:      TypeCUE,
				Message:   v.formatMessage(CodeTypeMismatch, f.path, detail),
				FieldType: f.fieldType,
			})
			continue
		}

		// List validation
		if f.isList && fieldData.IncompleteKind() == cue.ListKind {
			listUnified := f.schema.Unify(fieldData)
			if err := listUnified.Validate(cue.Concrete(true)); err != nil {
				cueErrs := cueerrors.Errors(err)
				collected := make([]ValidationError, 0, len(cueErrs))
				for _, e := range cueErrs {
					code := classifyCUEErrorStructured(e)
					if code == CodeCUEOther {
						code = CodeArrayElement
					}
					ePath := formatCUEErrorPath(f.path, e)
					collected = append(collected, ValidationError{
						Code:    code,
						Path:    ePath,
						Type:    TypeCUE,
						Message: e.Error(),
					})
				}
				// CUE emits one error per rejected disjunct plus a summary line;
				// collapse those into a single enum error before formatting, so
				// the caller sees one error per offending field.
				collected = collapseDisjunctionErrors(collected)
				for i := range collected {
					collected[i].Message = v.formatMessage(
						collected[i].Code, collected[i].Path, collected[i].Message)
				}
				result.Valid = false
				result.Errors = append(result.Errors, collected...)
			}
			continue
		}

		// List field with wrong type (e.g. string instead of list)
		if f.isList {
			detail := fmt.Sprintf("field %q expects list, got %T", f.path, goVal)
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Code:      CodeTypeMismatch,
				Path:      f.path,
				Type:      TypeCUE,
				Message:   v.formatMessage(CodeTypeMismatch, f.path, detail),
				FieldType: f.fieldType,
			})
			continue
		}

		// Scalar/enum validation: Unify + Validate
		unified := f.schema.Unify(fieldData)
		if err := unified.Validate(cue.Concrete(true)); err != nil {
			cueErrs := cueerrors.Errors(err)
			for _, e := range cueErrs {
				code := classifyCUEErrorStructured(e)
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:      code,
					Path:      f.path,
					Type:      TypeCUE,
					Message:   v.formatMessage(code, f.path, e.Error()),
					FieldType: f.fieldType,
				})
			}
		}
	}
}

// validateCUERecursive is the legacy recursive validation method.
// Kept for reference; the optimized validateCUEFields is used at runtime.
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

// ─── Global Validator Store ──────────────────────────────────────────────────
