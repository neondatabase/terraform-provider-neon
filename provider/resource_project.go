package provider

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int32planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonProjectResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonProjectResource)(nil)
var _ resource.ResourceWithImportState = (*neonProjectResource)(nil)

func NewNeonProjectResource() resource.Resource {
	return &neonProjectResource{}
}

func findDefaultDatabase(databases []neon.Database, defaultBranchID string) neon.Database {
	o := neon.Database{}

	if len(databases) > 0 {
		if len(databases) > 0 {
			var dbs []neon.Database
			for _, el := range databases {
				if el.BranchID == defaultBranchID {
					dbs = append(dbs, el)
				}
			}

			// select the default database based on the creation timestamp
			slices.SortStableFunc(dbs, func(a, b neon.Database) int {
				return a.CreatedAt.Compare(b.CreatedAt)
			})

			o = dbs[0]
		}
	}

	return o
}

func findDefaultEndpoint(endpoints []neon.Endpoint, defaultBranchID string) neon.Endpoint {
	o := neon.Endpoint{}

	if len(endpoints) > 0 {
		var eps []neon.Endpoint
		for _, el := range endpoints {
			// the default endpoint can only be of read_write type
			if !el.Disabled && el.Type == neon.EndpointTypeReadWrite && el.BranchID == defaultBranchID {
				eps = append(eps, el)
			}
		}

		// select the default endpoint based on the creation timestamp
		slices.SortStableFunc(eps, func(a, b neon.Endpoint) int {
			return a.CreatedAt.Compare(b.CreatedAt)
		})

		o = eps[0]
	}

	return o
}

type neonProjectResourceModel struct {
	ID       types.String `tfsdk:"id"`
	OrgID    types.String `tfsdk:"org_id"`
	Name     types.String `tfsdk:"name"`
	RegionID types.String `tfsdk:"region_id"`

	PgVersion types.Int32 `tfsdk:"pg_version"`

	StorePassword types.Bool `tfsdk:"store_password"`

	AllowedIPs                      types.List `tfsdk:"allowed_ips"`
	AllowedIPsProtectedBranchesOnly types.Bool `tfsdk:"allowed_ips_protected_branches_only"`

	ComputeProvisioner    types.String  `tfsdk:"compute_provisioner"`
	AutoscalingLimitMaxCU types.Float64 `tfsdk:"autoscaling_limit_max_cu"`
	AutoscalingLimitMinCU types.Float64 `tfsdk:"autoscaling_limit_min_cu"`
	SuspendTimeoutSeconds types.Int64   `tfsdk:"suspend_timeout_seconds"`

	HistoryRetentionSeconds  types.Int32 `tfsdk:"history_retention_seconds"`
	EnableLogicalReplication types.Bool  `tfsdk:"enable_logical_replication"`

	BlockPublicConnections types.Bool `tfsdk:"block_public_connections"`
	BlockVPCConnections    types.Bool `tfsdk:"block_vpc_connections"`

	DatabaseName types.String `tfsdk:"database_name"`
	DatabaseUser types.String `tfsdk:"database_user"`

	EnabledLibraries            types.List `tfsdk:"enabled_libraries"`
	EnabledLibrariesUseDefaults types.Bool `tfsdk:"enabled_libraries_use_defaults"`

	Hipaa types.Bool `tfsdk:"hipaa"`

	MaintenanceWindow *maintenanceWindow         `tfsdk:"maintenance_window"`
	Quota             *projectQuota              `tfsdk:"quota"`
	Branch            *neonBranchResourceModel   `tfsdk:"branch"`
	PrimaryCompute    *neonEndpointResourceModel `tfsdk:"primary_compute"`

	// 	TODO: add pgSettings (?) verify for bug in API behaviour
	// 	TODO: add audit level

	// TODO: delete after the off. release: it's here for BW compatibility only
	DefaultBranchID types.String `tfsdk:"default_branch_id"`
	// TODO: delete after the off. release: it's here for BW compatibility only
	DefaultBranchProtected types.Bool `tfsdk:"default_branch_protected"`
	// TODO: delete after the off. release: it's here for BW compatibility only
	DefaultEndpointID types.String `tfsdk:"default_endpoint_id"`
	// TODO: delete after the off. release: it's here for BW compatibility only
	DatabaseHost types.String `tfsdk:"database_host"`
	// TODO: delete after the off. release: it's here for BW compatibility only
	DatabaseHostPooler types.String `tfsdk:"database_host_pooler"`
}

func (v neonProjectResourceModel) maintenanceWindow(ctx context.Context, diagnostics *diag.Diagnostics) *neon.MaintenanceWindow {
	if v.MaintenanceWindow == nil {
		return nil
	}
	var tmp []int32
	diagnostics.Append(v.MaintenanceWindow.Weekdays.ElementsAs(ctx, &tmp, false)...)
	if diagnostics.HasError() {
		return nil
	}

	o := &neon.MaintenanceWindow{
		StartTime: v.MaintenanceWindow.StartTime.ValueString(),
		EndTime:   v.MaintenanceWindow.EndTime.ValueString(),
		Weekdays:  make([]int, 0, len(tmp)),
	}
	for _, el := range tmp {
		o.Weekdays = append(o.Weekdays, int(el))
	}
	return o
}

func (v neonProjectResourceModel) quota(_ context.Context, _ *diag.Diagnostics) *neon.ProjectQuota {
	if v.Quota == nil {
		return nil
	}
	return &neon.ProjectQuota{
		ActiveTimeSeconds:  v.Quota.ActiveTimeSeconds.ValueInt64Pointer(),
		ComputeTimeSeconds: v.Quota.ComputeTimeSeconds.ValueInt64Pointer(),
		DataTransferBytes:  v.Quota.DataTransferBytes.ValueInt64Pointer(),
		LogicalSizeBytes:   v.Quota.LogicalSizeBytes.ValueInt64Pointer(),
		WrittenDataBytes:   v.Quota.WrittenDataBytes.ValueInt64Pointer(),
	}
}

