package schemix

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
)

// parsefieldMeta extracts @meta(...) attribute parameters from a CUE field value.
func parsefieldMeta(val cue.Value) fieldMeta {
	meta := fieldMeta{}
	attr := val.Attribute(attrMeta)
	if attr.Err() != nil {
		return meta
	}
	for i := range attr.NumArgs() {
		key, value := attr.Arg(i)
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch {
		case key == metaPriority && value != "":
			fmt.Sscanf(value, "%d", &meta.Priority)
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
			if exec, err := bloblang.Parse(fmt.Sprintf(blobMappingTemplate, value)); err == nil {
				meta.RequiredIf = exec
				meta.RequiredIfExpr = value
			}
		case strings.HasPrefix(key, metaRequiredIf+"="):
			expr := strings.TrimPrefix(key, metaRequiredIf+"=")
			if exec, err := bloblang.Parse(fmt.Sprintf(blobMappingTemplate, expr)); err == nil {
				meta.RequiredIf = exec
				meta.RequiredIfExpr = expr
			}
		case key == metaSkipIf && value != "":
			if exec, err := bloblang.Parse(fmt.Sprintf(blobMappingTemplate, value)); err == nil {
				meta.SkipIf = exec
				meta.SkipIfExpr = value
			}
		case strings.HasPrefix(key, metaSkipIf+"="):
			expr := strings.TrimPrefix(key, metaSkipIf+"=")
			if exec, err := bloblang.Parse(fmt.Sprintf(blobMappingTemplate, expr)); err == nil {
				meta.SkipIf = exec
				meta.SkipIfExpr = expr
			}
		}
	}
	return meta
}
