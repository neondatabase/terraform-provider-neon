package provider

import (
	"context"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-mux/tf5to6server"
	"github.com/hashicorp/terraform-plugin-mux/tf6muxserver"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/neondatabase/terraform-provider-neon/provider/telemetry"
)

const Name = "neondatabase/neon"

func init() {
	rand.New(rand.NewSource(time.Now().Unix()))
	// Set descriptions to support markdown syntax, this will be used in document generation
	// and the language server.
	schema.DescriptionKind = schema.StringMarkdown
}

var p = &schema.Provider{
	Schema: map[string]*schema.Schema{
		"api_key": {
			Type:        schema.TypeString,
			Optional:    true,
			Sensitive:   true,
			Description: "API access key. Default is read from the environment variable `NEON_API_KEY`.",
			DefaultFunc: schema.EnvDefaultFunc("NEON_API_KEY", ""),
		},
	},
	DataSourcesMap: map[string]*schema.Resource{},
}

// New returns the provider.
func New(version string) *schema.Provider {
	var o = new(schema.Provider)
	*o = *p
	o.ConfigureContextFunc = func(ctx context.Context, d *schema.ResourceData) (c interface{},
		errs diag.Diagnostics) {
		var err error
		c, err = neon.NewClient(neon.Config{
			Key:        d.Get("api_key").(string),
			HTTPClient: telemetry.NewHTTPClient(Name, version, o.TerraformVersion),
		})
		if err != nil {
			errs = diag.FromErr(err)
		}
		return c, errs
	}
	return o
}

func newAccTest() *schema.Provider {
	return New("accTest")
}

// NewFramework returns the Framework provider used by the protocol mux.
func NewFramework(version string) frameworkprovider.Provider {
	return &frameworkProvider{version: version}
}

var _ frameworkprovider.ProviderWithEphemeralResources = (*frameworkProvider)(nil)

// frameworkProvider is the Framework portion of the provider. It is served
// through terraform-plugin-mux alongside the legacy SDK provider.
type frameworkProvider struct {
	version     string
	neonAdapter *providerAdapter
}

type frameworkProviderConfigModel struct {
	APIKey types.String `tfsdk:"api_key"`
}

func (p *frameworkProvider) Metadata(_ context.Context, _ frameworkprovider.MetadataRequest, resp *frameworkprovider.MetadataResponse) {
	resp.TypeName = "neon"
	resp.Version = p.version
}

func (p *frameworkProvider) Schema(_ context.Context, _ frameworkprovider.SchemaRequest, resp *frameworkprovider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		Attributes: map[string]providerschema.Attribute{
			"api_key": providerschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "API access key. Default is read from the environment variable `NEON_API_KEY`.",
			},
		},
	}
}

type httpClient interface {
	Do(r *http.Request) (*http.Response, error)
}

type providerAdapter struct {
	sdk        *neon.Client
	httpClient httpClient
}

func (p *frameworkProvider) Configure(ctx context.Context, req frameworkprovider.ConfigureRequest,
	resp *frameworkprovider.ConfigureResponse) {
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

func (p *frameworkProvider) Resources(_ context.Context) []func() resource.Resource {
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

func (p *frameworkProvider) DataSources(_ context.Context) []func() datasource.DataSource {
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

func (p *frameworkProvider) EphemeralResources(_ context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{
		NewNeonRoleEphemeralResource,
		NewNeonConnectionURIEphemeralResource,
	}
}

func NewServer(version string) (tfprotov6.ProviderServer, error) {
	legacyServer, err := tf5to6server.UpgradeServer(context.Background(), func() tfprotov5.ProviderServer {
		return New(version).GRPCProvider()
	})
	if err != nil {
		return nil, err
	}
	return tf6muxserver.NewMuxServer(context.Background(),
		func() tfprotov6.ProviderServer {
			return legacyServer
		},
		providerserver.NewProtocol6(NewFramework(version)),
	)
}

func newProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"neon": func() (tfprotov6.ProviderServer, error) {
			return NewServer("accTest")
		},
	}
}

func newUnitTestProviderFactories(neonAdapter *providerAdapter) map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"neon": func() (tfprotov6.ProviderServer, error) {
			p := &frameworkProvider{version: "unitTest", neonAdapter: neonAdapter}
			return providerserver.NewProtocol6(p)(), nil
		},
	}
}
