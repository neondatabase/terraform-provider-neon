package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.ResourceWithConfigure   = (*neonAuthResource)(nil)
	_ resource.ResourceWithImportState = (*neonAuthResource)(nil)
)

type neonAuthResource struct {
	client *neon.Client
}

type neonAuthResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	ProjectID             types.String `tfsdk:"project_id"`
	BranchID              types.String `tfsdk:"branch_id"`
	DatabaseName          types.String `tfsdk:"database_name"`
	AllowLocalhost        types.Bool   `tfsdk:"allow_localhost"`
	DeleteDataOnDestroy   types.Bool   `tfsdk:"delete_data_on_destroy"`
	AuthProvider          types.String `tfsdk:"auth_provider"`
	AuthProviderProjectID types.String `tfsdk:"auth_provider_project_id"`
	BaseURL               types.String `tfsdk:"base_url"`
	JWKSURL               types.String `tfsdk:"jwks_url"`
	OwnedBy               types.String `tfsdk:"owned_by"`
	CreatedAt             types.String `tfsdk:"created_at"`
}

func NewNeonAuthResource() resource.Resource {
	return &neonAuthResource{}
}

func (r *neonAuthResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_auth"
}

func (r *neonAuthResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:      true,
			Description:   description,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		Description: "Enables Neon Auth (Managed Better Auth) on a branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "`<project_id>/<branch_id>`.",
				PlanModifiers: idPlanModifiers(),
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				Description:   "Project ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				Description:   "Branch ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"database_name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Database that holds the `neon_auth` schema. Defaults to the project's default database.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"allow_localhost": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Whether sign-in redirects to localhost are allowed. Neon enables it by default.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"delete_data_on_destroy": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Whether destroying the resource also drops the `neon_auth` schema with its users and sessions. " +
					"Neon cannot enable Neon Auth again on a branch that still has the schema, so replacing this " +
					"resource on the same branch requires `true`.",
			},
			"auth_provider":            computed("Auth provider. Always `better_auth`."),
			"auth_provider_project_id": computed("Project ID assigned by the auth provider."),
			"base_url":                 computed("Base URL of the Neon Auth service, used as `NEON_AUTH_BASE_URL`."),
			"jwks_url":                 computed("URL of the JWKS endpoint that verifies the issued JWTs."),
			"owned_by":                 computed("Owner of the auth provider project: `neon` or `user`."),
			"created_at":               computed("When Neon Auth was enabled, in RFC 3339 format."),
		},
	}
}

