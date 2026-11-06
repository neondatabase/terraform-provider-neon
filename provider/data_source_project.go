package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ datasource.DataSource = (*neonProject)(nil)
var _ datasource.DataSourceWithConfigure = (*neonProject)(nil)

type neonProject struct {
	r *neonProjectResource
}

func NewProjectDataSource() datasource.DataSource {
	return &neonProject{}
}

func (d *neonProject) Metadata(_ context.Context, _ datasource.MetadataRequest,
	resp *datasource.MetadataResponse) {
	resp.TypeName = "neon_project"
}

func (d neonProject) Schema(ctx context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	r := &neonProjectResource{}
	resourceResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, resourceResp)
	resp.Diagnostics.Append(resourceResp.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	attsResource := resourceResp.Schema.Attributes
	blocksResource := resourceResp.Schema.Blocks
	resp.Schema = schema.Schema{
		Description: "Reads a Neon project by ID, including its default branch, primary compute, and settings.",
		Attributes:  make(map[string]schema.Attribute, len(attsResource)),
		Blocks:      make(map[string]schema.Block, len(blocksResource)),
	}
	for k, v := range attsResource {
		converted := newReadOnlyDataSourceAttrFromResourceAttr(v)
		if converted == nil {
			resp.Diagnostics.AddError(
				"Unsupported project attribute",
				fmt.Sprintf("%q has unsupported type %T", k, v.GetType()),
			)
			return
		}
		resp.Schema.Attributes[k] = converted
		// id is always required
		if k == "id" {
			tmp := resp.Schema.Attributes[k].(schema.StringAttribute)
			tmp.Computed = false
			tmp.Required = true
			resp.Schema.Attributes[k] = tmp
		}
	}

	for k, v := range blocksResource {
		resp.Schema.Blocks[k] = newReadOnlyDataSourceBlockFromResourceBlock(v)
	}
}

func (d *neonProject) Configure(ctx context.Context, req datasource.ConfigureRequest,
	resp *datasource.ConfigureResponse) {
	d.r = &neonProjectResource{}
	resourceResp := &resource.ConfigureResponse{}
	d.r.Configure(ctx, resource.ConfigureRequest{ProviderData: req.ProviderData}, resourceResp)
	resp.Diagnostics.Append(resourceResp.Diagnostics...)
}

func (d neonProject) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data neonProjectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	initializeProjectBlocks(&data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readResp := resource.ReadResponse{State: resp.State}
	d.r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}

	if readResp.State.Raw.IsNull() {
		resp.Diagnostics.AddError("project not found", "")
	}
	resp.State = readResp.State
}
