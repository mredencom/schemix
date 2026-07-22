package schemix

import (
	"fmt"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// cueField is a pre-compiled field descriptor extracted at schema parse time.
// This avoids calling schema.Fields() on every Process call (optimization #3).
type cueField struct {
	name     string          // field name (without "?")
	path     string          // full dot-separated path
	schema   cue.Value       // pre-resolved schema value
	optional bool            // whether the field is optional
	hasBlob  bool            // has @blob attribute — skip CUE validation (optimization #1)
	isStruct bool            // IncompleteKind == StructKind
	isList   bool            // IncompleteKind == ListKind
	fast     *fastConstraint // Go-native fast check (nil = use CUE path)
	children []cueField      // nested struct fields (pre-compiled recursively)
}

// Validator is a schema-driven validation and transformation engine.
// It combines CUE static constraints with Bloblang dynamic expressions,
// supporting recursive multi-level validation, structured error codes,
// and configurable fail strategies.
//
// Validator is safe for concurrent use after construction.
type Validator struct {
	ctx            *cue.Context
	schema         cue.Value
	blobRules      []blobRule
	cueFields      []cueField            // pre-compiled field descriptors for fast runtime validation
	errorFormatter ErrorFormatter        // optional custom error message formatter
	blobEnv        *bloblang.Environment // isolated Bloblang environment (nil = use global)
}

// formatMessage returns the user-facing error message. If an ErrorFormatter is
// configured, it delegates to the formatter; otherwise returns the default detail.
func (v *Validator) formatMessage(code ErrorCode, path, detail string) string {
	if v.errorFormatter != nil {
		return v.errorFormatter(code, path, detail)
	}
	return detail
}

// parseBloblang compiles a Bloblang mapping string using the Validator's
// isolated environment (if one exists) or the global environment.
func (v *Validator) parseBloblang(mapping string) (*bloblang.Executor, error) {
	if v.blobEnv != nil {
		return v.blobEnv.Parse(mapping)
	}
	return bloblang.Parse(mapping)
}

// buildBlobEnv creates a Bloblang environment with built-in validators and
// any user-registered custom functions/methods.
//
// Optimization: built-in methods are registered once into a shared base
// environment (package-level sync.Once). When no custom functions are needed,
// the shared environment is returned directly (zero allocation). When custom
// functions exist, it clones the shared env and appends registrations.
//
// Conflict detection: if a user-registered name collides with a built-in
// method/function, an error is returned.
func buildBlobEnv(cfg *validatorConfig) (*bloblang.Environment, error) {
	base, err := getBaseEnv()
	if err != nil {
		return nil, err
	}

	// No custom functions — reuse shared environment directly (zero cost)
	if len(cfg.customFuncs) == 0 {
		return base, nil
	}

	// Check for conflicts with built-in names (skip if overrideAll)
	if !cfg.overrideAll {
		if err := checkBuiltinConflicts(cfg.customFuncs, cfg.allowMethodOverrides, cfg.allowFuncOverrides); err != nil {
			return nil, err
		}
	}

	// Clone the shared base and add custom registrations
	env := base.Clone()
	for _, entry := range cfg.customFuncs {
		var regErr error
		switch entry.kind {
		case kindFuncV1:
			regErr = env.RegisterFunction(entry.name, entry.funcV1)
		case kindFuncV2:
			regErr = env.RegisterFunctionV2(entry.name, entry.spec, entry.funcV2)
		case kindMethodV1:
			regErr = env.RegisterMethod(entry.name, entry.methodV1)
		case kindMethodV2:
			regErr = env.RegisterMethodV2(entry.name, entry.spec, entry.methodV2)
		}
		if regErr != nil {
			return nil, fmt.Errorf("register %q: %w", entry.name, regErr)
		}
	}
	return env, nil
}

// builtinMethodNames and builtinFuncNames hold built-in names by namespace.
var (
	builtinMethodNames = func() map[string]bool {
		names := make(map[string]bool)
		for _, m := range builtinMethods() {
			names[m.name] = true
		}
		return names
	}()

	builtinFuncNames = func() map[string]bool {
		names := make(map[string]bool)
		for _, f := range builtinFunctions() {
			names[f.name] = true
		}
		return names
	}()
)

// checkBuiltinConflicts returns an error if any user-registered name conflicts
// with a built-in in the SAME namespace, unless explicitly allowed.
func checkBuiltinConflicts(entries []customFuncEntry, allowedMethods, allowedFuncs []string) error {
	methodSet := make(map[string]bool, len(allowedMethods))
	for _, name := range allowedMethods {
		methodSet[name] = true
	}
	funcSet := make(map[string]bool, len(allowedFuncs))
	for _, name := range allowedFuncs {
		funcSet[name] = true
	}
	for _, e := range entries {
		switch e.kind {
		case kindMethodV1, kindMethodV2:
			if builtinMethodNames[e.name] && !methodSet[e.name] {
				return fmt.Errorf("method %q conflicts with a built-in validator; use WithOverrideMethod(%q) to allow", e.name, e.name)
			}
		case kindFuncV1, kindFuncV2:
			if builtinFuncNames[e.name] && !funcSet[e.name] {
				return fmt.Errorf("function %q conflicts with a built-in validator; use WithOverrideFunc(%q) to allow", e.name, e.name)
			}
		}
	}
	return nil
}

// baseEnv is the shared Bloblang environment with all schemix built-in methods
// pre-registered. Initialized once via sync.Once.
var (
	baseEnv     *bloblang.Environment
	baseEnvErr  error
	baseEnvOnce sync.Once
)

// getBaseEnv returns the shared base environment, initializing it on first call.
func getBaseEnv() (*bloblang.Environment, error) {
	baseEnvOnce.Do(func() {
		env := bloblang.NewEnvironment()
		baseEnvErr = registerBuiltins(env)
		if baseEnvErr == nil {
			baseEnv = env
		}
	})
	return baseEnv, baseEnvErr
}

// registerBuiltins registers all schemix built-in validation methods and functions
// into the given Bloblang environment.
func registerBuiltins(env *bloblang.Environment) error {
	for _, m := range builtinMethods() {
		if err := env.RegisterMethodV2(m.name, m.spec, m.ctor); err != nil {
			return err
		}
	}
	for _, f := range builtinFunctions() {
		if err := env.RegisterFunctionV2(f.name, f.spec, f.ctor); err != nil {
			return err
		}
	}
	return nil
}

// New creates a Validator from a CUE schema string.
// The schema may use @blob() for dynamic expressions and @meta() for field controls.
func New(cueSrc string, opts ...Option) (*Validator, error) {
	ctx := cuecontext.New()
	return NewWithContext(ctx, cueSrc, opts...)
}

// NewWithContext creates a Validator from a CUE schema string using a shared
// CUE context. This is more efficient when creating many validators, as they
// can share compilation state.
func NewWithContext(ctx *cue.Context, cueSrc string, opts ...Option) (*Validator, error) {
	schema := ctx.CompileString(cueSrc)
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("CUE compile error: %w", err)
	}
	return buildValidator(ctx, schema, opts)
}

