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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonVPCEndpointRestriction)(nil)
var _ resource.ResourceWithImportState = (*neonVPCEndpointRestriction)(nil)

type neonVPCEndpointRestriction struct {
	client *neon.Client
}

type neonVPCEndpointRestrictionResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	VPCEndpointID types.String `tfsdk:"vpc_endpoint_id"`
	Label         types.String `tfsdk:"label"`
}

func NewNeonVPCEndpointRestrictionResource() resource.Resource {
	return &neonVPCEndpointRestriction{}
}

func (r *neonVPCEndpointRestriction) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r neonVPCEndpointRestriction) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_vpc_endpoint_restriction"
}

func (r neonVPCEndpointRestriction) Schema(_ context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: `Sets or updates a VPC endpoint restriction for a Neon project.
When a VPC endpoint restriction is set, the project only accepts connections
from the specified VPC.
A VPC endpoint can be set as a restriction only after it is assigned to the
parent organization of the Neon project.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The ID of this resource.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon project ID.",
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

func (r neonVPCEndpointRestriction) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointRestrictionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(func(_ context.Context) error {
		return r.client.AssignProjectVPCEndpoint(state.ProjectID.ValueString(), state.VPCEndpointID.ValueString(),
			neon.VPCEndpointAssignment{Label: state.Label.ValueString()})
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(state.VPCEndpointID.ValueString() + "/" + state.ProjectID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonVPCEndpointRestriction) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointRestrictionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var endpoints []neon.VPCEndpoint
	resp.Diagnostics.Append(projectReadiness.RetryWithFallback(func(_ context.Context) error {
		re, err := r.client.ListProjectVPCEndpoints(state.ProjectID.ValueString())
		endpoints = re.Endpoints
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	var found bool
	for _, endpoint := range endpoints {
		if state.VPCEndpointID.ValueString() == endpoint.VpcEndpointID {
			state.Label = types.StringValue(endpoint.Label)
			found = true
			break
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	if !found {
		resp.Diagnostics.AddWarning("VPC endpoint restriction not found",
			fmt.Sprintf("Restriction to the VPC %q was not found for the project %q.",
				state.VPCEndpointID.ValueString(), state.ProjectID.ValueString()))
		resp.State.RemoveResource(ctx)
	}
}

func (r neonVPCEndpointRestriction) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// reuse the Create method because the underlying API endpoint's command is idempotent,
	// i.e., used for creation and update
	request := resource.CreateRequest{
		Config: tfsdk.Config{
			Raw:    req.Plan.Raw,
			Schema: req.Plan.Schema,
		},
	}
	r.Create(ctx, request, &resource.CreateResponse{State: resp.State})
}

func (r neonVPCEndpointRestriction) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonVPCEndpointRestrictionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallback(func(_ context.Context) error {
		return r.client.DeleteProjectVPCEndpoint(state.ProjectID.ValueString(), state.VPCEndpointID.ValueString())
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound:            func(_ context.Context) error { return nil },
		http.StatusUnprocessableEntity: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r neonVPCEndpointRestriction) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 2)
	if len(els) != 2 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State VPC Restriction Assignment ID",
			"Expected an import ID in the form <vpc_id>/<project_id>.",
		)
		return
	}

	// seed the state using the import id string and read the remote resource
	request := resource.ReadRequest{}
	resp.Diagnostics.Append(request.State.Set(ctx, &neonVPCEndpointRestrictionResourceModel{
		ID:            types.StringValue(req.ID),
		VPCEndpointID: types.StringValue(els[0]),
		ProjectID:     types.StringValue(els[1]),
	})...)
	r.Read(ctx, request, &resource.ReadResponse{State: resp.State})
}
