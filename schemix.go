package schemix

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

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
	nullable bool            // schema allows null (e.g. `null | string`)
	hasBlob  bool            // has @blob attribute; absent input may be computed
	isStruct bool            // IncompleteKind == StructKind
	isList   bool            // IncompleteKind == ListKind
	priority int             // @meta(priority=N), default 0
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
	metrics        MetricsRecorder       // optional observability hook (nil = zero overhead)
}

// formatMessage returns the user-facing error message. If an ErrorFormatter is
// configured, it delegates to the formatter; otherwise returns the default detail.
func (v *Validator) formatMessage(code ErrorCode, path, detail string) string {
	if v.errorFormatter != nil {
		return v.errorFormatter(code, path, detail)
	}
	return detail
}

// parseBlob compiles a Bloblang mapping string using the Validator's
// isolated environment (if one exists) or the global environment.
func (v *Validator) parseBlob(mapping string) (*bloblang.Executor, error) {
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
		metrics:        cfg.metricsRecorder,
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
			nullable: fieldSchema.IncompleteKind()&cue.NullKind != 0,
			hasBlob:  blobAttr.Err() == nil,
			isStruct: fieldSchema.IncompleteKind() == cue.StructKind,
			isList:   fieldSchema.IncompleteKind() == cue.ListKind,
			priority: extractFieldPriority(fieldSchema),
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

		meta, err := parsefieldMeta(fieldValue, v.parseBlob)
		if err != nil {
			return fmt.Errorf("field %q @meta: %w", fullPath, err)
		}
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
				exec, err := v.parseBlob(mapping)
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

// withValidationMetrics runs fn and, if a MetricsRecorder is configured,
// times the call and reports (duration, valid) via ObserveValidation.
// When no recorder is configured, fn runs with zero added overhead — no
// timer is started and no interface call is made.
func (v *Validator) withValidationMetrics(fn func() Result) Result {
	if v.metrics == nil {
		return fn()
	}
	start := time.Now()
	result := fn()
	v.metrics.ObserveValidation(time.Since(start), result.Valid)
	return result
}

// Validate performs validation only and returns (valid, errors).
// Unlike Process, it skips deepCopy and Output construction for better performance.
func (v *Validator) Validate(data map[string]any) (bool, []ValidationError) {
	r := v.withValidationMetrics(func() Result {
		return v.processInternal(data, FailAll, false)
	})
	return r.Valid, r.Errors
}

// Process performs validation and value computation using the default FailAll mode.
func (v *Validator) Process(data map[string]any) Result {
	return v.ProcessWithMode(data, FailAll)
}

// ProcessWithMode performs validation and value computation with the specified FailMode.
func (v *Validator) ProcessWithMode(data map[string]any, mode FailMode) Result {
	if err := validateFailMode(mode); err != nil {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}},
		}
	}
	return v.withValidationMetrics(func() Result {
		return v.processInternal(data, mode, true)
	})
}

// ─── Flexible Input API ──────────────────────────────────────────────────────

// ProcessValue validates and processes any supported input type.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
// Returns a config error Result if the input type is unsupported or conversion fails.
func (v *Validator) ProcessValue(data any) Result {
	return v.ProcessValueWithMode(data, FailAll)
}

// ProcessValueWithMode validates and processes any supported input type with the given FailMode.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
func (v *Validator) ProcessValueWithMode(data any, mode FailMode) Result {
	m, err := toMapAny(data)
	if err != nil {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}},
		}
	}
	return v.ProcessWithMode(m, mode)
}

// ValidateValue performs validation (no Output) on any supported input type.
// Accepts: map[string]any, struct, *struct, []byte (JSON), or Processable.
func (v *Validator) ValidateValue(data any) (bool, []ValidationError) {
	m, err := toMapAny(data)
	if err != nil {
		return false, []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: err.Error()}}
	}
	return v.Validate(m)
}

// ProcessStruct validates and processes a struct value with compile-time type safety.
// The struct is converted to map[string]any via JSON serialization (respects json tags).
// For hot paths, implement Processable or pass map[string]any directly.
func ProcessStruct[T any](v *Validator, data T) Result {
	return v.ProcessValue(data)
}

// ProcessStructWithMode is like ProcessStruct but accepts a FailMode.
func ProcessStructWithMode[T any](v *Validator, data T, mode FailMode) Result {
	return v.ProcessValueWithMode(data, mode)
}

