package schemix

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
)

// cueField is a pre-compiled field descriptor extracted at schema parse time.
// This avoids calling schema.Fields() on every Process call (optimization #3).
type cueField struct {
	name     string    // field name (without "?")
	path     string    // full dot-separated path
	schema   cue.Value // pre-resolved schema value
	optional bool      // whether the field is optional
	hasBlob  bool      // has @blob attribute — skip CUE validation (optimization #1)
	isStruct bool      // IncompleteKind == StructKind
	isList   bool      // IncompleteKind == ListKind
	children []cueField // nested struct fields (pre-compiled recursively)
}

// Validator is a schema-driven validation and transformation engine.
// It combines CUE static constraints with Bloblang dynamic expressions,
// supporting recursive multi-level validation, structured error codes,
// and configurable fail strategies.
type Validator struct {
	ctx       *cue.Context
	schema    cue.Value
	blobRules []blobRule
	cueFields []cueField // pre-compiled field descriptors for fast runtime validation
}

// New creates a Validator from a CUE schema string.
// The schema may use @blob() for dynamic expressions and @meta() for field controls.
func New(cueSrc string) (*Validator, error) {
	ctx := cuecontext.New()
	return NewWithContext(ctx, cueSrc)
}

// NewWithContext creates a Validator from a CUE schema string using a shared
// CUE context. This is more efficient when creating many validators, as they
// can share compilation state.
func NewWithContext(ctx *cue.Context, cueSrc string) (*Validator, error) {
	schema := ctx.CompileString(cueSrc)
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("CUE compile error: %w", err)
	}

	v := &Validator{
		ctx:    ctx,
		schema: schema,
	}

	if err := v.extractRules(schema, ""); err != nil {
		return nil, err
	}

	sortblobRules(v.blobRules)

	// Optimization #3: pre-compile CUE field descriptors
	v.cueFields = compileCUEFields(schema, "")

	return v, nil
}

// MustNew is like New but panics on error. Useful for package-level
// initialization with schema literals.
func MustNew(cueSrc string) *Validator {
	v, err := New(cueSrc)
	if err != nil {
		panic(fmt.Sprintf("schemix.MustNew: %v", err))
	}
	return v
}

// compileCUEFields recursively extracts field metadata at compile time.
func compileCUEFields(schema cue.Value, prefix string) []cueField {
	if schema.IncompleteKind() != cue.StructKind {
		return nil
	}

	iter, err := schema.Fields(cue.Optional(true))
	if err != nil {
		return nil
	}

	var fields []cueField
	for iter.Next() {
		name := strings.TrimSuffix(iter.Selector().String(), "?")
		fieldSchema := iter.Value()

		fullPath := name
		if prefix != "" {
			fullPath = prefix + "." + name
		}

		blobAttr := fieldSchema.Attribute(attrBlob)

		f := cueField{
			name:     name,
			path:     fullPath,
			schema:   fieldSchema,
			optional: iter.IsOptional(),
			hasBlob:  blobAttr.Err() == nil,
			isStruct: fieldSchema.IncompleteKind() == cue.StructKind,
			isList:   fieldSchema.IncompleteKind() == cue.ListKind,
		}

		// Recursively compile nested struct fields
		if f.isStruct {
			f.children = compileCUEFields(fieldSchema, fullPath)
		}

		fields = append(fields, f)
	}

	return fields
}

