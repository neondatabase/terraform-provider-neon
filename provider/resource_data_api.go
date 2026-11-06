package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.Resource                = (*neonDataAPIResource)(nil)
	_ resource.ResourceWithConfigure   = (*neonDataAPIResource)(nil)
	_ resource.ResourceWithImportState = (*neonDataAPIResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*neonDataAPIResource)(nil)
)

type neonDataAPIResource struct{ client *neon.Client }

type neonDataAPIModel struct {
	ID               types.String `tfsdk:"id"`
	ProjectID        types.String `tfsdk:"project_id"`
	BranchID         types.String `tfsdk:"branch_id"`
	DatabaseName     types.String `tfsdk:"database_name"`
	URL              types.String `tfsdk:"url"`
	AuthProvider     types.String `tfsdk:"auth_provider"`
	JWKSURL          types.String `tfsdk:"jwks_url"`
	ProviderName     types.String `tfsdk:"provider_name"`
	JWTAudience      types.String `tfsdk:"jwt_audience"`
	AddDefaultGrants types.Bool   `tfsdk:"add_default_grants"`
	SkipAuthSchema   types.Bool   `tfsdk:"skip_auth_schema"`
	Settings         types.Object `tfsdk:"settings"`
}

type neonDataAPISettingsModel struct {
	DBAggregatesEnabled      types.Bool   `tfsdk:"db_aggregates_enabled"`
	DBAnonRole               types.String `tfsdk:"db_anon_role"`
	DBExtraSearchPath        types.String `tfsdk:"db_extra_search_path"`
	DBMaxRows                types.Int64  `tfsdk:"db_max_rows"`
	DBSchemas                types.List   `tfsdk:"db_schemas"`
	JWTCacheMaxLifetime      types.Int64  `tfsdk:"jwt_cache_max_lifetime"`
	JWTRoleClaimKey          types.String `tfsdk:"jwt_role_claim_key"`
	OpenAPIMode              types.String `tfsdk:"openapi_mode"`
	ServerCORSAllowedOrigins types.String `tfsdk:"server_cors_allowed_origins"`
	ServerTimingEnabled      types.Bool   `tfsdk:"server_timing_enabled"`
}

// NewNeonDataAPIResource manages a branch database's Neon Data API endpoint.
func NewNeonDataAPIResource() resource.Resource { return &neonDataAPIResource{} }

func (r *neonDataAPIResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_data_api"
}

func (r *neonDataAPIResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replacementString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	replacementBool := []planmodifier.Bool{boolplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages a Neon Data API endpoint for a branch database. Authentication options apply only during creation; changes require replacement. Deletion does not revoke database grants or remove authentication schema objects created during setup.",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Computed: true, PlanModifiers: idPlanModifiers(), Description: "Resource identity and import ID: <project_id>/<branch_id>/<database_name>."},
			"project_id":         schema.StringAttribute{Required: true, PlanModifiers: replacementString, Description: "The Neon project ID. Changes require replacement."},
			"branch_id":          schema.StringAttribute{Required: true, PlanModifiers: replacementString, Description: "The Neon branch ID. Changes require replacement."},
			"database_name":      schema.StringAttribute{Required: true, PlanModifiers: replacementString, Description: "The database served by the Data API. Changes require replacement."},
			"url":                schema.StringAttribute{Computed: true, Description: "The Data API endpoint URL used by applications."},
			"auth_provider":      schema.StringAttribute{Optional: true, PlanModifiers: replacementString, Description: "Authentication provider: neon_auth or external. The API validates authentication configuration. Changes require replacement; GET cannot discover this value on import or detect external changes."},
			"jwks_url":           schema.StringAttribute{Optional: true, PlanModifiers: replacementString, Description: "JWKS endpoint for external JWT verification. Changes require replacement."},
			"provider_name":      schema.StringAttribute{Optional: true, PlanModifiers: replacementString, Description: "Authentication provider display name. Changes require replacement."},
			"jwt_audience":       schema.StringAttribute{Optional: true, PlanModifiers: replacementString, Description: "Expected JWT audience. Omit to skip audience validation. Changes require replacement."},
			"add_default_grants": schema.BoolAttribute{Optional: true, PlanModifiers: replacementBool, Description: "Grant authenticated users permissions on public-schema tables during creation. Changes require replacement; deletion does not revoke these grants."},
			"skip_auth_schema":   schema.BoolAttribute{Optional: true, PlanModifiers: replacementBool, Description: "Skip auth schema and RLS function creation. Changes require replacement."},
			"settings": schema.SingleNestedAttribute{Optional: true, Description: "Mutable Data API settings. Only configured members are managed. Removing a managed member requires replacement because the SDK has no reset operation. Some backends return no settings, preventing remote drift detection.", Attributes: map[string]schema.Attribute{
				"db_aggregates_enabled":       schema.BoolAttribute{Optional: true, Description: "Enable aggregate queries."},
				"db_anon_role":                schema.StringAttribute{Optional: true, Description: "Database role for anonymous requests."},
				"db_extra_search_path":        schema.StringAttribute{Optional: true, Description: "Extra schemas added to the search path."},
				"db_max_rows":                 schema.Int64Attribute{Optional: true, Description: "Maximum rows returned in one response. Omit to leave the server setting unmanaged."},
				"db_schemas":                  schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Schemas exposed through the API. The pinned SDK cannot send an explicit empty list; use a non-empty list or omit this member."},
				"jwt_cache_max_lifetime":      schema.Int64Attribute{Optional: true, Description: "Maximum JWT cache lifetime in seconds."},
				"jwt_role_claim_key":          schema.StringAttribute{Optional: true, Description: "JWT claim path used to extract the database role."},
				"openapi_mode":                schema.StringAttribute{Optional: true, Description: "OpenAPI specification mode, such as disabled or ignore-privileges. The API validates this value."},
				"server_cors_allowed_origins": schema.StringAttribute{Optional: true, Description: "Allowed CORS origins."},
				"server_timing_enabled":       schema.BoolAttribute{Optional: true, Description: "Add Server-Timing response headers."},
			}},
		},
	}
}

