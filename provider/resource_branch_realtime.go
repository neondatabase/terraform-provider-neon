package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.Resource                = (*neonBranchRealtimeResource)(nil)
	_ resource.ResourceWithConfigure   = (*neonBranchRealtimeResource)(nil)
	_ resource.ResourceWithImportState = (*neonBranchRealtimeResource)(nil)
)

type neonBranchRealtimeResource struct{ client *neon.Client }
type neonBranchRealtimeModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	BranchID       types.String `tfsdk:"branch_id"`
	AllowedOrigins types.List   `tfsdk:"allowed_origins"`
	InvocationURL  types.String `tfsdk:"invocation_url"`
	Revision       types.Int64  `tfsdk:"revision"`
}

func NewNeonBranchRealtimeResource() resource.Resource { return &neonBranchRealtimeResource{} }
func (r *neonBranchRealtimeResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_branch_realtime"
}
func (r *neonBranchRealtimeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replacement := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages Realtime on a Neon branch. Destroy disables Realtime on the branch and discards its secret.",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Computed: true, PlanModifiers: idPlanModifiers(), Description: "Resource identity and import ID: <project_id>/<branch_id>."},
			"project_id":      schema.StringAttribute{Required: true, PlanModifiers: replacement, Description: "The Neon project ID. Changes require replacement."},
			"branch_id":       schema.StringAttribute{Required: true, PlanModifiers: replacement, Description: "The Neon branch ID. Changes require replacement."},
			"allowed_origins": schema.ListAttribute{Optional: true, ElementType: types.StringType, Description: "Browser origins to manage. Omitted or null preserves the current or inherited policy and leaves origins unmanaged. An explicit empty list or [\"*\"] allows any origin. The API validates origin syntax."},
			"invocation_url":  schema.StringAttribute{Computed: true, Description: "Application Realtime endpoint, or null when not returned by the API."},
			"revision":        schema.Int64Attribute{Computed: true, Description: "Observed Realtime configuration revision, or null when not returned by the API."},
		},
	}
}
func (r *neonBranchRealtimeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	adapter, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter for the Neon branch Realtime resource.")
		return
	}
	if adapter == nil || adapter.sdk == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	r.client = adapter.sdk
}
func (r *neonBranchRealtimeResource) configured(diags *diag.Diagnostics) bool {
	if r.client != nil {
		return true
	}
	diags.AddError("Client Not Configured", "The Neon provider client is not configured.")
	return false
}
func (r *neonBranchRealtimeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid Branch Realtime Import ID", "Expected <project_id>/<branch_id> with two non-empty components.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch_id"), parts[1])...)
}
func (m neonBranchRealtimeModel) sdkPathIDs() (string, string) {
	return url.PathEscape(m.ProjectID.ValueString()), url.PathEscape(m.BranchID.ValueString())
}
func (m neonBranchRealtimeModel) sdkOptions(ctx context.Context, diags *diag.Diagnostics) *neon.RealtimeOptions {
	if m.AllowedOrigins.IsNull() {
		return &neon.RealtimeOptions{}
	}
	origins := make([]string, 0, len(m.AllowedOrigins.Elements()))
	diags.Append(m.AllowedOrigins.ElementsAs(ctx, &origins, false)...)
	return &neon.RealtimeOptions{AllowedOrigins: origins}
}
func (m *neonBranchRealtimeModel) observe(result neon.Realtime) {
	m.InvocationURL = types.StringPointerValue(result.InvocationURL)
	m.Revision = types.Int64Null()
	if result.Revision != nil {
		m.Revision = types.Int64Value(int64(*result.Revision))
	}
}
func (r *neonBranchRealtimeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var model neonBranchRealtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if !resp.Diagnostics.HasError() {
		r.enable(ctx, &model, &resp.State, &resp.Diagnostics)
	}
}
func (r *neonBranchRealtimeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var model neonBranchRealtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if !resp.Diagnostics.HasError() {
		r.enable(ctx, &model, &resp.State, &resp.Diagnostics)
	}
}
func (r *neonBranchRealtimeResource) enable(ctx context.Context, model *neonBranchRealtimeModel, state *tfsdk.State, diags *diag.Diagnostics) {
	ctx, cancel := realtimeContext(ctx)
	defer cancel()
	options := model.sdkOptions(ctx, diags)
	if diags.HasError() {
		return
	}
	projectID, branchID := model.sdkPathIDs()
	accepted := false
	err := projectReadiness.Do(ctx, func(ctx context.Context) error {
		return realtimeAttempt(ctx, func() error {
			err := r.client.EnableProjectBranchRealtime(projectID, branchID, options)
			accepted = err == nil
			return err
		})
	}, nil)
	if accepted {
		model.ID = types.StringValue(model.ProjectID.ValueString() + "/" + model.BranchID.ValueString())
		model.InvocationURL = types.StringNull()
		model.Revision = types.Int64Null()
		diags.Append(state.Set(ctx, model)...)
	}
	diags.Append(newDiagnostics(err)...)
	if diags.HasError() {
		return
	}
	var result neon.Realtime
	err = realtimePoll(ctx, realtimePollInterval, func(ctx context.Context) (bool, error) {
		var err error
		result, err = r.get(ctx, projectID, branchID)
		if err != nil {
			return false, err
		}
		if result.Pending {
			return false, nil
		}
		if !result.Enabled {
			return false, fmt.Errorf("realtime enable completed but the branch is disabled")
		}
		return true, nil
	})
	diags.Append(newDiagnostics(err)...)
	if diags.HasError() {
		return
	}
	model.observe(result)
	diags.Append(state.Set(ctx, model)...)
}
func (r *neonBranchRealtimeResource) get(ctx context.Context, projectID, branchID string) (neon.Realtime, error) {
	var result neon.Realtime
	err := projectReadiness.Do(ctx, func(ctx context.Context) error {
		return realtimeAttempt(ctx, func() error {
			var err error
			result, err = r.client.GetProjectBranchRealtime(projectID, branchID)
			return err
		})
	}, nil)
	return result, err
}
func realtimeNotFound(err error) bool {
	e, ok := err.(neon.Error)
	return ok && e.HTTPCode == http.StatusNotFound
}
func unrestrictedOrigins(origins []string) bool {
	return len(origins) == 0 || (len(origins) == 1 && origins[0] == "*")
}
func (r *neonBranchRealtimeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var model neonBranchRealtimeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := realtimeContext(ctx)
	defer cancel()
	projectID, branchID := model.sdkPathIDs()
	result, err := r.get(ctx, projectID, branchID)
	if realtimeNotFound(err) || (err == nil && !result.Enabled) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(newDiagnostics(err)...)
	if resp.Diagnostics.HasError() {
		return
	}
	model.observe(result)
	if !model.AllowedOrigins.IsNull() {
		if result.AllowedOrigins == nil {
			resp.Diagnostics.AddWarning("Realtime Origins Not Observed", "The API did not return allowed_origins. Retaining the managed list; origins drift could not be observed.")
		} else {
			var prior []string
			resp.Diagnostics.Append(model.AllowedOrigins.ElementsAs(ctx, &prior, false)...)
			if !(unrestrictedOrigins(prior) && unrestrictedOrigins(result.AllowedOrigins)) {
				var d diag.Diagnostics
				model.AllowedOrigins, d = types.ListValueFrom(ctx, types.StringType, result.AllowedOrigins)
				resp.Diagnostics.Append(d...)
			}
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
func (r *neonBranchRealtimeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if !r.configured(&resp.Diagnostics) {
		return
	}
	var model neonBranchRealtimeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := realtimeContext(ctx)
	defer cancel()
	projectID, branchID := model.sdkPathIDs()
	err := projectReadiness.Do(ctx, func(ctx context.Context) error {
		return realtimeAttempt(ctx, func() error { return r.client.DisableProjectBranchRealtime(projectID, branchID) })
	}, nil)
	if realtimeNotFound(err) {
		return
	}
	resp.Diagnostics.Append(newDiagnostics(err)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err = realtimePoll(ctx, realtimePollInterval, func(ctx context.Context) (bool, error) {
		result, err := r.get(ctx, projectID, branchID)
		if realtimeNotFound(err) {
			return true, nil
		}
		return err == nil && !result.Enabled, err
	})
	resp.Diagnostics.Append(newDiagnostics(err)...)
}