// extractRules recursively extracts @blob and @meta rules from all struct levels.
func (v *Validator) extractRules(val cue.Value, prefix string) error {
	if val.IncompleteKind() != cue.StructKind {
		return nil
	}

	iter, err := val.Fields(cue.Attributes(true), cue.Optional(true))
	if err != nil {
		return nil
	}

	for iter.Next() {
		fieldName := strings.TrimSuffix(iter.Selector().String(), "?")
		fieldValue := iter.Value()
		isOptional := iter.IsOptional()

		fullPath := fieldName
		if prefix != "" {
			fullPath = prefix + "." + fieldName
		}

		meta := parsefieldMeta(fieldValue)
		if isOptional {
			meta.Optional = true
		}

		attr := fieldValue.Attribute(attrBlob)
		if attr.Err() == nil {
			numArgs := attr.NumArgs()
			for i := range numArgs {
				key, _ := attr.Arg(i)
				expr := strings.TrimSpace(key)
				if expr == "" {
					continue
				}
				mapping := fmt.Sprintf(blobMappingTemplate, expr)
				exec, err := bloblang.Parse(mapping)
				if err != nil {
					return fmt.Errorf("field %q @blob(%s) compile error: %w", fullPath, expr, err)
				}
				v.blobRules = append(v.blobRules, blobRule{
					Path: fullPath,
					Exec: exec,
					Expr: expr,
					Meta: meta,
				})
			}
		}

		// Record meta-only nodes (for required_if/skip_if/omit controls without @blob)
		if attr.Err() != nil && (meta.RequiredIf != nil || meta.SkipIf != nil ||
			meta.SkipEmpty || meta.OmitEmpty || meta.OmitIfSkip) {
			v.blobRules = append(v.blobRules, blobRule{
				Path: fullPath,
				Meta: meta,
			})
		}

		// Recurse into nested structs
		if fieldValue.IncompleteKind() == cue.StructKind {
			if err := v.extractRules(fieldValue, fullPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// Validate performs validation only and returns (valid, errors).
func (v *Validator) Validate(data map[string]any) (bool, []ValidationError) {
	r := v.Process(data)
	return r.Valid, r.Errors
}

// Process performs validation and value computation using the default FailAll mode.
func (v *Validator) Process(data map[string]any) Result {
	return v.ProcessWithMode(data, FailAll)
}

// ProcessWithMode performs validation and value computation with the specified FailMode.
func (v *Validator) ProcessWithMode(data map[string]any, mode FailMode) Result {
	result := Result{
		Valid:  true,
		Errors: []ValidationError{},
		Output: deepCopy(data),
	}

	// Layer 1: CUE validation using pre-compiled field descriptors
	dataValue := v.ctx.Encode(data)
	v.validateCUEFields(v.cueFields, dataValue, data, &result)

	if mode == FailFast && !result.Valid {
		if len(result.Errors) > 1 {
			result.Errors = result.Errors[:1]
		}
		return result
	}

	if mode == FailPriority && !result.Valid {
		return result
	}

	// Layer 2: @blob + @meta rules
	failedPaths := map[string]bool{}
	currentPriority := -1
	priorityHasError := false

	for _, rule := range v.blobRules {
		meta := rule.Meta

		// FailPriority: check priority group transition
		if mode == FailPriority && meta.Priority > currentPriority {
			if priorityHasError {
				break
			}
			currentPriority = meta.Priority
			priorityHasError = false
		}

		// Field-level fail_fast
		if meta.FailFast && failedPaths[rule.Path] {
			continue
		}

		// skip_if
		if meta.SkipIf != nil {
			if res, err := meta.SkipIf.Query(data); err == nil {
				if skip, ok := res.(bool); ok && skip {
					if meta.OmitIfSkip {
						deleteNestedKey(result.Output, rule.Path)
					}
					continue
				}
			}
		}

		// Get field value
		fieldVal := getNestedValue(data, rule.Path)
		fieldEmpty := isEmpty(fieldVal)

		// skip_empty
		if meta.SkipEmpty && fieldEmpty {
			if meta.OmitIfSkip || meta.OmitEmpty {
				deleteNestedKey(result.Output, rule.Path)
			}
			continue
		}

		// omit_empty
		if meta.OmitEmpty && fieldEmpty {
			deleteNestedKey(result.Output, rule.Path)
		}

		// optional + required_if
		if meta.Optional && fieldVal == nil {
			if meta.OmitEmpty {
				deleteNestedKey(result.Output, rule.Path)
			}
			if meta.RequiredIf != nil {
				if res, err := meta.RequiredIf.Query(data); err == nil {
					if required, ok := res.(bool); ok && required {
						result.Valid = false
						result.Errors = append(result.Errors, ValidationError{
							Code:    CodeCondRequired,
							Path:    rule.Path,
							Type:    TypeMeta,
							Message: fmt.Sprintf("conditional required (%s)", meta.RequiredIfExpr),
						})
						failedPaths[rule.Path] = true
						priorityHasError = true
						if mode == FailFast {
							return result
						}
					}
				}
			}
			continue
		}

		// conditional + required_if
		if meta.Conditional && fieldVal == nil {
			if meta.RequiredIf != nil {
				if res, err := meta.RequiredIf.Query(data); err == nil {
					if required, ok := res.(bool); ok && required {
						result.Valid = false
						result.Errors = append(result.Errors, ValidationError{
							Code:    CodeCondRequired,
							Path:    rule.Path,
							Type:    TypeMeta,
							Message: fmt.Sprintf("conditional required (%s)", meta.RequiredIfExpr),
						})
						failedPaths[rule.Path] = true
						priorityHasError = true
						if mode == FailFast {
							return result
						}
					}
				}
			}
			continue
		}

		// @blob execution
		if rule.Exec != nil {
			res, err := rule.Exec.Query(data)
			if err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:    CodeExprExecError,
					Path:    rule.Path,
					Type:    TypeBloblang,
					Message: fmt.Sprintf("expression error: %v", err),
				})
				failedPaths[rule.Path] = true
				priorityHasError = true
				if mode == FailFast {
					return result
				}
				continue
			}

			if valid, ok := res.(bool); ok {
				if !valid {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    CodeBizRuleFailed,
						Path:    rule.Path,
						Type:    TypeBloblang,
						Message: fmt.Sprintf("failed: %s", rule.Expr),
					})
					failedPaths[rule.Path] = true
					priorityHasError = true
					if mode == FailFast {
						return result
					}
				}
			} else {
				// Value mode: write computed result to output
				setNestedValue(result.Output, rule.Path, res)
			}
		}
	}

	return result
}

