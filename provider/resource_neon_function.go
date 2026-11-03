package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.Resource                = (*neonFunctionResource)(nil)
	_ resource.ResourceWithConfigure   = (*neonFunctionResource)(nil)
	_ resource.ResourceWithImportState = (*neonFunctionResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*neonFunctionResource)(nil)
)

type neonFunctionResource struct {
	client *neon.Client
}

type neonFunctionResourceModel struct {
	ID                       types.String      `tfsdk:"id"`
	ProjectID                types.String      `tfsdk:"project_id"`
	BranchID                 types.String      `tfsdk:"branch_id"`
	Slug                     types.String      `tfsdk:"slug"`
	Runtime                  types.String      `tfsdk:"runtime"`
	Name                     types.String      `tfsdk:"name"`
	ZipFilePath              types.String      `tfsdk:"zip_file_path"`
	EnvironmentVariables     map[string]string `tfsdk:"environment_variables"`
	CreatedAt                types.String      `tfsdk:"created_at"`
	InvocationURL            types.String      `tfsdk:"invocation_url"`
	CurrentDeploymentID      types.Int64       `tfsdk:"current_deployment_id"`
	EnvironmentVariableNames types.List        `tfsdk:"environment_variable_names"`
}

func NewNeonFunctionResource() resource.Resource {
	return &neonFunctionResource{}
}

func (r *neonFunctionResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_function"
}

func (r *neonFunctionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	mapRequiresReplace := []planmodifier.Map{mapplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: "Manages a Neon Serverless Function on a branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite ID of the form <project_id>/<branch_id>/<slug>.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon branch ID.",
			},
			"slug": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Branch-unique identifier for the function. Forms the invocation URL host together with the branch ID.",
			},
			"runtime": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Validators: []validator.String{
					functionRuntimeValidator{},
				},
				Description: "Runtime for the function. Currently only `nodejs24` is supported.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Free-form display name for the function. Updatable in place.",
			},
			"zip_file_path": schema.StringAttribute{
				Optional:      true,
				Sensitive:     true,
				PlanModifiers: requiresReplace,
				Description:   "Path to a local ZIP archive of the function source. Required on create.",
			},
			"environment_variables": schema.MapAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Sensitive:     true,
				PlanModifiers: mapRequiresReplace,
				Description:   "Environment variables to expose to the function. Values are write-only and never returned by the API. Known limit: setting non-empty values currently fails at apply due to an upstream Plugin Framework reflection bug. Omit the attribute or set it to `{}` to avoid the failure.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp at which the function was created.",
			},
			"invocation_url": schema.StringAttribute{
				Computed:    true,
				Description: "URL at which the function is invoked.",
			},
			"current_deployment_id": schema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic deployment version number of the most recent deployment, regardless of build status.",
			},
			"environment_variable_names": schema.ListAttribute{
				ElementType:   types.StringType,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				Description:   "Names of the function's environment variables. Values are never returned by the API.",
			},
		},
	}
}

// functionRuntimeValidator delegates to the SDK so adding a new runtime
// in the SDK updates the validator without a code change here.
type functionRuntimeValidator struct{}

func (functionRuntimeValidator) Description(context.Context) string {
	return "validates that the runtime is supported by the Neon Functions API"
}

func (v functionRuntimeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (functionRuntimeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := neon.NewFunctionDeployRequestRuntime(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid function runtime",
			err.Error(),
		)
	}
}

func (r *neonFunctionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			"Expected *providerAdapter, got an unexpected type.",
		)
		return
	}

	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}

	r.client = client.sdk
}

// ModifyPlan enforces the project convention declared above. All immutable
// attributes carry RequiresReplace plan modifiers, so any change to them
// results in a replace plan. Name is the only attribute that may be updated
// in place; no in-place update needs to be surfaced as an error here.
func (r *neonFunctionResource) ModifyPlan(_ context.Context, _ resource.ModifyPlanRequest, _ *resource.ModifyPlanResponse) {
}

