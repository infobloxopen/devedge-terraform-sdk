package tfkit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func newTestProvider() provider.Provider {
	return NewProvider(ProviderConfig{
		TypeName:  "toy",
		Version:   "1.2.3",
		Resources: []func() resource.Resource{func() resource.Resource { return nil }},
	})
}

func TestProviderMetadataAndSchema(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider()

	var mr provider.MetadataResponse
	p.Metadata(ctx, provider.MetadataRequest{}, &mr)
	if mr.TypeName != "toy" || mr.Version != "1.2.3" {
		t.Fatalf("metadata = %+v", mr)
	}

	var sr provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sr)
	if _, ok := sr.Schema.Attributes["endpoint"]; !ok {
		t.Fatalf("provider schema missing endpoint attribute")
	}
	tok, ok := sr.Schema.Attributes["token"].(providerschema.StringAttribute)
	if !ok || !tok.Sensitive {
		t.Fatalf("token attribute should be a sensitive StringAttribute, got %#v", sr.Schema.Attributes["token"])
	}

	if got := p.Resources(ctx); len(got) != 1 {
		t.Fatalf("Resources() should pass through the configured constructors, got %d", len(got))
	}
	if got := p.DataSources(ctx); got != nil {
		t.Fatalf("DataSources() should be nil")
	}
}

// nullConfig builds a provider config whose object is known but every attribute
// is null, so Configure reads no attributes and falls back to the environment.
func nullConfig(ctx context.Context, p provider.Provider) tfsdk.Config {
	var sr provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &sr)
	obj := sr.Schema.Type().TerraformType(ctx).(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(obj.AttributeTypes))
	for name, at := range obj.AttributeTypes {
		vals[name] = tftypes.NewValue(at, nil)
	}
	return tfsdk.Config{Schema: sr.Schema, Raw: tftypes.NewValue(obj, vals)}
}

func TestProviderConfigureEnvFallback(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider()
	t.Setenv("TOY_ENDPOINT", "https://env.example.com")
	t.Setenv("TOY_TOKEN", "env-token")

	resp := &provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{Config: nullConfig(ctx, p)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Configure diagnostics: %v", resp.Diagnostics)
	}
	c, ok := resp.ResourceData.(*Client)
	if !ok {
		t.Fatalf("ResourceData should be *Client, got %T", resp.ResourceData)
	}
	if c.BaseURL != "https://env.example.com" || c.Token != "env-token" {
		t.Fatalf("client not configured from env: %+v", c)
	}
}

func TestProviderConfigureMissingEndpoint(t *testing.T) {
	ctx := context.Background()
	p := newTestProvider()
	t.Setenv("TOY_ENDPOINT", "")
	t.Setenv("TOY_TOKEN", "")

	resp := &provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{Config: nullConfig(ctx, p)}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("Configure should fail loud when no endpoint is set")
	}
}