func (r *neonDataAPIResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter for the Neon Data API resource.")
		return
	}
	if adapter == nil || adapter.sdk == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	r.client = adapter.sdk
}

func (r *neonDataAPIResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var prior, configured neonDataAPIModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &configured)...)
	if resp.Diagnostics.HasError() || prior.Settings.IsNull() || prior.Settings.IsUnknown() || configured.Settings.IsUnknown() {
		return
	}
	if configured.Settings.IsNull() {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("settings"))
		return
	}
	configuredAttributes := configured.Settings.Attributes()
	for name, value := range prior.Settings.Attributes() {
		next := configuredAttributes[name]
		if !value.IsNull() && !value.IsUnknown() && next.IsNull() {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("settings").AtName(name))
		}
	}
}

func (r *neonDataAPIResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid Data API Import ID", "Expected <project_id>/<branch_id>/<database_name> with three non-empty components.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database_name"), parts[2])...)
}

func (r *neonDataAPIResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var plan neonDataAPIModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	settings := plan.sdkSettings(ctx, &resp.Diagnostics)
	cfg := neon.DataAPICreateRequest{JwksURL: plan.JWKSURL.ValueStringPointer(), ProviderName: plan.ProviderName.ValueStringPointer(), JwtAudience: plan.JWTAudience.ValueStringPointer(), AddDefaultGrants: plan.AddDefaultGrants.ValueBoolPointer(), SkipAuthSchema: plan.SkipAuthSchema.ValueBoolPointer(), Settings: settings}
	if !plan.AuthProvider.IsNull() {
		provider, err := neon.NewDataAPICreateRequestAuthProvider(plan.AuthProvider.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Encode Data API Authentication Provider", err.Error())
		} else {
			cfg.AuthProvider = &provider
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	var result neon.DataAPICreateResponse
	projectID, branchID, databaseName := plan.sdkPathIDs()
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		result, err = r.client.CreateProjectBranchDataAPI(projectID, branchID, databaseName, &cfg)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", plan.ProjectID.ValueString(), plan.BranchID.ValueString(), plan.DatabaseName.ValueString()))
	plan.URL = types.StringValue(result.URL)
	// Keep identity when POST succeeds but the follow-up GET fails.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current, _ := r.get(ctx, &plan, &resp.Diagnostics, false)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.URL = types.StringValue(current.URL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonDataAPIResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var state neonDataAPIModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	result, found := r.get(ctx, &state, &resp.Diagnostics, true)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	state.URL = types.StringValue(result.URL)
	state.refreshSettings(ctx, result.Settings, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonDataAPIResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var plan, prior neonDataAPIModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg := neon.DataAPIUpdateRequest{Settings: plan.sdkSettings(ctx, &resp.Diagnostics)}
	if resp.Diagnostics.HasError() {
		return
	}
	projectID, branchID, databaseName := plan.sdkPathIDs()
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		_, err := r.client.UpdateProjectBranchDataAPI(projectID, branchID, databaseName, &cfg)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = prior.ID
	plan.URL = prior.URL
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	result, _ := r.get(ctx, &plan, &resp.Diagnostics, false)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.URL = types.StringValue(result.URL)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonDataAPIResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var state neonDataAPIModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	projectID, branchID, databaseName := state.sdkPathIDs()
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		_, err := r.client.DeleteProjectBranchDataAPI(projectID, branchID, databaseName)
		return err
	}, map[int]func(context.Context) error{http.StatusNotFound: func(context.Context) error { return nil }})...)
}

func (r *neonDataAPIResource) configured(diagnostics *diag.Diagnostics) bool {
	if r.client == nil {
		diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return false
	}
	return true
}

