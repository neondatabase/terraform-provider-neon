package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.Resource = (*neonOrgMemberRoleResource)(nil)
var _ resource.ResourceWithConfigure = (*neonOrgMemberRoleResource)(nil)
var _ resource.ResourceWithImportState = (*neonOrgMemberRoleResource)(nil)

type neonOrgMemberRoleResource struct {
	client *neon.Client
}

type neonOrgMemberRoleResourceModel struct {
	ID       types.String `tfsdk:"id"`
	OrgID    types.String `tfsdk:"org_id"`
	MemberID types.String `tfsdk:"member_id"`
	Role     types.String `tfsdk:"role"`
}

func NewNeonOrgMemberRoleResource() resource.Resource {
	return &neonOrgMemberRoleResource{}
}

func (r *neonOrgMemberRoleResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_org_member_role"
}

func (r *neonOrgMemberRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: `Manages permissions of an organization's member.

	Note that the deletion will only delete the resource from the tf state.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "The org. member role ID.",
			},
			"org_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The organisation ID.",
			},
			"member_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The organization member ID.",
			},
			"role": schema.StringAttribute{
				Required:    true,
				Validators:  []validator.String{orgMemberRoleValidator{}},
				Description: "The member's explicit organization role: `viewer`, `editor`, `admin`, or `collaborator`.",
			},
		},
	}
}

type orgMemberRoleValidator struct{}

func (orgMemberRoleValidator) Description(context.Context) string {
	return "validates that the organization member role is supported by the Neon API"
}

func (v orgMemberRoleValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (orgMemberRoleValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if _, err := neon.NewMemberRole(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid organization member role", err.Error())
	}
}

func (r *neonOrgMemberRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	r.client = client.sdk
}

func (r *neonOrgMemberRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 2)
	if len(els) != 2 {
		resp.Diagnostics.AddError(
			"Invalid Organization Member Role Import ID",
			"Expected an import ID in the form <org_id>/<member_id>.",
		)
		return
	}

	state := neonOrgMemberRoleResourceModel{
		ID:       types.StringValue(req.ID),
		OrgID:    types.StringValue(els[0]),
		MemberID: types.StringValue(els[1]),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
}

func (r *neonOrgMemberRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.setRole(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *neonOrgMemberRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.setRole(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *neonOrgMemberRoleResource) setRole(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	if r.client == nil {
		diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var model neonOrgMemberRoleResourceModel
	diagnostics.Append(plan.Get(ctx, &model)...)
	if diagnostics.HasError() {
		return
	}

	role, err := neon.NewMemberRole(model.Role.ValueString())
	if err != nil {
		diagnostics.AddError("Invalid Org. Member Role", err.Error())
		return
	}

	diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		_, err := r.client.UpdateOrganizationMember(
			model.OrgID.ValueString(),
			model.MemberID.ValueString(),
			neon.OrganizationMemberUpdateRequest{
				Role: role,
			},
		)
		return err
	}, nil)...)
	if diagnostics.HasError() {
		return
	}

	model.ID = types.StringValue(fmt.Sprintf("%s/%s", model.OrgID.ValueString(), model.MemberID.ValueString()))
	diagnostics.Append(state.Set(ctx, &model)...)
}

func (r *neonOrgMemberRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonOrgMemberRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var member neon.Member
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		var err error
		member, err = r.client.GetOrganizationMember(state.OrgID.ValueString(), state.MemberID.ValueString())
		return err
	}, nil)...)

	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Role = types.StringValue(member.Role.String())
	state.ID = types.StringValue(fmt.Sprintf("%s/%s", state.OrgID.ValueString(), state.MemberID.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonOrgMemberRoleResource) Delete(ctx context.Context, _ resource.DeleteRequest,
	resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Org. member's role is only deleted from the tf state",
		"Delete the org. member if you intend to revoke their access.")
	resp.State.RemoveResource(ctx)
}