func (v *neonProjectResourceModel) inferAttr(ctx context.Context, project neon.Project, branch neon.Branch,
	endpoint neon.Endpoint, database neon.Database, diagnostics *diag.Diagnostics) {
	v.ID = types.StringValue(project.ID)
	v.Name = types.StringValue(project.Name)
	v.OrgID = types.StringPointerValue(project.OrgID)
	v.RegionID = types.StringValue(project.RegionID)
	v.ComputeProvisioner = types.StringValue(string(project.Provisioner))
	v.PgVersion = types.Int32Value(int32(project.PgVersion))
	v.StorePassword = types.BoolValue(project.StorePasswords)
	v.HistoryRetentionSeconds = types.Int32Value(project.HistoryRetentionSeconds)

	v.EnableLogicalReplication = types.BoolNull()
	v.BlockPublicConnections = types.BoolNull()
	v.BlockVPCConnections = types.BoolNull()
	v.EnableLogicalReplication = types.BoolNull()
	v.Hipaa = types.BoolNull()

	v.AllowedIPsProtectedBranchesOnly = types.BoolNull()
	previousAllowedIPs := v.AllowedIPs
	v.AllowedIPs = types.ListNull(types.StringType)

	v.EnabledLibraries = types.ListNull(types.StringType)
	v.EnabledLibrariesUseDefaults = types.BoolNull()

	initializeProjectBlocks(v)

	if project.Settings != nil {
		settings := project.Settings

		v.Hipaa = types.BoolPointerValue(settings.Hipaa)
		v.EnableLogicalReplication = types.BoolPointerValue(settings.EnableLogicalReplication)
		v.BlockPublicConnections = types.BoolPointerValue(settings.BlockPublicConnections)
		v.BlockVPCConnections = types.BoolPointerValue(settings.BlockVpcConnections)
		v.EnableLogicalReplication = types.BoolPointerValue(settings.EnableLogicalReplication)

		var d diag.Diagnostics
		if settings.AllowedIps != nil {
			v.AllowedIPsProtectedBranchesOnly = types.BoolPointerValue(settings.AllowedIps.ProtectedBranchesOnly)
			if len(settings.AllowedIps.Ips) > 0 {
				v.AllowedIPs, d = types.ListValueFrom(ctx, types.StringType, settings.AllowedIps.Ips)
				diagnostics.Append(d...)
			}
		}

		// The API represents both an omitted allowlist and an explicit [] as empty.
		// Preserve a known list as [], while preserving a null manifest value as null.
		if v.AllowedIPs.IsNull() &&
			!previousAllowedIPs.IsNull() &&
			!previousAllowedIPs.IsUnknown() {
			v.AllowedIPs, d = types.ListValueFrom(ctx, types.StringType, []string{})
			diagnostics.Append(d...)
		}

		if v.Quota != nil && settings.Quota != nil {
			v.Quota = &projectQuota{
				ActiveTimeSeconds:  types.Int64PointerValue(settings.Quota.ActiveTimeSeconds),
				ComputeTimeSeconds: types.Int64PointerValue(settings.Quota.ComputeTimeSeconds),
				DataTransferBytes:  types.Int64PointerValue(settings.Quota.DataTransferBytes),
				LogicalSizeBytes:   types.Int64PointerValue(settings.Quota.LogicalSizeBytes),
				WrittenDataBytes:   types.Int64PointerValue(settings.Quota.WrittenDataBytes),
			}
		}

		if settings.PreloadLibraries != nil {
			v.EnabledLibrariesUseDefaults = types.BoolPointerValue(settings.PreloadLibraries.UseDefaults)
			if len(settings.PreloadLibraries.EnabledLibraries) > 0 {
				v.EnabledLibraries, d = types.ListValueFrom(ctx, types.StringType, settings.PreloadLibraries.EnabledLibraries)
				diagnostics.Append(d...)
			}
		}

		if settings.MaintenanceWindow != nil {
			v.MaintenanceWindow = &maintenanceWindow{
				StartTime: types.StringValue(settings.MaintenanceWindow.StartTime),
				EndTime:   types.StringValue(settings.MaintenanceWindow.EndTime),
			}
			weekdays := make([]int32, len(settings.MaintenanceWindow.Weekdays))
			for i, weekday := range settings.MaintenanceWindow.Weekdays {
				weekdays[i] = int32(weekday)
			}
			v.MaintenanceWindow.Weekdays, d = types.ListValueFrom(ctx, types.Int32Type, weekdays)
			diagnostics.Append(d...)
		}
	}

	// default compute a/k/a endpoints settings
	v.AutoscalingLimitMinCU = types.Float64Null()
	v.AutoscalingLimitMaxCU = types.Float64Null()
	v.SuspendTimeoutSeconds = types.Int64Null()
	if project.DefaultEndpointSettings != nil {
		settings := project.DefaultEndpointSettings
		if settings.AutoscalingLimitMaxCu != nil {
			v.AutoscalingLimitMaxCU = types.Float64Value(float64(*settings.AutoscalingLimitMaxCu))
		}
		if settings.AutoscalingLimitMinCu != nil {
			v.AutoscalingLimitMinCU = types.Float64Value(float64(*settings.AutoscalingLimitMinCu))
		}
		if settings.SuspendTimeoutSeconds != nil {
			v.SuspendTimeoutSeconds = types.Int64Value(int64(*settings.SuspendTimeoutSeconds))
		}
	}

	v.Branch = &neonBranchResourceModel{}
	v.Branch.inferAttr(branch)
	v.DefaultBranchID = v.Branch.ID
	v.DefaultBranchProtected = v.Branch.Protected

	v.PrimaryCompute = &neonEndpointResourceModel{}
	v.PrimaryCompute.inferAttr(ctx, endpoint, diagnostics)
	v.DefaultEndpointID = v.PrimaryCompute.ID
	v.DatabaseHost = v.PrimaryCompute.Host
	v.DatabaseHostPooler = v.PrimaryCompute.HostPooler

	v.DatabaseName = types.StringValue(database.Name)
	v.DatabaseUser = types.StringValue(database.OwnerName)
}

func initializeProjectBlocks(state *neonProjectResourceModel) {
	if state.MaintenanceWindow == nil {
		state.MaintenanceWindow = &maintenanceWindow{
			Weekdays: types.ListNull(types.Int32Type),
		}
	}
	if state.Quota == nil {
		state.Quota = &projectQuota{}
	}
	if state.Branch == nil {
		state.Branch = &neonBranchResourceModel{}
	}
	if state.PrimaryCompute == nil {
		state.PrimaryCompute = &neonEndpointResourceModel{
			EnabledLibraries: types.ListNull(types.StringType),
		}
	}
}

type maintenanceWindow struct {
	Weekdays  types.List   `tfsdk:"weekdays"`
	StartTime types.String `tfsdk:"start_time"`
	EndTime   types.String `tfsdk:"end_time"`
}

type validatorMaintenanceWindow struct{}

func (v *validatorMaintenanceWindow) Description(_ context.Context) string {
	return "Maintenance window's validator."
}

