package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithImportState = (*branchAuthResource)(nil)
var _ resource.ResourceWithConfigure = (*branchAuthResource)(nil)

type branchAuthResource struct{ client *neonClient }
type branchAuthModel struct {
	ID                    types.String `tfsdk:"id"`
	ProjectID             types.String `tfsdk:"project_id"`
	BranchID              types.String `tfsdk:"branch_id"`
	DatabaseName          types.String `tfsdk:"database_name"`
	AuthProvider          types.String `tfsdk:"auth_provider"`
	AuthProviderProjectID types.String `tfsdk:"auth_provider_project_id"`
	BaseURL               types.String `tfsdk:"base_url"`
	JWKSURL               types.String `tfsdk:"jwks_url"`
	OwnedBy               types.String `tfsdk:"owned_by"`
	CreatedAt             types.String `tfsdk:"created_at"`
}

func NewBranchAuthResource() resource.Resource { return &branchAuthResource{} }
func (r *branchAuthResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_branch_auth"
}
func (r *branchAuthResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureBackend(req, resp)
}
func (r *branchAuthResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Enables Neon-managed Better Auth on a branch/database. Neon supports one integration per branch. Destroy disables the integration and preserves the neon_auth schema and user data (delete_data=false). User/OAuth/email configuration is outside this resource's scope.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:    true,
			Description: "Composite ID: <project_id>/<branch_id>/<database_name>.",
		},
		"project_id":    backendIdentityAttribute("Neon project ID."),
		"branch_id":     backendIdentityAttribute("Neon branch ID. Only one Auth integration can exist per branch."),
		"database_name": backendIdentityAttribute("Database used for managed Better Auth. Reference neon_database.app.name for dependency ordering."),
		"auth_provider": schema.StringAttribute{
			Computed:    true,
			Description: "Always better_auth for integrations managed by this resource.",
		},
		"auth_provider_project_id": schema.StringAttribute{
			Computed:    true,
			Description: "Auth provider's integration identifier.",
		},
		"base_url": schema.StringAttribute{
			Computed:    true,
			Description: "Neon Auth service URL.",
		},
		"jwks_url": schema.StringAttribute{
			Computed:    true,
			Description: "JWKS endpoint for verifying auth JWTs.",
		},
		"owned_by": schema.StringAttribute{
			Computed:    true,
			Description: "neon for a managed integration.",
		},
		"created_at": schema.StringAttribute{
			Computed:    true,
			Description: "Integration creation timestamp.",
		},
	}}
}

func validateManagedAuth(auth neon.NeonAuthIntegration) error {
	if auth.AuthProvider != neon.NeonAuthSupportedAuthProviderBetterAuth || auth.OwnedBy != neon.NeonAuthProviderProjectOwnedByNeon {
		return fmt.Errorf("expected Neon-managed Better Auth, got provider %q owned by %q", auth.AuthProvider.String(), auth.OwnedBy.String())
	}
	if auth.DbName == "" || auth.BranchID == "" || auth.AuthProviderProjectID == "" {
		return fmt.Errorf("incomplete Neon Auth integration response")
	}
	return nil
}

func (r *branchAuthResource) get(ctx context.Context, state branchAuthModel) (neon.NeonAuthIntegration, error) {
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err != nil {
		return neon.NeonAuthIntegration{}, err
	}
	auth, err := client.GetNeonAuth(url.PathEscape(state.ProjectID.ValueString()), url.PathEscape(state.BranchID.ValueString()))
	if err == nil {
		err = validateManagedAuth(auth)
	}
	if err == nil && auth.BranchID != state.BranchID.ValueString() {
		err = fmt.Errorf("neon returned Auth for a different branch")
	}
	return auth, err
}

func setBranchAuth(model *branchAuthModel, auth neon.NeonAuthIntegration) {
	model.DatabaseName = types.StringValue(auth.DbName)
	model.ID = types.StringValue(strings.Join([]string{model.ProjectID.ValueString(), model.BranchID.ValueString(), auth.DbName}, "/"))
	model.AuthProvider = types.StringValue(auth.AuthProvider.String())
	model.AuthProviderProjectID = types.StringValue(auth.AuthProviderProjectID)
	model.BaseURL = types.StringPointerValue(auth.BaseURL)
	model.JWKSURL = types.StringValue(auth.JwksURL)
	model.OwnedBy = types.StringValue(auth.OwnedBy.String())
	model.CreatedAt = types.StringValue(auth.CreatedAt.UTC().Format(time.RFC3339))
}

