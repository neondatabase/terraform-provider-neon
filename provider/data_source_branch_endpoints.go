package provider

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonBranchEndpointsDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonBranchEndpointsDataSource)(nil)

type neonBranchEndpointsDataSource struct {
	client *neon.Client
}

type neonBranchEndpointsDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Endpoints types.List   `tfsdk:"endpoints"`
}

type branchEndpointModel struct {
	ID                    types.String  `tfsdk:"id"`
	Host                  types.String  `tfsdk:"host"`
	HostPooling           types.String  `tfsdk:"host_pooling"`
	Type                  types.String  `tfsdk:"type"`
	RegionID              types.String  `tfsdk:"region_id"`
	ProxyHost             types.String  `tfsdk:"proxy_host"`
	Name                  types.String  `tfsdk:"name"`
	AutoscalingLimitMinCU types.Float64 `tfsdk:"autoscaling_limit_min_cu"`
	AutoscalingLimitMaxCU types.Float64 `tfsdk:"autoscaling_limit_max_cu"`
	SuspendTimeoutSeconds types.Int64   `tfsdk:"suspend_timeout_seconds"`
}

func NewBranchEndpointsDataSource() datasource.DataSource {
	return &neonBranchEndpointsDataSource{}
}

func (d *neonBranchEndpointsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_branch_endpoints"
}

func (d *neonBranchEndpointsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetch Branch Endpoints",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Endpoint-list scope in the form `<project_id>/<branch_id>`. Setting it has no effect.",
				DeprecationMessage: "neon_branch_endpoints sets id to <project_id>/<branch_id> and ignores a configured value. " +
					"Remove id from the configuration; a future release makes it read-only.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID. Use 1 to 60 lowercase ASCII letters, digits, or hyphens.",
				Validators:  []validator.String{endpointListScopeValidator{}},
			},
			"branch_id": schema.StringAttribute{
				Required:    true,
				Description: "Branch ID. Use 1 to 60 lowercase ASCII letters, digits, or hyphens.",
				Validators:  []validator.String{endpointListScopeValidator{}},
			},
		},
		Blocks: map[string]schema.Block{
			"endpoints": schema.ListNestedBlock{
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Endpoint ID.",
						},
						"host": schema.StringAttribute{
							Computed:    true,
							Description: "Endpoint URI.",
						},
						"host_pooling": schema.StringAttribute{
							Computed:    true,
							Description: "Endpoint URI for connection pooling.",
						},
						"type": schema.StringAttribute{
							Computed:    true,
							Description: "Access type.",
						},
						"region_id": schema.StringAttribute{
							Computed:    true,
							Description: "Deployment region: https://neon.tech/docs/introduction/regions",
						},
						"proxy_host": schema.StringAttribute{
							Computed: true,
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Compute name.",
						},
						"autoscaling_limit_min_cu": schema.Float64Attribute{
							Computed:    true,
							Description: "Minimal value of the compute autoscaling limit.",
						},
						"autoscaling_limit_max_cu": schema.Float64Attribute{
							Computed:    true,
							Description: "Maximal value of the compute autoscaling limit.",
						},
						"suspend_timeout_seconds": schema.Int64Attribute{
							Computed:    true,
							Description: "Duration of inactivity in seconds after which the compute endpoint is automatically suspended.",
						},
					},
				},
			},
		},
	}
}

var endpointListScopePattern = regexp.MustCompile(`^[a-z0-9-]{1,60}$`)

type endpointListScopeValidator struct{}

func (endpointListScopeValidator) Description(context.Context) string {
	return "Use 1 to 60 lowercase ASCII letters, digits, or hyphens."
}

func (v endpointListScopeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v endpointListScopeValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !endpointListScopePattern.MatchString(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+req.Path.String(), v.Description(ctx))
	}
}

func (d *neonBranchEndpointsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// TODO: reuse the Read method of the resource neonEndpointResource.
func (d *neonBranchEndpointsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	var data neonBranchEndpointsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.EndpointsResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = d.client.ListProjectBranchEndpoints(data.ProjectID.ValueString(), data.BranchID.ValueString())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoints := make([]branchEndpointModel, 0, len(result.Endpoints))
	for _, endpoint := range result.Endpoints {
		name := ""
		if endpoint.Name != nil {
			name = *endpoint.Name
		}
		endpoints = append(endpoints, branchEndpointModel{
			ID:   types.StringValue(endpoint.ID),
			Host: types.StringValue(endpoint.Host),
			HostPooling: types.StringValue(endpoint.ID + "-pooler" +
				strings.TrimPrefix(endpoint.Host, endpoint.ID)),
			Type:                  types.StringValue(endpoint.Type.String()),
			RegionID:              types.StringValue(endpoint.RegionID),
			ProxyHost:             types.StringValue(endpoint.ProxyHost),
			Name:                  types.StringValue(name),
			AutoscalingLimitMinCU: types.Float64Value(float64(endpoint.AutoscalingLimitMinCu)),
			AutoscalingLimitMaxCU: types.Float64Value(float64(endpoint.AutoscalingLimitMaxCu)),
			SuspendTimeoutSeconds: types.Int64Value(int64(endpoint.SuspendTimeoutSeconds)),
		})
	}

	endpointList, diags := types.ListValueFrom(ctx, types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"id":                       types.StringType,
			"host":                     types.StringType,
			"host_pooling":             types.StringType,
			"type":                     types.StringType,
			"region_id":                types.StringType,
			"proxy_host":               types.StringType,
			"name":                     types.StringType,
			"autoscaling_limit_min_cu": types.Float64Type,
			"autoscaling_limit_max_cu": types.Float64Type,
			"suspend_timeout_seconds":  types.Int64Type,
		},
	}, endpoints)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Endpoints = endpointList
	data.ID = types.StringValue(data.ProjectID.ValueString() + "/" + data.BranchID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