// ValidateStruct validates a struct value with compile-time type safety (no Output).
func ValidateStruct[T any](v *Validator, data T) (bool, []ValidationError) {
	return v.ValidateValue(data)
}

// validateFailMode returns an error if mode is not a recognized FailMode value.
func validateFailMode(mode FailMode) error {
	switch mode {
	case FailAll, FailFast, FailPriority:
		return nil
	default:
		return fmt.Errorf("invalid FailMode(%d): must be FailAll, FailFast, or FailPriority", int(mode))
	}
}

// processInternal is the unified validation/processing engine.
// When needOutput is false, it skips deepCopy and all Output mutations for performance.
func (v *Validator) processInternal(data map[string]any, mode FailMode, needOutput bool) (result Result) {
	result = Result{
		Valid:  true,
		Errors: []ValidationError{},
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
	dataValue := v.ctx.Encode(data)
	v.validateCUEFields(v.cueFields, dataValue, data, &result)

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
	failedPaths := map[string]bool{}
	currentPriority := -1
	priorityHasError := false

	for _, rule := range v.blobRules {
		meta := rule.Meta

		if hasValidationErrorAtPath(cueErrors, rule.Path) {
			failedPaths[rule.Path] = true
			continue
		}

		// FailPriority: check priority group transition
		if mode == FailPriority && meta.Priority > currentPriority {
			if priorityHasError {
				break
			}
			currentPriority = meta.Priority
			priorityHasError = false
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
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:    CodeMetaRuntimeError,
					Path:    rule.Path,
					Type:    TypeMeta,
					Message: v.formatMessage(CodeMetaRuntimeError, rule.Path, detail),
				})
				failedPaths[rule.Path] = true
				priorityHasError = true
				if mode == FailFast {
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
		if meta.Optional && fieldVal == nil {
			if meta.OmitEmpty && result.Output != nil {
				deleteNestedKey(result.Output, rule.Path)
			}
			if meta.RequiredIf != nil {
				res, err := meta.RequiredIf.Query(data)
				if err != nil {
					detail := fmt.Sprintf("required_if expression error (%s): %v", meta.RequiredIfExpr, err)
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    CodeMetaRuntimeError,
						Path:    rule.Path,
						Type:    TypeMeta,
						Message: v.formatMessage(CodeMetaRuntimeError, rule.Path, detail),
					})
					failedPaths[rule.Path] = true
					priorityHasError = true
					if mode == FailFast {
						return result
					}
				} else if required, ok := res.(bool); ok && required {
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
			continue
		}

		// conditional + required_if
		if meta.Conditional && fieldVal == nil {
			if meta.RequiredIf != nil {
				res, err := meta.RequiredIf.Query(data)
				if err != nil {
					detail := fmt.Sprintf("required_if expression error (%s): %v", meta.RequiredIfExpr, err)
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Code:    CodeMetaRuntimeError,
						Path:    rule.Path,
						Type:    TypeMeta,
						Message: v.formatMessage(CodeMetaRuntimeError, rule.Path, detail),
					})
					failedPaths[rule.Path] = true
					priorityHasError = true
					if mode == FailFast {
						return result
					}
				} else if required, ok := res.(bool); ok && required {
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
				// Value mode: write computed result to output only when needed.
				// Strict type contract: verify the computed value matches the CUE field type.
				if !v.checkBlobResultType(rule.Path, res, &result, mode) {
					failedPaths[rule.Path] = true
					priorityHasError = true
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

// validateCUEFields validates data against pre-compiled field descriptors.
// This is significantly faster than the old validateCUERecursive because:
//   - Optimization #1: Go map check before CUE LookupPath (fast path for missing fields)
//   - Optimization #2: Field metadata is pre-compiled, no schema.Fields() iteration at runtime
//   - Correctness: present @blob fields still satisfy their CUE constraints before Blob execution
func (v *Validator) validateCUEFields(fields []cueField, data cue.Value, rawData map[string]any, result *Result) {
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
					Code:    CodeRequiredMissing,
					Path:    f.path,
					Type:    TypeCUE,
					Message: v.formatMessage(CodeRequiredMissing, f.path, detail),
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
				Code:    CodeRequiredMissing,
				Path:    f.path,
				Type:    TypeCUE,
				Message: v.formatMessage(CodeRequiredMissing, f.path, detail),
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
						Code:    fr.Code,
						Path:    f.path,
						Type:    TypeCUE,
						Message: v.formatMessage(fr.Code, f.path, fr.Detail),
					})
				}
				continue
			}
			// fr.Handled=false: fall through to CUE Unify
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

		// Struct field with wrong type (e.g. int instead of struct)
		if f.isStruct {
			detail := fmt.Sprintf("field %q expects struct, got %T", f.path, goVal)
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Code:    CodeTypeMismatch,
				Path:    f.path,
				Type:    TypeCUE,
				Message: v.formatMessage(CodeTypeMismatch, f.path, detail),
			})
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
					ePath := formatCUEErrorPath(f.path, e)
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

		// List field with wrong type (e.g. string instead of list)
		if f.isList {
			detail := fmt.Sprintf("field %q expects list, got %T", f.path, goVal)
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Code:    CodeTypeMismatch,
				Path:    f.path,
				Type:    TypeCUE,
				Message: v.formatMessage(CodeTypeMismatch, f.path, detail),
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
					Code:    code,
					Path:    f.path,
					Type:    TypeCUE,
					Message: v.formatMessage(code, f.path, e.Error()),
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

// globalStore uses sync.Map for lock-free reads in the common case.
// This is ideal because validators are registered at init/startup (few writes)
// and looked up per-request (many concurrent reads).
var globalStore sync.Map // map[string]*Validator

// Register stores a pre-compiled Validator globally under name.
// If name already exists, it is silently replaced (use MustRegister to reject duplicates).
func Register(name string, v *Validator) {
	globalStore.Store(name, v)
}

// MustRegister stores a pre-compiled Validator globally under name.
// Panics if name already exists (conflict).
func MustRegister(name string, v *Validator) {
	if _, loaded := globalStore.LoadOrStore(name, v); loaded {
		panic(fmt.Sprintf("schemix.MustRegister: %q already registered", name))
	}
}

// Get retrieves a globally registered Validator by name.
// Returns (nil, false) if name is not registered.
func Get(name string) (*Validator, bool) {
	val, ok := globalStore.Load(name)
	if !ok {
		return nil, false
	}
	return val.(*Validator), true
}

// MustGet retrieves a globally registered Validator by name.
// Panics if name is not registered.
func MustGet(name string) *Validator {
	v, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("schemix.MustGet: %q not registered", name))
	}
	return v
}