func (r *neonDataAPIResource) get(ctx context.Context, model *neonDataAPIModel, diagnostics *diag.Diagnostics, tolerateNotFound bool) (neon.DataAPIReponse, bool) {
	var result neon.DataAPIReponse
	found := true
	projectID, branchID, databaseName := model.sdkPathIDs()
	call := func(context.Context) error {
		var err error
		result, err = r.client.GetProjectBranchDataAPI(projectID, branchID, databaseName)
		return err
	}
	var fallbacks map[int]func(context.Context) error
	if tolerateNotFound {
		fallbacks = map[int]func(context.Context) error{
			http.StatusNotFound: func(context.Context) error {
				found = false
				return nil
			},
		}
	}
	diagnostics.Append(projectReadiness.Retry(ctx, call, fallbacks)...)
	return result, found
}

func (m *neonDataAPIModel) sdkPathIDs() (string, string, string) {
	// The pinned SDK concatenates path parameters without escaping reserved characters.
	return url.PathEscape(m.ProjectID.ValueString()), url.PathEscape(m.BranchID.ValueString()), url.PathEscape(m.DatabaseName.ValueString())
}

func (m *neonDataAPIModel) sdkSettings(ctx context.Context, diagnostics *diag.Diagnostics) *neon.DataAPISettings {
	if m.Settings.IsNull() {
		return nil
	}
	var model neonDataAPISettingsModel
	diagnostics.Append(m.Settings.As(ctx, &model, basetypes.ObjectAsOptions{})...)
	if diagnostics.HasError() {
		return nil
	}
	result := neon.DataAPISettings{
		DbAggregatesEnabled: model.DBAggregatesEnabled.ValueBoolPointer(), DbAnonRole: model.DBAnonRole.ValueStringPointer(), DbExtraSearchPath: model.DBExtraSearchPath.ValueStringPointer(),
		JwtRoleClaimKey: model.JWTRoleClaimKey.ValueStringPointer(), OpenapiMode: model.OpenAPIMode.ValueStringPointer(), ServerCorsAllowedOrigins: model.ServerCORSAllowedOrigins.ValueStringPointer(), ServerTimingEnabled: model.ServerTimingEnabled.ValueBoolPointer(),
	}
	if !model.DBMaxRows.IsNull() {
		value := int(model.DBMaxRows.ValueInt64())
		result.DbMaxRows = &value
	}
	if !model.JWTCacheMaxLifetime.IsNull() {
		value := int(model.JWTCacheMaxLifetime.ValueInt64())
		result.JwtCacheMaxLifetime = &value
	}
	if !model.DBSchemas.IsNull() {
		diagnostics.Append(model.DBSchemas.ElementsAs(ctx, &result.DbSchemas, false)...)
		if len(result.DbSchemas) == 0 && !diagnostics.HasError() {
			diagnostics.AddAttributeError(path.Root("settings").AtName("db_schemas"), "Unable to Encode Empty Data API Schema List", "The pinned Neon SDK cannot encode an explicit empty db_schemas list. Use a non-empty list or omit db_schemas; the provider will not silently omit a configured empty list.")
		}
	}
	return &result
}

func (m *neonDataAPIModel) refreshSettings(ctx context.Context, remote *neon.DataAPIReponseSettings, diagnostics *diag.Diagnostics) {
	if m.Settings.IsNull() || m.Settings.IsUnknown() {
		return
	}
	current := m.Settings.Attributes()
	observed := map[string]attr.Value{}
	if remote != nil {
		observed = map[string]attr.Value{
			"db_aggregates_enabled": types.BoolPointerValue(remote.DbAggregatesEnabled), "db_anon_role": types.StringPointerValue(remote.DbAnonRole), "db_extra_search_path": types.StringPointerValue(remote.DbExtraSearchPath),
			"jwt_role_claim_key": types.StringPointerValue(remote.JwtRoleClaimKey), "openapi_mode": types.StringPointerValue(remote.OpenapiMode), "server_cors_allowed_origins": types.StringPointerValue(remote.ServerCorsAllowedOrigins), "server_timing_enabled": types.BoolPointerValue(remote.ServerTimingEnabled),
		}
		if remote.DbMaxRows != nil {
			observed["db_max_rows"] = types.Int64Value(int64(*remote.DbMaxRows))
		}
		if remote.JwtCacheMaxLifetime != nil {
			observed["jwt_cache_max_lifetime"] = types.Int64Value(int64(*remote.JwtCacheMaxLifetime))
		}
		if remote.DbSchemas != nil {
			value, diags := types.ListValueFrom(ctx, types.StringType, remote.DbSchemas)
			diagnostics.Append(diags...)
			observed["db_schemas"] = value
		}
	}
	var unavailable []string
	for name, value := range current {
		if value.IsNull() || value.IsUnknown() {
			continue
		}
		returned, ok := observed[name]
		if !ok || returned.IsNull() {
			unavailable = append(unavailable, name)
			continue
		}
		current[name] = returned
	}
	if len(unavailable) > 0 {
		diagnostics.AddWarning("Data API Settings Not Observable", "The API did not return some configured settings. The provider retains last applied values and cannot detect remote drift for these settings: "+strings.Join(unavailable, ", ")+".")
	}
	value, diags := types.ObjectValue(m.Settings.AttributeTypes(ctx), current)
	diagnostics.Append(diags...)
	m.Settings = value
}