func (r *neonFunctionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.Split(req.ID, "/")
	if len(els) != 3 {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Invalid Neon Function Import ID",
			"Expected an import ID in the form <project_id>/<branch_id>/<slug>.",
		)
		return
	}
	projectID, branchID, slug := els[0], els[1], els[2]

	var fn neon.NeonFunction
	resp.Diagnostics.Append(
		projectReadiness.RetryWithFallback(
			func(ctx context.Context) error {
				rsp, err := r.client.GetProjectBranchFunction(projectID, branchID, slug)
				if err != nil {
					return err
				}
				fn = rsp.Function
				return nil
			}, ctx,
			map[int]func(context.Context) error{
				http.StatusNotFound: func(_ context.Context) error {
					resp.Diagnostics.AddAttributeError(
						path.Root("id"),
						"Function Not Found",
						"The requested function was not found.",
					)
					return nil
				},
			},
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	state := neonFunctionResourceModel{
		ID:        types.StringValue(req.ID),
		ProjectID: types.StringValue(projectID),
		BranchID:  types.StringValue(branchID),
		Slug:      types.StringValue(slug),
	}
	setNeonFunctionModel(&state, fn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonFunctionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonFunctionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.ZipFilePath.IsNull() || plan.ZipFilePath.IsUnknown() || plan.ZipFilePath.ValueString() == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("zip_file_path"),
			"Missing zip_file_path",
			"zip_file_path is required when creating a function (the SDK requires a ZIP archive on first deploy).",
		)
		return
	}

	zipPath := plan.ZipFilePath.ValueString()
	zipFile, err := os.Open(zipPath)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("zip_file_path"),
			"Cannot open zip_file_path",
			fmt.Sprintf("Could not open ZIP archive at %q: %s", zipPath, err),
		)
		return
	}
	defer zipFile.Close()

	runtime := plan.Runtime.ValueString()

	resp.Diagnostics.Append(
		projectReadiness.Retry(
			func(ctx context.Context) error {
				_, err := r.client.CreateProjectBranchFunctionDeployment(
					plan.ProjectID.ValueString(),
					plan.BranchID.ValueString(),
					plan.Slug.ValueString(),
					zipFile,
					plan.EnvironmentVariables,
					&runtime,
				)
				return err
			}, ctx,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	var fn neon.NeonFunction
	resp.Diagnostics.Append(
		projectReadiness.Retry(
			func(ctx context.Context) error {
				rsp, err := r.client.GetProjectBranchFunction(
					plan.ProjectID.ValueString(),
					plan.BranchID.ValueString(),
					plan.Slug.ValueString(),
				)
				if err != nil {
					return err
				}
				fn = rsp.Function
				return nil
			}, ctx,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonFunctionModel(&plan, fn)
	// Echo back user-supplied sensitive inputs so subsequent plans are
	// stable. Computed attributes are filled by setNeonFunctionModel.
	plan.Runtime = types.StringValue(runtime)
	plan.ZipFilePath = types.StringValue(zipPath)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonFunctionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonFunctionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var fn neon.NeonFunction
	resp.Diagnostics.Append(
		projectReadiness.RetryWithFallback(
			func(ctx context.Context) error {
				rsp, err := r.client.GetProjectBranchFunction(
					state.ProjectID.ValueString(),
					state.BranchID.ValueString(),
					state.Slug.ValueString(),
				)
				if err != nil {
					return err
				}
				fn = rsp.Function
				return nil
			}, ctx,
			map[int]func(context.Context) error{
				http.StatusNotFound: func(_ context.Context) error {
					return nil
				},
			},
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	if fn.ID == "" {
		// Fallback path: SDK returned no error but the function is empty.
		// Treat as drift-by-deletion.
		resp.State.RemoveResource(ctx)
		return
	}

	setNeonFunctionModel(&state, fn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonFunctionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan, state neonFunctionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only `name` is updatable. All other attrs have RequiresReplace plan
	// modifiers, so the framework will not call Update when those change.
	var fn neon.NeonFunction
	resp.Diagnostics.Append(
		projectReadiness.Retry(
			func(ctx context.Context) error {
				rsp, err := r.client.UpdateProjectBranchFunction(
					state.ProjectID.ValueString(),
					state.BranchID.ValueString(),
					state.Slug.ValueString(),
					neon.NeonFunctionUpdateRequest{
						Name: plan.Name.ValueString(),
					},
				)
				if err != nil {
					return err
				}
				fn = rsp.Function
				return nil
			}, ctx,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonFunctionModel(&state, fn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonFunctionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonFunctionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(
		projectReadiness.RetryWithFallback(
			func(ctx context.Context) error {
				return r.client.DeleteProjectBranchFunction(
					state.ProjectID.ValueString(),
					state.BranchID.ValueString(),
					state.Slug.ValueString(),
				)
			}, ctx,
			map[int]func(context.Context) error{
				http.StatusNotFound: func(_ context.Context) error {
					return nil
				},
			},
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func setNeonFunctionModel(model *neonFunctionResourceModel, fn neon.NeonFunction) {
	model.ID = types.StringValue(neonFunctionID(model.ProjectID.ValueString(), model.BranchID.ValueString(), fn.Slug))
	model.Slug = types.StringValue(fn.Slug)
	model.Name = types.StringValue(fn.Name)
	model.CreatedAt = types.StringValue(fn.CreatedAt)
	model.InvocationURL = types.StringValue(fn.InvocationURL)

	if fn.CurrentDeployment != nil {
		model.CurrentDeploymentID = types.Int64Value(int64(fn.CurrentDeployment.ID))
		if len(fn.CurrentDeployment.Environment) == 0 {
			model.EnvironmentVariableNames = types.ListNull(types.StringType)
		} else {
			elems := make([]attr.Value, 0, len(fn.CurrentDeployment.Environment))
			for _, name := range fn.CurrentDeployment.Environment {
				elems = append(elems, types.StringValue(name))
			}
			model.EnvironmentVariableNames = types.ListValueMust(types.StringType, elems)
		}
	} else {
		model.CurrentDeploymentID = types.Int64Null()
		model.EnvironmentVariableNames = types.ListNull(types.StringType)
	}
}

func neonFunctionID(projectID, branchID, slug string) string {
	return fmt.Sprintf("%s/%s/%s", projectID, branchID, slug)
}
