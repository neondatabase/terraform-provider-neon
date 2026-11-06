package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ ephemeral.EphemeralResourceWithConfigure = (*neonEphemeralConnectionURI)(nil)

type neonEphemeralConnectionURI struct {
	client *neon.Client
}

type neonEphemeralConnectionURIResourceModel struct {
	ProjectID  types.String `tfsdk:"project_id"`
	RoleName   types.String `tfsdk:"role"`
	Database   types.String `tfsdk:"database"`
	BranchID   types.String `tfsdk:"branch_id"`
	EndpointID types.String `tfsdk:"endpoint_id"`
	URI        types.String `tfsdk:"uri"`
	URIPooler  types.String `tfsdk:"uri_pooler"`
}

func NewNeonConnectionURIEphemeralResource() ephemeral.EphemeralResource {
	return &neonEphemeralConnectionURI{}
}

func (r *neonEphemeralConnectionURI) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *providerAdapter, got an unexpected type.")
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	r.client = client.sdk
}

func (r *neonEphemeralConnectionURI) Metadata(_ context.Context, _ ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = "neon_connection_uri"
}

func (r *neonEphemeralConnectionURI) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Ephemeral resource to retrieve the connection URIs of a Neon database for a role. The URIs contain the role's password and are never stored in the Terraform state.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Role name.",
			},
			"database": schema.StringAttribute{
				Required:    true,
				Description: "Database name.",
			},
			"branch_id": schema.StringAttribute{
				Optional:    true,
				Description: "Branch ID.",
			},
			"endpoint_id": schema.StringAttribute{
				Optional:    true,
				Description: "Endpoint ID.",
			},
			"uri": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "URI of the direct database connection. It contains the role's password.",
			},
			"uri_pooler": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "URI of the pooled connection. It contains the role's password.",
			},
		},
	}
}

func (r *neonEphemeralConnectionURI) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var data neonEphemeralConnectionURIResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var uri string
	var pooled bool
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		r, err := r.client.GetConnectionURI(
			data.ProjectID.ValueString(),
			data.BranchID.ValueStringPointer(),
			data.EndpointID.ValueStringPointer(),
			data.Database.ValueString(),
			data.RoleName.ValueString(),
			&pooled,
		)
		if err == nil {
			uri = r.URI
		}
		return err
	}, nil)...)

	if resp.Diagnostics.HasError() {
		return
	}

	data.URI = types.StringValue(uri)
	data.URIPooler = types.StringValue(newPoolerURI(uri))
	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)
}

// The logic defining the pooling connection URI is derived from
// https://neon.com/docs/connect/connection-pooling#how-to-use-connection-pooling
func newPoolerURI(s string) string {
	els := strings.SplitN(s, "@", 2)
	suff := strings.SplitN(els[1], ".", 2)
	return els[0] + "@" + suff[0] + "-pooler." + suff[1]
}
