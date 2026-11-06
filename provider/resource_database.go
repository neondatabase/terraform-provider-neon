package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	schemaTFSDK "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	typesTFSDK "github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonDatabase)(nil)
var _ resource.ResourceWithUpgradeState = (*neonDatabase)(nil)
var _ resource.ResourceWithImportState = (*neonDatabase)(nil)

type neonDatabase struct {
	client *neon.Client
}

type neonDatabaseResourceModel struct {
	ID        typesTFSDK.String `tfsdk:"id"`
	ProjectID typesTFSDK.String `tfsdk:"project_id"`
	BranchID  typesTFSDK.String `tfsdk:"branch_id"`
	Name      typesTFSDK.String `tfsdk:"name"`
	OwnerName typesTFSDK.String `tfsdk:"owner_name"`
}

func (m *neonDatabaseResourceModel) inferAttr(db neon.Database) {
	m.ID = typesTFSDK.StringValue(m.ProjectID.ValueString() + "/" + db.BranchID + "/" + db.Name)
	m.Name = typesTFSDK.StringValue(db.Name)
	m.OwnerName = typesTFSDK.StringValue(db.OwnerName)
}

func NewNeonDatabaseResource() resource.Resource {
	return &neonDatabase{}
}

func (r *neonDatabase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *providerAdapter, got an unexpected type.")
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	r.client = client.sdk
}

func (r neonDatabase) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_database"
}

func (r neonDatabase) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schemaTFSDK.Schema{
		Version:     8,
		Description: `Project Database. See details: https://neon.tech/docs/manage/databases/`,
		Attributes: map[string]schemaTFSDK.Attribute{
			"id": schemaTFSDK.StringAttribute{
				Computed:    true,
				Description: "The database ID.",
			},
			"project_id": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Project ID.",
			},
			"branch_id": schemaTFSDK.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Branch ID.",
			},
			"name": schemaTFSDK.StringAttribute{
				Required:    true,
				Description: "Database name.",
			},
			"owner_name": schemaTFSDK.StringAttribute{
				Required:    true,
				Description: "Database owner name.",
			},
		},
	}
}

func (r neonDatabase) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.DatabaseCreateRequest{
		Database: neon.DatabaseCreateRequestDatabase{
			Name:      state.Name.ValueString(),
			OwnerName: state.OwnerName.ValueString(),
		},
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		re, err := r.client.CreateProjectBranchDatabase(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			cfg)
		if err != nil {
			return err
		}
		if err := waitUnfinishedOperations(ctx, r.client, re.OperationsResponse.Operations); err != nil {
			return err
		}
		db = re.DatabaseResponse.Database
		return nil
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonDatabase) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		dbResp, err := r.client.GetProjectBranchDatabase(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Name.ValueString())
		db = dbResp.Database
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)

	if resp.Diagnostics.HasError() {
		return
	}

	if db.BranchID == "" {
		resp.Diagnostics.AddWarning("database not found",
			fmt.Sprintf("database %q not found in the project/branch \"%s/%s\"", state.Name.ValueString(),
				state.ProjectID.ValueString(), state.BranchID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonDatabase) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.DatabaseUpdateRequest{}
	if !state.Name.Equal(plan.Name) {
		cfg.Database.Name = plan.Name.ValueStringPointer()
	}
	if !state.OwnerName.Equal(plan.OwnerName) {
		cfg.Database.OwnerName = plan.OwnerName.ValueStringPointer()
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		dbResp, err := r.client.UpdateProjectBranchDatabase(state.ProjectID.ValueString(),
			state.BranchID.ValueString(), state.Name.ValueString(), cfg)
		if err := waitUnfinishedOperations(ctx, r.client, dbResp.OperationsResponse.Operations); err != nil {
			return err
		}
		db = dbResp.Database
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	if db.BranchID == "" {
		resp.Diagnostics.AddWarning("database not found",
			fmt.Sprintf("database %q not found in the project/branch \"%s/%s\"", state.Name.ValueString(),
				state.ProjectID.ValueString(), state.BranchID.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r neonDatabase) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		re, err := r.client.DeleteProjectBranchDatabase(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Name.ValueString())
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

func (r neonDatabase) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 3)
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Terraform State Database ID",
			"Expected an import ID in the form <project_id>/<neon_branch_id>/<database_name>.",
		)
		return
	}

	projectID := els[0]
	branchID := els[1]
	databaseName := els[2]
	if !isValidBranchID(branchID) {
		resp.Diagnostics.AddError("provided Neon Branch ID is not valid", "")
		return
	}

	var db neon.Database
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		dbResp, err := r.client.GetProjectBranchDatabase(projectID, branchID, databaseName)
		db = dbResp.Database
		return err
	}, nil)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if db.BranchID == "" {
		resp.Diagnostics.AddWarning("database not found",
			fmt.Sprintf("database %q not found in the project/branch \"%s/%s\"", databaseName, projectID, branchID))
		return
	}

	var state neonDatabaseResourceModel
	state.ProjectID = typesTFSDK.StringValue(projectID)
	state.BranchID = typesTFSDK.StringValue(branchID)
	state.inferAttr(db)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// UpgradeState migrates state written by kislerdm/neon v0.18.0, the last Terraform Plugin SDK v2 release.
func (r neonDatabase) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		7: legacyStateUpgrader(r.Schema, nil),
	}
}
