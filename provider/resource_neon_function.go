package provider

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
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
	SourceCodeHash           types.String      `tfsdk:"source_code_hash"`
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

	resp.Schema = schema.Schema{
		Description: "Manages a Neon Serverless Function on a branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "Composite ID of the form <project_id>/<branch_id>/<slug>.",
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
				Required: true,
				Validators: []validator.String{
					functionRuntimeValidator{},
				},
				Description: "Runtime for the function. Currently only `nodejs24` is supported. Changing it deploys the function again.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Free-form display name for the function. Updatable in place.",
			},
			"zip_file_path": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Path to a local ZIP archive of the function source. Required on create. Changing it deploys the function again.",
			},
			"source_code_hash": schema.StringAttribute{
				Optional: true,
				Description: "Hash of the ZIP archive, for example `filebase64sha256(\"function.zip\")`. " +
					"Set it so that a new archive at the same `zip_file_path` deploys the function again.",
			},
			"environment_variables": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Sensitive:   true,
				Description: "Environment variables to expose to the function. The API never returns their values. Terraform stores configured values in state and marks them sensitive. Changing them deploys the function again.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "RFC3339 timestamp at which the function was created.",
			},
			"invocation_url": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "URL at which the function is invoked.",
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

// ModifyPlan marks environment_variable_names unknown when the configured variables change,
// because a new deployment replaces the names the API reports.
func (r *neonFunctionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var planEnv, stateEnv types.Map
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("environment_variables"), &planEnv)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("environment_variables"), &stateEnv)...)
	if resp.Diagnostics.HasError() || planEnv.Equal(stateEnv) {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("environment_variable_names"),
		types.ListUnknown(types.StringType))...)
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
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				rsp, err := r.client.GetProjectBranchFunction(projectID, branchID, slug)
				if err != nil {
					return err
				}
				fn = rsp.Function
				return nil
			},
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
	if fn.CurrentDeployment != nil {
		state.Runtime = types.StringValue(fn.CurrentDeployment.Runtime)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// deploy uploads the archive at plan.ZipFilePath as a new deployment of the function.
// The API keeps environment variables a deployment does not mention, so env must also
// carry every removed key with an empty value, which deletes it.
func (r *neonFunctionResource) deploy(ctx context.Context, plan neonFunctionResourceModel, env map[string]string) diag.Diagnostics {
	if plan.ZipFilePath.IsNull() || plan.ZipFilePath.IsUnknown() || plan.ZipFilePath.ValueString() == "" {
		var diags diag.Diagnostics
		diags.AddAttributeError(
			path.Root("zip_file_path"),
			"Missing zip_file_path",
			"Set zip_file_path to the function's ZIP archive. Neon needs the archive to deploy the function.",
		)
		return diags
	}
	zipPath := plan.ZipFilePath.ValueString()
	runtime := plan.Runtime.ValueString()
	var deployment neon.NeonFunctionDeployment
	diags := projectReadiness.Retry(ctx,
		func(ctx context.Context) error {
			// Each attempt reads the archive from the start.
			zipFile, err := os.Open(zipPath)
			if err != nil {
				return fmt.Errorf("could not open the ZIP archive at %q: %w", zipPath, err)
			}
			defer zipFile.Close()
			rsp, err := r.client.CreateProjectBranchFunctionDeployment(
				plan.ProjectID.ValueString(),
				plan.BranchID.ValueString(),
				plan.Slug.ValueString(),
				zipFile,
				env,
				&runtime,
			)
			deployment = rsp.Deployment
			return err
		}, nil,
	)
	if diags.HasError() {
		return diags
	}
	return r.waitDeploymentActive(ctx, plan, deployment.ID)
}

const (
	functionDeploymentPollInterval = 2 * time.Second
	functionDeploymentTimeout      = 10 * time.Minute
)

// waitDeploymentActive blocks until the deployment has built and serves requests,
// so that apply finishes once the new code is live.
func (r *neonFunctionResource) waitDeploymentActive(ctx context.Context, plan neonFunctionResourceModel,
	deploymentID int32) diag.Diagnostics {
	var diags diag.Diagnostics
	ctx, cancel := context.WithTimeout(ctx, functionDeploymentTimeout)
	defer cancel()
	for {
		fn, d := r.getFunction(ctx, plan)
		if d.HasError() {
			return d
		}
		if current := fn.CurrentDeployment; current != nil && current.ID == deploymentID {
			switch current.Status {
			case neon.NeonFunctionDeploymentStatusFailed:
				reason := "no reason reported"
				if current.Error != nil && *current.Error != "" {
					reason = *current.Error
				}
				diags.AddAttributeError(path.Root("zip_file_path"), "Function deployment failed",
					fmt.Sprintf("Deployment %d of function %q failed to build: %s. Fix the function code, then run Terraform again.",
						deploymentID, plan.Slug.ValueString(), reason))
				return diags
			case neon.NeonFunctionDeploymentStatusCompleted:
				if fn.ActiveDeployment != nil && fn.ActiveDeployment.ID == deploymentID {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			diags.AddError("Function deployment did not finish",
				fmt.Sprintf("Deployment %d of function %q was not serving requests after %s: %s. "+
					"Check the function in the Neon Console, then run Terraform again.",
					deploymentID, plan.Slug.ValueString(), functionDeploymentTimeout, ctx.Err()))
			return diags
		case <-time.After(functionDeploymentPollInterval):
		}
	}
}

func (r *neonFunctionResource) getFunction(ctx context.Context, plan neonFunctionResourceModel) (neon.NeonFunction, diag.Diagnostics) {
	var fn neon.NeonFunction
	diags := projectReadiness.Retry(ctx,
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
		}, nil,
	)
	return fn, diags
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

	resp.Diagnostics.Append(r.deploy(ctx, plan, plan.EnvironmentVariables)...)
	if resp.Diagnostics.HasError() {
		// A failed build or rollout still leaves the function in Neon. Track it so Terraform can replace or destroy it.
		if fn, diags := r.getFunction(ctx, plan); !diags.HasError() {
			setNeonFunctionModel(&plan, fn)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		}
		return
	}

	fn, diags := r.getFunction(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	requestedName := plan.Name
	setNeonFunctionModel(&plan, fn)
	// Keep the deployed identity available if the separate rename request fails.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() || requestedName.IsNull() || requestedName.IsUnknown() || requestedName.Equal(plan.Name) {
		return
	}

	resp.Diagnostics.Append(
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				rsp, err := r.client.UpdateProjectBranchFunction(
					plan.ProjectID.ValueString(),
					plan.BranchID.ValueString(),
					plan.Slug.ValueString(),
					neon.NeonFunctionUpdateRequest{Name: requestedName.ValueString()},
				)
				if err != nil {
					return err
				}
				fn = rsp.Function
				return nil
			}, nil,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonFunctionModel(&plan, fn)
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
		projectReadiness.Retry(ctx,
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
			},
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

	if !plan.ZipFilePath.Equal(state.ZipFilePath) || !plan.SourceCodeHash.Equal(state.SourceCodeHash) ||
		!plan.Runtime.Equal(state.Runtime) || !maps.Equal(plan.EnvironmentVariables, state.EnvironmentVariables) {
		env := maps.Clone(plan.EnvironmentVariables)
		for key := range state.EnvironmentVariables {
			if _, ok := plan.EnvironmentVariables[key]; !ok {
				if env == nil {
					env = map[string]string{}
				}
				env[key] = ""
			}
		}
		resp.Diagnostics.Append(r.deploy(ctx, plan, env)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if !plan.Name.IsUnknown() && !plan.Name.Equal(state.Name) {
		resp.Diagnostics.Append(
			projectReadiness.Retry(ctx,
				func(ctx context.Context) error {
					_, err := r.client.UpdateProjectBranchFunction(
						state.ProjectID.ValueString(),
						state.BranchID.ValueString(),
						state.Slug.ValueString(),
						neon.NeonFunctionUpdateRequest{Name: plan.Name.ValueString()},
					)
					return err
				}, nil,
			)...,
		)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	fn, diags := r.getFunction(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonFunctionModel(&plan, fn)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				return r.client.DeleteProjectBranchFunction(
					state.ProjectID.ValueString(),
					state.BranchID.ValueString(),
					state.Slug.ValueString(),
				)
			},
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
