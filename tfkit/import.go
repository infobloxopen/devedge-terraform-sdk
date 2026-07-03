package tfkit

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ResourceClient extracts the shared *[Client] from a resource Configure
// request's ProviderData. It returns nil (without error) before the provider is
// configured — the framework calls Configure with nil ProviderData first — and
// records a diagnostic if the data is present but of an unexpected type.
// Generated resource glue calls it from Configure.
func ResourceClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *tfkit.Client, got %T. This is a provider bug.", req.ProviderData),
		)
		return nil
	}
	return c
}

// ImportStatePassthroughID implements `terraform import` by writing the import
// id into the named attribute (usually the resource key). It wraps the
// framework passthrough so a generated resource does not import the path
// package directly.
func ImportStatePassthroughID(ctx context.Context, attr string, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root(attr), req, resp)
}

// pathRootEndpoint is the config path of the provider endpoint attribute, used
// for attribute-scoped diagnostics.
func pathRootEndpoint() path.Path { return path.Root("endpoint") }

// stringValue returns the Go string of a framework value, or "" when null or
// unknown.
func stringValue(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// firstNonEmpty returns the first non-empty string in order.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// envName maps a provider type name and suffix to an environment variable name,
// e.g. ("toy", "ENDPOINT") → "TOY_ENDPOINT", ("my-svc", "TOKEN") → "MY_SVC_TOKEN".
func envName(typeName, suffix string) string {
	var b strings.Builder
	for _, r := range typeName {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String() + "_" + suffix
}
