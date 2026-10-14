package provider

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.ResourceWithConfigure   = (*functionResource)(nil)
	_ resource.ResourceWithImportState = (*functionResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*functionResource)(nil)
)

type functionResource struct {
	client       *neonClient
	pollInterval time.Duration
}

type functionModel struct {
	ID                       types.String `tfsdk:"id"`
	FunctionID               types.String `tfsdk:"function_id"`
	ProjectID                types.String `tfsdk:"project_id"`
	BranchID                 types.String `tfsdk:"branch_id"`
	Slug                     types.String `tfsdk:"slug"`
	Name                     types.String `tfsdk:"name"`
	Runtime                  types.String `tfsdk:"runtime"`
	ZipFilePath              types.String `tfsdk:"zip_file_path"`
	SourceCodeHash           types.String `tfsdk:"source_code_hash"`
	EnvironmentVariables     types.Map    `tfsdk:"environment_variables"`
	DeploymentTimeout        types.String `tfsdk:"deployment_timeout"`
	CreatedAt                types.String `tfsdk:"created_at"`
	InvocationURL            types.String `tfsdk:"invocation_url"`
	CurrentDeploymentID      types.Int64  `tfsdk:"current_deployment_id"`
	CurrentDeploymentStatus  types.String `tfsdk:"current_deployment_status"`
	ActiveDeploymentID       types.Int64  `tfsdk:"active_deployment_id"`
	EnvironmentVariableNames types.Set    `tfsdk:"environment_variable_names"`
}

func NewFunctionResource() resource.Resource { return &functionResource{pollInterval: time.Second} }
func (r *functionResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_function"
}
func (r *functionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureBackend(req, resp)
}
func (r *functionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	slug := backendIdentityAttribute("Branch-unique function slug, 1-20 lowercase alphanumeric characters (Neon API path constraint). Changing it replaces the function.")
	slug.Validators = []validator.String{backendStringValidator{"slug"}}
	resp.Schema = schema.Schema{Description: "Manages a branch-scoped Neon Function and waits for its deployment to become active. Upload a prebuilt ZIP with index.mjs at its root; the runtime does not build TypeScript or install dependencies.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:    true,
			Description: "Composite ID: <project_id>/<branch_id>/<slug>.",
		},
		"function_id": schema.StringAttribute{
			Computed:    true,
			Description: "Function identifier exactly as returned by Neon. It may equal the slug; generation uniqueness is not guaranteed.",
		},
		"project_id": backendIdentityAttribute("Neon project ID."),
		"branch_id":  backendIdentityAttribute("Neon branch ID."),
		"slug":       slug,
		"name": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Display name, defaults to the slug. Updated in place.",
			Validators:  []validator.String{backendStringValidator{"name"}},
		},
		"runtime": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString("nodejs24"),
			Description: "Function runtime. Currently nodejs24.",
			Validators:  []validator.String{backendStringValidator{"runtime"}},
		},
		"zip_file_path": schema.StringAttribute{
			Optional:    true,
			Description: "Local prebuilt ZIP, required on create. Omit after import to manage only metadata/environment. Code changes deploy in place.",
		},
		"source_code_hash": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Description: "Base64 SHA256 of the ZIP bytes. Computed at plan time from zip_file_path, including when the filename is unchanged. An explicit filebase64sha256 value is checked again at apply.",
		},
		"environment_variables": schema.MapAttribute{
			Optional:    true,
			ElementType: types.StringType,
			Sensitive:   true,
			Description: "Desired user environment. Omit to leave it unmanaged; {} removes all user variables. Values must be nonempty (the API uses empty values to delete keys). Values are kept in Terraform state; Neon returns only names. External deployment IDs and names detect drift, but values cannot be read back.",
		},
		"deployment_timeout": schema.StringAttribute{
			Optional:    true,
			Computed:    true,
			Default:     stringdefault.StaticString("10m"),
			Description: "Maximum time for create/update and deployment activation, from 1s to 1h. Terraform cancellation interrupts HTTP and polling.",
			Validators:  []validator.String{backendStringValidator{"deployment_timeout"}},
		},
		"created_at": schema.StringAttribute{
			Computed:    true,
			Description: "Function creation timestamp, also compared as a time instant to detect recreation when identifiers are reused.",
		},
		"invocation_url": schema.StringAttribute{
			Computed:    true,
			Description: "Neon invocation URL, exactly as returned by the API.",
		},
		"current_deployment_id": schema.Int64Attribute{
			Computed:    true,
			Description: "Latest deployment version, including pending or failed deployments.",
		},
		"current_deployment_status": schema.StringAttribute{
			Computed:    true,
			Description: "Latest deployment status: pending, building, completed or failed.",
		},
		"active_deployment_id": schema.Int64Attribute{
			Computed:    true,
			Description: "Version currently serving invocations.",
		},
		"environment_variable_names": schema.SetAttribute{
			Computed:    true,
			ElementType: types.StringType,
			Description: "Names of the latest deployment's user environment variables.",
		},
	}}
}

