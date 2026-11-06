package provider

import (
	"context"
	"net/http"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/neondatabase/terraform-provider-neon/provider/telemetry"
)

const Name = "neondatabase/neon"

type httpClient interface {
	Do(r *http.Request) (*http.Response, error)
}

type providerAdapter struct {
	sdk        *neon.Client
	httpClient httpClient
}

var _ provider.ProviderWithEphemeralResources = (*Provider)(nil)

type Provider struct {
	version     string
	neonAdapter *providerAdapter
}

type frameworkProviderConfigModel struct {
	APIKey types.String `tfsdk:"api_key"`
}

func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "neon"
	resp.Version = p.version
}

func (p *Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "API access key. Default is read from the environment variable `NEON_API_KEY`.",
			},
		},
	}
}

func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest,
	resp *provider.ConfigureResponse) {
	if p.neonAdapter != nil {
		resp.ResourceData = p.neonAdapter
		resp.DataSourceData = p.neonAdapter
		return
	}

	var config frameworkProviderConfigModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := config.APIKey.ValueString()
	if key == "" {
		key = os.Getenv("NEON_API_KEY")
	}

	cfg := neon.Config{
		Key:        key,
		HTTPClient: telemetry.NewHTTPClient(Name, p.version, req.TerraformVersion),
	}
	client, err := neon.NewClient(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure Neon Provider", err.Error())
		return
	}

	providerClient := providerAdapter{
		sdk:        client,
		httpClient: cfg.HTTPClient,
	}

	resp.ResourceData = &providerClient
	resp.DataSourceData = &providerClient
	resp.EphemeralResourceData = &providerClient
}

func (p *Provider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAPIKeyResource,
		NewOrgAPIKeyResource,
		NewJWKSURLResource,
		NewBranchBackupScheduleResource,
		NewNeonBucketResource,
		NewNeonServiceCredentialResource,
		NewNeonBucketObjectResource,
		NewNeonFunctionResource,
		NewNeonProjectMemberRoleResource,
		NewNeonTriggerResource,
		NewSnapshotResource,
		NewNeonOrgMemberRoleResource,
		NewNeonOrgSpendingLimitResource,
		NewNeonCustomBranchDomainResource,
		NewNeonBranchResource,
		NewNeonRoleResource,
		NewNeonDataAPIResource,
		NewNeonDatabaseResource,
		NewNeonProjectPermissionResource,
		NewNeonVPCEndpointAssignmentResource,
		NewNeonVPCEndpointRestrictionResource,
		NewNeonEndpointResource,
		NewNeonProjectResource,
	}
}

func (p *Provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAIGatewayDataSource,
		NewBucketDataSource,
		NewActiveRegionsDataSource,
		NewBranchRolesDataSource,
		NewBranchEndpointsDataSource,
		NewProjectDataSource,
		NewBranchesDataSource,
	}
}

func (p *Provider) EphemeralResources(_ context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{
		NewNeonRoleEphemeralResource,
		NewNeonConnectionURIEphemeralResource,
	}
}

func NewServer(version string) tfprotov6.ProviderServer {
	return providerserver.NewProtocol6(&Provider{version: version})()
}

func newProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"neon": func() (tfprotov6.ProviderServer, error) {
			return NewServer("accTest"), nil
		},
	}
}

func newUnitTestProviderFactories(neonAdapter *providerAdapter) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"neon": func() (tfprotov6.ProviderServer, error) {
			return providerserver.NewProtocol6WithError(&Provider{version: "unitTest", neonAdapter: neonAdapter})()
		},
	}
}