// MustNew is like New but panics on error. Useful for package-level
// initialization with schema literals.
func MustNew(cueSrc string, opts ...Option) *Validator {
	v, err := New(cueSrc, opts...)
	if err != nil {
		panic(fmt.Sprintf("schemix.MustNew: %v", err))
	}
	return v
}

// NewFromValue creates a Validator from a pre-compiled CUE value.
// This enables schema composition by allowing users to build complex schemas
// using CUE's native import/definition mechanisms and pass the result directly.
//
// Example:
//
//	ctx := cuecontext.New()
//	defs := ctx.CompileString(`#PAN: =~"^[0-9]{16}$"`)
//	schema := ctx.CompileString(`{ pan: #PAN, amount: int & >0 }`, cue.Scope(defs))
//	v, err := schemix.NewFromValue(schema)
func NewFromValue(schema cue.Value, opts ...Option) (*Validator, error) {
	if err := schema.Err(); err != nil {
		return nil, fmt.Errorf("CUE value error: %w", err)
	}
	return buildValidator(cuecontext.New(), schema, opts)
}

// buildValidator is the shared constructor logic for all New* functions.
func buildValidator(ctx *cue.Context, schema cue.Value, opts []Option) (*Validator, error) {
	cfg := &validatorConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.funcMapErr != nil {
		return nil, cfg.funcMapErr
	}

	env, err := buildBlobEnv(cfg)
	if err != nil {
		return nil, err
	}

	v := &Validator{
		ctx:            ctx,
		schema:         schema,
		errorFormatter: cfg.errorFormatter,
		blobEnv:        env,
	}

	if err := v.extractRules(schema, ""); err != nil {
		return nil, err
	}
	sortBlobRules(v.blobRules)
	v.cueFields = compileCUEFields(schema, "")

	return v, nil
}

