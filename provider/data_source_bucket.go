package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ datasource.DataSource = (*neonBucketDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*neonBucketDataSource)(nil)

type neonBucketDataSource struct {
	client *neon.Client
}

type neonBucketDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	BranchID       types.String `tfsdk:"branch_id"`
	S3Endpoint     types.String `tfsdk:"s3_endpoint"`
	Region         types.String `tfsdk:"region"`
	ForcePathStyle types.Bool   `tfsdk:"force_path_style"`
}

func NewBucketDataSource() datasource.DataSource {
	return &neonBucketDataSource{}
}

func (d *neonBucketDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_bucket"
}

func (d *neonBucketDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the S3-compatible connection details for buckets on a Neon branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The data source identifier in the form `<project_id>/<branch_id>`.",
			},
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:    true,
				Description: "The Neon branch ID.",
			},
			"s3_endpoint": schema.StringAttribute{
				Computed:    true,
				Description: "The S3-compatible endpoint URL for this branch's object storage.",
			},
			"region": schema.StringAttribute{
				Computed:    true,
				Description: "The AWS region for this branch's object storage.",
			},
			"force_path_style": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the S3 client must use path-style addressing (bucket-in-path rather than virtual-hosted subdomain). Always true for Neon buckets.",
			},
		},
	}
}

func (d *neonBucketDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *neonBucketDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data neonBucketDataSourceModel
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

	var storage neon.BranchStorage
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		storage, err = d.client.GetProjectBranchStorage(projectID, branchID)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.ID = types.StringValue(projectID + "/" + branchID)
	data.S3Endpoint = types.StringValue(storage.S3Endpoint)
	data.Region = types.StringValue(storage.Region)
	data.ForcePathStyle = types.BoolValue(storage.ForcePathStyle)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
