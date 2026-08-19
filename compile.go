package schemix

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"github.com/warpstreamlabs/bento/public/bloblang"
)

// cueField is a pre-compiled field descriptor extracted at schema parse time.
// This avoids calling schema.Fields() on every Process call (optimization #3).
type cueField struct {
	name      string          // field name (without "?")
	path      string          // full dot-separated path
	cuePath   cue.Path        // pre-built lookup path for name; see compileCUEFields
	schema    cue.Value       // pre-resolved schema value
	optional  bool            // whether the field is optional
	nullable  bool            // schema allows null (e.g. `null | string`)
	hasBlob   bool            // has @blob attribute; absent input may be computed
	isStruct  bool            // IncompleteKind == StructKind
	isList    bool            // IncompleteKind == ListKind
	priority  int             // @meta(priority=N), default 0
	fieldType string          // schema type name for diagnostics ("string", "int", ...)
	fast      *fastConstraint // Go-native fast check (nil = use CUE path)
	children  []cueField      // nested struct fields (pre-compiled recursively)
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
			Type:     f.fieldType,
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
			// Resolved once here so the error path never pays for kind lookup.
			fieldType: cueKindToString(fieldSchema.IncompleteKind()),
			// MakePath builds the selector directly. cue.ParsePath would run the
			// CUE expression parser — 36 allocations for a name already known
			// here — and validateCUEFields navigates once per field per call.
			cuePath: cue.MakePath(cue.Str(name)),
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

// checkDefinitionAttrs rejects @blob/@meta written on a definition (#Name).
//
// A definition is a reusable template, while a @blob expression binds to an
// absolute path (this.field). The same definition may be referenced by several
// fields, so there is no single field the expression could bind to — such an
// attribute is never extracted, and silently dropping it would let invalid data
// pass validation. Attributes on fields *inside* a struct definition are fine,
// because a reference expands them onto real field paths.
func checkDefinitionAttrs(val cue.Value, prefix string, depth, limit int) error {
	if depth > limit {
		return errSchemaTooDeep(prefix, limit)
	}
	if val.IncompleteKind() != cue.StructKind {
		return nil
	}
	iter, err := val.Fields(cue.Attributes(true), cue.Optional(true), cue.Definitions(true))
	if err != nil {
		return nil
	}
	for iter.Next() {
		sel := iter.Selector()
		name := sel.String()
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		fieldValue := iter.Value()

		if sel.IsDefinition() {
			if hasSchemixAttr(fieldValue) {
				return fmt.Errorf("definition %q: @blob/@meta is not supported on a "+
					"definition and would be silently ignored, because a definition is a "+
					"reusable template while the expression binds to an absolute path; "+
					"put the attribute on the field that references it, e.g. "+
					"field: %s @blob(this.field...)", path, name)
			}
		}

		// Descend to find definitions declared at deeper levels.
		if fieldValue.IncompleteKind() == cue.StructKind {
			if err := checkDefinitionAttrs(fieldValue, path, depth+1, limit); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractRules recursively extracts @blob and @meta rules from all struct levels.
func (v *Validator) extractRules(val cue.Value, prefix string, depth, limit int) error {
	if depth > limit {
		return errSchemaTooDeep(prefix, limit)
	}
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
			if err := v.extractRules(fieldValue, fullPath, depth+1, limit); err != nil {
				return err
			}
		}

		// Attributes inside an array element schema are never extracted, because
		// rules are compiled per field path and an element index is unknown until
		// runtime. Silently dropping them would let invalid data pass validation,
		// so reject the schema instead and point at the supported form.
		if fieldValue.IncompleteKind() == cue.ListKind {
			bad, err := findAttrInListElements(fieldValue, fullPath, depth+1, limit)
			if err != nil {
				return err
			}
			if bad != "" {
				// The suggested expression uses the full path because @blob
				// resolves `this` against the root object, not the local struct.
				return fmt.Errorf("field %q: @blob/@meta is not supported inside array "+
					"elements (found at %q) and would be silently ignored; put the "+
					"attribute on the array field itself, e.g. %s: [...{...}] "+
					"@blob(this.%s.all(i -> i.field > 0))",
					fullPath, bad, fieldName, fullPath)
			}
		}
	}

	return nil
}

// hasSchemixAttr reports whether a value carries an @blob or @meta attribute.
func hasSchemixAttr(val cue.Value) bool {
	if a := val.Attribute(attrBlob); a.Err() == nil {
		return true
	}
	if a := val.Attribute(attrMeta); a.Err() == nil {
		return true
	}
	return false
}

// findAttrInListElements returns the path of the first @blob/@meta attribute
// found inside the element schema of a list, or "" when there is none.
// Both list forms are covered: open lists ([...T]) expose their element schema
// through Elem, while closed lists ([A, B]) must be iterated.
func findAttrInListElements(list cue.Value, path string, depth, limit int) (string, error) {
	if depth > limit {
		return "", errSchemaTooDeep(path, limit)
	}
	// An open list ([...T]) exposes its element schema at any index; a closed
	// list ([A, B]) has none and must be iterated instead.
	if elem := list.LookupPath(cue.MakePath(cue.AnyIndex)); elem.Exists() {
		return findAttrInElementSchema(elem, path+"[]", depth+1, limit)
	}
	it, err := list.List()
	if err != nil {
		return "", nil
	}
	for i := 0; it.Next(); i++ {
		found, err := findAttrInElementSchema(it.Value(), fmt.Sprintf("%s[%d]", path, i), depth+1, limit)
		if err != nil {
			return "", err
		}
		if found != "" {
			return found, nil
		}
	}
	return "", nil
}

// findAttrInElementSchema walks an array element schema looking for @blob/@meta,
// descending through nested structs and nested lists.
func findAttrInElementSchema(elem cue.Value, path string, depth, limit int) (string, error) {
	if depth > limit {
		return "", errSchemaTooDeep(path, limit)
	}
	if hasSchemixAttr(elem) {
		return path, nil
	}
	if elem.IncompleteKind() != cue.StructKind {
		return "", nil
	}
	iter, err := elem.Fields(cue.Attributes(true), cue.Optional(true))
	if err != nil {
		return "", nil
	}
	for iter.Next() {
		name := strings.TrimSuffix(iter.Selector().String(), "?")
		fieldPath := path + "." + name
		fieldValue := iter.Value()

		if hasSchemixAttr(fieldValue) {
			return fieldPath, nil
		}
		var found string
		switch fieldValue.IncompleteKind() {
		case cue.StructKind:
			found, err = findAttrInElementSchema(fieldValue, fieldPath, depth+1, limit)
		case cue.ListKind:
			found, err = findAttrInListElements(fieldValue, fieldPath, depth+1, limit)
		default:
			continue
		}
		if err != nil {
			return "", err
		}
		if found != "" {
			return found, nil
		}
	}
	return "", nil
}

// errSchemaTooDeep reports that analysis hit the configured bound.
func errSchemaTooDeep(path string, limit int) error {
	return fmt.Errorf("schema nesting at %q exceeds the maximum depth of %d; "+
		"raise it with WithMaxSchemaDepth if the schema is legitimately this deep, "+
		"or check for mutually recursive definitions such as "+
		"#A: {bs: [...#B]} with #B: {as: [...#A]}", path, limit)
}

// FieldInfo describes a field in the schema. Returned by Validator.Fields().
// This is useful for generating documentation, API specs, or UI forms.
type FieldInfo struct {
	Name     string      `json:"name"`               // field name
	Path     string      `json:"path"`               // full dot-path
	Type     string      `json:"type"`               // "string", "int", "float", "bool", "struct", "list", "number", "unknown"
	Optional bool        `json:"optional"`           // whether the field is optional
	HasBlob  bool        `json:"has_blob"`           // has @blob() annotation
	Children []FieldInfo `json:"children,omitempty"` // nested struct fields
}

// blobRule is an extracted @blob rule with its field path and meta controls.
type blobRule struct {
	Path string             // field path (e.g. "address.city")
	Exec *bloblang.Executor // compiled Bloblang expression (nil = pure meta node)
	Expr string             // raw expression text
	Meta fieldMeta          // field behavior controls
}

// fieldMeta holds all @meta() attribute parameters for a field.
type fieldMeta struct {
	Priority       int                // execution priority (lower = first)
	Optional       bool               // field absence is not an error
	Conditional    bool               // @meta(conditional) was present; recorded but not read, see parsefieldMeta
	SkipEmpty      bool               // skip validation when empty/zero
	FailFast       bool               // skip remaining rules for this field on failure
	OmitIfSkip     bool               // remove from output when skipped
	OmitEmpty      bool               // remove from output when empty
	SkipIf         *bloblang.Executor // conditional skip expression
	SkipIfExpr     string
	RequiredIf     *bloblang.Executor // conditional required expression
	RequiredIfExpr string
}

// sortBlobRules sorts rules by priority (stable), overflow-safe via cmp.Compare.
func sortBlobRules(rules []blobRule) {
	slices.SortStableFunc(rules, func(a, b blobRule) int {
		return cmp.Compare(a.Meta.Priority, b.Meta.Priority)
	})
}

// extractFieldPriority reads the @meta(priority=N) value from a CUE field value.
// Returns 0 (default priority) if no priority is specified or if parsing fails.
func extractFieldPriority(fieldSchema cue.Value) int {
	metaAttr := fieldSchema.Attribute(attrMeta)
	if metaAttr.Err() != nil {
		return 0
	}
	for i := range metaAttr.NumArgs() {
		key, val := metaAttr.Arg(i)
		key = strings.TrimSpace(key)
		if key == metaPriority && val != "" {
			p, err := strconv.Atoi(val)
			if err == nil {
				return p
			}
		}
	}
	return 0
}

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
			// conditional implies optional, which makes it behave exactly like
			// @meta(optional) at execution time: processInternal's optional
			// branch performs the required_if check for both, and no code reads
			// meta.Conditional.
			//
			// The field is kept deliberately rather than dropped: it records
			// the author's intent at the schema level, so a future release can
			// give conditional its own semantics (for example, requiring the
			// field to be absent unless required_if holds) without changing the
			// schema syntax. Anything that starts reading it must first decide
			// what conditional means on its own, and must run before the
			// optional branch to be reachable at all.
			meta.Conditional = true
			meta.Optional = true
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
