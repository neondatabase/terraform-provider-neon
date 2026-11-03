package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonBranchesDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonBranchesDataSource)(nil)

type neonBranchesDataSource struct {
	client *neon.Client
}

type neonBranchesDataSourceModel struct {
	ID        types.String              `tfsdk:"id"`
	ProjectID types.String              `tfsdk:"project_id"`
	Filter    types.String              `tfsdk:"filter"`
	Branches  []neonBranchResourceModel `tfsdk:"branches"`
}

func NewBranchesDataSource() datasource.DataSource {
	return &neonBranchesDataSource{}
}

func (d *neonBranchesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_branches"
}

func (d *neonBranchesDataSource) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	var resourceResponse resource.SchemaResponse
	NewNeonBranchResource().Schema(ctx, resource.SchemaRequest{}, &resourceResponse)
	resp.Diagnostics.Append(resourceResponse.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	attributes := make(map[string]schema.Attribute, len(resourceResponse.Schema.Attributes))
	for name, attribute := range resourceResponse.Schema.Attributes {
		converted := newReadOnlyDataSourceAttrFromResourceAttr(attribute)
		if converted == nil {
			resp.Diagnostics.AddError("Unsupported branch attribute",
				fmt.Sprintf("%q has unsupported type %T", name, attribute.GetType()))
			return
		}
		attributes[name] = converted
	}

	resp.Schema = schema.Schema{
		Description: "Fetch Project Branches.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Project ID.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"filter": schema.StringAttribute{
				Optional:    true,
				Description: "Filter by partial branch name or ID.",
			},
			"branches": schema.ListNestedAttribute{
				Computed:    true,
				Description: "Project branches in API response order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: attributes,
				},
			},
		},
	}
}

func (d *neonBranchesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if adapter == nil || adapter.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}
	d.client = adapter.sdk
}

func (d *neonBranchesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	var data neonBranchesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var search *string
	if data.Filter.ValueString() != "" {
		value := url.QueryEscape(data.Filter.ValueString())
		search = &value
	}
	data.Branches = make([]neonBranchResourceModel, 0)
	var cursor *string
	sortBy := neon.ListProjectBranchesSortByName
	sortOrder := neon.SortOrderParamAsc
	for {
		if err := ctx.Err(); err != nil {
			resp.Diagnostics.Append(frameworkDiagnostics(err)...)
			return
		}
		var result neon.ListProjectBranchesRespObj
		resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
			var err error
			result, err = d.client.ListProjectBranches(
				data.ProjectID.ValueString(), search, &sortBy, cursor, &sortOrder, nil, nil,
			)
			return err
		}, ctx)...)
		if resp.Diagnostics.HasError() {
			return
		}

		for _, branch := range result.Branches {
			var model neonBranchResourceModel
			model.inferAttr(branch)
			data.Branches = append(data.Branches, model)
		}

		cursor = nil
		if result.Pagination != nil {
			cursor = result.Pagination.Next
		}
		if cursor == nil {
			break
		}
		next := url.QueryEscape(*cursor)
		cursor = &next
	}

	data.ID = data.ProjectID
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
