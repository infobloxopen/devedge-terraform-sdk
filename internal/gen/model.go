// Package gen turns an enriched OpenAPI v3 spec (native required/readOnly/
// writeOnly/enum plus x-aip-* extensions) into a terraform-plugin-framework
// provider. It is the Terraform-side analog of the cligen generator in
// devedge-cli-sdk: parse the contract, emit a typed surface, import the runtime
// (here, tfkit).
//
// The pipeline has three stages, the first two exec-free and unit-testable:
//
//  1. [Parse] loads the spec into a [Model].
//  2. [BuildSpec] turns the [Model] into a HashiCorp Provider Code Specification
//     (github.com/hashicorp/terraform-plugin-codegen-spec) with the enriched
//     semantics set explicitly — because tfplugingen-openapi ignores
//     field_behavior and would otherwise infer required/computed wrongly.
//  3. [Generate] writes the spec JSON, runs the pinned tfplugingen-framework to
//     emit schema+models, then templates the CRUD glue and registration.
//
// This file (parse → Model) and spec.go (Model → Provider Code Spec) import no
// terraform-plugin-framework runtime, only the framework-free tfkit/behavior
// helper, so the generator stays dependency-light.
package gen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/infobloxopen/devedge-terraform-sdk/tfkit/behavior"
)

// Field is one resource property projected to a Terraform attribute.
type Field struct {
	JSONName    string             // wire name, e.g. "displayName"
	AttrName    string             // terraform attribute / tfsdk name, e.g. "display_name"
	GoName      string             // generated model field, e.g. "DisplayName"
	TFType      string             // "string", "int64", "bool", "float64"
	Description string             // one-line help
	Enum        []string           // allowed values, if a string enum
	Reference   string             // x-aip-references target type, if any
	InputOnly   bool               // INPUT_ONLY/writeOnly: sent on write, stripped from responses
	Semantics   behavior.Semantics // required/optional/computed + plan modifiers + sensitivity
}

// Writable reports whether the field is accepted on create/update (i.e. not a
// server-set OUTPUT_ONLY/computed field).
func (f Field) Writable() bool { return f.Semantics.Disposition != behavior.DispComputed }

// Mutable reports whether the field may change after create (writable and not
// IMMUTABLE).
func (f Field) Mutable() bool { return f.Writable() && !f.Semantics.RequiresReplace }

// Method is a standard AIP method present for a resource.
type Method struct {
	Kind       string // Get, List, Create, Update, Delete
	HTTPMethod string // GET, POST, PATCH, DELETE
}

// Resource is one AIP resource (x-aip-resource) and its methods/fields.
type Resource struct {
	GoName         string            // "Widget"
	TFName         string            // terraform resource name (snake), "widget"
	Collection     string            // "widgets"
	CollectionPath string            // "/v1/widgets"
	ResourceType   string            // "toy.example.com/Widget"
	Key            string            // "id"
	Fields         []Field           // sorted by wire name
	Methods        map[string]Method // by kind
}

// Has reports whether a standard method kind is present.
func (r Resource) Has(kind string) bool { _, ok := r.Methods[kind]; return ok }

// SchemaFunc is the schema constructor tfplugingen-framework generates,
// e.g. "WidgetResourceSchema".
func (r Resource) SchemaFunc() string { return pascal(r.TFName) + "ResourceSchema" }

// ModelType is the model struct tfplugingen-framework generates, e.g. "WidgetModel".
func (r Resource) ModelType() string { return pascal(r.TFName) + "Model" }

// Model is the full input to spec building and rendering.
type Model struct {
	Module    string // provider go module path (informational)
	Package   string // generated Go package name (e.g. "provider")
	Provider  string // provider type name (e.g. "toy")
	Resources []Resource
}

type aipResource struct {
	Type    string   `json:"type"`
	Key     string   `json:"key"`
	Pattern []string `json:"pattern"`
}

type aipReferences struct {
	Type string `json:"type"`
}

var standardKinds = []string{"Get", "List", "Create", "Update", "Delete"}

// Parse loads an enriched OpenAPI v3 document and builds a [Model]. It fails
// loud when the spec carries no x-aip-resource — tfgen has nothing to generate.
func Parse(specData []byte, module, pkg, provider string) (*Model, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	if doc.Components == nil || len(doc.Components.Schemas) == 0 {
		return nil, fmt.Errorf("spec has no component schemas")
	}

	ops := collectOperations(doc)

	var resources []Resource
	for name, ref := range doc.Components.Schemas {
		if ref == nil || ref.Value == nil {
			continue
		}
		var res aipResource
		if !decodeExt(ref.Value.Extensions, "x-aip-resource", &res) {
			continue
		}
		r, err := buildResource(name, ref.Value, res, ops)
		if err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	if len(resources) == 0 {
		return nil, fmt.Errorf("spec has no x-aip-resource schemas; nothing to generate")
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].GoName < resources[j].GoName })

	if pkg == "" {
		pkg = "provider"
	}
	return &Model{Module: module, Package: pkg, Provider: provider, Resources: resources}, nil
}

type opInfo struct {
	kind       string
	httpMethod string
	path       string
}

