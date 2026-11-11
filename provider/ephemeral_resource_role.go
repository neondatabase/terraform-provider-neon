package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ ephemeral.EphemeralResourceWithConfigure = (*neonEphemeralRole)(nil)

type neonEphemeralRole struct {
	client *neon.Client
}

type neonEphemeralRoleResourceModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Name      types.String `tfsdk:"name"`
	Password  types.String `tfsdk:"password"`
}

func NewNeonRoleEphemeralResource() ephemeral.EphemeralResource {
	return &neonEphemeralRole{}
}

func (r *neonEphemeralRole) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
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

func (r *neonEphemeralRole) Metadata(_ context.Context, _ ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = "neon_role"
}

func (r *neonEphemeralRole) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Ephemeral resource to retrieve the password of a Neon Postgres role. The password is never stored in the Terraform state.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:    true,
				Description: "Branch ID.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Role name.",
			},
			"password": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Database authentication password.",
			},
		},
	}
}

func (r *neonEphemeralRole) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var data neonEphemeralRoleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var pass string
	err := projectReadiness.Do(ctx, func(ctx context.Context) error {
		roleResp, err := r.client.GetProjectBranchRolePassword(data.ProjectID.ValueString(), data.BranchID.ValueString(),
			data.Name.ValueString())
		if err == nil {
			pass = roleResp.Password
		}
		return err
	}, nil)
	resp.Diagnostics.Append(newEphemeralOpenDiagnostics(err, "role")...)

	if resp.Diagnostics.HasError() {
		return
	}

	data.Password = types.StringValue(pass)
	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)
}
