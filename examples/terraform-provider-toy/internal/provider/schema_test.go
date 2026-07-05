package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// TestWidgetSchemaSemantics asserts that the generated Widget schema carries the
// enriched field-behavior semantics that tfplugingen-openapi would otherwise
// drop: REQUIRED→required, OUTPUT_ONLY→computed, IMMUTABLE→RequiresReplace,
// secret→sensitive, and enum→OneOf validator.
func TestWidgetSchemaSemantics(t *testing.T) {
	ctx := context.Background()
	s := WidgetResourceSchema(ctx)
	attrs := s.Attributes

	strAttr := func(name string) schema.StringAttribute {
		a, ok := attrs[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("attribute %q is not a StringAttribute: %T", name, attrs[name])
		}
		return a
	}

	// REQUIRED → required input.
	if dn := strAttr("display_name"); !dn.Required || dn.Computed {
		t.Fatalf("display_name should be Required (got Required=%v Computed=%v)", dn.Required, dn.Computed)
	}

	// OUTPUT_ONLY → computed, with a UseStateForUnknown plan modifier.
	name := strAttr("name")
	if !name.Computed || name.Required || name.Optional {
		t.Fatalf("name should be Computed only (Required=%v Optional=%v Computed=%v)", name.Required, name.Optional, name.Computed)
	}
	if len(name.PlanModifiers) != 1 || !strings.Contains(name.PlanModifiers[0].Description(ctx), "will not change") {
		t.Fatalf("name should carry a UseStateForUnknown plan modifier, got %+v", name.PlanModifiers)
	}

	// Resource identity (id) and the tenant key (account_id) are server-populated
	// → computed_optional with a UseStateForUnknown plan modifier so a server-set
	// value is not a "provider produced inconsistent result after apply" (issue
	// #7). Both are IMMUTABLE, so they also carry a RequiresReplace modifier.
	hasPM := func(pms []planmodifier.String, want string) bool {
		for _, pm := range pms {
			if strings.Contains(pm.Description(ctx), want) {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"id", "account_id"} {
		a := strAttr(name)
		if !a.Computed || !a.Optional || a.Required {
			t.Fatalf("%s should be computed_optional (Computed=%v Optional=%v Required=%v)", name, a.Computed, a.Optional, a.Required)
		}
		if !hasPM(a.PlanModifiers, "will not change") {
			t.Fatalf("%s should carry a UseStateForUnknown plan modifier, got %+v", name, a.PlanModifiers)
		}
		if !hasPM(a.PlanModifiers, "destroy and recreate") {
			t.Fatalf("%s (IMMUTABLE) should carry a RequiresReplace plan modifier, got %+v", name, a.PlanModifiers)
		}
	}

	// secret → sensitive.
	if st := strAttr("secret_token"); !st.Sensitive {
		t.Fatalf("secret_token should be Sensitive")
	}

	// enum → a OneOf validator.
	cat := strAttr("category")
	if len(cat.Validators) != 1 || !strings.Contains(cat.Validators[0].Description(ctx), "must be one of") {
		t.Fatalf("category should carry a OneOf validator, got %+v", cat.Validators)
	}

	// not_null must NOT have made sku required (proves not_null↛REQUIRED).
	if sku := strAttr("sku"); sku.Required {
		t.Fatalf("sku (storage not_null, no client behavior) must not be Required")
	}

	// weight is an int64 attribute (type mapping).
	if _, ok := attrs["weight"].(schema.Int64Attribute); !ok {
		t.Fatalf("weight should be an Int64Attribute, got %T", attrs["weight"])
	}
}
