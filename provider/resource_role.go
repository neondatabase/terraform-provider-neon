package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	schemaTFSDK "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	typesTFSDK "github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonRole)(nil)
var _ resource.ResourceWithUpgradeState = (*neonRole)(nil)
var _ resource.ResourceWithImportState = (*neonRole)(nil)

type neonRole struct {
	client *neon.Client
}

type neonRoleResourceModel struct {
	ID        typesTFSDK.String `tfsdk:"id"`
	ProjectID typesTFSDK.String `tfsdk:"project_id"`
	BranchID  typesTFSDK.String `tfsdk:"branch_id"`
	Name      typesTFSDK.String `tfsdk:"name"`
	Protected typesTFSDK.Bool   `tfsdk:"protected"`
	// 	TODO: add no_login support
}

func (m *neonRoleResourceModel) inferAttr(role neon.Role) {
	m.ID = typesTFSDK.StringValue(m.ProjectID.ValueString() + "/" + role.BranchID + "/" + role.Name)
	m.Name = typesTFSDK.StringValue(role.Name)
	m.Protected = typesTFSDK.BoolPointerValue(role.Protected)
}

func NewNeonRoleResource() resource.Resource {
	return &neonRole{}
}

func (r *neonRole) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r neonRole) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_role"
}

func (r neonRole) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schemaTFSDK.Schema{
		Version: 8,
		Description: `Project Role. **Note** that User and Role are synonymous terms in Neon. 
See details: https://neon.tech/docs/manage/users/
`,
		Attributes: map[string]schemaTFSDK.Attribute{
			"id": schemaTFSDK.StringAttribute{
				Computed:    true,
				Description: "The role ID.",
			},
			"project_id": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Project ID.",
			},
			"branch_id": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Branch ID.",
			},
			"name": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Role name.",
			},
			"protected": schemaTFSDK.BoolAttribute{
				Computed:    true,
				Description: "Indicates if the role is protected.",
			},
		},
	}
}

func (r neonRole) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonRoleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.RoleCreateRequest{
		Role: neon.RoleCreateRequestRole{
			Name: state.Name.ValueString(),
		},
	}

	var role neon.Role
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		re, err := r.client.CreateProjectBranchRole(state.ProjectID.ValueString(), state.BranchID.ValueString(), cfg)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations)
		role = re.RoleResponse.Role
		return nil
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.inferAttr(role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonRole) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var role neon.Role
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		roleResp, err := r.client.GetProjectBranchRole(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Name.ValueString())
		role = roleResp.Role
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)

	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if role.Name == "" {
		resp.Diagnostics.AddWarning("role not found",
			fmt.Sprintf("role %q not found in the project/branch \"%s/%s\"", state.Name.ValueString(),
				state.ProjectID.ValueString(), state.BranchID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(role)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonRole) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {}

func (r neonRole) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		re, err := r.client.DeleteProjectBranchRole(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Name.ValueString())
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations)
		return nil
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)

	resp.State.RemoveResource(ctx)
}

func (r neonRole) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 3)
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State Role ID",
			"Expected an import ID in the form <project_id>/<neon_branch_id>/<role_name>.",
		)
		return
	}

	if !isValidBranchID(els[1]) {
		resp.Diagnostics.AddError("provided Neon Branch ID is not valid", "")
		return
	}

	state := neonRoleResourceModel{
		ProjectID: typesTFSDK.StringValue(els[0]),
		BranchID:  typesTFSDK.StringValue(els[1]),
		Name:      typesTFSDK.StringValue(els[2]),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if resp.State.Raw.IsNull() {
		resp.Diagnostics.AddWarning("role not found",
			fmt.Sprintf("role %q not found in the project/branch \"%s/%s\"", els[2], els[0], els[1]))
		return
	}
}

// UpgradeState migrates state written by kislerdm/neon v0.18.0, the last Terraform Plugin SDK v2 release.
func (r neonRole) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		7: legacyStateUpgrader(r.Schema, nil),
	}
}
