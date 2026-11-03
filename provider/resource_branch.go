package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonBranchResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonBranchResource)(nil)
var _ resource.ResourceWithImportState = (*neonBranchResource)(nil)

type neonBranchResource struct {
	client *neon.Client
}

type neonBranchResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ProjectID       types.String `tfsdk:"project_id"`
	Name            types.String `tfsdk:"name"`
	ParentID        types.String `tfsdk:"parent_id"`
	ParentLsn       types.String `tfsdk:"parent_lsn"`
	ParentTimestamp types.Int64  `tfsdk:"parent_timestamp"`
	LogicalSize     types.Int64  `tfsdk:"logical_size"`
	Protected       types.Bool   `tfsdk:"protected"`
	Primary         types.Bool   `tfsdk:"primary"`
	Default         types.Bool   `tfsdk:"default"`
	// TODO: add annotations as map[string]any
}

func (v *neonBranchResourceModel) inferAttr(branch neon.Branch) {
	v.ID = types.StringValue(branch.ID)
	v.ProjectID = types.StringValue(branch.ProjectID)
	v.Name = types.StringValue(branch.Name)
	v.ParentID = types.StringPointerValue(branch.ParentID)
	v.ParentLsn = types.StringPointerValue(branch.ParentLsn)
	var parentTimestamp int64
	if branch.ParentTimestamp != nil {
		parentTimestamp = branch.ParentTimestamp.Unix()
	}
	v.ParentTimestamp = types.Int64Value(parentTimestamp)
	v.Protected = types.BoolValue(branch.Protected)
	v.LogicalSize = types.Int64PointerValue(branch.LogicalSize)
	v.Primary = types.BoolPointerValue(branch.Primary)
	v.Default = types.BoolValue(branch.Default)
}

func NewNeonBranchResource() resource.Resource {
	return &neonBranchResource{}
}

func (r *neonBranchResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_branch"
}

func (r *neonBranchResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Project Branch. See details: https://neon.tech/docs/introduction/branching/",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Branch ID.",
			},
			"project_id": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
				Description: "Project ID.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Branch name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"parent_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Parent branch ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"parent_lsn": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Parent branch Log Sequence Number (LSN).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"parent_timestamp": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Validators:  []validator.Int64{notNegativeInt64},
				Description: "Parent branch point-in-time timestamp in Unix seconds. Zero when not provided by the API.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"logical_size": schema.Int64Attribute{
				Computed:    true,
				Description: "Branch logical size in bytes.",
			},
			"protected": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Protected branch flag.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"primary": schema.BoolAttribute{
				Computed:           true,
				Description:        "Primary branch flag.",
				DeprecationMessage: "Use default instead.",
			},
			"default": schema.BoolAttribute{
				Computed:    true,
				Description: "Default branch flag.",
			},
		},
	}
}