func (r *branchAuthResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan branchAuthModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err := r.get(ctx, plan)
	if err == nil {
		resp.Diagnostics.AddError("Branch Auth Already Exists", "Import the existing integration. Neon supports one Auth integration per branch.")
		return
	}
	if !apiNotFound(err) {
		resp.Diagnostics.AddError("Unable to Check Branch Auth", err.Error())
		return
	}
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure Neon Client", err.Error())
		return
	}
	db := plan.DatabaseName.ValueString()
	created, err := client.CreateNeonAuth(url.PathEscape(plan.ProjectID.ValueString()), url.PathEscape(plan.BranchID.ValueString()), neon.EnableNeonAuthIntegrationRequest{AuthProvider: neon.NeonAuthSupportedAuthProviderBetterAuth, DatabaseName: &db})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Enable Branch Auth", err.Error())
		return
	}
	// Record the accepted integration before a follow-up GET can fail. Do not
	// persist secret_server_key: the minimal resource does not manage secrets.
	plan.ID = types.StringValue(strings.Join([]string{plan.ProjectID.ValueString(), plan.BranchID.ValueString(), db}, "/"))
	plan.AuthProvider = types.StringValue("better_auth")
	plan.AuthProviderProjectID = types.StringValue(created.AuthProviderProjectID)
	plan.BaseURL = types.StringPointerValue(created.BaseURL)
	plan.JWKSURL = types.StringValue(created.JwksURL)
	plan.OwnedBy = types.StringValue("neon")
	plan.CreatedAt = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	auth, err := r.get(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Enabled Branch Auth", err.Error())
		return
	}
	if auth.DbName != db {
		resp.Diagnostics.AddError("Unexpected Auth Database", "Neon enabled Auth on a different database than requested.")
		return
	}
	setBranchAuth(&plan, auth)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *branchAuthResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state branchAuthModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	auth, err := r.get(ctx, state)
	if apiNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Branch Auth", err.Error())
		return
	}
	if auth.DbName != state.DatabaseName.ValueString() {
		resp.Diagnostics.AddError("Branch Auth Database Changed", fmt.Sprintf("This resource manages database %q, but the branch integration now uses %q. Remove the old resource from state and import the intended integration before managing it.", state.DatabaseName.ValueString(), auth.DbName))
		return
	}
	setBranchAuth(&state, auth)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *branchAuthResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected Branch Auth Update", "Changing project, branch or database requires replacement.")
}

func (r *branchAuthResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state branchAuthModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	auth, err := r.get(ctx, state)
	if apiNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Check Branch Auth Before Disable", err.Error())
		return
	}
	if auth.DbName != state.DatabaseName.ValueString() || (!state.AuthProviderProjectID.IsNull() && auth.AuthProviderProjectID != state.AuthProviderProjectID.ValueString()) {
		resp.Diagnostics.AddError("Branch Auth Integration Changed", "The remote integration no longer matches this resource. Refresh/import the intended integration before destroying it.")
		return
	}
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	deleteData := false
	if err == nil {
		err = client.DisableNeonAuth(url.PathEscape(state.ProjectID.ValueString()), url.PathEscape(state.BranchID.ValueString()), &neon.DisableNeonAuthCfg{DeleteData: &deleteData})
	}
	if err != nil && !apiNotFound(err) {
		resp.Diagnostics.AddError("Unable to Disable Branch Auth", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *branchAuthResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitBackendID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Branch Auth Import ID", err.Error())
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	state := branchAuthModel{ProjectID: types.StringValue(parts[0]), BranchID: types.StringValue(parts[1]), DatabaseName: types.StringValue(parts[2])}
	auth, err := r.get(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Import Branch Auth", err.Error())
		return
	}
	if auth.DbName != parts[2] {
		resp.Diagnostics.AddError("Wrong Auth Import Database", fmt.Sprintf("Branch Auth uses database %q, not %q.", auth.DbName, parts[2]))
		return
	}
	setBranchAuth(&state, auth)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