func (v *validatorMaintenanceWindow) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v *validatorMaintenanceWindow) ValidateObject(ctx context.Context, req validator.ObjectRequest,
	resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	var window maintenanceWindow
	resp.Diagnostics.Append(req.ConfigValue.As(ctx, &window, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	var isValidTime = func(s string) bool {
		_, err := time.Parse("15:04", s)
		return err == nil
	}
	errMsg := "undefined maintenance window"
	if window.Weekdays.IsNull() || window.Weekdays.IsUnknown() {
		resp.Diagnostics.AddError(errMsg, "undefined weekdays, it must be a list of values between 1 and 7")
	}
	var weekdays []int32
	resp.Diagnostics.Append(window.Weekdays.ElementsAs(ctx, &weekdays, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	switch {
	case len(weekdays) > 7 || len(weekdays) < 1:
		resp.Diagnostics.AddError(errMsg, "invalid number of weekdays, it must be between 1 and 7")
	default:
		for j, weekday := range weekdays {
			switch weekday {
			case 1, 2, 3, 4, 5, 6, 7:
			default:
				resp.Diagnostics.AddError(errMsg,
					fmt.Sprintf("invalid weekday %d, the value must be from 1 (Mon.) to 7 (Sun.)", j))
			}
		}
	}
	if window.StartTime.IsUnknown() || window.StartTime.IsNull() {
		resp.Diagnostics.AddError(errMsg, "start_time is required")
	}
	if window.EndTime.IsUnknown() || window.EndTime.IsNull() {
		resp.Diagnostics.AddError(errMsg, "end_time is required")
	}

	if !isValidTime(window.StartTime.ValueString()) {
		resp.Diagnostics.AddError(errMsg, "invalid start_time, it must be in the format of \"HH:MM\"")
	}
	if !isValidTime(window.EndTime.ValueString()) {
		resp.Diagnostics.AddError(errMsg, "invalid end_time, it must be in the format of \"HH:MM\"")
	}
}

type projectQuota struct {
	ActiveTimeSeconds  types.Int64 `tfsdk:"active_time_seconds"`
	ComputeTimeSeconds types.Int64 `tfsdk:"compute_time_seconds"`
	WrittenDataBytes   types.Int64 `tfsdk:"written_data_bytes"`
	DataTransferBytes  types.Int64 `tfsdk:"data_transfer_bytes"`
	LogicalSizeBytes   types.Int64 `tfsdk:"logical_size_bytes"`
}

type neonProjectResource struct {
	client          *neon.Client
	endpointHandler *neonEndpointResource
	branchHandler   *neonBranchResource
}

func (r neonProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_project"
}

func (r *neonProjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

	r.endpointHandler = &neonEndpointResource{}
	r.endpointHandler.Configure(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	r.branchHandler = &neonBranchResource{}
	r.branchHandler.Configure(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *neonProjectResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
}

type boolReplacerFromTrue struct{}

func (v boolReplacerFromTrue) Description(_ context.Context) string {
	return "Force replacement if the attribute is changed from true to false explicitly, or implicitly (attr. is removed from the manifest)."
}

func (v boolReplacerFromTrue) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v boolReplacerFromTrue) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest,
	resp *planmodifier.BoolResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || !req.StateValue.ValueBool() {
		return
	}
	if req.ConfigValue.IsUnknown() || (!req.ConfigValue.IsNull() && req.ConfigValue.ValueBool()) {
		return
	}

	resp.RequiresReplace = true
}

var requiresReplaceFromTrue = boolReplacerFromTrue{}

type setterIfUnknown struct {
	v bool
}

func (v setterIfUnknown) Description(_ context.Context) string {
	return fmt.Sprintf(`Modifies unknown value to "%v"`, v.v)
}

func (v setterIfUnknown) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v setterIfUnknown) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if req.ConfigValue.IsNull() && !req.StateValue.IsNull() &&
		!req.StateValue.IsUnknown() && req.StateValue.ValueBool() != v.v {
		resp.PlanValue = types.BoolValue(v.v)
	}
}

var setFalseIfUnknown = setterIfUnknown{v: false}

type validatorNonEmptyString struct{}

func (v validatorNonEmptyString) Description(_ context.Context) string {
	return "Checks if the string is empty"
}

func (v validatorNonEmptyString) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v validatorNonEmptyString) ValidateString(_ context.Context, req validator.StringRequest,
	resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.ConfigValue.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid value", "String cannot be empty")
	}
}

var validateNonEmptyString = validatorNonEmptyString{}

