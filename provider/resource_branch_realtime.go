package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.Resource                = (*neonBranchRealtimeResource)(nil)
	_ resource.ResourceWithConfigure   = (*neonBranchRealtimeResource)(nil)
	_ resource.ResourceWithImportState = (*neonBranchRealtimeResource)(nil)
)

// The Neon API documents these limits for allowed_origins.
var realtimeOriginPattern = regexp.MustCompile(`^(\*|https?://[^/?#@,*\s]+)$`)

const (
	realtimeMaxOrigins   = 16
	realtimePollInterval = 2 * time.Second
	realtimeSettleWithin = 10 * time.Minute
)

type neonBranchRealtimeResource struct{ client *neon.Client }

type neonBranchRealtimeModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	BranchID       types.String `tfsdk:"branch_id"`
	AllowedOrigins types.List   `tfsdk:"allowed_origins"`
	InvocationURL  types.String `tfsdk:"invocation_url"`
}

// NewNeonBranchRealtimeResource manages Neon Realtime on a branch.
func NewNeonBranchRealtimeResource() resource.Resource { return &neonBranchRealtimeResource{} }

func (r *neonBranchRealtimeResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_branch_realtime"
}

func (r *neonBranchRealtimeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Enables Neon Realtime on a branch. Deleting the resource disables Realtime and discards the branch's shared secret. " +
			"Child branches inherit Realtime from their parent when they are created.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: idPlanModifiers(),
				Description:   "Resource identity and import ID: <project_id>/<branch_id>.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
				Description:   "Project ID. Changes require replacement.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: replace,
				Description:   "Branch ID. Changes require replacement.",
			},
			"allowed_origins": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.SizeAtMost(realtimeMaxOrigins),
					listvalidator.ValueStringsAre(
						stringvalidator.LengthBetween(1, 2048),
						stringvalidator.RegexMatches(realtimeOriginPattern,
							"must be * or an http or https origin with a host, an optional port, and no path"),
					),
				},
				Description: "Browser origins allowed to connect, such as `https://app.example.com`. An empty list, or `[\"*\"]`, " +
					"allows any origin. When omitted, the branch keeps its current or inherited origins.",
			},
			"invocation_url": schema.StringAttribute{
				Computed:    true,
				Description: "URL that clients connect to.",
			},
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

func (r *neonBranchRealtimeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid Branch Realtime Import ID", "Expected <project_id>/<branch_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch_id"), parts[1])...)
}

func (r *neonBranchRealtimeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan neonBranchRealtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !r.configured(&resp.Diagnostics) {
		return
	}
	plan.ID = types.StringValue(plan.ProjectID.ValueString() + "/" + plan.BranchID.ValueString())
	r.apply(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *neonBranchRealtimeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan neonBranchRealtimeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !r.configured(&resp.Diagnostics) {
		return
	}
	r.apply(ctx, &plan, &resp.State, &resp.Diagnostics)
}

// apply enables Realtime with the planned options, which the API also uses to change the options of an enabled branch.
func (r *neonBranchRealtimeResource) apply(ctx context.Context, plan *neonBranchRealtimeModel, state *tfsdk.State,
	diags *diag.Diagnostics) {
	var cfg neon.RealtimeOptions
	if !plan.AllowedOrigins.IsNull() && !plan.AllowedOrigins.IsUnknown() {
		cfg.AllowedOrigins = []string{}
		diags.Append(plan.AllowedOrigins.ElementsAs(ctx, &cfg.AllowedOrigins, false)...)
		if diags.HasError() {
			return
		}
	}

	projectID, branchID := plan.ProjectID.ValueString(), plan.BranchID.ValueString()
	diags.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		return r.client.EnableProjectBranchRealtime(projectID, branchID, &cfg)
	}, nil)...)
	if diags.HasError() {
		return
	}

	current, err := r.waitSettled(ctx, projectID, branchID)
	diags.Append(newDiagnostics(err)...)
	if diags.HasError() {
		return
	}
	plan.setFromAPI(ctx, current, diags)
	diags.Append(state.Set(ctx, plan)...)
}

func (r *neonBranchRealtimeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state neonBranchRealtimeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !r.configured(&resp.Diagnostics) {
		return
	}

	var current neon.Realtime
	found := true
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		var err error
		current, err = r.client.GetProjectBranchRealtime(state.ProjectID.ValueString(), state.BranchID.ValueString())
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error {
			found = false
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found || !current.Enabled {
		resp.State.RemoveResource(ctx)
		return
	}
	state.setFromAPI(ctx, current, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBranchRealtimeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state neonBranchRealtimeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || !r.configured(&resp.Diagnostics) {
		return
	}

	projectID, branchID := state.ProjectID.ValueString(), state.BranchID.ValueString()
	gone := false
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(context.Context) error {
		return r.client.DisableProjectBranchRealtime(projectID, branchID)
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(context.Context) error {
			gone = true
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() || gone {
		return
	}
	_, err := r.waitSettled(ctx, projectID, branchID)
	if isNotFoundError(err) {
		return
	}
	resp.Diagnostics.Append(newDiagnostics(err)...)
}

func (r *neonBranchRealtimeResource) waitSettled(ctx context.Context, projectID, branchID string) (neon.Realtime, error) {
	return waitRealtimeSettled(ctx, projectID, branchID, func() (neon.Realtime, error) {
		return r.client.GetProjectBranchRealtime(projectID, branchID)
	}, func(v neon.Realtime) bool { return v.Pending })
}

// waitRealtimeSettled polls get until Neon finishes applying the last Realtime change to the branch.
func waitRealtimeSettled[T any](ctx context.Context, projectID, branchID string, get func() (T, error),
	pending func(T) bool) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, realtimeSettleWithin)
	defer cancel()
	for {
		var current T
		err := projectReadiness.Do(ctx, func(context.Context) error {
			var err error
			current, err = get()
			return err
		}, nil)
		if err != nil || !pending(current) {
			return current, err
		}
		select {
		case <-ctx.Done():
			return current, fmt.Errorf("realtime change for branch %s in project %s still pending after %s: %w",
				branchID, projectID, realtimeSettleWithin, ctx.Err())
		case <-time.After(realtimePollInterval):
		}
	}
}

func (r *neonBranchRealtimeResource) configured(diags *diag.Diagnostics) bool {
	if r.client == nil {
		diags.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return false
	}
	return true
}

func (m *neonBranchRealtimeModel) setFromAPI(ctx context.Context, current neon.Realtime, diags *diag.Diagnostics) {
	m.InvocationURL = types.StringPointerValue(current.InvocationURL)
	if len(current.AllowedOrigins) == 0 && allowsAnyOriginWithWildcard(m.AllowedOrigins) {
		// Neon stores ["*"] as an empty list; both allow any origin.
		return
	}
	origins := current.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	value, d := types.ListValueFrom(ctx, types.StringType, origins)
	diags.Append(d...)
	m.AllowedOrigins = value
}

func allowsAnyOriginWithWildcard(origins types.List) bool {
	elements := origins.Elements()
	if len(elements) != 1 {
		return false
	}
	origin, ok := elements[0].(types.String)
	return ok && origin.ValueString() == "*"
}
