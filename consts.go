package schemix

// Mode string values for FailMode selection (user-facing).
const (
	ModeAll      = "all"
	ModeFast     = "fast"
	ModePriority = "priority"
)

// Validation error type identifiers (user-facing, for filtering ValidationError.Type).
const (
	TypeCUE      = "cue"
	TypeBloblang = "bloblang"
	TypeMeta     = "meta"
)

// --- Internal constants below (unexported) ---

// Plugin names registered with Bloblang.
const (
	pluginValidateSchema = "validate_schema"
	pluginProcessSchema  = "process_schema"
)

// Plugin spec metadata.
const categoryValidation = "Validation"

// Parameter names used in Bloblang plugin specs.
const (
	paramName = "name"
	paramMode = "mode"
	paramData = "data"
)

// Result map keys returned by validate_schema / process_schema.
const (
	keyValid   = "valid"
	keyErrors  = "errors"
	keyOutput  = "output"
	keyCode    = "code"
	keyPath    = "path"
	keyType    = "type"
	keyMessage = "message"
)

// Meta attribute keys parsed from @meta(...).
const (
	metaPriority    = "priority"
	metaOptional    = "optional"
	metaConditional = "conditional"
	metaSkipEmpty   = "skip_empty"
	metaFailFast    = "fail_fast"
	metaOmitIfSkip  = "omit_if_skip"
	metaOmitEmpty   = "omit_empty"
	metaRequiredIf  = "required_if"
	metaSkipIf      = "skip_if"
)

// Bloblang mapping template for compiling expressions.
const blobMappingTemplate = "root = %s"

// CUE attribute names used in schema parsing.
const (
	attrBlob = "blob"
	attrMeta = "meta"
)
