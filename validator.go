package schemix

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/warpstreamlabs/bento/public/bloblang"
	"go.opentelemetry.io/otel/trace"
)

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
	localizer      Localizer             // default for Result.LocalizedMessages (nil = EnUS)
	blobEnv        *bloblang.Environment // isolated Bloblang environment (nil = use global)
	metrics        MetricsRecorder       // optional observability hook (nil = zero overhead)
	schemaName     string                // optional name for observability labels
	tracer         trace.Tracer          // nil = zero tracing overhead
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
		localizer:      cfg.localizer,
		blobEnv:        env,
		metrics:        cfg.metricsRecorder,
		schemaName:     cfg.schemaName,
		tracer:         buildTracer(cfg.tracerProvider),
	}

	maxDepth, err := resolveMaxSchemaDepth(cfg.maxSchemaDepth, cfg.maxSchemaDepthSet)
	if err != nil {
		return nil, err
	}

	// Reject attributes that would be silently dropped before compiling rules,
	// so a schema never validates less than it appears to.
	if err := checkDefinitionAttrs(schema, "", 0, maxDepth); err != nil {
		return nil, err
	}
	if err := v.extractRules(schema, "", 0, maxDepth); err != nil {
		return nil, err
	}
	sortBlobRules(v.blobRules)
	v.cueFields = compileCUEFields(schema, "")

	return v, nil
}
