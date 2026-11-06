package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonAIGatewayDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonAIGatewayDataSource)(nil)

type neonAIGatewayDataSource struct {
	client *neon.Client
}

type neonAIGatewayDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	BaseURL   types.String `tfsdk:"base_url"`
}

func NewAIGatewayDataSource() datasource.DataSource {
	return &neonAIGatewayDataSource{}
}

func (d *neonAIGatewayDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_ai_gateway"
}

func (d *neonAIGatewayDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the AI Gateway configuration for a Neon branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The data source identifier in the form <project_id>/<branch_id>.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:    true,
				Description: "The Neon branch ID.",
			},
			"base_url": schema.StringAttribute{
				Computed:    true,
				Description: "The OpenAI-compatible AI Gateway base URL for the branch.",
			},
		},
	}
}

func (d *neonAIGatewayDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			"Expected *providerAdapter, got an unexpected type.",
		)
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	d.client = client.sdk
}

func (d *neonAIGatewayDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data neonAIGatewayDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	projectID := data.ProjectID.ValueString()
	branchID := data.BranchID.ValueString()

	var gateway neon.BranchAiGateway
	err := projectReadiness.Do(ctx, func(_ context.Context) error {
		var err error
		gateway, err = d.client.GetProjectBranchAiGateway(projectID, branchID)
		return err
	}, nil)
	var apiErr neon.Error
	if errors.As(err, &apiErr) && slices.ContainsFunc(apiErr.Details, func(e neon.ErrorDetails) bool {
		return e.Code == "AI_GATEWAY_NOT_ENABLED"
	}) {
		resp.Diagnostics.AddError("AI Gateway unavailable on this branch",
			fmt.Sprintf("%s\n\nCheck that branch %s exists in project %s, that the project is in a region the AI Gateway "+
				"serves, and that the gateway is enabled on the branch in the Neon Console or with aiGateway in neon.ts "+
				"and neon config apply. Then run Terraform again.", err, branchID, projectID))
		return
	}
	resp.Diagnostics.Append(newDiagnostics(err)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.StringValue(projectID + "/" + branchID)
	data.BaseURL = types.StringValue(gateway.BaseURL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
