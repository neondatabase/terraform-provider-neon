package provider

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ ephemeral.EphemeralResourceWithConfigure = (*neonEphemeralBranchRealtimeSecret)(nil)

type neonEphemeralBranchRealtimeSecret struct {
	client *neon.Client
}

type neonEphemeralBranchRealtimeSecretModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Secret    types.String `tfsdk:"secret"`
}

func NewNeonBranchRealtimeSecretEphemeralResource() ephemeral.EphemeralResource {
	return &neonEphemeralBranchRealtimeSecret{}
}

func (r *neonEphemeralBranchRealtimeSecret) Configure(_ context.Context, req ephemeral.ConfigureRequest,
	resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if adapter == nil || adapter.sdk == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	r.client = adapter.sdk
}

func (r *neonEphemeralBranchRealtimeSecret) Metadata(_ context.Context, _ ephemeral.MetadataRequest,
	resp *ephemeral.MetadataResponse) {
	resp.TypeName = "neon_branch_realtime_secret"
}

func (r *neonEphemeralBranchRealtimeSecret) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads the secret an application backend issues Neon Realtime tokens with. The secret is never stored in the Terraform state.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{
				Required:    true,
				Description: "Project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:    true,
				Description: "Branch ID. Realtime must be enabled on the branch.",
			},
			"secret": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Realtime secret, starting with `nrt_live_`.",
			},
		},
	}
}

func (r *neonEphemeralBranchRealtimeSecret) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var data neonEphemeralBranchRealtimeSecretModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectID, branchID := data.ProjectID.ValueString(), data.BranchID.ValueString()
	// During a rotation Neon returns the previous secret, which stops working once the rotation settles.
	result, err := waitRealtimeSettled(ctx, projectID, branchID, func() (neon.RealtimeSecret, error) {
		return r.client.GetProjectBranchRealtimeSecret(projectID, branchID)
	}, func(v neon.RealtimeSecret) bool { return v.Pending })
	resp.Diagnostics.Append(newRealtimeSecretDiagnostics(err)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Secret = types.StringValue(result.Secret)
	resp.Diagnostics.Append(resp.Result.Set(ctx, &data)...)
}

// newRealtimeSecretDiagnostics explains a branch without Realtime. Terraform opens an ephemeral resource during plan
// once its arguments are known, which is before the run enables Realtime with neon_branch_realtime.
func newRealtimeSecretDiagnostics(err error) diag.Diagnostics {
	var apiErr neon.Error
	if !errors.As(err, &apiErr) || apiErr.HTTPCode != http.StatusNotFound ||
		!slices.ContainsFunc(apiErr.Details, func(d neon.ErrorDetails) bool {
			return strings.Contains(strings.ToLower(d.Message), "realtime is not enabled")
		}) {
		return newDiagnostics(err)
	}
	var diags diag.Diagnostics
	diags.AddError("Neon Realtime not enabled", err.Error()+"\n\nEnable it with the neon_branch_realtime resource. "+
		"Terraform opens ephemeral resources during plan, so if this run also enables Realtime, enable it first with "+
		"terraform apply -target=<address>, then apply the rest of the configuration.")
	return diags
}