// validateCUEFields validates data against pre-compiled field descriptors.
// This is significantly faster than the old validateCUERecursive because:
//   - Optimization #1: @blob fields are skipped BEFORE Unify (zero cost)
//   - Optimization #2: Go map check before CUE LookupPath (fast path for missing fields)
//   - Optimization #3: Field metadata is pre-compiled, no schema.Fields() iteration at runtime
func (v *Validator) validateCUEFields(fields []cueField, data cue.Value, rawData map[string]any, result *Result) {
	for i := range fields {
		f := &fields[i]

		// Optimization #1: skip @blob fields entirely — they are validated by Bloblang layer
		if f.hasBlob {
			continue
		}

		// Optimization #2: fast Go-level existence check before touching CUE
		// Use field name (not full path) since rawData is the current level map
		goVal, exists := rawData[f.name]
		if !exists || goVal == nil {
			continue
		}

		// Only now do we touch CUE for actual constraint validation
		fieldData := data.LookupPath(cue.ParsePath(f.name))
		if !fieldData.Exists() {
			continue
		}

		// Struct validation: recurse into children
		if f.isStruct && fieldData.IncompleteKind() == cue.StructKind {
			nestedRaw, _ := goVal.(map[string]any)
			if nestedRaw != nil && len(f.children) > 0 {
				v.validateCUEFields(f.children, fieldData, nestedRaw, result)
			}
			continue
		}

		// List validation
		if f.isList && fieldData.IncompleteKind() == cue.ListKind {
			listUnified := f.schema.Unify(fieldData)
			if err := listUnified.Validate(cue.Concrete(true)); err != nil {
				cueErrs := cueerrors.Errors(err)
				for _, e := range cueErrs {
					code := classifyCUEError(e.Error())
					if code == CodeCUEOther {
						code = CodeArrayElement
					}
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    code,
						Path:    f.path + "." + extractIndex(e.Error()),
						Type:    TypeCUE,
						Message: e.Error(),
					})
				}
			}
			continue
		}

		// Scalar/enum validation: Unify + Validate
		unified := f.schema.Unify(fieldData)
		if err := unified.Validate(cue.Concrete(true)); err != nil {
			if !f.optional {
				cueErrs := cueerrors.Errors(err)
				for _, e := range cueErrs {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    classifyCUEError(e.Error()),
						Path:    f.path,
						Type:    TypeCUE,
						Message: e.Error(),
					})
				}
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
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    classifyCUEError(e.Error()),
						Path:    fullPath,
						Type:    TypeCUE,
						Message: e.Error(),
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
					code := classifyCUEError(e.Error())
					if code == CodeCUEOther {
						code = CodeArrayElement
					}
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    code,
						Path:    fullPath + "." + extractIndex(e.Error()),
						Type:    TypeCUE,
						Message: e.Error(),
					})
				}
			}
		}
	}
}