func collectOperations(doc *openapi3.T) []opInfo {
	var out []opInfo
	if doc.Paths == nil {
		return out
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op == nil {
				continue
			}
			kind, _ := op.Extensions["x-aip-method"].(string)
			if kind == "" {
				continue
			}
			out = append(out, opInfo{kind: kind, httpMethod: strings.ToUpper(method), path: path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func buildResource(schemaName string, schema *openapi3.Schema, res aipResource, ops []opInfo) (Resource, error) {
	r := Resource{
		GoName:       goNameFromType(res.Type, schemaName),
		Collection:   collectionFromPattern(res.Pattern, res.Type),
		ResourceType: res.Type,
		Key:          res.Key,
		Methods:      map[string]Method{},
	}
	if r.Key == "" {
		r.Key = "id"
	}
	r.TFName = snake(r.GoName)

	var names []string
	for pn := range schema.Properties {
		names = append(names, pn)
	}
	sort.Strings(names)
	requiredSet := map[string]bool{}
	for _, req := range schema.Required {
		requiredSet[req] = true
	}
	for _, pn := range names {
		pref := schema.Properties[pn]
		if pref == nil || pref.Value == nil {
			continue
		}
		r.Fields = append(r.Fields, buildField(pn, pref.Value, requiredSet[pn], isIdentity(pn, r.Key)))
	}

	for _, oi := range ops {
		if !isStandardKind(oi.kind) || !pathHasSegment(oi.path, r.Collection) {
			continue
		}
		r.Methods[oi.kind] = Method{Kind: oi.kind, HTTPMethod: oi.httpMethod}
		switch oi.kind {
		case "List":
			r.CollectionPath = oi.path
		case "Create":
			if r.CollectionPath == "" {
				r.CollectionPath = oi.path
			}
		}
	}
	if r.CollectionPath == "" {
		return r, fmt.Errorf("resource %s has no List/Create path to derive its collection URL", r.GoName)
	}
	return r, nil
}

// isIdentity reports whether a property is the resource identity (the resource
// key, e.g. "id") or the conventional tenant key (account_id). Both are
// server-populated when the client omits them, so the generator resolves them
// as computed_optional to avoid a "provider produced inconsistent result after
// apply" error (see behavior.Resolve's identity rule).
func isIdentity(jsonName, key string) bool {
	if key == "" {
		key = "id"
	}
	if jsonName == key {
		return true
	}
	return snake(jsonName) == behavior.AccountKey
}

func buildField(name string, schema *openapi3.Schema, required, identity bool) Field {
	f := Field{
		JSONName:    name,
		AttrName:    snake(name),
		GoName:      pascal(name),
		TFType:      tfType(schema),
		Description: firstLine(schema.Description),
	}
	behaviors := fieldBehaviors(schema)
	f.Semantics = behavior.Resolve(behaviors, required, schema.ReadOnly, schema.WriteOnly, identity)
	f.InputOnly = schema.WriteOnly || hasBehavior(behaviors, behavior.InputOnly)

	var refs aipReferences
	if decodeExt(schema.Extensions, "x-aip-references", &refs) {
		f.Reference = refs.Type
	}
	for _, e := range schema.Enum {
		f.Enum = append(f.Enum, fmt.Sprint(e))
	}
	return f
}

func hasBehavior(behaviors []string, want string) bool {
	for _, b := range behaviors {
		if b == want {
			return true
		}
	}
	return false
}

func fieldBehaviors(schema *openapi3.Schema) []string {
	var out []string
	if raw, ok := schema.Extensions["x-aip-field-behavior"]; ok {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// tfType maps an OpenAPI scalar to a Terraform framework primitive. Complex
// types (array/object) are not yet projected and fall back to string; the toy
// contract has no such resource fields.
func tfType(s *openapi3.Schema) string {
	if s == nil || s.Type == nil {
		return "string"
	}
	switch {
	case s.Type.Is("string"):
		return "string"
	case s.Type.Is("integer"):
		return "int64"
	case s.Type.Is("number"):
		return "float64"
	case s.Type.Is("boolean"):
		return "bool"
	default:
		return "string"
	}
}

// decodeExt round-trips an OpenAPI extension value into out via JSON.
func decodeExt(ext map[string]any, key string, out any) bool {
	raw, ok := ext[key]
	if !ok {
		return false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, out) == nil
}

func isStandardKind(kind string) bool {
	for _, k := range standardKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func pathHasSegment(path, seg string) bool {
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		if s == seg {
			return true
		}
	}
	return false
}

func goNameFromType(resourceType, schemaName string) string {
	if i := strings.LastIndexByte(resourceType, '/'); i >= 0 && i+1 < len(resourceType) {
		return pascal(resourceType[i+1:])
	}
	return pascal(strings.TrimPrefix(schemaName, "v1"))
}

func collectionFromPattern(pattern []string, resourceType string) string {
	if len(pattern) > 0 {
		if i := strings.IndexByte(pattern[0], '/'); i > 0 {
			return pattern[0][:i]
		}
		return pattern[0]
	}
	if i := strings.LastIndexByte(resourceType, '/'); i >= 0 {
		return strings.ToLower(resourceType[i+1:]) + "s"
	}
	return strings.ToLower(resourceType)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// pascal converts a camelCase/snake/kebab wire name to an exported Go name,
// matching how tfplugingen-framework names model fields from snake attributes.
func pascal(s string) string {
	parts := splitWords(s)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	out := b.String()
	if out == "" {
		return "Field"
	}
	return out
}

// snake converts a camelCase/kebab wire name to a snake_case terraform
// attribute name.
func snake(s string) string {
	return strings.Join(splitWords(s), "_")
}

// splitWords breaks a name on separators and camelCase boundaries, lowercasing.
func splitWords(s string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	for i, r := range s {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			flush()
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				flush()
			}
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}