// Hash the same bounded byte snapshot that will be uploaded. Recheck at apply
// so an artifact changed after a saved plan cannot silently deploy other bytes.
func readFunctionArchive(filename string) ([]byte, string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > 32<<20 {
		return nil, "", errors.New("function ZIP exceeds the API's 32 MiB compressed limit")
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, "", fmt.Errorf("invalid function ZIP: %w", err)
	}
	entry := false
	for _, f := range z.File {
		if f.Name == "index.mjs" && !f.FileInfo().IsDir() {
			entry = true
		}
	}
	if !entry {
		return nil, "", errors.New("function ZIP must contain a prebuilt index.mjs at its root")
	}
	sum := sha256.Sum256(b)
	return b, base64.StdEncoding.EncodeToString(sum[:]), nil
}

func (r *functionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan, config functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Name.IsNull() {
		plan.Name = plan.Slug
	}
	if !plan.EnvironmentVariables.IsNull() && !plan.EnvironmentVariables.IsUnknown() {
		known := true
		for _, value := range plan.EnvironmentVariables.Elements() {
			known = known && !value.IsUnknown()
		}
		if known {
			if _, err := functionEnvironment(ctx, plan.EnvironmentVariables, nil); err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("environment_variables"), "Invalid Environment Variables", err.Error())
				return
			}
		}
	}
	originalHash := plan.SourceCodeHash
	if plan.ZipFilePath.IsUnknown() {
		if config.SourceCodeHash.IsNull() {
			plan.SourceCodeHash = types.StringUnknown()
		}
	} else if !plan.ZipFilePath.IsNull() {
		_, hash, err := readFunctionArchive(plan.ZipFilePath.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("zip_file_path"), "Cannot Read Function ZIP", err.Error())
			return
		}
		if !config.SourceCodeHash.IsNull() && !config.SourceCodeHash.IsUnknown() && config.SourceCodeHash.ValueString() != hash {
			resp.Diagnostics.AddAttributeError(path.Root("source_code_hash"), "Function ZIP Hash Mismatch", "source_code_hash must equal filebase64sha256(zip_file_path).")
			return
		}
		if config.SourceCodeHash.IsUnknown() {
			plan.SourceCodeHash = types.StringUnknown()
		} else {
			plan.SourceCodeHash = types.StringValue(hash)
		}
	} else {
		if !config.SourceCodeHash.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("source_code_hash"), "Missing Function ZIP", "source_code_hash requires zip_file_path.")
			return
		}
		plan.SourceCodeHash = types.StringNull()
	}
	if !plan.SourceCodeHash.Equal(originalHash) {
		plan.CurrentDeploymentID = types.Int64Unknown()
		plan.CurrentDeploymentStatus = types.StringUnknown()
		plan.ActiveDeploymentID = types.Int64Unknown()
		plan.EnvironmentVariableNames = types.SetUnknown(types.StringType)
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *functionResource) rename(ctx context.Context, state *functionModel, plan tfsdk.Plan) error {
	var name types.String
	if diags := plan.GetAttribute(ctx, path.Root("name"), &name); diags.HasError() {
		return errors.New("cannot read planned function name")
	}
	if name.Equal(state.Name) {
		return nil
	}
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err != nil {
		return err
	}
	resp, err := client.UpdateProjectBranchFunction(url.PathEscape(state.ProjectID.ValueString()), url.PathEscape(state.BranchID.ValueString()), url.PathEscape(state.Slug.ValueString()), neon.NeonFunctionUpdateRequest{Name: name.ValueString()})
	if err != nil {
		return err
	}
	return setFunctionModel(ctx, state, resp.Function)
}

