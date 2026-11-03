package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonActiveRegions)(nil)
var _ datasource.DataSourceWithConfigure = (*neonActiveRegions)(nil)

type neonActiveRegions struct {
	client *neon.Client
}

type neonActiveRegionsModel struct {
	ID      types.String        `tfsdk:"id"`
	OrgID   types.String        `tfsdk:"org_id"`
	Regions []activeRegionModel `tfsdk:"regions"`
}

type activeRegionModel struct {
	RegionID types.String `tfsdk:"region_id"`
	Name     types.String `tfsdk:"name"`
	Default  types.Bool   `tfsdk:"default"`
}

func NewActiveRegionsDataSource() datasource.DataSource {
	return &neonActiveRegions{}
}

func (d *neonActiveRegions) Metadata(_ context.Context, _ datasource.MetadataRequest,
	resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_active_regions"
}

func (d *neonActiveRegions) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists supported Neon regions.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The terraform resource ID.",
			},
			"org_id": schema.StringAttribute{
				Optional:    true,
				Description: "The Neon organization ID.",
			},
			"regions": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The list of active regions.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"region_id": schema.StringAttribute{
							Computed:    true,
							Description: "The ID of the Neon region.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "The name of the Neon region.",
						},
						"default": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the region is the default.",
						},
					},
				},
			},
		},
	}
}

func (d *neonActiveRegions) Configure(_ context.Context, req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse) {
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

func (d *neonActiveRegions) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data neonActiveRegionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("SDK is not configured", "The Neon provider client is unavailable.")
		return
	}

	var r neon.ActiveRegionsResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		var err error
		r, err = d.client.GetActiveRegions(data.OrgID.ValueStringPointer())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Regions = make([]activeRegionModel, 0, len(r.Regions))
	for _, region := range r.Regions {
		data.Regions = append(data.Regions, activeRegionModel{
			RegionID: types.StringValue(region.RegionID),
			Name:     types.StringValue(region.Name),
			Default:  types.BoolValue(region.Default),
		})
	}
	data.ID = types.StringValue("activeRegions")

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
