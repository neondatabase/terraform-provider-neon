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
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.ResourceWithConfigure   = (*neonAuthTrustedDomainResource)(nil)
	_ resource.ResourceWithImportState = (*neonAuthTrustedDomainResource)(nil)
)

type neonAuthTrustedDomainResource struct {
	client *neon.Client
}

type neonAuthTrustedDomainResourceModel struct {
	ID        types.String `tfsdk:"id"`
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Domain    types.String `tfsdk:"domain"`
}

func NewNeonAuthTrustedDomainResource() resource.Resource {
	return &neonAuthTrustedDomainResource{}
}

func (r *neonAuthTrustedDomainResource) Metadata(_ context.Context, _ resource.MetadataRequest,
	resp *resource.MetadataResponse) {
	resp.TypeName = "neon_auth_trusted_domain"
}

func (r *neonAuthTrustedDomainResource) Schema(_ context.Context, _ resource.SchemaRequest,
	resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Adds a domain that Neon Auth on a branch accepts as a redirect target. Requires `neon_auth` on the branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "`<project_id>/<branch_id>/<domain>`.",
				PlanModifiers: idPlanModifiers(),
			},
			"project_id": schema.StringAttribute{
				Required: true, Description: "Project ID.", PlanModifiers: requiresReplace,
			},
			"branch_id": schema.StringAttribute{
				Required: true, Description: "Branch ID.", PlanModifiers: requiresReplace,
			},
			"domain": schema.StringAttribute{
				Required:      true,
				Description:   "Origin to trust, for example `https://app.example.com`.",
				PlanModifiers: requiresReplace,
			},
		},
	}
}

func (r *neonAuthTrustedDomainResource) Configure(_ context.Context, req resource.ConfigureRequest,
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

func (r *neonAuthTrustedDomainResource) Create(ctx context.Context, req resource.CreateRequest,
	resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var plan neonAuthTrustedDomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		return r.client.AddBranchNeonAuthTrustedDomain(plan.ProjectID.ValueString(), plan.BranchID.ValueString(),
			neon.NeonAuthAddDomainToRedirectURIWhitelistRequest{
				AuthProvider: neon.NeonAuthSupportedAuthProviderBetterAuth,
				Domain:       plan.Domain.ValueString(),
			})
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue(plan.ProjectID.ValueString() + "/" + plan.BranchID.ValueString() + "/" + plan.Domain.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonAuthTrustedDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonAuthTrustedDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, diags := r.find(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddWarning("Trusted domain not found",
			fmt.Sprintf("Neon Auth on branch %s of project %s no longer trusts %s; removing it from the state.",
				state.BranchID.ValueString(), state.ProjectID.ValueString(), state.Domain.ValueString()))
		resp.State.RemoveResource(ctx)
	}
}

func (r *neonAuthTrustedDomainResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *neonAuthTrustedDomainResource) Delete(ctx context.Context, req resource.DeleteRequest,
	resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonAuthTrustedDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		return r.client.DeleteBranchNeonAuthTrustedDomain(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			neon.NeonAuthDeleteDomainFromRedirectURIWhitelistRequest{
				AuthProvider: neon.NeonAuthSupportedAuthProviderBetterAuth,
				Domains:      []neon.NeonAuthDeleteDomainFromRedirectURIWhitelistItem{{Domain: state.Domain.ValueString()}},
			})
	}, map[int]func(context.Context) error{
		// Neon Auth is no longer enabled on the branch, which drops its trusted domains.
		http.StatusNotFound: func(context.Context) error { return nil },
	})...)
}

func (r *neonAuthTrustedDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 3)
	if len(els) != 3 || els[0] == "" || els[1] == "" || els[2] == "" {
		resp.Diagnostics.AddError("Invalid Trusted Domain Import ID", "Expected <project_id>/<branch_id>/<domain>.")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	state := neonAuthTrustedDomainResourceModel{
		ID:        types.StringValue(req.ID),
		ProjectID: types.StringValue(els[0]),
		BranchID:  types.StringValue(els[1]),
		Domain:    types.StringValue(els[2]),
	}
	found, diags := r.find(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("Trusted domain not found",
			fmt.Sprintf("Neon Auth on branch %s of project %s does not trust %s.", els[1], els[0], els[2]))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// find reports whether the branch's Neon Auth trusts the domain. A branch without Neon Auth trusts none.
func (r *neonAuthTrustedDomainResource) find(ctx context.Context, m neonAuthTrustedDomainResourceModel) (bool, diag.Diagnostics) {
	var domains neon.NeonAuthRedirectURIWhitelistResponse
	diags := projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		domains, err = r.client.ListBranchNeonAuthTrustedDomains(m.ProjectID.ValueString(), m.BranchID.ValueString())
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error {
			domains = neon.NeonAuthRedirectURIWhitelistResponse{}
			return nil
		},
	})
	for _, d := range domains.Domains {
		if d.Domain == m.Domain.ValueString() {
			return true, diags
		}
	}
	return false, diags
}