// Fields returns the schema's field descriptors for runtime introspection.
// This is useful for generating documentation, API specs, or UI forms.
func (v *Validator) Fields() []FieldInfo {
	return convertCUEFields(v.cueFields)
}

// convertCUEFields recursively converts internal cueField descriptors to exported FieldInfo.
func convertCUEFields(fields []cueField) []FieldInfo {
	if len(fields) == 0 {
		return []FieldInfo{}
	}
	result := make([]FieldInfo, len(fields))
	for i := range fields {
		f := &fields[i]
		result[i] = FieldInfo{
			Name:     f.name,
			Path:     f.path,
			Type:     cueKindToString(f.schema.IncompleteKind()),
			Optional: f.optional,
			HasBlob:  f.hasBlob,
		}
		if len(f.children) > 0 {
			result[i].Children = convertCUEFields(f.children)
		}
	}
	return result
}

// cueKindToString maps a CUE IncompleteKind to a human-readable type string.
func cueKindToString(k cue.Kind) string {
	switch k {
	case cue.StringKind:
		return "string"
	case cue.IntKind:
		return "int"
	case cue.FloatKind:
		return "float"
	case cue.NumberKind:
		return "number"
	case cue.BoolKind:
		return "bool"
	case cue.StructKind:
		return "struct"
	case cue.ListKind:
		return "list"
	default:
		return "unknown"
	}
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

		// Check if @meta marks the field as optional/conditional
		isOptional := iter.IsOptional()
		if !isOptional {
			metaAttr := fieldSchema.Attribute(attrMeta)
			if metaAttr.Err() == nil {
				for i := range metaAttr.NumArgs() {
					key, _ := metaAttr.Arg(i)
					key = strings.TrimSpace(key)
					if key == metaOptional || key == metaConditional {
						isOptional = true
						break
					}
				}
			}
		}

		f := cueField{
			name:     name,
			path:     fullPath,
			schema:   fieldSchema,
			optional: isOptional,
			hasBlob:  blobAttr.Err() == nil,
			isStruct: fieldSchema.IncompleteKind() == cue.StructKind,
			isList:   fieldSchema.IncompleteKind() == cue.ListKind,
		}

		// Recursively compile nested struct fields
		if f.isStruct {
			f.children = compileCUEFields(fieldSchema, fullPath)
		}

		// Optimization #4: extract Go-native fast constraint for scalar fields
		if !f.hasBlob && !f.isStruct && !f.isList {
			f.fast = extractFastConstraint(fieldSchema)
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

		meta := parsefieldMeta(fieldValue, v.parseBloblang)
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
				exec, err := v.parseBloblang(mapping)
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
// Unlike Process, it skips deepCopy and Output construction for better performance.
func (v *Validator) Validate(data map[string]any) (bool, []ValidationError) {
	r := v.processInternal(data, FailAll, false)
	return r.Valid, r.Errors
}

// Process performs validation and value computation using the default FailAll mode.
func (v *Validator) Process(data map[string]any) Result {
	return v.ProcessWithMode(data, FailAll)
}

// ProcessWithMode performs validation and value computation with the specified FailMode.
func (v *Validator) ProcessWithMode(data map[string]any, mode FailMode) Result {
	return v.processInternal(data, mode, true)
}

// processInternal is the unified validation/processing engine.
// When needOutput is false, it skips deepCopy and all Output mutations for performance.
func (v *Validator) processInternal(data map[string]any, mode FailMode, needOutput bool) Result {
	result := Result{
		Valid:  true,
		Errors: []ValidationError{},
	}

	if needOutput {
		result.Output = deepCopy(data)
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
					if meta.OmitIfSkip && result.Output != nil {
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
		if meta.Optional && fieldVal == nil {
			if meta.OmitEmpty && result.Output != nil {
				deleteNestedKey(result.Output, rule.Path)
			}
			if meta.RequiredIf != nil {
				if res, err := meta.RequiredIf.Query(data); err == nil {
					if required, ok := res.(bool); ok && required {
						detail := fmt.Sprintf("conditional required (%s)", meta.RequiredIfExpr)
						result.Valid = false
						result.Errors = append(result.Errors, ValidationError{
							Code:    CodeCondRequired,
							Path:    rule.Path,
							Type:    TypeMeta,
							Message: v.formatMessage(CodeCondRequired, rule.Path, detail),
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
						detail := fmt.Sprintf("conditional required (%s)", meta.RequiredIfExpr)
						result.Valid = false
						result.Errors = append(result.Errors, ValidationError{
							Code:    CodeCondRequired,
							Path:    rule.Path,
							Type:    TypeMeta,
							Message: v.formatMessage(CodeCondRequired, rule.Path, detail),
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
				detail := fmt.Sprintf("expression error: %v", err)
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:    CodeExprExecError,
					Path:    rule.Path,
					Type:    TypeBloblang,
					Message: v.formatMessage(CodeExprExecError, rule.Path, detail),
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
					detail := fmt.Sprintf("failed: %s", rule.Expr)
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    CodeBizRuleFailed,
						Path:    rule.Path,
						Type:    TypeBloblang,
						Message: v.formatMessage(CodeBizRuleFailed, rule.Path, detail),
					})
					failedPaths[rule.Path] = true
					priorityHasError = true
					if mode == FailFast {
						return result
					}
				}
			} else {
				// Value mode: write computed result to output only when needed
				if result.Output != nil {
					setNestedValue(result.Output, rule.Path, res)
				}
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
		if !exists {
			// Field is truly missing from input data
			if !f.optional && !f.hasBlob {
				detail := fmt.Sprintf("required field %q is missing", f.name)
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:    CodeRequiredMissing,
					Path:    f.path,
					Type:    TypeCUE,
					Message: v.formatMessage(CodeRequiredMissing, f.path, detail),
				})
			}
			continue
		}
		if goVal == nil {
			// Field exists but value is nil — let CUE validate nullability
			// (e.g. `null | string` schemas allow nil)
			continue
		}

		// Optimization #4: Go-native fast path — skip CUE Encode+Unify for simple constraints
		if f.fast != nil {
			valid, code, detail := validateFast(f.fast, goVal)
			if !valid {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:    code,
					Path:    f.path,
					Type:    TypeCUE,
					Message: v.formatMessage(code, f.path, detail),
				})
			}
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
					code := classifyCUEErrorStructured(e)
					if code == CodeCUEOther {
						code = CodeArrayElement
					}
					ePath := f.path + "." + extractIndex(e.Error())
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    code,
						Path:    ePath,
						Type:    TypeCUE,
						Message: v.formatMessage(code, ePath, e.Error()),
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
					code := classifyCUEErrorStructured(e)
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    code,
						Path:    f.path,
						Type:    TypeCUE,
						Message: v.formatMessage(code, f.path, e.Error()),
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
					ePath := fullPath + "." + extractIndex(e.Error())
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