func setFunctionModel(ctx context.Context, model *functionModel, fn neon.NeonFunction) error {
	model.ID = types.StringValue(strings.Join([]string{model.ProjectID.ValueString(), model.BranchID.ValueString(), model.Slug.ValueString()}, "/"))
	model.FunctionID = types.StringNull()
	if fn.ID != "" {
		model.FunctionID = types.StringValue(fn.ID)
	}
	model.Name = types.StringValue(fn.Name)
	model.CreatedAt = types.StringValue(fn.CreatedAt)
	model.InvocationURL = types.StringValue(fn.InvocationURL)
	model.CurrentDeploymentID = types.Int64Null()
	model.CurrentDeploymentStatus = types.StringNull()
	model.ActiveDeploymentID = types.Int64Null()
	names := []string{}
	if d := fn.CurrentDeployment; d != nil {
		model.CurrentDeploymentID = types.Int64Value(int64(d.ID))
		model.CurrentDeploymentStatus = types.StringValue(d.Status.String())
		model.Runtime = types.StringValue(d.Runtime)
		names = append(names, d.Environment...)
	}
	if fn.ActiveDeployment != nil {
		model.ActiveDeploymentID = types.Int64Value(int64(fn.ActiveDeployment.ID))
	}
	value, diags := types.SetValueFrom(ctx, types.StringType, names)
	if diags.HasError() {
		return errors.New("could not encode environment variable names")
	}
	model.EnvironmentVariableNames = value
	return nil
}

func functionEnvironment(ctx context.Context, value types.Map, previous []string) (map[string]string, error) {
	if value.IsNull() {
		return nil, nil
	}
	env := map[string]string{}
	if diags := value.ElementsAs(ctx, &env, false); diags.HasError() {
		return nil, errors.New("environment_variables must contain known string values")
	}
	for key, val := range env {
		if key == "" || strings.ContainsAny(key, "=\x00") || val == "" || strings.ContainsRune(val, '\x00') {
			return nil, fmt.Errorf("invalid environment variable %q: use a nonempty name/value; remove the key to delete it", key)
		}
	}
	// Neon merges environment patches; an empty value is a deletion tombstone.
	for _, key := range previous {
		if _, keep := env[key]; !keep {
			env[key] = ""
		}
	}
	return env, nil
}

