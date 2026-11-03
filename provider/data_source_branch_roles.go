package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonBranchRolesDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonBranchRolesDataSource)(nil)

type neonBranchRolesDataSource struct {
	client *neon.Client
}

type neonBranchRolesDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Roles     types.List   `tfsdk:"roles"`
}

type branchRoleModel struct {
	Name      types.String `tfsdk:"name"`
	Protected types.Bool   `tfsdk:"protected"`
}

// NewBranchRolesDataSource returns the branch roles data source.
func NewBranchRolesDataSource() datasource.DataSource {
	return &neonBranchRolesDataSource{}
}

func (d *neonBranchRolesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_branch_roles"
}

func (d *neonBranchRolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists PostgreSQL roles in a Neon branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Role-list scope in the form `<project_id>/<branch_id>`.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:    true,
				Description: "Branch ID.",
			},
			"roles": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Branch roles in API response order. An empty branch role list returns an empty list.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "PostgreSQL role name.",
						},
						"protected": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the role is system-protected. Defaults to true when the API omits this field.",
						},
					},
				},
			},
		},
	}
}

func (d *neonBranchRolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if adapter.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}
	d.client = adapter.sdk
}

func (d *neonBranchRolesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	var data neonBranchRolesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.RolesResponse
	resp.Diagnostics.Append(projectReadiness.Retry(func(_ context.Context) error {
		var err error
		result, err = d.client.ListProjectBranchRoles(data.ProjectID.ValueString(), data.BranchID.ValueString())
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	roles := make([]branchRoleModel, 0, len(result.Roles))
	for _, role := range result.Roles {
		protected := true
		if role.Protected != nil {
			protected = *role.Protected
		}
		roles = append(roles, branchRoleModel{
			Name:      types.StringValue(role.Name),
			Protected: types.BoolValue(protected),
		})
	}

	var diags diag.Diagnostics
	data.Roles, diags = types.ListValueFrom(ctx,
		types.ObjectType{AttrTypes: map[string]attr.Type{
			"name":      types.StringType,
			"protected": types.BoolType,
		}}, roles)

	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.StringValue(fmt.Sprintf("%s/%s", data.ProjectID.ValueString(), data.BranchID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