func (r neonProjectResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "Project ID.",
			Computed:    true,
		},
		"org_id": schema.StringAttribute{
			Description: "Identifier of the organisation to which this project belongs.",
			Computed:    true,
			Optional:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
				stringplanmodifier.RequiresReplace(),
			},
		},
		"name": schema.StringAttribute{
			Description: "Project name.",
			Optional:    true,
			Computed:    true,
			Validators: []validator.String{
				validateNonEmptyString,
			},
		},
		"region_id": schema.StringAttribute{
			Description: "Deployment region: https://neon.com/docs/introduction/regions",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
				stringplanmodifier.RequiresReplace(),
			},
		},
		"pg_version": schema.Int32Attribute{
			Description: "Postgres version",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Int32{
				int32planmodifier.UseStateForUnknown(),
				int32planmodifier.RequiresReplace(),
			},
		},
		"store_password": schema.BoolAttribute{
			Description: "Whether or not passwords are stored for roles in the Neon project.\nStoring passwords facilitates access to Neon features that require authorization.",
			Optional:    true,
			Computed:    true,
			Default:     booldefault.StaticBool(true),
			PlanModifiers: []planmodifier.Bool{
				boolplanmodifier.UseStateForUnknown(),
				boolplanmodifier.RequiresReplace(),
			},
		},
		"history_retention_seconds": schema.Int32Attribute{
			Description: "The number of seconds to retain the point-in-time restore (PITR) backup history for this project.\nDefault: 1 day, see https://neon.com/docs/reference/glossary#point-in-time-restore.",
			Optional:    true,
			Computed:    true,
		},
		"allowed_ips": schema.ListAttribute{
			Description: "A list of IP addresses that are allowed to connect to the endpoints.\nNote that the feature is available to the Neon Scale plans only. Details: https://neon.com/docs/manage/projects#configure-ip-allow",
			Optional:    true,
			ElementType: types.StringType,
		},
		"allowed_ips_protected_branches_only": schema.BoolAttribute{
			Description: "Apply the allow-list to the protected branches only.\nNote that the feature is available to the Neon Scale plans only.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				setFalseIfUnknown,
			},
		},
		"block_public_connections": schema.BoolAttribute{
			Description: "Block connections from public internet. This supersedes the AllowedIPs list.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				setFalseIfUnknown,
			},
		},
		"block_vpc_connections": schema.BoolAttribute{
			Description: "Block connections that use VPC endpoints.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				setFalseIfUnknown,
			},
		},
		"enable_logical_replication": schema.BoolAttribute{
			Description: "Sets wal_level=logical for all compute endpoints in this project.\nAll active endpoints will be suspended. See details: https://neon.com/docs/introduction/logical-replication\n**Note**: removal of this config from the manifest will correspond to the API call with the respective setting set to false.\n\n**Warning**: Once enabled, logical replication cannot be disabled.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				setFalseIfUnknown,
				requiresReplaceFromTrue,
			},
		},
		"database_name": schema.StringAttribute{
			Description: "Default database name.",
			Computed:    true,
			Optional:    true,
			Validators: []validator.String{
				validateNonEmptyString,
			},
		},
		"database_user": schema.StringAttribute{
			Description: "Default database role.",
			Computed:    true,
			Optional:    true,
			Validators: []validator.String{
				validateNonEmptyString,
			},
		},
		"autoscaling_limit_min_cu": schema.Float64Attribute{
			Description: "Minimal value of the project-wide autoscaling limit.\nNote that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Float64{
				float64planmodifier.UseStateForUnknown(),
			},
		},
		"autoscaling_limit_max_cu": schema.Float64Attribute{
			Description: "Maximal value of the project-wide autoscaling limit.\nNote that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Float64{
				float64planmodifier.UseStateForUnknown(),
			},
		},
		"suspend_timeout_seconds": schema.Int64Attribute{
			Description: "Project-wide scale-to-zero idle timeout, in seconds, before the compute suspends." +
				"\nNote that it is not retrospectively applicable, i.e., existing compute resources are not affected by this configuration.\n" +
				"   - `0` means the plan default (`300` sec.) applies;\n" +
				"   - `-1` means scale-to-zero is disabled (never suspends), can be set only on paid plans;\n" +
				"   - `604800` sec. (1 week) is the maximum.\n" +
				"   - `60` sec. (1 minute) is the minimum.\n" +
				"See details [here](https://neon.com/docs/manage/endpoints#scale-to-zero-configuration).",
			Optional:   true,
			Computed:   true,
			Default:    int64default.StaticInt64(0),
			Validators: []validator.Int64{validateSuspendTimeout},
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(),
			},
		},
		"compute_provisioner": schema.StringAttribute{
			Description:   "Project-wide compute provisioner. `k8s-neonvm` (default) supports Autoscaling; `k8s-pod` is fixed-size compute. Also `docker` and `serverless-platform`.",
			Optional:      true,
			Computed:      true,
			Default:       stringdefault.StaticString("k8s-neonvm"),
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			Validators:    []validator.String{validateComputeProvisioner},
		},
		"hipaa": schema.BoolAttribute{
			Description: "Enable HIPAA compliance for the project.\nNote that HIPAA must be configured for the organization first.\n\n**Warning**: Once enabled, HIPAA cannot be disabled.",
			Computed:    true,
			Optional:    true,
			PlanModifiers: []planmodifier.Bool{
				setFalseIfUnknown,
				requiresReplaceFromTrue,
			},
		},
		"enabled_libraries": schema.ListAttribute{
			Description: "Names of shared libraries preload by default when provisioning compute resources, i.e., endpoints.",
			Optional:    true,
			Computed:    true,
			ElementType: types.StringType,
		},
		"enabled_libraries_use_defaults": schema.BoolAttribute{
			Description: "When true, the project's preload libraries include the platform default set in addition to any libraries listed in `enabled_libraries`.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				setFalseIfUnknown,
			},
		},
		"default_branch_id": schema.StringAttribute{
			Description:        "Default branch ID.",
			DeprecationMessage: "Use the `id` attribute in the block `branch` instead.",
			Computed:           true,
		},
		"default_branch_protected": schema.BoolAttribute{
			Description:        "Set default branch as protected. **Note** that the default value is false.",
			DeprecationMessage: "Use the `protected` attribute in the block `branch` instead.",
			Optional:           true,
			Computed:           true,
			Default:            booldefault.StaticBool(false),
		},
		"default_endpoint_id": schema.StringAttribute{
			Description:        "Default endpoint ID.",
			DeprecationMessage: "Use the `id` attribute in the block `primary_compute` instead.",
			Computed:           true,
		},
		"database_host": schema.StringAttribute{
			Description:        "The hostname of the default compute endpoint.",
			DeprecationMessage: "Use the `host` attribute in the block `primary_compute` instead.",
			Computed:           true,
		},
		"database_host_pooler": schema.StringAttribute{
			Description:        "The hostname of the default compute endpoint accessible through the connection pool (pgbouncer).",
			DeprecationMessage: "Use the `host_pooler` attribute in the block `primary_compute` instead.",
			Computed:           true,
		},
	}

	endpointSchemaResp := resource.SchemaResponse{}
	endpointResource := &neonEndpointResource{}
	endpointResource.Schema(ctx, resource.SchemaRequest{}, &endpointSchemaResp)
	projectEndpointSchemaAttributes := maps.Clone(endpointSchemaResp.Schema.Attributes)
	for _, name := range []string{"project_id", "branch_id", "region_id", "type"} {
		if _, ok := projectEndpointSchemaAttributes[name]; ok {
			projectEndpointSchemaAttributes[name] =
				newReadOnlyResourceAttrFromResourceAttr(projectEndpointSchemaAttributes[name])
		}
	}

	branchSchemaResp := resource.SchemaResponse{}
	branchResource := &neonBranchResource{}
	branchResource.Schema(ctx, resource.SchemaRequest{}, &branchSchemaResp)
	projectBranchSchemaAttributes := maps.Clone(branchSchemaResp.Schema.Attributes)
	for _, name := range []string{"project_id"} {
		if _, ok := projectBranchSchemaAttributes[name]; ok {
			projectBranchSchemaAttributes[name] =
				newReadOnlyResourceAttrFromResourceAttr(projectBranchSchemaAttributes[name])
		}
	}

	blocks := map[string]schema.Attribute{
		"branch": schema.SingleNestedAttribute{
			Description: "Main branch provisioned with the project.",
			Optional:    true,
			Computed:    true,
			Attributes:  projectBranchSchemaAttributes,
			PlanModifiers: []planmodifier.Object{
				objectplanmodifier.UseStateForUnknown(),
			},
		},
		"primary_compute": schema.SingleNestedAttribute{
			Description: "Primary read-write compute resource provisioned with the project.",
			Optional:    true,
			Computed:    true,
			Attributes:  projectEndpointSchemaAttributes,
			PlanModifiers: []planmodifier.Object{
				objectplanmodifier.UseStateForUnknown(),
			},
		},
		"quota": schema.SingleNestedAttribute{
			Description: "Per-project consumption quota. If the quota is exceeded, all active computes\nare automatically suspended and it will not be possible to start them with\nan API method call or incoming proxy connections. The only exception is\nlogical_size_bytes, which is applied on per-branch basis, i.e., only the\ncompute on the branch that exceeds the logical_size quota will be suspended.\n\nQuotas are enforced based on per-project consumption metrics with the same names,\nwhich are reset at the end of each billing period (the first day of the month).\nLogical size is also an exception in this case, as it represents the total size\nof data stored in a branch, so it is not reset.\n\nThe zero value per attributed means 'unlimited'.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Object{
				objectplanmodifier.UseStateForUnknown(),
			},
			Attributes: map[string]schema.Attribute{
				"active_time_seconds": schema.Int64Attribute{
					Description: "The total amount of wall-clock time allowed to be spent by the project's compute endpoints.",
					Optional:    true,
					Computed:    true,
				},
				"compute_time_seconds": schema.Int64Attribute{
					Description: "The total amount of CPU seconds allowed to be spent by the project's compute endpoints.",
					Optional:    true,
					Computed:    true,
				},
				"written_data_bytes": schema.Int64Attribute{
					Description: "Total amount of data written to all of a project's branches.",
					Optional:    true,
					Computed:    true,
				},
				"data_transfer_bytes": schema.Int64Attribute{
					Description: "Total amount of data transferred from all of a project's branches using the proxy.",
					Optional:    true,
					Computed:    true,
				},
				"logical_size_bytes": schema.Int64Attribute{
					Description: "Limit on the logical size of every project's branch.",
					Optional:    true,
					Computed:    true,
				},
			},
		},
		"maintenance_window": schema.SingleNestedAttribute{
			Description: "A time period during which Neon may perform maintenance on the project's infrastructure. During this time, the project's compute endpoints may be unavailable and existing connections can be interrupted.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Object{
				objectplanmodifier.UseStateForUnknown(),
			},
			Validators: []validator.Object{&validatorMaintenanceWindow{}},
			Attributes: map[string]schema.Attribute{
				"weekdays": schema.ListAttribute{
					Description: "A list of weekdays when the maintenance window is active. Encoded as ints, where 1 - Monday, and 7 - Sunday.",
					ElementType: types.Int32Type,
					Optional:    true,
					Computed:    true,
				},
				"start_time": schema.StringAttribute{
					Description: "Start time of the maintenance window, in the format of \"HH:MM\". Uses UTC.",
					Optional:    true,
					Computed:    true,
				},
				"end_time": schema.StringAttribute{
					Description: "End time of the maintenance window, in the format of \"HH:MM\". Uses UTC.",
					Optional:    true,
					Computed:    true,
				},
			},
		},
	}
	maps.Copy(attrs, blocks)

	resp.Schema = schema.Schema{
		Description: `Neon Project.

Note that this resource manages five resources:

- Project: https://neon.com/docs/reference/glossary#project
- Root branch: https://neon.com/docs/reference/glossary#root-branch
- Primary compute with the default read-write endpoint: https://neon.com/docs/reference/glossary#compute
- Default database: https://neon.com/docs/reference/glossary#database
- Default role: https://neon.com/docs/reference/glossary#postgres-role
`,
		Attributes: attrs,
	}
}