// Unregister removes a globally registered Validator by name.
// Returns true if it existed and was removed.
func Unregister(name string) bool {
	_, loaded := globalStore.LoadAndDelete(name)
	return loaded
}

// Has reports whether a Validator with the given name is globally registered.
func Has(name string) bool {
	_, ok := globalStore.Load(name)
	return ok
}

// List returns the names of all globally registered Validators.
func List() []string {
	var names []string
	globalStore.Range(func(key, _ any) bool {
		names = append(names, key.(string))
		return true
	})
	return names
}

// Len returns the number of globally registered Validators.
func Len() int {
	n := 0
	globalStore.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// ProcessWith validates and processes data using a globally registered Validator.
// Accepts any supported input type (map, struct, JSON bytes, Processable).
// Returns a config error if the name is not registered.
func ProcessWith(name string, data any) Result {
	return ProcessWithMode(name, data, FailAll)
}

// ProcessWithMode validates and processes data using a named global Validator with mode.
func ProcessWithMode(name string, data any, mode FailMode) Result {
	v, ok := Get(name)
	if !ok {
		return Result{
			Valid:  false,
			Errors: []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: fmt.Sprintf("schemix: validator %q not registered", name)}},
		}
	}
	return v.ProcessValueWithMode(data, mode)
}

// ValidateWith validates data using a globally registered Validator (no Output).
func ValidateWith(name string, data any) (bool, []ValidationError) {
	v, ok := Get(name)
	if !ok {
		return false, []ValidationError{{Code: CodeConfigError, Type: TypeConfig, Message: fmt.Sprintf("schemix: validator %q not registered", name)}}
	}
	return v.ValidateValue(data)
}

// resetGlobalStore clears the global store. For testing only.
func resetGlobalStore() {
	globalStore.Range(func(key, _ any) bool {
		globalStore.Delete(key)
		return true
	})
}
