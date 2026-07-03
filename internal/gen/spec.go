package gen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-codegen-spec/code"
	"github.com/hashicorp/terraform-plugin-codegen-spec/provider"
	"github.com/hashicorp/terraform-plugin-codegen-spec/resource"
	"github.com/hashicorp/terraform-plugin-codegen-spec/schema"
	"github.com/hashicorp/terraform-plugin-codegen-spec/spec"
)

// specVersion is the Provider Code Specification JSON schema version the pinned
// codegen-spec / tfplugingen-framework understand.
const specVersion = "0.1"

// planModifierPkg maps a Terraform primitive to its plan-modifier package alias
// and import path.
var planModifierPkg = map[string]struct{ alias, path string }{
	"string":  {"stringplanmodifier", "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"},
	"int64":   {"int64planmodifier", "github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"},
	"bool":    {"boolplanmodifier", "github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"},
	"float64": {"float64planmodifier", "github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"},
}

const stringValidatorImport = "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"

// BuildSpec turns a [Model] into a HashiCorp Provider Code Specification with
// the enriched field-behavior semantics set explicitly. This is the step that
// tfplugingen-openapi cannot do — it ignores field_behavior and infers
// required/computed from request-vs-response body membership — so tfgen emits
// the specification itself and drives the framework generator off it.
func BuildSpec(m *Model) (spec.Specification, error) {
	s := spec.Specification{
		Version:  specVersion,
		Provider: &provider.Provider{Name: m.Provider},
	}
	for _, r := range m.Resources {
		attrs := make(resource.Attributes, 0, len(r.Fields))
		for _, f := range r.Fields {
			attrs = append(attrs, buildAttribute(f))
		}
		s.Resources = append(s.Resources, resource.Resource{
			Name:   r.TFName,
			Schema: &resource.Schema{Attributes: attrs},
		})
	}
	return s, nil
}

// EmitSpecJSON builds the specification and marshals it to indented JSON.
func EmitSpecJSON(m *Model) ([]byte, error) {
	s, err := BuildSpec(m)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal spec: %w", err)
	}
	return append(b, '\n'), nil
}

func buildAttribute(f Field) resource.Attribute {
	cor := schema.ComputedOptionalRequired(f.Semantics.Disposition)
	desc := descPtr(f.Description)
	sensitive := boolPtrIf(f.Semantics.Sensitive)
	planDefs := planModifierDefs(f)

	switch f.TFType {
	case "int64":
		return resource.Attribute{Name: f.AttrName, Int64: &resource.Int64Attribute{
			ComputedOptionalRequired: cor,
			Description:              desc,
			Sensitive:                sensitive,
			PlanModifiers:            int64PlanModifiers(f, planDefs),
		}}
	case "bool":
		return resource.Attribute{Name: f.AttrName, Bool: &resource.BoolAttribute{
			ComputedOptionalRequired: cor,
			Description:              desc,
			Sensitive:                sensitive,
			PlanModifiers:            boolPlanModifiers(f, planDefs),
		}}
	case "float64":
		return resource.Attribute{Name: f.AttrName, Float64: &resource.Float64Attribute{
			ComputedOptionalRequired: cor,
			Description:              desc,
			Sensitive:                sensitive,
			PlanModifiers:            float64PlanModifiers(f, planDefs),
		}}
	default: // string
		return resource.Attribute{Name: f.AttrName, String: &resource.StringAttribute{
			ComputedOptionalRequired: cor,
			Description:              desc,
			Sensitive:                sensitive,
			PlanModifiers:            stringPlanModifiers(f, planDefs),
			Validators:               stringValidators(f),
		}}
	}
}

// planModifierDef is a rendered plan-modifier call plus its import.
type planModifierDef struct {
	call string
	imp  code.Import
}

// planModifierDefs returns the plan-modifier calls a field needs, typed to its
// Terraform primitive (stringplanmodifier.RequiresReplace(), etc.).
func planModifierDefs(f Field) []planModifierDef {
	pkg, ok := planModifierPkg[f.TFType]
	if !ok {
		return nil
	}
	var out []planModifierDef
	if f.Semantics.RequiresReplace {
		out = append(out, planModifierDef{
			call: pkg.alias + ".RequiresReplace()",
			imp:  code.Import{Path: pkg.path},
		})
	}
	if f.Semantics.UseStateForUnknown {
		out = append(out, planModifierDef{
			call: pkg.alias + ".UseStateForUnknown()",
			imp:  code.Import{Path: pkg.path},
		})
	}
	return out
}

func customPlanModifiers(defs []planModifierDef) []*schema.CustomPlanModifier {
	if len(defs) == 0 {
		return nil
	}
	out := make([]*schema.CustomPlanModifier, 0, len(defs))
	for _, d := range defs {
		out = append(out, &schema.CustomPlanModifier{
			Imports:          []code.Import{d.imp},
			SchemaDefinition: d.call,
		})
	}
	return out
}

func stringPlanModifiers(_ Field, defs []planModifierDef) schema.StringPlanModifiers {
	var out schema.StringPlanModifiers
	for _, c := range customPlanModifiers(defs) {
		out = append(out, schema.StringPlanModifier{Custom: c})
	}
	return out
}

func int64PlanModifiers(_ Field, defs []planModifierDef) schema.Int64PlanModifiers {
	var out schema.Int64PlanModifiers
	for _, c := range customPlanModifiers(defs) {
		out = append(out, schema.Int64PlanModifier{Custom: c})
	}
	return out
}

func boolPlanModifiers(_ Field, defs []planModifierDef) schema.BoolPlanModifiers {
	var out schema.BoolPlanModifiers
	for _, c := range customPlanModifiers(defs) {
		out = append(out, schema.BoolPlanModifier{Custom: c})
	}
	return out
}

func float64PlanModifiers(_ Field, defs []planModifierDef) schema.Float64PlanModifiers {
	var out schema.Float64PlanModifiers
	for _, c := range customPlanModifiers(defs) {
		out = append(out, schema.Float64PlanModifier{Custom: c})
	}
	return out
}

// stringValidators renders a stringvalidator.OneOf(...) for an enum field.
func stringValidators(f Field) schema.StringValidators {
	if len(f.Enum) == 0 {
		return nil
	}
	args := make([]string, 0, len(f.Enum))
	for _, e := range f.Enum {
		b, _ := json.Marshal(e) // quote + escape
		args = append(args, string(b))
	}
	def := "stringvalidator.OneOf(" + strings.Join(args, ", ") + ")"
	return schema.StringValidators{
		schema.StringValidator{Custom: &schema.CustomValidator{
			Imports:          []code.Import{{Path: stringValidatorImport}},
			SchemaDefinition: def,
		}},
	}
}

func descPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func boolPtrIf(b bool) *bool {
	if !b {
		return nil
	}
	v := true
	return &v
}
