package tfkit

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ProviderModel is the provider configuration block a generated provider
// exposes: an endpoint and a bearer token, both optional so they can also come
// from the environment.
type ProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Token    types.String `tfsdk:"token"`
	Headers  types.Map    `tfsdk:"headers"`
}

// ProviderConfig configures [NewProvider]. A generated (or scaffolded) provider
// composes the base by supplying its type name, version, and resource
// constructors; the base owns endpoint/token configuration and building the
// shared [Client].
type ProviderConfig struct {
	// TypeName is the provider address type name, e.g. "toy" for
	// registry.terraform.io/infobloxopen/toy. Resource type names are derived
	// from it as "<TypeName>_<resource>".
	TypeName string
	// Version is the provider version surfaced in Metadata.
	Version string
	// EndpointEnv/TokenEnv name the environment variables consulted when the
	// configuration block omits endpoint/token. Defaults are derived from
	// TypeName as "<UPPER(TypeName)>_ENDPOINT" / "<UPPER(TypeName)>_TOKEN".
	EndpointEnv string
	TokenEnv    string
	// Resources are the resource constructors the provider serves; a generated
	// registration file supplies them.
	Resources []func() resource.Resource
}

// NewProvider returns a reusable [provider.Provider] built from cfg. The
// returned provider configures a shared [Client] from the endpoint/token (falling
// back to the environment) and passes it to every resource via ProviderData.
func NewProvider(cfg ProviderConfig) provider.Provider {
	if cfg.EndpointEnv == "" {
		cfg.EndpointEnv = envName(cfg.TypeName, "ENDPOINT")
	}
	if cfg.TokenEnv == "" {
		cfg.TokenEnv = envName(cfg.TypeName, "TOKEN")
	}
	return &baseProvider{cfg: cfg}
}

type baseProvider struct {
	cfg ProviderConfig
}

var _ provider.Provider = (*baseProvider)(nil)

func (p *baseProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = p.cfg.TypeName
	resp.Version = p.cfg.Version
}

func (p *baseProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		Description: fmt.Sprintf("Provider for the %s service.", p.cfg.TypeName),
		Attributes: map[string]providerschema.Attribute{
			"endpoint": providerschema.StringAttribute{
				Optional:    true,
				Description: fmt.Sprintf("Service base URL. May also be set via the %s environment variable.", p.cfg.EndpointEnv),
			},
			"token": providerschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: fmt.Sprintf("Bearer token for authentication. May also be set via the %s environment variable.", p.cfg.TokenEnv),
			},
			"headers": providerschema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Extra headers sent on every request. Use for local dev against a service whose authorizer reads request metadata rather than a bearer token, e.g. { \"account-id\" = \"t1\", \"groups\" = \"admin\" }.",
			},
		},
	}
}

func (p *baseProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m ProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := firstNonEmpty(stringValue(m.Endpoint), os.Getenv(p.cfg.EndpointEnv))
	token := firstNonEmpty(stringValue(m.Token), os.Getenv(p.cfg.TokenEnv))

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			pathRootEndpoint(),
			"Missing service endpoint",
			fmt.Sprintf("Set the provider `endpoint` attribute or the %s environment variable.", p.cfg.EndpointEnv),
		)
		return
	}

	var headers map[string]string
	if !m.Headers.IsNull() && !m.Headers.IsUnknown() {
		headers = make(map[string]string, len(m.Headers.Elements()))
		resp.Diagnostics.Append(m.Headers.ElementsAs(ctx, &headers, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	client := NewClientWithHeaders(endpoint, token, headers)
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *baseProvider) Resources(context.Context) []func() resource.Resource {
	return p.cfg.Resources
}

func (p *baseProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