func (r *neonBranchResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonBranchResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBranchResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := &neon.CreateProjectBranchCfg{
		BranchCreateRequest: neon.BranchCreateRequest{
			Branch: &neon.BranchCreateRequestBranch{
				Name:      state.Name.ValueStringPointer(),
				ParentID:  state.ParentID.ValueStringPointer(),
				ParentLsn: state.ParentLsn.ValueStringPointer(),
				Protected: state.Protected.ValueBoolPointer(),
			},
		},
	}

	if state.ParentTimestamp.ValueInt64() != 0 {
		parentTimestamp := time.Unix(state.ParentTimestamp.ValueInt64(), 0)
		cfg.BranchCreateRequest.Branch.ParentTimestamp = &parentTimestamp
	}

	var branch neon.Branch
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(_ context.Context) error {
		re, err := r.client.CreateProjectBranch(state.ProjectID.ValueString(), cfg)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations)
		branch = re.BranchResponse.Branch
		return nil
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.inferAttr(branch)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBranchResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var branch neon.Branch
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		branchResp, err := r.client.GetProjectBranch(state.ProjectID.ValueString(), state.ID.ValueString())
		branch = branchResp.BranchResponse.Branch
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)

	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if branch.ID == "" {
		resp.Diagnostics.AddWarning("branch not found",
			fmt.Sprintf("branch %q not found in the project %q", state.ID.ValueString(),
				state.ProjectID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(branch)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBranchResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonBranchResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var state neonBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.BranchUpdateRequest{
		Branch: neon.BranchUpdateRequestBranch{
			Protected: plan.Protected.ValueBoolPointer(),
		},
	}
	if !state.Name.Equal(plan.Name) {
		cfg.Branch.Name = plan.Name.ValueStringPointer()
	}

	var branch neon.Branch
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		branchResp, err := r.client.UpdateProjectBranch(state.ProjectID.ValueString(), state.ID.ValueString(), cfg)
		waitUnfinishedOperations(ctx, r.client, branchResp.OperationsResponse.Operations)
		branch = branchResp.BranchResponse.Branch
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if branch.ID == "" {
		resp.Diagnostics.AddWarning("branch not found",
			fmt.Sprintf("branch %q not found in the project %q", state.ID.ValueString(),
				state.ProjectID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(branch)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBranchResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		re, err := r.client.DeleteProjectBranch(state.ProjectID.ValueString(), state.ID.ValueString())
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations)
		return nil
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)

	resp.State.RemoveResource(ctx)
}

func (r *neonBranchResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var planned neonBranchResourceModel
	var config neonBranchResourceModel
	var state neonBranchResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	parentLsnDefined := !config.ParentLsn.IsNull() && !config.ParentLsn.IsUnknown()
	parentTimestampDefined := !config.ParentTimestamp.IsNull() && !config.ParentTimestamp.IsUnknown()
	if parentLsnDefined && parentTimestampDefined {
		resp.Diagnostics.AddError("Invalid Configuration",
			"parent_lsn and parent_timestamp cannot be set together")
	}

	if !planned.ParentID.IsNull() && !planned.ParentID.IsUnknown() && !planned.ParentID.Equal(state.ParentID) {
		resp.RequiresReplace.Append(path.Root("parent_id"))
		resp.Diagnostics.AddWarning("the resource requires replacement", "parent_id has changed")
	}

	if !planned.ParentLsn.IsNull() && !planned.ParentLsn.IsUnknown() && !planned.ParentLsn.Equal(state.ParentLsn) {
		resp.RequiresReplace.Append(path.Root("parent_lsn"))
		resp.Diagnostics.AddWarning("the resource requires replacement", "parent_lsn has changed")
	}

	if !planned.ParentTimestamp.IsNull() && !planned.ParentTimestamp.IsUnknown() &&
		!planned.ParentTimestamp.Equal(state.ParentTimestamp) {
		resp.RequiresReplace.Append(path.Root("parent_timestamp"))
		resp.Diagnostics.AddWarning("the resource requires replacement", "parent_timestamp has changed")
	}
}

func (r *neonBranchResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 2)
	if len(els) != 2 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State Branch ID",
			"Expected an import ID in the form <project_id>/<neon_branch_id>.",
		)
		return
	}

	projectID := els[0]
	branchID := els[1]
	if !isValidBranchID(branchID) {
		resp.Diagnostics.AddError("provided Neon Branch ID is not valid", "")
		return
	}

	var branch neon.Branch
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
		branchResp, err := r.client.GetProjectBranch(projectID, branchID)
		branch = branchResp.BranchResponse.Branch
		return err
	}, ctx)...)

	if resp.Diagnostics.HasError() {
		resp.State.RemoveResource(ctx)
		return
	}

	if branch.ID == "" {
		resp.Diagnostics.AddWarning("branch not found",
			fmt.Sprintf("branch %q not found in the project %q", branchID, projectID))
		return
	}

	var state neonBranchResourceModel
	state.inferAttr(branch)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

type validatorNotNegativeInt64 struct{}

func (validatorNotNegativeInt64) ValidateInt64(_ context.Context, req validator.Int64Request,
	resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if req.ConfigValue.ValueInt64() < 0 {
		resp.Diagnostics.AddError("invalid value", "value must be a non-negative integer")
	}
}

func (validatorNotNegativeInt64) Description(context.Context) string {
	return "validates that provided int64 is not negative"
}

func (v validatorNotNegativeInt64) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

var notNegativeInt64 = validatorNotNegativeInt64{}

func isValidBranchID(s string) bool {
	const prefix = "br-"
	return strings.HasPrefix(s, prefix) && len(strings.TrimPrefix(s, prefix)) > 0
}
