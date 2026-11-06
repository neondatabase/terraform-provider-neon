package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
var _ resource.ResourceWithUpgradeState = (*neonBranchResource)(nil)
var _ resource.ResourceWithConfigValidators = (*neonBranchResource)(nil)
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
		Version:     9,
		Description: "Project Branch. See details: https://neon.tech/docs/introduction/branching/",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "Branch ID.",
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
					stringplanmodifier.RequiresReplace(),
				},
			},
			"parent_lsn": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Parent branch Log Sequence Number (LSN).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"parent_timestamp": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Validators:  []validator.Int64{notNegativeInt64},
				Description: "Parent branch point-in-time timestamp in Unix seconds. Zero when not provided by the API.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplace(),
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

	var created neon.CreatedBranch
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		created, err = r.client.CreateProjectBranch(state.ProjectID.ValueString(), cfg)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.inferAttr(created.BranchResponse.Branch)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	resp.Diagnostics.Append(newDiagnostics(waitUnfinishedOperations(ctx, r.client, created.OperationsResponse.Operations))...)
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
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		branchResp, err := r.client.GetProjectBranch(state.ProjectID.ValueString(), state.ID.ValueString())
		branch = branchResp.BranchResponse.Branch
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)

	if resp.Diagnostics.HasError() {
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
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		branchResp, err := r.client.UpdateProjectBranch(state.ProjectID.ValueString(), state.ID.ValueString(), cfg)
		if err := waitUnfinishedOperations(ctx, r.client, branchResp.OperationsResponse.Operations); err != nil {
			return err
		}
		branch = branchResp.BranchResponse.Branch
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
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

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		re, err := r.client.DeleteProjectBranch(state.ProjectID.ValueString(), state.ID.ValueString())
		if err != nil {
			return err
		}
		if err := waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations); err != nil {
			return err
		}
		return nil
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *neonBranchResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(path.MatchRoot("parent_lsn"), path.MatchRoot("parent_timestamp")),
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
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		branchResp, err := r.client.GetProjectBranch(projectID, branchID)
		branch = branchResp.BranchResponse.Branch
		return err
	}, nil)...)

	if resp.Diagnostics.HasError() {
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

// UpgradeState migrates state written by kislerdm/neon v0.18.0, the last Terraform Plugin SDK v2 release.
func (r *neonBranchResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		8: legacyStateUpgrader(r.Schema, func(attrs map[string]any) {
			legacyYesNoToBool(attrs, "protected")
		}),
	}
}