func (r *neonAuthResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonAuthResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var plan neonAuthResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.EnableNeonAuthIntegrationRequest{AuthProvider: neon.NeonAuthSupportedAuthProviderBetterAuth}
	if !plan.DatabaseName.IsNull() && !plan.DatabaseName.IsUnknown() {
		cfg.DatabaseName = plan.DatabaseName.ValueStringPointer()
	}
	var created neon.NeonAuthCreateIntegrationResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		created, err = r.client.CreateNeonAuth(plan.ProjectID.ValueString(), plan.BranchID.ValueString(), cfg)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Neon Auth exists from here on; state records it before the follow-up calls that can fail.
	state := plan
	state.ID = types.StringValue(neonAuthID(plan.ProjectID.ValueString(), plan.BranchID.ValueString()))
	state.AuthProvider = types.StringValue(created.AuthProvider.String())
	state.AuthProviderProjectID = types.StringValue(created.AuthProviderProjectID)
	state.BaseURL = types.StringPointerValue(created.BaseURL)
	state.JWKSURL = types.StringValue(created.JwksURL)
	if state.DatabaseName.IsUnknown() {
		state.DatabaseName = types.StringNull()
	}
	if state.AllowLocalhost.IsUnknown() {
		state.AllowLocalhost = types.BoolNull()
	}
	state.OwnedBy = types.StringNull()
	state.CreatedAt = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.AllowLocalhost.IsNull() && !plan.AllowLocalhost.IsUnknown() {
		resp.Diagnostics.Append(r.setAllowLocalhost(ctx, plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	found, diags := r.read(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Neon Auth disappeared after it was enabled",
			fmt.Sprintf("Neon Auth on branch %s of project %s was not found right after it was enabled. Run Terraform again.",
				plan.BranchID.ValueString(), plan.ProjectID.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonAuthResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonAuthResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, diags := r.read(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddWarning("Neon Auth not enabled",
			fmt.Sprintf("Neon Auth is no longer enabled on branch %s of project %s; removing it from the state.",
				state.BranchID.ValueString(), state.ProjectID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonAuthResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var plan, state neonAuthResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.AllowLocalhost.IsNull() && !plan.AllowLocalhost.IsUnknown() && !plan.AllowLocalhost.Equal(state.AllowLocalhost) {
		resp.Diagnostics.Append(r.setAllowLocalhost(ctx, plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	state.DeleteDataOnDestroy = plan.DeleteDataOnDestroy
	found, diags := r.read(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Neon Auth not enabled",
			fmt.Sprintf("Neon Auth is no longer enabled on branch %s of project %s. Run Terraform again to enable it.",
				state.BranchID.ValueString(), state.ProjectID.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonAuthResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonAuthResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Disabling acts on whatever integration the branch has, so a different one is left alone.
	found, diags := r.read(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() || !found {
		return
	}

	deleteData := state.DeleteDataOnDestroy.ValueBool()
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		return r.client.DisableNeonAuth(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			&neon.DisableNeonAuthCfg{DeleteData: &deleteData})
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error { return nil },
	})...)
}

func (r *neonAuthResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projectID, branchID, ok := strings.Cut(req.ID, "/")
	if !ok || projectID == "" || branchID == "" || strings.Contains(branchID, "/") {
		resp.Diagnostics.AddError("Invalid Neon Auth Import ID", "Expected <project_id>/<branch_id>.")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	state := neonAuthResourceModel{
		ProjectID:           types.StringValue(projectID),
		BranchID:            types.StringValue(branchID),
		DeleteDataOnDestroy: types.BoolValue(false),
	}
	found, diags := r.read(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Neon Auth not enabled",
			fmt.Sprintf("Neon Auth is not enabled on branch %s of project %s.", branchID, projectID))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// read refreshes everything except the configuration-only delete_data_on_destroy. found is false when the branch
// has no Neon Auth.
func (r *neonAuthResource) read(ctx context.Context, model *neonAuthResourceModel) (found bool, diags diag.Diagnostics) {
	projectID, branchID := model.ProjectID.ValueString(), model.BranchID.ValueString()
	var integration neon.NeonAuthIntegration
	found = true
	diags.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		integration, err = r.client.GetNeonAuth(projectID, branchID)
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error {
			found = false
			return nil
		},
	})...)
	if diags.HasError() || !found {
		return found, diags
	}
	if provider := integration.AuthProvider.String(); provider != neon.NeonAuthSupportedAuthProviderBetterAuth.String() {
		diags.AddError("Unsupported Neon Auth provider",
			fmt.Sprintf("Branch %s of project %s uses the Neon Auth provider %q. neon_auth manages Managed Better Auth "+
				"(better_auth) only and leaves this integration unchanged.", branchID, projectID, provider))
		return found, diags
	}

	var localhost neon.NeonAuthAllowLocalhostResponse
	diags.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		localhost, err = r.client.GetNeonAuthAllowLocalhost(projectID, branchID)
		return err
	}, nil)...)
	if diags.HasError() {
		return found, diags
	}

	model.ID = types.StringValue(neonAuthID(projectID, branchID))
	model.DatabaseName = types.StringValue(integration.DbName)
	model.AllowLocalhost = types.BoolValue(localhost.AllowLocalhost)
	model.AuthProvider = types.StringValue(integration.AuthProvider.String())
	model.AuthProviderProjectID = types.StringValue(integration.AuthProviderProjectID)
	model.BaseURL = types.StringPointerValue(integration.BaseURL)
	model.JWKSURL = types.StringValue(integration.JwksURL)
	model.OwnedBy = types.StringValue(integration.OwnedBy.String())
	model.CreatedAt = types.StringValue(integration.CreatedAt.UTC().Format(time.RFC3339))
	return found, diags
}

func (r *neonAuthResource) setAllowLocalhost(ctx context.Context, plan neonAuthResourceModel) diag.Diagnostics {
	return projectReadiness.Retry(ctx, func(context.Context) error {
		_, err := r.client.UpdateNeonAuthAllowLocalhost(plan.ProjectID.ValueString(), plan.BranchID.ValueString(),
			neon.UpdateNeonAuthAllowLocalhostRequest{AllowLocalhost: plan.AllowLocalhost.ValueBool()})
		return err
	}, nil)
}

func neonAuthID(projectID, branchID string) string {
	return projectID + "/" + branchID
}
