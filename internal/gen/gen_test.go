package gen

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-codegen-spec/resource"
	"github.com/hashicorp/terraform-plugin-codegen-spec/schema"
	"github.com/infobloxopen/devedge-terraform-sdk/tfkit/behavior"
)

const specPath = "../../testdata/toy.openapi.yaml"

func loadModel(t *testing.T) *Model {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	m, err := Parse(data, "example.com/terraform-provider-toy", "provider", "toy")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func TestParseModelFieldBehavior(t *testing.T) {
	m := loadModel(t)
	if len(m.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(m.Resources))
	}
	r := m.Resources[0]
	if r.GoName != "Widget" || r.TFName != "widget" || r.Collection != "widgets" || r.CollectionPath != "/v1/widgets" {
		t.Fatalf("resource = %+v", r)
	}
	if r.Key != "id" {
		t.Fatalf("key = %q, want id", r.Key)
	}
	for _, kind := range standardKinds {
		if !r.Has(kind) {
			t.Fatalf("expected method %s present", kind)
		}
	}

	byName := map[string]Field{}
	for _, f := range r.Fields {
		byName[f.JSONName] = f
	}
	// OUTPUT_ONLY / readOnly → computed.
	for _, n := range []string{"name", "archivedTime", "deleteTime"} {
		if byName[n].Semantics.Disposition != behavior.DispComputed {
			t.Fatalf("%s should be computed, got %q", n, byName[n].Semantics.Disposition)
		}
		if !byName[n].Semantics.UseStateForUnknown {
			t.Fatalf("%s (computed) should carry UseStateForUnknown", n)
		}
	}
	// REQUIRED → required.
	if byName["displayName"].Semantics.Disposition != behavior.DispRequired {
		t.Fatalf("displayName should be required")
	}
	// not_null must NOT become required (sku has no behavior).
	if byName["sku"].Semantics.Disposition != behavior.DispOptional {
		t.Fatalf("sku (not_null, no behavior) must be optional, got %q", byName["sku"].Semantics.Disposition)
	}
	// IMMUTABLE → RequiresReplace, disposition preserved (optional here).
	for _, n := range []string{"color", "id"} {
		if !byName[n].Semantics.RequiresReplace {
			t.Fatalf("%s should require replace", n)
		}
		if byName[n].Semantics.Disposition != behavior.DispOptional {
			t.Fatalf("%s should stay optional, got %q", n, byName[n].Semantics.Disposition)
		}
	}
	// INPUT_ONLY / writeOnly → sensitive + input-only (excluded from apply).
	if !byName["secretToken"].Semantics.Sensitive || !byName["secretToken"].InputOnly {
		t.Fatalf("secretToken should be sensitive input-only: %+v", byName["secretToken"])
	}
	// enum.
	if got := byName["category"].Enum; len(got) != 2 || got[0] != "standard" {
		t.Fatalf("category enum = %v", got)
	}
	// int type.
	if byName["weight"].TFType != "int64" {
		t.Fatalf("weight should be int64, got %q", byName["weight"].TFType)
	}
	// reference surfaced.
	if byName["parentId"].Reference == "" {
		t.Fatalf("parentId should carry a reference")
	}
}

// attrByName finds a resource attribute in the built spec.
func attrByName(t *testing.T, attrs resource.Attributes, name string) resource.Attribute {
	t.Helper()
	for _, a := range attrs {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("attribute %q not found", name)
	return resource.Attribute{}
}

func TestBuildSpecSemantics(t *testing.T) {
	m := loadModel(t)
	s, err := BuildSpec(m)
	if err != nil {
		t.Fatalf("BuildSpec: %v", err)
	}
	if err := s.Validate(context.Background()); err != nil {
		t.Fatalf("spec is not a valid Provider Code Specification: %v", err)
	}
	if len(s.Resources) != 1 || s.Resources[0].Name != "widget" {
		t.Fatalf("resources = %+v", s.Resources)
	}
	attrs := s.Resources[0].Schema.Attributes

	// display_name Required.
	if got := attrByName(t, attrs, "display_name").String.ComputedOptionalRequired; got != schema.Required {
		t.Fatalf("display_name = %q, want required", got)
	}
	// name Computed.
	if got := attrByName(t, attrs, "name").String.ComputedOptionalRequired; got != schema.Computed {
		t.Fatalf("name = %q, want computed", got)
	}
	// id IMMUTABLE → RequiresReplace plan modifier.
	idPM := attrByName(t, attrs, "id").String.PlanModifiers
	if len(idPM) != 1 || idPM[0].Custom == nil || !strings.Contains(idPM[0].Custom.SchemaDefinition, "RequiresReplace") {
		t.Fatalf("id should have a RequiresReplace plan modifier, got %+v", idPM)
	}
	if len(idPM[0].Custom.Imports) != 1 || !strings.Contains(idPM[0].Custom.Imports[0].Path, "stringplanmodifier") {
		t.Fatalf("id plan modifier import wrong: %+v", idPM[0].Custom.Imports)
	}
	// secret_token Sensitive.
	if s := attrByName(t, attrs, "secret_token").String.Sensitive; s == nil || !*s {
		t.Fatalf("secret_token should be sensitive")
	}
	// category enum → OneOf validator.
	catV := attrByName(t, attrs, "category").String.Validators
	if len(catV) != 1 || catV[0].Custom == nil || !strings.Contains(catV[0].Custom.SchemaDefinition, `stringvalidator.OneOf("standard", "premium")`) {
		t.Fatalf("category should have a OneOf validator, got %+v", catV)
	}
	// weight is an int64 attribute.
	if attrByName(t, attrs, "weight").Int64 == nil {
		t.Fatalf("weight should be an int64 attribute")
	}
}

func TestEmitSpecJSONDeterministic(t *testing.T) {
	m := loadModel(t)
	a, err := EmitSpecJSON(m)
	if err != nil {
		t.Fatalf("EmitSpecJSON: %v", err)
	}
	b, err := EmitSpecJSON(m)
	if err != nil {
		t.Fatalf("EmitSpecJSON (2nd): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("EmitSpecJSON is not deterministic")
	}
	if !strings.Contains(string(a), `"version": "0.1"`) {
		t.Fatalf("spec JSON missing version:\n%s", a)
	}
}