func (r neonProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var project neon.Project
	var defaultBranch neon.Branch
	var defaultEndpoint neon.Endpoint
	var defaultDatabase neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		// read the project details
		result, err := r.client.GetProject(state.ID.ValueString())
		if err != nil {
			return err
		}
		project = result.Project

		// read the default branch details
		var cursor *string
		for {
			res, err := r.client.ListProjectBranches(project.ID, nil,
				&neon.ListProjectBranchesSortByCreatedAt, cursor, &neon.SortOrderParamAsc,
				nil, nil)
			if err != nil {
				return err
			}

			if res.CursorPaginationResponse.Pagination != nil {
				cursor = res.CursorPaginationResponse.Pagination.Next
			} else {
				cursor = nil
			}

			for _, br := range res.Branches {
				if br.Default {
					defaultBranch = br
					cursor = nil
					break
				}
			}

			if cursor == nil {
				break
			}
		}

		if defaultBranch.ID == "" {
			return nil
		}

		var errs = make(chan error, 2)
		// read default endpoint
		go func() {
			resEndpoints, err := r.client.ListProjectEndpoints(project.ID)
			if err != nil {
				errs <- err
				return
			}
			defaultEndpoint = findDefaultEndpoint(resEndpoints.Endpoints, defaultBranch.ID)
			errs <- nil
		}()

		// read default database
		go func() {
			resDatabases, err := r.client.ListProjectBranchDatabases(project.ID, defaultBranch.ID)
			if err != nil {
				errs <- err
				return
			}
			defaultDatabase = findDefaultDatabase(resDatabases.Databases, defaultBranch.ID)
			errs <- nil
		}()

		for i := 0; i < cap(errs); i++ {
			if e := <-errs; e != nil {
				return e
			}
		}

		return nil
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	if project.ID == "" {
		resp.State.RemoveResource(ctx)
		return
	}

	state.inferAttr(ctx, project, defaultBranch, defaultEndpoint, defaultDatabase, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r neonProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var cfg neon.ProjectCreateRequest
	if state.Name.ValueStringPointer() != nil && state.Name.ValueString() != "" {
		cfg.Project.Name = state.Name.ValueStringPointer()
	}
	if !state.OrgID.IsNull() && !state.OrgID.IsUnknown() && state.OrgID.ValueString() != "" {
		cfg.Project.OrgID = state.OrgID.ValueStringPointer()
	}
	if !state.RegionID.IsNull() && !state.RegionID.IsUnknown() && state.RegionID.ValueString() != "" {
		cfg.Project.RegionID = state.RegionID.ValueStringPointer()
	}
	if !state.PgVersion.IsNull() && !state.PgVersion.IsUnknown() && state.PgVersion.ValueInt32() != 0 {
		cfg.Project.PgVersion = pointer(neon.PgVersion(state.PgVersion.ValueInt32()))
	}
	if !state.StorePassword.IsNull() && !state.StorePassword.IsUnknown() {
		cfg.Project.StorePasswords = state.StorePassword.ValueBoolPointer()
	}
	if !state.HistoryRetentionSeconds.IsNull() && !state.HistoryRetentionSeconds.IsUnknown() {
		cfg.Project.HistoryRetentionSeconds = state.HistoryRetentionSeconds.ValueInt32Pointer()
	}
	if !state.ComputeProvisioner.IsNull() && !state.ComputeProvisioner.IsUnknown() {
		cfg.Project.Provisioner = pointer(neon.Provisioner(state.ComputeProvisioner.ValueString()))
	}

	// attributes of the cfg.Project.Settings
	if !state.EnableLogicalReplication.IsUnknown() && !state.EnableLogicalReplication.IsNull() {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.EnableLogicalReplication = state.EnableLogicalReplication.ValueBoolPointer()
	}
	if !state.BlockPublicConnections.IsUnknown() && !state.BlockPublicConnections.IsNull() {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.BlockPublicConnections = state.BlockPublicConnections.ValueBoolPointer()
	}
	if !state.BlockVPCConnections.IsUnknown() && !state.BlockVPCConnections.IsNull() {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.BlockVpcConnections = state.BlockVPCConnections.ValueBoolPointer()
	}
	if !state.Hipaa.IsUnknown() && !state.Hipaa.IsNull() && !state.Hipaa.ValueBool() {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.Hipaa = state.Hipaa.ValueBoolPointer()
	}
	if !state.AllowedIPs.IsNull() && !state.AllowedIPs.IsUnknown() {
		var allowedIps []string
		resp.Diagnostics.Append(state.AllowedIPs.ElementsAs(ctx, &allowedIps, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if len(allowedIps) > 0 {
			if cfg.Project.Settings == nil {
				cfg.Project.Settings = &neon.ProjectSettingsData{}
			}
			if cfg.Project.Settings.AllowedIps == nil {
				cfg.Project.Settings.AllowedIps = &neon.AllowedIps{}
			}
			cfg.Project.Settings.AllowedIps.Ips = allowedIps
		}
	}
	if !state.AllowedIPsProtectedBranchesOnly.IsNull() && !state.AllowedIPsProtectedBranchesOnly.IsUnknown() {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		if cfg.Project.Settings.AllowedIps == nil {
			cfg.Project.Settings.AllowedIps = &neon.AllowedIps{}
		}
		cfg.Project.Settings.AllowedIps.ProtectedBranchesOnly = state.AllowedIPsProtectedBranchesOnly.ValueBoolPointer()
	}
	if state.MaintenanceWindow != nil {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.MaintenanceWindow = state.maintenanceWindow(ctx, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if !state.EnabledLibraries.IsNull() && !state.EnabledLibraries.IsUnknown() {
		var enabledLibs []string
		resp.Diagnostics.Append(state.EnabledLibraries.ElementsAs(ctx, &enabledLibs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if len(enabledLibs) > 0 {
			if cfg.Project.Settings == nil {
				cfg.Project.Settings = &neon.ProjectSettingsData{}
			}
			if cfg.Project.Settings.PreloadLibraries == nil {
				cfg.Project.Settings.PreloadLibraries = &neon.PreloadLibraries{}
			}
			cfg.Project.Settings.PreloadLibraries.EnabledLibraries = enabledLibs
		}
	}
	if !state.EnabledLibrariesUseDefaults.IsNull() && !state.EnabledLibrariesUseDefaults.IsUnknown() {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		if cfg.Project.Settings.PreloadLibraries == nil {
			cfg.Project.Settings.PreloadLibraries = &neon.PreloadLibraries{}
		}
		cfg.Project.Settings.PreloadLibraries.UseDefaults = state.EnabledLibrariesUseDefaults.ValueBoolPointer()
	}
	if state.Quota != nil {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.Quota = state.quota(ctx, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if !state.AutoscalingLimitMinCU.IsNull() && !state.AutoscalingLimitMinCU.IsUnknown() {
		if cfg.Project.DefaultEndpointSettings == nil {
			cfg.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		cfg.Project.DefaultEndpointSettings.AutoscalingLimitMinCu =
			pointer(neon.ComputeUnit(state.AutoscalingLimitMinCU.ValueFloat64()))
	}
	if !state.AutoscalingLimitMaxCU.IsNull() && !state.AutoscalingLimitMaxCU.IsUnknown() {
		if cfg.Project.DefaultEndpointSettings == nil {
			cfg.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		cfg.Project.DefaultEndpointSettings.AutoscalingLimitMaxCu =
			pointer(neon.ComputeUnit(state.AutoscalingLimitMaxCU.ValueFloat64()))
	}
	if !state.SuspendTimeoutSeconds.IsNull() && !state.SuspendTimeoutSeconds.IsUnknown() {
		if cfg.Project.DefaultEndpointSettings == nil {
			cfg.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		cfg.Project.DefaultEndpointSettings.SuspendTimeoutSeconds =
			pointer(neon.SuspendTimeoutSeconds(state.SuspendTimeoutSeconds.ValueInt64()))
	}

	if (state.Branch != nil && !state.Branch.Name.IsNull() && !state.Branch.Name.IsUnknown()) ||
		(!state.DatabaseName.IsNull() && !state.DatabaseName.IsUnknown()) ||
		(!state.DatabaseUser.IsNull() && !state.DatabaseUser.IsUnknown()) {
		cfg.Project.Branch = &neon.ProjectCreateRequestProjectBranch{}
		if state.Branch != nil && !state.Branch.Name.IsNull() && !state.Branch.Name.IsUnknown() {
			cfg.Project.Branch.Name = state.Branch.Name.ValueStringPointer()
		}
		if !state.DatabaseName.IsNull() && !state.DatabaseName.IsUnknown() {
			cfg.Project.Branch.DatabaseName = state.DatabaseName.ValueStringPointer()
		}
		if !state.DatabaseUser.IsNull() && !state.DatabaseUser.IsUnknown() {
			cfg.Project.Branch.RoleName = state.DatabaseUser.ValueStringPointer()
		}
	}

	var project neon.Project
	var defaultBranch neon.Branch
	var endpoints []neon.Endpoint
	var databases []neon.Database
	resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
		result, err := r.client.CreateProject(cfg)
		if err != nil {
			return err
		}
		waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
		project = result.ProjectResponse.Project
		defaultBranch = result.BranchResponse.Branch
		endpoints = result.EndpointsResponse.Endpoints
		databases = result.DatabasesResponse.Databases
		return nil
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	primaryComputeSettings := state.PrimaryCompute
	branchSettings := state.Branch

	defaultEndpoint := findDefaultEndpoint(endpoints, defaultBranch.ID)
	defaultDatabase := findDefaultDatabase(databases, defaultBranch.ID)
	state.inferAttr(ctx, project, defaultBranch, defaultEndpoint, defaultDatabase, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// set custom config for the primary compute
	if primaryComputeSettings != nil && defaultEndpoint.ID != "" {
		state.PrimaryCompute = r.updatePrimaryCompute(ctx, state.PrimaryCompute, primaryComputeSettings, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// set custom config for the default branch
	if branchSettings != nil && defaultBranch.ID != "" {
		state.Branch = r.updateBranch(ctx, state.Branch, branchSettings, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r neonProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var config, plan, prior neonProjectResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var cfg neon.ProjectUpdateRequest
	if !prior.Name.Equal(plan.Name) && plan.Name.ValueString() != "" {
		cfg.Project.Name = plan.Name.ValueStringPointer()
	}
	if !plan.HistoryRetentionSeconds.Equal(prior.HistoryRetentionSeconds) &&
		!plan.HistoryRetentionSeconds.IsNull() && !plan.HistoryRetentionSeconds.IsUnknown() {
		cfg.Project.HistoryRetentionSeconds = plan.HistoryRetentionSeconds.ValueInt32Pointer()
	}

	// attributes of the cfg.Project.Settings
	if !plan.EnableLogicalReplication.IsUnknown() &&
		!plan.EnableLogicalReplication.Equal(prior.EnableLogicalReplication) {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.EnableLogicalReplication = plan.EnableLogicalReplication.ValueBoolPointer()
	}
	if !plan.Hipaa.IsUnknown() &&
		!plan.Hipaa.Equal(prior.Hipaa) {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.Hipaa = plan.Hipaa.ValueBoolPointer()
	}
	if !plan.BlockPublicConnections.IsUnknown() &&
		!plan.BlockPublicConnections.Equal(prior.BlockPublicConnections) {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.BlockPublicConnections = plan.BlockPublicConnections.ValueBoolPointer()
	}
	if !plan.BlockVPCConnections.IsUnknown() &&
		!plan.BlockVPCConnections.Equal(prior.BlockVPCConnections) {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.BlockVpcConnections = plan.BlockVPCConnections.ValueBoolPointer()
	}

	priorHasAllowedIPs := !prior.AllowedIPs.IsNull() && !prior.AllowedIPs.IsUnknown() &&
		len(prior.AllowedIPs.Elements()) > 0
	planHasAllowedIPs := !plan.AllowedIPs.IsNull() && !plan.AllowedIPs.IsUnknown() &&
		len(plan.AllowedIPs.Elements()) > 0
	updateAllowedIPs := false
	switch {
	case priorHasAllowedIPs:
		updateAllowedIPs = plan.AllowedIPs.IsUnknown() ||
			config.AllowedIPs.IsNull() || config.AllowedIPs.IsUnknown() ||
			!plan.AllowedIPs.Equal(prior.AllowedIPs)
	case planHasAllowedIPs:
		updateAllowedIPs = true
	}
	if updateAllowedIPs {
		allowedIPs := []string{}

		if !plan.AllowedIPs.IsNull() && !plan.AllowedIPs.IsUnknown() {
			resp.Diagnostics.Append(plan.AllowedIPs.ElementsAs(ctx, &allowedIPs, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		if cfg.Project.Settings.AllowedIps == nil {
			cfg.Project.Settings.AllowedIps = &neon.AllowedIps{}
		}
		cfg.Project.Settings.AllowedIps.Ips = allowedIPs
	}
	if !plan.AllowedIPsProtectedBranchesOnly.IsUnknown() &&
		!plan.AllowedIPsProtectedBranchesOnly.Equal(prior.AllowedIPsProtectedBranchesOnly) {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		if cfg.Project.Settings.AllowedIps == nil {
			cfg.Project.Settings.AllowedIps = &neon.AllowedIps{}
		}
		cfg.Project.Settings.AllowedIps.ProtectedBranchesOnly = plan.AllowedIPsProtectedBranchesOnly.ValueBoolPointer()
	}

	var updateMaintenanceWindow bool
	if plan.MaintenanceWindow != nil {
		if prior.MaintenanceWindow != nil {
			if !plan.MaintenanceWindow.Weekdays.Equal(prior.MaintenanceWindow.Weekdays) ||
				!plan.MaintenanceWindow.StartTime.Equal(prior.MaintenanceWindow.StartTime) ||
				!plan.MaintenanceWindow.EndTime.Equal(prior.MaintenanceWindow.EndTime) {
				updateMaintenanceWindow = true
			}
		} else {
			updateMaintenanceWindow = true
		}
	}
	if updateMaintenanceWindow {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.MaintenanceWindow = plan.maintenanceWindow(ctx, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	var updateQuota bool
	if plan.Quota != nil {
		if prior.Quota != nil {
			if !plan.Quota.ActiveTimeSeconds.Equal(prior.Quota.ActiveTimeSeconds) ||
				!plan.Quota.ComputeTimeSeconds.Equal(prior.Quota.ComputeTimeSeconds) ||
				!plan.Quota.DataTransferBytes.Equal(prior.Quota.DataTransferBytes) ||
				!plan.Quota.LogicalSizeBytes.Equal(prior.Quota.LogicalSizeBytes) ||
				!plan.Quota.WrittenDataBytes.Equal(prior.Quota.WrittenDataBytes) {
				updateQuota = true
			}
		} else {
			updateQuota = true
		}
	}
	if updateQuota {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		cfg.Project.Settings.Quota = plan.quota(ctx, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	priorHasEnabledLibraries := !prior.EnabledLibraries.IsNull() && !prior.EnabledLibraries.IsUnknown() &&
		len(prior.EnabledLibraries.Elements()) > 0
	planHasEnabledLibraries := !plan.EnabledLibraries.IsNull() && !plan.EnabledLibraries.IsUnknown() &&
		len(plan.EnabledLibraries.Elements()) > 0
	updateEnabledLibraries := false
	switch {
	case priorHasEnabledLibraries:
		updateEnabledLibraries = plan.EnabledLibraries.IsUnknown() ||
			config.EnabledLibraries.IsNull() || config.EnabledLibraries.IsUnknown() ||
			!plan.EnabledLibraries.Equal(prior.EnabledLibraries)
	case planHasEnabledLibraries:
		updateEnabledLibraries = true
	}
	if updateEnabledLibraries {
		preloadedLibs := []string{}

		if !plan.EnabledLibraries.IsNull() && !plan.EnabledLibraries.IsUnknown() {
			resp.Diagnostics.Append(plan.EnabledLibraries.ElementsAs(ctx, &preloadedLibs, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		if cfg.Project.Settings.PreloadLibraries == nil {
			cfg.Project.Settings.PreloadLibraries = &neon.PreloadLibraries{}
		}
		cfg.Project.Settings.PreloadLibraries.EnabledLibraries = preloadedLibs
	}

	if !plan.EnabledLibrariesUseDefaults.IsUnknown() &&
		!plan.EnabledLibrariesUseDefaults.Equal(prior.EnabledLibrariesUseDefaults) {
		if cfg.Project.Settings == nil {
			cfg.Project.Settings = &neon.ProjectSettingsData{}
		}
		if cfg.Project.Settings.PreloadLibraries == nil {
			cfg.Project.Settings.PreloadLibraries = &neon.PreloadLibraries{}
		}
		cfg.Project.Settings.PreloadLibraries.UseDefaults = plan.EnabledLibrariesUseDefaults.ValueBoolPointer()
	}

	// attributes to modify project endpoints' defaults
	if !plan.AutoscalingLimitMinCU.IsNull() && !plan.AutoscalingLimitMinCU.IsUnknown() &&
		!plan.AutoscalingLimitMinCU.Equal(prior.AutoscalingLimitMinCU) {
		if cfg.Project.DefaultEndpointSettings == nil {
			cfg.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		cfg.Project.DefaultEndpointSettings.AutoscalingLimitMinCu =
			pointer(neon.ComputeUnit(plan.AutoscalingLimitMinCU.ValueFloat64()))
	}
	if !plan.AutoscalingLimitMaxCU.IsNull() && !plan.AutoscalingLimitMaxCU.IsUnknown() &&
		!plan.AutoscalingLimitMaxCU.Equal(prior.AutoscalingLimitMaxCU) {
		if cfg.Project.DefaultEndpointSettings == nil {
			cfg.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		cfg.Project.DefaultEndpointSettings.AutoscalingLimitMaxCu =
			pointer(neon.ComputeUnit(plan.AutoscalingLimitMaxCU.ValueFloat64()))
	}
	if !plan.SuspendTimeoutSeconds.IsNull() && !plan.SuspendTimeoutSeconds.IsUnknown() &&
		!plan.SuspendTimeoutSeconds.Equal(prior.SuspendTimeoutSeconds) {
		if cfg.Project.DefaultEndpointSettings == nil {
			cfg.Project.DefaultEndpointSettings = &neon.DefaultEndpointSettings{}
		}
		cfg.Project.DefaultEndpointSettings.SuspendTimeoutSeconds =
			pointer(neon.SuspendTimeoutSeconds(plan.SuspendTimeoutSeconds.ValueInt64()))
	}

	if cfg.Project != (neon.ProjectUpdateRequestProject{}) {
		resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
			result, err := r.client.UpdateProject(prior.ID.ValueString(), cfg)
			if err != nil {
				return err
			}
			waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
			return nil
		}, ctx)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if stringChanged(prior.DatabaseName, plan.DatabaseName) ||
		stringChanged(prior.DatabaseUser, plan.DatabaseUser) {
		resp.Diagnostics.Append(projectReadiness.RetryFramework(func(ctx context.Context) error {
			return r.updateDefaultDatabase(ctx, prior, plan)
		}, ctx)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if config.Branch != nil && plan.Branch != nil && prior.Branch != nil &&
		(!plan.Branch.Name.Equal(prior.Branch.Name) ||
			!plan.Branch.Protected.Equal(prior.Branch.Protected)) {
		plan.Branch = r.updateBranch(ctx, prior.Branch, plan.Branch, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if config.PrimaryCompute != nil && plan.PrimaryCompute != nil && prior.PrimaryCompute != nil {
		plan.PrimaryCompute = r.updatePrimaryCompute(ctx, prior.PrimaryCompute, plan.PrimaryCompute, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	readResp := resource.ReadResponse{State: req.State}
	r.Read(ctx, resource.ReadRequest{State: req.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State = readResp.State

	// if config does not include allowed_ips attr, preserve the empty list in the state
	if config.AllowedIPs.IsNull() && plan.AllowedIPs.IsNull() {
		var state neonProjectResourceModel
		resp.Diagnostics.Append(resp.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.AllowedIPs = types.ListNull(types.StringType)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}

	if config.EnabledLibraries.IsNull() && plan.EnabledLibraries.IsNull() {
		var state neonProjectResourceModel
		resp.Diagnostics.Append(resp.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.EnabledLibraries = types.ListNull(types.StringType)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	}
}

func (r neonProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonProjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallbackFramework(func(ctx context.Context) error {
		_, err := r.client.DeleteProject(state.ID.ValueString())
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r neonProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	state := neonProjectResourceModel{
		ID: types.StringValue(req.ID),
		// init to respect the tf types
		AllowedIPs: types.ListNull(types.StringType),
		// init to respect the tf types
		EnabledLibraries: types.ListNull(types.StringType),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if resp.State.Raw.IsNull() {
		resp.Diagnostics.AddError("project not found", "")
	}
}

func (r neonProjectResource) updateDefaultDatabase(ctx context.Context, prior, plan neonProjectResourceModel) error {
	if prior.Branch == nil {
		return fmt.Errorf("cannot update default database: branch is not set")
	}

	projectID := prior.ID.ValueString()
	branchID := prior.Branch.ID.ValueString()
	databaseName := prior.DatabaseName.ValueString()
	if projectID == "" || branchID == "" || databaseName == "" {
		return fmt.Errorf("cannot update default database: project, branch, and database identifiers must be set")
	}

	cfg := neon.DatabaseUpdateRequest{}
	if stringChanged(prior.DatabaseName, plan.DatabaseName) {
		cfg.Database.Name = plan.DatabaseName.ValueStringPointer()
	}
	if stringChanged(prior.DatabaseUser, plan.DatabaseUser) {
		roleName := plan.DatabaseUser.ValueString()
		_, err := r.client.GetProjectBranchRole(projectID, branchID, roleName)
		if err != nil {
			return err
		}
		cfg.Database.OwnerName = &roleName
	}

	updated, err := r.client.UpdateProjectBranchDatabase(projectID, branchID, databaseName, cfg)
	if err != nil {
		return err
	}
	waitUnfinishedOperations(ctx, r.client, updated.OperationsResponse.Operations)
	return nil
}

func (r neonProjectResource) updateBranch(ctx context.Context, prior, plan *neonBranchResourceModel,
	diagnostics *diag.Diagnostics) *neonBranchResourceModel {
	schemaResp := resource.SchemaResponse{}
	r.branchHandler.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	diagnostics.Append(schemaResp.Diagnostics...)
	if diagnostics.HasError() {
		return nil
	}

	updateReq := resource.UpdateRequest{
		State: tfsdk.State{Schema: schemaResp.Schema},
		Plan:  tfsdk.Plan{Schema: schemaResp.Schema},
	}
	diagnostics.Append(updateReq.State.Set(ctx, prior)...)
	diagnostics.Append(updateReq.Plan.Set(ctx, plan)...)
	if diagnostics.HasError() {
		return nil
	}

	updateResp := resource.UpdateResponse{State: updateReq.State}
	r.branchHandler.Update(ctx, updateReq, &updateResp)
	diagnostics.Append(updateResp.Diagnostics...)
	if diagnostics.HasError() {
		return nil
	}

	newState := new(neonBranchResourceModel)
	diagnostics.Append(updateResp.State.Get(ctx, newState)...)
	if diagnostics.HasError() {
		return nil
	}
	return newState
}

func (r neonProjectResource) updatePrimaryCompute(ctx context.Context, prior *neonEndpointResourceModel,
	plan *neonEndpointResourceModel, diagnostics *diag.Diagnostics) *neonEndpointResourceModel {
	schemaResp := resource.SchemaResponse{}
	r.endpointHandler.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	diagnostics.Append(schemaResp.Diagnostics...)
	if diagnostics.HasError() {
		return nil
	}

	updateReq := resource.UpdateRequest{
		State: tfsdk.State{Schema: schemaResp.Schema},
		Plan:  tfsdk.Plan{Schema: schemaResp.Schema},
	}
	diagnostics.Append(updateReq.State.Set(ctx, prior)...)
	diagnostics.Append(updateReq.Plan.Set(ctx, plan)...)
	if diagnostics.HasError() {
		return nil
	}

	updateResp := resource.UpdateResponse{State: updateReq.State}
	r.endpointHandler.Update(ctx, updateReq, &updateResp)
	diagnostics.Append(updateResp.Diagnostics...)
	if diagnostics.HasError() {
		return nil
	}

	var newState = new(neonEndpointResourceModel)
	diagnostics.Append(updateResp.State.Get(ctx, newState)...)
	if diagnostics.HasError() {
		return nil
	}
	return newState
}
