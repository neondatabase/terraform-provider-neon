package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.ResourceWithConfigure      = (*neonAuthOAuthProviderResource)(nil)
	_ resource.ResourceWithImportState    = (*neonAuthOAuthProviderResource)(nil)
	_ resource.ResourceWithValidateConfig = (*neonAuthOAuthProviderResource)(nil)
)

// The OAuth provider Neon creates when Neon Auth is enabled uses Neon's development keys.
const sharedOAuthProviderType = "shared"

type neonAuthOAuthProviderResource struct {
	client *neon.Client
}

type neonAuthOAuthProviderResourceModel struct {
	ID                types.String `tfsdk:"id"`
	ProjectID         types.String `tfsdk:"project_id"`
	BranchID          types.String `tfsdk:"branch_id"`
	ProviderID        types.String `tfsdk:"provider_id"`
	ClientID          types.String `tfsdk:"client_id"`
	ClientSecret      types.String `tfsdk:"client_secret"`
	MicrosoftTenantID types.String `tfsdk:"microsoft_tenant_id"`
	AppleKeyID        types.String `tfsdk:"apple_key_id"`
	AppleTeamID       types.String `tfsdk:"apple_team_id"`
	Type              types.String `tfsdk:"type"`
}

func NewNeonAuthOAuthProviderResource() resource.Resource {
	return &neonAuthOAuthProviderResource{}
}

func (r *neonAuthOAuthProviderResource) Metadata(_ context.Context, _ resource.MetadataRequest,
	resp *resource.MetadataResponse) {
	resp.TypeName = "neon_auth_oauth_provider"
}

func (r *neonAuthOAuthProviderResource) Schema(_ context.Context, _ resource.SchemaRequest,
	resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Configures an OAuth provider for Neon Auth on a branch with your own OAuth app credentials. " +
			"Requires `neon_auth` on the branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "`<project_id>/<branch_id>/<provider_id>`.",
				PlanModifiers: idPlanModifiers(),
			},
			"project_id": schema.StringAttribute{
				Required: true, Description: "Project ID.", PlanModifiers: requiresReplace,
			},
			"branch_id": schema.StringAttribute{
				Required: true, Description: "Branch ID.", PlanModifiers: requiresReplace,
			},
			"provider_id": schema.StringAttribute{
				Required: true,
				Description: "OAuth provider: `google`, `github`, `microsoft`, `vercel`, or `apple`. Neon enables `google` " +
					"with shared development keys when Neon Auth is enabled; this resource replaces them with yours.",
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					stringvalidator.OneOf("google", "github", "microsoft", "vercel", "apple"),
				},
			},
			"client_id": schema.StringAttribute{
				Required:    true,
				Description: "Client ID of your OAuth app.",
			},
			"client_secret": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				Description: "Client secret of your OAuth app; for `apple`, the contents of the `.p8` private key. Neon " +
					"only returns it masked, so a change made outside Terraform is not detected.",
			},
			"microsoft_tenant_id": schema.StringAttribute{
				Optional:    true,
				Description: "Tenant that `microsoft` sign-in is restricted to. Neon cannot remove a tenant, so removing it replaces the provider.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.StringRequest,
						resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
						resp.RequiresReplace = !req.StateValue.IsNull() && req.ConfigValue.IsNull()
					}, "Removing the tenant replaces the provider.", "Removing the tenant replaces the provider."),
				},
			},
			"apple_key_id": schema.StringAttribute{
				Optional:    true,
				Description: "Key ID of the Sign in with Apple private key. Required for `apple`.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"apple_team_id": schema.StringAttribute{
				Optional:    true,
				Description: "Apple Developer Team ID. Required for `apple`.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"type": schema.StringAttribute{
				Computed:    true,
				Description: "`standard` once your credentials are set.",
			},
		},
	}
}

func (r *neonAuthOAuthProviderResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse) {
	var config neonAuthOAuthProviderResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.ProviderID.IsUnknown() {
		return
	}
	provider := config.ProviderID.ValueString()
	if provider != "microsoft" && !config.MicrosoftTenantID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("microsoft_tenant_id"), "Attribute only applies to microsoft",
			fmt.Sprintf("Remove microsoft_tenant_id; provider_id is %q.", provider))
	}
	for _, attr := range []struct {
		name  string
		value types.String
	}{{"apple_key_id", config.AppleKeyID}, {"apple_team_id", config.AppleTeamID}} {
		switch {
		case provider == "apple" && attr.value.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(attr.name), "Missing required attribute",
				fmt.Sprintf("Set %s; Sign in with Apple needs it.", attr.name))
		case provider != "apple" && !attr.value.IsNull():
			resp.Diagnostics.AddAttributeError(path.Root(attr.name), "Attribute only applies to apple",
				fmt.Sprintf("Remove %s; provider_id is %q.", attr.name, provider))
		}
	}
}

func (r *neonAuthOAuthProviderResource) Configure(_ context.Context, req resource.ConfigureRequest,
	resp *resource.ConfigureResponse) {
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

func (r *neonAuthOAuthProviderResource) Create(ctx context.Context, req resource.CreateRequest,
	resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var plan neonAuthOAuthProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	providerID, err := neon.NewNeonAuthOauthProviderId(plan.ProviderID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("provider_id"), "Unknown OAuth provider", err.Error())
		return
	}

	existing, diags := r.find(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var configured neon.NeonAuthOauthProvider
	switch {
	case existing == nil:
		resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
			var err error
			configured, err = r.client.AddBranchNeonAuthOauthProvider(plan.ProjectID.ValueString(),
				plan.BranchID.ValueString(), neon.NeonAuthAddOAuthProviderRequest{
					ID:                providerID,
					ClientID:          plan.ClientID.ValueStringPointer(),
					ClientSecret:      plan.ClientSecret.ValueStringPointer(),
					MicrosoftTenantID: plan.MicrosoftTenantID.ValueStringPointer(),
					AppleKeyID:        plan.AppleKeyID.ValueStringPointer(),
					AppleTeamID:       plan.AppleTeamID.ValueStringPointer(),
				})
			return err
		}, nil)...)
	case existing.Type.String() == sharedOAuthProviderType:
		configured, diags = r.update(ctx, plan, providerID)
		resp.Diagnostics.Append(diags...)
	default:
		id := oauthProviderID(plan)
		resp.Diagnostics.AddError("OAuth provider already configured",
			fmt.Sprintf("Neon Auth on branch %s of project %s already has %s configured with its own credentials. "+
				"Import it to manage it with Terraform: terraform import <address> %q",
				plan.BranchID.ValueString(), plan.ProjectID.ValueString(), plan.ProviderID.ValueString(), id))
	}
	if resp.Diagnostics.HasError() {
		return
	}

	setOAuthProviderModel(&plan, configured)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonAuthOAuthProviderResource) Read(ctx context.Context, req resource.ReadRequest,
	resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonAuthOAuthProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	existing, diags := r.find(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if existing == nil {
		resp.Diagnostics.AddWarning("OAuth provider not found",
			fmt.Sprintf("Neon Auth on branch %s of project %s no longer has %s; removing it from the state.",
				state.BranchID.ValueString(), state.ProjectID.ValueString(), state.ProviderID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	setOAuthProviderModel(&state, *existing)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonAuthOAuthProviderResource) Update(ctx context.Context, req resource.UpdateRequest,
	resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var plan neonAuthOAuthProviderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	providerID, err := neon.NewNeonAuthOauthProviderId(plan.ProviderID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("provider_id"), "Unknown OAuth provider", err.Error())
		return
	}
	configured, diags := r.update(ctx, plan, providerID)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	setOAuthProviderModel(&plan, configured)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonAuthOAuthProviderResource) Delete(ctx context.Context, req resource.DeleteRequest,
	resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonAuthOAuthProviderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	providerID, err := neon.NewNeonAuthOauthProviderId(state.ProviderID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("provider_id"), "Unknown OAuth provider", err.Error())
		return
	}
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		return r.client.DeleteBranchNeonAuthOauthProvider(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			providerID)
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error { return nil },
	})...)
}

