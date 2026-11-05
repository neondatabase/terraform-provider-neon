package provider

import (
	"context"
	"fmt"
	"net/http"
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

var _ resource.Resource = (*neonProjectMemberRoleResource)(nil)
var _ resource.ResourceWithConfigure = (*neonProjectMemberRoleResource)(nil)
var _ resource.ResourceWithImportState = (*neonProjectMemberRoleResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonProjectMemberRoleResource)(nil)

type neonProjectMemberRoleResource struct {
	client *neon.Client
}

type neonProjectMemberRoleResourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	MemberID  types.String `tfsdk:"member_id"`
	Role      types.String `tfsdk:"role"`
}

func NewNeonProjectMemberRoleResource() resource.Resource {
	return &neonProjectMemberRoleResource{}
}

func (r *neonProjectMemberRoleResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_project_member_role"
}

func (r *neonProjectMemberRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: `Manages per-project permissions for an organization's member.
	Note that the Neon member assigned to the terraform process can self-demote and self-lockout itself. 
	Also, note that the org. admins may not have per-project permissions set to something other than admin.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "The project member role ID.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon project ID.",
			},
			"member_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The organization member ID.",
			},
			"role": schema.StringAttribute{
				Required:    true,
				Validators:  []validator.String{projectMemberRoleValidator{}},
				Description: "The member's explicit project role: `viewer`, `editor`, or `admin`.",
			},
		},
	}
}

type projectMemberRoleValidator struct{}

func (projectMemberRoleValidator) Description(context.Context) string {
	return "validates that the project member role is supported by the Neon API"
}

func (v projectMemberRoleValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (projectMemberRoleValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if _, err := neon.NewProjectRole(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid project member role", err.Error())
	}
}

func (r *neonProjectMemberRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonProjectMemberRoleResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var state neonProjectMemberRoleResourceModel
	switch {
	case !req.Plan.Raw.IsNull():
		resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	case !req.State.Raw.IsNull():
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	default:
		return
	}

	if resp.Diagnostics.HasError() {
		return
	}

	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	member, err := findMember(ctx, r.client, state.ProjectID.ValueString(), state.MemberID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("failed to find member", err.Error())
		return
	}

	// A role may not be granted on per-project basis to the members with org-level roles
	if member.GrantSource != nil && *member.GrantSource == neon.ProjectMemberGrantSourceOrgRoleDefault &&
		member.OrgDefaultProjectPermission != nil && *member.OrgDefaultProjectPermission == neon.ProjectPermissionLevelAdmin &&
		member.OrgRole == neon.ProjectMemberOrgRoleAdmin &&
		state.Role.ValueString() != neon.ProjectMemberOrgRoleAdmin.String() {
		resp.Diagnostics.AddError("cannot set project-level role",
			fmt.Sprintf("The member %q has the org-level role that is inherited by the project %q; change it to %q first.",
				state.MemberID.ValueString(), state.ProjectID.ValueString(), neon.ProjectMemberOrgRoleCollaborator.String()))
		return
	}
}

func (r *neonProjectMemberRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 2)
	if len(els) != 2 {
		resp.Diagnostics.AddError(
			"Invalid Project Member Role Import ID",
			"Expected an import ID in the form <project_id>/<member_id>.",
		)
		return
	}

	state := neonProjectMemberRoleResourceModel{
		ID:        types.StringValue(req.ID),
		ProjectID: types.StringValue(els[0]),
		MemberID:  types.StringValue(els[1]),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
}

func (r *neonProjectMemberRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.setRole(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *neonProjectMemberRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.setRole(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *neonProjectMemberRoleResource) setRole(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	if r.client == nil {
		diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var model neonProjectMemberRoleResourceModel
	diagnostics.Append(plan.Get(ctx, &model)...)
	if diagnostics.HasError() {
		return
	}

	role, err := neon.NewProjectRole(model.Role.ValueString())
	if err != nil {
		diagnostics.AddError("Invalid Project Member Role", err.Error())
		return
	}

	var selfDemotion = true
	diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		_, err := r.client.SetProjectMemberRole(
			model.ProjectID.ValueString(),
			model.MemberID.ValueString(),
			&selfDemotion,
			neon.SetProjectMemberRoleRequest{Role: role},
		)
		return err
	}, nil)...)
	if diagnostics.HasError() {
		return
	}

	model.ID = types.StringValue(fmt.Sprintf("%s/%s", model.ProjectID.ValueString(), model.MemberID.ValueString()))
	diagnostics.Append(state.Set(ctx, &model)...)
}

func (r *neonProjectMemberRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectMemberRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	member, err := findMember(ctx, r.client, state.ProjectID.ValueString(), state.MemberID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(err.Error(), "")
		return
	}

	if member.ProjectRole != nil {
		state.Role = types.StringValue(member.ProjectRole.String())
		state.ID = types.StringValue(fmt.Sprintf("%s/%s", state.ProjectID.ValueString(),
			state.MemberID.ValueString()))
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *neonProjectMemberRoleResource) Delete(ctx context.Context, req resource.DeleteRequest,
	resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectMemberRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	selfLockOut := true
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		_, err := r.client.RemoveProjectMemberRole(state.ProjectID.ValueString(), state.MemberID.ValueString(),
			&selfLockOut)
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func findMember(ctx context.Context, client *neon.Client, projectID string, memberID string) (neon.ProjectMember, error) {
	if client == nil {
		return neon.ProjectMember{}, fmt.Errorf("client is nil")
	}
	var member neon.ProjectMember
	err := projectReadiness.Do(ctx, func(ctx context.Context) error {
		var cursor *string
		for {
			resp, err := client.ListProjectMembers(projectID, cursor, nil)
			if err != nil {
				return err
			}
			for _, el := range resp.ProjectMembers {
				if memberID == el.MemberID {
					member = el
					return nil
				}
			}
			cursor = resp.Pagination.Next
			if cursor == nil {
				break
			}
		}
		return nil
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			return nil
		},
	})
	return member, err
}
