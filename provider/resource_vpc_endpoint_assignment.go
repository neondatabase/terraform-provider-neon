package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonVPCEndpointAssignment)(nil)
var _ resource.ResourceWithImportState = (*neonVPCEndpointAssignment)(nil)

type neonVPCEndpointAssignment struct {
	client *neon.Client
}

type neonVPCEndpointAssignmentResourceModel struct {
	ID            types.String `tfsdk:"id"`
	OrgID         types.String `tfsdk:"org_id"`
	RegionID      types.String `tfsdk:"region_id"`
	VPCEndpointID types.String `tfsdk:"vpc_endpoint_id"`
	Label         types.String `tfsdk:"label"`
}

func NewNeonVPCEndpointAssignmentResource() resource.Resource {
	return &neonVPCEndpointAssignment{}
}

func (r *neonVPCEndpointAssignment) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r neonVPCEndpointAssignment) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_vpc_endpoint_assignment"
}

func (r neonVPCEndpointAssignment) Schema(_ context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: `Assigns, or updates existing assignment of a VPC endpoint to a Neon organization.
See details: https://neon.tech/docs/guides/neon-private-networking#enable-private-dns
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "The ID of this resource.",
			},
			"org_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon organization ID.",
			},
			"region_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon region ID.",
			},
			"vpc_endpoint_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The VPC endpoint ID.",
			},
			"label": schema.StringAttribute{
				Required:    true,
				Description: "A descriptive label for the VPC endpoint.",
			},
		},
	}
}

func (r neonVPCEndpointAssignment) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointAssignmentResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		return r.client.AssignOrganizationVPCEndpoint(state.OrgID.ValueString(), state.RegionID.ValueString(),
			state.VPCEndpointID.ValueString(), neon.VPCEndpointAssignment{Label: state.Label.ValueString()})
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(state.OrgID.ValueString() + "/" + state.RegionID.ValueString() + "/" +
		state.VPCEndpointID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonVPCEndpointAssignment) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var endpoint neon.VPCEndpointDetails
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		endpoint, err = r.client.GetOrganizationVPCEndpointDetails(state.OrgID.ValueString(), state.RegionID.ValueString(),
			state.VPCEndpointID.ValueString())
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	if endpoint.VpcEndpointID == "" {
		resp.Diagnostics.AddWarning("No VPC endpoint found",
			fmt.Sprintf("Endpoint %q not found in the org %q for the region %q",
				state.VPCEndpointID.ValueString(), state.OrgID.ValueString(), state.RegionID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.Label = types.StringValue(endpoint.Label)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonVPCEndpointAssignment) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		return r.client.AssignOrganizationVPCEndpoint(state.OrgID.ValueString(), state.RegionID.ValueString(),
			state.VPCEndpointID.ValueString(), neon.VPCEndpointAssignment{Label: state.Label.ValueString()})
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(state.OrgID.ValueString() + "/" + state.RegionID.ValueString() + "/" +
		state.VPCEndpointID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonVPCEndpointAssignment) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		return r.client.DeleteOrganizationVPCEndpoint(state.OrgID.ValueString(), state.RegionID.ValueString(),
			state.VPCEndpointID.ValueString())
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r neonVPCEndpointAssignment) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 3)
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State VPC Endpoint Assignment ID",
			"Expected an import ID in the form <org_id>/<region_id>/<vpc_endpoint_id>.",
		)
		return
	}

	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var endpoint neon.VPCEndpointDetails
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		endpoint, err = r.client.GetOrganizationVPCEndpointDetails(els[0], els[1], els[2])
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &neonVPCEndpointAssignmentResourceModel{
		ID:            types.StringValue(req.ID),
		OrgID:         types.StringValue(els[0]),
		RegionID:      types.StringValue(els[1]),
		VPCEndpointID: types.StringValue(els[2]),
		Label:         types.StringValue(endpoint.Label),
	})...)
}