func (r *neonAuthOAuthProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.Split(req.ID, "/")
	if len(els) != 3 || els[0] == "" || els[1] == "" || els[2] == "" {
		resp.Diagnostics.AddError("Invalid OAuth Provider Import ID", "Expected <project_id>/<branch_id>/<provider_id>.")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	state := neonAuthOAuthProviderResourceModel{
		ProjectID:    types.StringValue(els[0]),
		BranchID:     types.StringValue(els[1]),
		ProviderID:   types.StringValue(els[2]),
		ClientSecret: types.StringNull(),
	}
	existing, diags := r.find(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if existing == nil {
		resp.Diagnostics.AddError("OAuth provider not found",
			fmt.Sprintf("Neon Auth on branch %s of project %s has no %s provider.", els[1], els[0], els[2]))
		return
	}
	setOAuthProviderModel(&state, *existing)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// find returns the branch's provider with m's provider_id, or nil. A branch without Neon Auth has none.
func (r *neonAuthOAuthProviderResource) find(ctx context.Context, m neonAuthOAuthProviderResourceModel) (
	*neon.NeonAuthOauthProvider, diag.Diagnostics) {
	var providers neon.ListNeonAuthOauthProvidersResponse
	diags := projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		providers, err = r.client.ListBranchNeonAuthOauthProviders(m.ProjectID.ValueString(), m.BranchID.ValueString())
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error {
			providers = neon.ListNeonAuthOauthProvidersResponse{}
			return nil
		},
	})
	for i := range providers.Providers {
		if providers.Providers[i].ID.String() == m.ProviderID.ValueString() {
			return &providers.Providers[i], diags
		}
	}
	return nil, diags
}

// update sends every configured credential: Neon needs the Apple key ID together with a new private key.
func (r *neonAuthOAuthProviderResource) update(ctx context.Context, plan neonAuthOAuthProviderResourceModel,
	providerID neon.NeonAuthOauthProviderId) (neon.NeonAuthOauthProvider, diag.Diagnostics) {
	var configured neon.NeonAuthOauthProvider
	diags := projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		configured, err = r.client.UpdateBranchNeonAuthOauthProvider(plan.ProjectID.ValueString(),
			plan.BranchID.ValueString(), providerID, neon.NeonAuthUpdateOAuthProviderRequest{
				ClientID:          plan.ClientID.ValueStringPointer(),
				ClientSecret:      plan.ClientSecret.ValueStringPointer(),
				MicrosoftTenantID: plan.MicrosoftTenantID.ValueStringPointer(),
				AppleKeyID:        plan.AppleKeyID.ValueStringPointer(),
				AppleTeamID:       plan.AppleTeamID.ValueStringPointer(),
			})
		return err
	}, nil)
	return configured, diags
}

// setOAuthProviderModel keeps client_secret as configured, since Neon only returns it masked.
func setOAuthProviderModel(m *neonAuthOAuthProviderResourceModel, p neon.NeonAuthOauthProvider) {
	m.ID = types.StringValue(oauthProviderID(*m))
	m.ClientID = types.StringPointerValue(p.ClientID)
	m.MicrosoftTenantID = types.StringPointerValue(p.MicrosoftTenantID)
	m.AppleKeyID = types.StringPointerValue(p.AppleKeyID)
	m.AppleTeamID = types.StringPointerValue(p.AppleTeamID)
	m.Type = types.StringValue(p.Type.String())
}

func oauthProviderID(m neonAuthOAuthProviderResourceModel) string {
	return m.ProjectID.ValueString() + "/" + m.BranchID.ValueString() + "/" + m.ProviderID.ValueString()
}
