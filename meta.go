package schemix

import (
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// blobParser is a function that compiles a Bloblang mapping string.
// It abstracts away the choice between global and isolated environments.
type blobParser func(mapping string) (*bloblang.Executor, error)

// knownMetaParams is the set of recognized @meta parameter keys.
var knownMetaParams = map[string]bool{
	metaPriority:    true,
	metaOptional:    true,
	metaConditional: true,
	metaSkipEmpty:   true,
	metaFailFast:    true,
	metaOmitIfSkip:  true,
	metaOmitEmpty:   true,
	metaRequiredIf:  true,
	metaSkipIf:      true,
}

// parsefieldMeta extracts @meta(...) attribute parameters from a CUE field value.
// The parse function is used to compile required_if and skip_if expressions,
// allowing use of custom Bloblang environments.
// Returns an error if any parameter is unknown, has invalid value, or expression fails to parse.
func parsefieldMeta(val cue.Value, parse blobParser) (fieldMeta, error) {
	meta := fieldMeta{}
	attr := val.Attribute(attrMeta)
	if attr.Err() != nil {
		return meta, nil
	}
	for i := range attr.NumArgs() {
		key, value := attr.Arg(i)
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Detect "key=value" form where CUE parsed it into key with HasPrefix
		baseKey := key
		if idx := strings.Index(key, "="); idx >= 0 {
			baseKey = key[:idx]
		}

		if !knownMetaParams[baseKey] {
			return fieldMeta{}, fmt.Errorf("unknown @meta parameter %q", key)
		}

		switch {
		case key == metaPriority && value != "":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fieldMeta{}, fmt.Errorf("@meta priority=%q: %w", value, err)
			}
			meta.Priority = n
		case key == metaOptional:
			meta.Optional = true
		case key == metaConditional:
			meta.Conditional = true
			meta.Optional = true // conditional implies optional
		case key == metaSkipEmpty:
			meta.SkipEmpty = true
		case key == metaFailFast:
			meta.FailFast = true
		case key == metaOmitIfSkip:
			meta.OmitIfSkip = true
		case key == metaOmitEmpty:
			meta.OmitEmpty = true
		case key == metaRequiredIf && value != "":
			exec, err := parse(fmt.Sprintf(blobMappingTemplate, value))
			if err != nil {
				return fieldMeta{}, fmt.Errorf("@meta required_if=%q: %w", value, err)
			}
			meta.RequiredIf = exec
			meta.RequiredIfExpr = value
		case key == metaRequiredIf && value == "":
			return fieldMeta{}, fmt.Errorf("@meta required_if expression must not be empty")
		case strings.HasPrefix(key, metaRequiredIf+"="):
			expr := strings.TrimPrefix(key, metaRequiredIf+"=")
			if expr == "" {
				return fieldMeta{}, fmt.Errorf("@meta required_if expression must not be empty")
			}
			exec, err := parse(fmt.Sprintf(blobMappingTemplate, expr))
			if err != nil {
				return fieldMeta{}, fmt.Errorf("@meta required_if=%q: %w", expr, err)
			}
			meta.RequiredIf = exec
			meta.RequiredIfExpr = expr
		case key == metaSkipIf && value != "":
			exec, err := parse(fmt.Sprintf(blobMappingTemplate, value))
			if err != nil {
				return fieldMeta{}, fmt.Errorf("@meta skip_if=%q: %w", value, err)
			}
			meta.SkipIf = exec
			meta.SkipIfExpr = value
		case key == metaSkipIf && value == "":
			return fieldMeta{}, fmt.Errorf("@meta skip_if expression must not be empty")
		case strings.HasPrefix(key, metaSkipIf+"="):
			expr := strings.TrimPrefix(key, metaSkipIf+"=")
			if expr == "" {
				return fieldMeta{}, fmt.Errorf("@meta skip_if expression must not be empty")
			}
			exec, err := parse(fmt.Sprintf(blobMappingTemplate, expr))
			if err != nil {
				return fieldMeta{}, fmt.Errorf("@meta skip_if=%q: %w", expr, err)
			}
			meta.SkipIf = exec
			meta.SkipIfExpr = expr
		case key == metaPriority && value == "":
			// priority with no value — treat as zero (no-op)
		}
	}
	return meta, nil
}
