package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ ephemeral.EphemeralResourceWithConfigure = (*neonEphemeralBranchRealtimeSecret)(nil)

type neonEphemeralBranchRealtimeSecret struct{ client *neon.Client }
type neonEphemeralBranchRealtimeSecretModel struct {
	ProjectID types.String `tfsdk:"project_id"`
	BranchID  types.String `tfsdk:"branch_id"`
	Secret    types.String `tfsdk:"secret"`
	Pending   types.Bool   `tfsdk:"pending"`
}

// NewNeonBranchRealtimeSecretEphemeralResource reads a secret without persistent state.
func NewNeonBranchRealtimeSecretEphemeralResource() ephemeral.EphemeralResource {
	return &neonEphemeralBranchRealtimeSecret{}
}
func (r *neonEphemeralBranchRealtimeSecret) Metadata(_ context.Context, _ ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = "neon_branch_realtime_secret"
}
func (r *neonEphemeralBranchRealtimeSecret) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter for the Neon branch Realtime secret ephemeral resource.")
		return
	}
	if adapter == nil || adapter.sdk == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	r.client = adapter.sdk
}
func (r *neonEphemeralBranchRealtimeSecret) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves a branch's Realtime shared signing secret without storing it in Terraform state or plan. Requires Terraform 1.10+ and permission to read project credentials. Returns the first successful response, including the previous usable secret when pending is true; does not rotate secrets or enable Realtime.",
		Attributes: map[string]schema.Attribute{
			"project_id": schema.StringAttribute{Required: true, Description: "The Neon project ID."},
			"branch_id":  schema.StringAttribute{Required: true, Description: "The Neon branch ID."},
			"secret":     schema.StringAttribute{Computed: true, Sensitive: true, Description: "Shared secret used by the application backend to sign Realtime tokens. Never persist or log this value."},
			"pending":    schema.BoolAttribute{Computed: true, Description: "Whether a change is pending. During rotation the returned secret remains the previous usable secret until completion."},
		},
	}
}
func (r *neonEphemeralBranchRealtimeSecret) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var model neonEphemeralBranchRealtimeSecretModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if model.ProjectID.IsUnknown() || model.ProjectID.IsNull() || model.BranchID.IsUnknown() || model.BranchID.IsNull() {
		resp.Diagnostics.AddError("Unknown Branch Realtime Identity", "Project and branch must be known before opening the Realtime secret.")
		return
	}
	ctx, cancel := realtimeContext(ctx)
	defer cancel()
	projectID, branchID := url.PathEscape(model.ProjectID.ValueString()), url.PathEscape(model.BranchID.ValueString())
	var result neon.RealtimeSecret
	var lastNotFound error
	err := realtimePoll(ctx, realtimePollInterval, func(ctx context.Context) (bool, error) {
		err := projectReadiness.Do(ctx, func(ctx context.Context) error {
			return realtimeAttempt(ctx, func() error {
				var err error
				result, err = r.client.GetProjectBranchRealtimeSecret(projectID, branchID)
				if realtimeNotFound(err) {
					lastNotFound = err
				}
				return err
			})
		}, nil)
		if realtimeNotFound(err) {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		if lastNotFound != nil && ctx.Err() != nil {
			resp.Diagnostics.Append(newDiagnostics(lastNotFound)...)
		}
		resp.Diagnostics.Append(newDiagnostics(err)...)
		return
	}
	model.Secret = types.StringValue(result.Secret)
	model.Pending = types.BoolValue(result.Pending)
	resp.Diagnostics.Append(resp.Result.Set(ctx, &model)...)
}
