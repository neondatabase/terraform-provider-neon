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

var _ resource.ResourceWithConfigure = (*neonProjectPermission)(nil)
var _ resource.ResourceWithUpgradeState = (*neonProjectPermission)(nil)
var _ resource.ResourceWithImportState = (*neonProjectPermission)(nil)

type neonProjectPermission struct {
	client *neon.Client
}

type neonProjectPermissionResourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	Grantee   types.String `tfsdk:"grantee"`
}

func NewNeonProjectPermissionResource() resource.Resource {
	return &neonProjectPermission{}
}

func (r *neonProjectPermission) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonProjectPermission) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_project_permission"
}

func (r *neonProjectPermission) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Version:     2,
		Description: `Project's access permission.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "The permission ID.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Project ID.",
			},
			"grantee": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Email of the user whom to grant project permission.",
			},
		},
	}
}

func (r *neonProjectPermission) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectPermissionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.GrantPermissionToProjectRequest{
		Email: state.Grantee.ValueString(),
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		re, err := r.client.GrantPermissionToProject(state.ProjectID.ValueString(), cfg)
		if err != nil {
			return err
		}
		state.ID = types.StringValue(re.ID)
		return nil
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonProjectPermission) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectPermissionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var permissions []neon.ProjectPermission
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		re, err := r.client.ListProjectPermissions(state.ProjectID.ValueString())
		permissions = re.ProjectPermissions
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	for _, permission := range permissions {
		if state.Grantee.ValueString() == permission.GrantedToEmail {
			state.ID = types.StringValue(permission.ID)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.Diagnostics.AddWarning("Permission Not Found",
		fmt.Sprintf("Permission for email %q not found in the project %q", state.Grantee.ValueString(),
			state.ProjectID.ValueString()))
	resp.State.RemoveResource(ctx)
}

func (r *neonProjectPermission) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

func (r *neonProjectPermission) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectPermissionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		_, err := r.client.RevokePermissionFromProject(state.ProjectID.ValueString(), state.ID.ValueString())
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *neonProjectPermission) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 2)
	if len(els) != 2 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State Project Permission ID",
			"Expected an import ID in the form <project_id>/<permission_id>.",
		)
		return
	}

	projectID := els[0]
	permissionID := els[1]
	var permissions []neon.ProjectPermission
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		re, err := r.client.ListProjectPermissions(projectID)
		permissions = re.ProjectPermissions
		return err
	}, nil)...)

	if resp.Diagnostics.HasError() {
		return
	}

	for _, permission := range permissions {
		if permissionID == permission.ID {
			resp.Diagnostics.Append(resp.State.Set(ctx, &neonProjectPermissionResourceModel{
				ID:        types.StringValue(permissionID),
				ProjectID: types.StringValue(projectID),
				Grantee:   types.StringValue(permission.GrantedToEmail),
			})...)
			return
		}
	}

	resp.Diagnostics.AddError("Permission Not Found",
		fmt.Sprintf("Permission %q not found in the project %q", permissionID, projectID))
}

// UpgradeState migrates state written by kislerdm/neon v0.18.0, the last Terraform Plugin SDK v2 release.
func (r *neonProjectPermission) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		1: legacyStateUpgrader(r.Schema, nil),
	}
}