func (r *functionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	if plan.ZipFilePath.IsNull() {
		resp.Diagnostics.AddError("Missing Function ZIP", "zip_file_path is required when creating a function.")
		return
	}
	ctx, cancel := functionContext(ctx, plan.DeploymentTimeout)
	defer cancel()
	_, err := r.client.getFunction(ctx, plan.ProjectID.ValueString(), plan.BranchID.ValueString(), plan.Slug.ValueString())
	if err == nil {
		resp.Diagnostics.AddError("Function Already Exists", "Import the existing function before managing it with Terraform.")
		return
	}
	if !apiNotFound(err) {
		resp.Diagnostics.AddError("Unable to Check Function", err.Error())
		return
	}
	fn, accepted, err := r.deploy(ctx, &plan, true)
	if accepted {
		// Retain identity on an asynchronous failure so destroy/import/retry work.
		if err != nil {
			plan.SourceCodeHash = types.StringValue("")
		}
		if stateErr := setFunctionModel(ctx, &plan, fn); stateErr != nil {
			resp.Diagnostics.AddError("Unable to Set Function State", stateErr.Error())
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Deploy Function", err.Error())
		return
	}
	if err := r.rename(ctx, &plan, req.Plan); err != nil {
		resp.Diagnostics.AddError("Unable to Rename Function", err.Error())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func functionContext(ctx context.Context, timeout types.String) (context.Context, context.CancelFunc) {
	d, err := time.ParseDuration(timeout.ValueString())
	if err != nil || d <= 0 {
		d = 10 * time.Minute
	}
	return context.WithTimeout(ctx, d)
}

func (r *functionResource) deploy(ctx context.Context, plan *functionModel, upload bool) (neon.NeonFunction, bool, error) {
	var archive []byte
	if upload {
		var hash string
		var err error
		archive, hash, err = readFunctionArchive(plan.ZipFilePath.ValueString())
		if err != nil {
			return neon.NeonFunction{}, false, err
		}
		if plan.SourceCodeHash.ValueString() != hash {
			return neon.NeonFunction{}, false, errors.New("function ZIP changed after planning; run terraform plan again")
		}
	}
	previous := []string{}
	if !plan.EnvironmentVariableNames.IsNull() && !plan.EnvironmentVariableNames.IsUnknown() {
		if diags := plan.EnvironmentVariableNames.ElementsAs(ctx, &previous, false); diags.HasError() {
			return neon.NeonFunction{}, false, errors.New("invalid environment names in state")
		}
	}
	env, err := functionEnvironment(ctx, plan.EnvironmentVariables, previous)
	if err != nil {
		return neon.NeonFunction{}, false, err
	}
	d, err := r.client.deployFunction(ctx, plan.ProjectID.ValueString(), plan.BranchID.ValueString(), plan.Slug.ValueString(), archive, env, plan.Runtime.ValueString())
	if err != nil {
		return neon.NeonFunction{}, false, err
	}
	initial := neon.NeonFunction{Slug: plan.Slug.ValueString(), Name: plan.Slug.ValueString(), CurrentDeployment: &neon.NeonFunctionCurrentDeployment{NeonFunctionDeployment: d}}
	if d.Status == neon.NeonFunctionDeploymentStatusFailed {
		return initial, true, fmt.Errorf("deployment %d failed: %v", d.ID, stringOrEmpty(d.Error))
	}
	interval := r.pollInterval
	if interval <= 0 {
		interval = time.Second
	}
	fn, err := r.client.waitFunction(ctx, plan.ProjectID.ValueString(), plan.BranchID.ValueString(), plan.Slug.ValueString(), d.ID, interval)
	if fn.ID == "" {
		fn = initial
	}
	return fn, true, err
}

// Neon may reuse the slug as the function ID. Compare the function's creation
// time as another generation signal, accepting equivalent timestamp encodings.
func functionCreatedAtChanged(previous, current types.String) bool {
	if previous.IsNull() || previous.IsUnknown() || previous.ValueString() == "" || previous.Equal(current) {
		return false
	}
	previousTime, previousErr := time.Parse(time.RFC3339Nano, previous.ValueString())
	currentTime, currentErr := time.Parse(time.RFC3339Nano, current.ValueString())
	if previousErr != nil || currentErr != nil {
		return true
	}
	return !previousTime.Equal(currentTime)
}

func (r *functionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	fn, err := r.client.getFunction(ctx, state.ProjectID.ValueString(), state.BranchID.ValueString(), state.Slug.ValueString())
	if apiNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Function", err.Error())
		return
	}
	previousID, previousNames, previousFunctionID := state.CurrentDeploymentID, state.EnvironmentVariableNames, state.FunctionID
	previousCreatedAt := state.CreatedAt
	if err := setFunctionModel(ctx, &state, fn); err != nil {
		resp.Diagnostics.AddError("Unable to Set Function State", err.Error())
		return
	}
	drift := (!previousID.IsNull() && !previousID.Equal(state.CurrentDeploymentID)) || (!previousFunctionID.IsNull() && !previousFunctionID.Equal(state.FunctionID)) || functionCreatedAtChanged(previousCreatedAt, state.CreatedAt)
	if !state.EnvironmentVariables.IsNull() {
		names := make([]string, 0, len(state.EnvironmentVariables.Elements()))
		for key := range state.EnvironmentVariables.Elements() {
			names = append(names, key)
		}
		desired, diags := types.SetValueFrom(ctx, types.StringType, names)
		resp.Diagnostics.Append(diags...)
		if drift || !desired.Equal(state.EnvironmentVariableNames) {
			state.EnvironmentVariables = types.MapNull(types.StringType)
			drift = true
		}
	}
	if !previousNames.IsNull() && !previousNames.Equal(state.EnvironmentVariableNames) {
		drift = true
	}
	if drift && !state.ZipFilePath.IsNull() {
		state.SourceCodeHash = types.StringValue("")
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *functionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := functionContext(ctx, plan.DeploymentTimeout)
	defer cancel()
	upload := !plan.ZipFilePath.IsNull() && (!plan.SourceCodeHash.Equal(state.SourceCodeHash) || !plan.ZipFilePath.Equal(state.ZipFilePath))
	if upload || !plan.EnvironmentVariables.Equal(state.EnvironmentVariables) || !plan.Runtime.Equal(state.Runtime) {
		// Get names fresh: imported functions and out-of-band additions also need
		// deletion tombstones when the configured map is authoritative.
		fn, err := r.client.getFunction(ctx, state.ProjectID.ValueString(), state.BranchID.ValueString(), state.Slug.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Read Function Before Deploy", err.Error())
			return
		}
		remote := state
		if err := setFunctionModel(ctx, &remote, fn); err != nil {
			resp.Diagnostics.AddError("Unable to Set Function State", err.Error())
			return
		}
		plan.EnvironmentVariableNames = remote.EnvironmentVariableNames
		fn, accepted, err := r.deploy(ctx, &plan, upload)
		if err != nil {
			if accepted {
				if stateErr := setFunctionModel(ctx, &state, fn); stateErr != nil {
					resp.Diagnostics.AddError("Unable to Set Function State", stateErr.Error())
				}
				if !state.ZipFilePath.IsNull() {
					state.SourceCodeHash = types.StringValue("")
				}
				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			}
			resp.Diagnostics.AddError("Unable to Deploy Function", err.Error())
			return
		}
		if err := setFunctionModel(ctx, &plan, fn); err != nil {
			resp.Diagnostics.AddError("Unable to Set Function State", err.Error())
			return
		}
	} else {
		fn, err := r.client.getFunction(ctx, state.ProjectID.ValueString(), state.BranchID.ValueString(), state.Slug.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Read Function", err.Error())
			return
		}
		if err := setFunctionModel(ctx, &plan, fn); err != nil {
			resp.Diagnostics.AddError("Unable to Set Function State", err.Error())
			return
		}
	}
	if err := r.rename(ctx, &plan, req.Plan); err != nil {
		resp.Diagnostics.AddError("Unable to Rename Function", err.Error())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *functionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err == nil {
		err = client.DeleteProjectBranchFunction(url.PathEscape(state.ProjectID.ValueString()), url.PathEscape(state.BranchID.ValueString()), url.PathEscape(state.Slug.ValueString()))
	}
	if err != nil && !apiNotFound(err) {
		resp.Diagnostics.AddError("Unable to Delete Function", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *functionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitBackendID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Function Import ID", err.Error())
		return
	}
	state := functionModel{ID: types.StringValue(req.ID), ProjectID: types.StringValue(parts[0]), BranchID: types.StringValue(parts[1]), Slug: types.StringValue(parts[2]), EnvironmentVariables: types.MapNull(types.StringType), EnvironmentVariableNames: types.SetNull(types.StringType), DeploymentTimeout: types.StringValue("10m")}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
