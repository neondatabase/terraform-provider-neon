package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.ResourceWithConfigure   = (*neonTriggerResource)(nil)
	_ resource.ResourceWithImportState = (*neonTriggerResource)(nil)
)

type neonTriggerResource struct {
	client *neon.Client
}

type neonTriggerResourceModel struct {
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	BranchID     types.String `tfsdk:"branch_id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	FunctionSlug types.String `tfsdk:"function_slug"`
	FunctionPath types.String `tfsdk:"function_path"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	Cron         types.String `tfsdk:"cron"`
	BucketName   types.String `tfsdk:"bucket_name"`
	Prefix       types.String `tfsdk:"prefix"`
	TriggerID    types.String `tfsdk:"trigger_id"`
	Version      types.Int64  `tfsdk:"version"`
	Inherited    types.Bool   `tfsdk:"inherited"`
}

func NewNeonTriggerResource() resource.Resource {
	return &neonTriggerResource{}
}

func (r *neonTriggerResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_trigger"
}

func (r *neonTriggerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Neon trigger (Function invocation on a schedule or after a branch-bucket object upload).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "Composite ID of the form <project_id>/<branch_id>/<trigger_id>.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description:   "The Neon branch ID.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable trigger name. Unique among triggers visible on the branch.",
			},
			"type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{triggerTypeValidator{}},
				Description:   "Trigger type discriminator. One of `schedule` or `storage_object_created`.",
			},
			"function_slug": schema.StringAttribute{
				Required:    true,
				Description: "The branch-local Function slug resolved when an occurrence is consumed.",
			},
			"function_path": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Path passed to the target Function. Defaults to `/`.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether future occurrences should fire the trigger.",
			},
			"cron": schema.StringAttribute{
				Optional:    true,
				Description: "Numeric five-field cron expression (minute through day-of-week), interpreted in UTC. Required when `type` is `schedule`.",
			},
			"bucket_name": schema.StringAttribute{
				Optional:    true,
				Description: "The exact object-storage bucket name to watch. Required when `type` is `storage_object_created`.",
			},
			"prefix": schema.StringAttribute{
				Optional:    true,
				Description: "Optional object-key prefix matched against uploaded object keys. Max 1024 UTF-8 bytes. Used only when `type` is `storage_object_created`.",
			},
			"trigger_id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "Opaque, server-minted trigger ID.",
			},
			"version": schema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic configuration version.",
			},
			"inherited": schema.BoolAttribute{
				Computed:    true,
				Description: "True when the effective configuration was authored on an ancestor branch.",
			},
		},
	}
}

func (r *neonTriggerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var planned neonTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() {
		return
	}

	typ := planned.Type.ValueString()
	scheduleSet := !planned.Cron.IsNull() && !planned.Cron.IsUnknown()
	storageSet := !planned.BucketName.IsNull() && !planned.BucketName.IsUnknown()

	switch typ {
	case "schedule":
		if !scheduleSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("cron"),
				"Missing required attribute",
				"When type is `schedule`, `cron` is required.",
			)
		}
		if storageSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("bucket_name"),
				"Incompatible attribute for type",
				"When type is `schedule`, `bucket_name` must not be set.",
			)
		}
	case "storage_object_created":
		if !storageSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("bucket_name"),
				"Missing required attribute",
				"When type is `storage_object_created`, `bucket_name` is required.",
			)
		}
		if scheduleSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("cron"),
				"Incompatible attribute for type",
				"When type is `storage_object_created`, `cron` must not be set.",
			)
		}
	}
}

func (r *neonTriggerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonTriggerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	validID := len(parts) == 3
	// The SDK concatenates identifiers into the URL without escaping them.
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || url.PathEscape(part) != part {
			validID = false
			break
		}
	}
	if !validID {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Invalid Neon Trigger Import ID",
			"Expected an import ID in the form <project_id>/<branch_id>/<trigger_id>, with three nonempty literal identifiers and no URL encoding, query/fragment delimiters, or dot segments.",
		)
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	if err := ctx.Err(); err != nil {
		resp.Diagnostics.AddError("Trigger Import Canceled", err.Error())
		return
	}

	projectID, branchID, triggerID := parts[0], parts[1], parts[2]
	result, err := r.client.GetProjectBranchTrigger(projectID, branchID, neon.TriggerID(triggerID))
	if err != nil {
		if isNotFoundError(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("id"),
				"Trigger Not Found",
				fmt.Sprintf("Trigger %q was not found on branch %q in project %q.", triggerID, branchID, projectID),
			)
			return
		}
		resp.Diagnostics.AddError("Failed to Import Neon Trigger", err.Error())
		return
	}

	returnedTriggerID := triggerIDFromTrigger(result.Trigger)
	if returnedTriggerID == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Invalid Neon Trigger Response",
			"The API returned an empty trigger_id; no state was imported.",
		)
		return
	}
	if returnedTriggerID != triggerID {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Neon Trigger ID Mismatch",
			fmt.Sprintf("The API returned trigger ID %q, which does not match requested trigger ID %q; no state was imported.", returnedTriggerID, triggerID),
		)
		return
	}
	if result.Trigger.Type != "schedule" && result.Trigger.Type != "storage_object_created" {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Unsupported Neon Trigger Type",
			fmt.Sprintf("The API returned unsupported trigger type %q; no state was imported.", result.Trigger.Type),
		)
		return
	}
	if triggerIsInherited(result.Trigger) {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Cannot Import Inherited Neon Trigger",
			"The trigger is inherited from an ancestor branch. Import only branch-local triggers because updating an inherited trigger creates a local shadow and deleting it creates a tombstone.",
		)
		return
	}

	state := neonTriggerResourceModel{
		ProjectID: types.StringValue(projectID),
		BranchID:  types.StringValue(branchID),
	}
	setNeonTriggerModel(&state, result.Trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func triggerIsInherited(t neon.Trigger) bool {
	switch t.Type {
	case "schedule":
		return t.ScheduleTrigger.Inherited
	case "storage_object_created":
		return t.StorageObjectCreatedTrigger.Inherited
	default:
		return false
	}
}

func (r *neonTriggerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := buildTriggerCreateRequest(&plan)

	var result neon.TriggerResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.CreateProjectBranchTrigger(
			plan.ProjectID.ValueString(),
			plan.BranchID.ValueString(),
			cfg,
		)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonTriggerModel(&plan, result.Trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonTriggerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.TriggersListResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.ListProjectBranchTriggers(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
		)
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			resp.State.RemoveResource(ctx)
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	var metadata *neon.Trigger
	for i := range result.Triggers {
		if state.TriggerID.ValueString() == triggerIDFromTrigger(result.Triggers[i]) {
			metadata = &result.Triggers[i]
			break
		}
	}
	if metadata == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	setNeonTriggerModel(&state, *metadata)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonTriggerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan, prior neonTriggerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := buildTriggerUpdateRequest(&plan)

	var result neon.TriggerResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.UpdateProjectBranchTrigger(
			plan.ProjectID.ValueString(),
			plan.BranchID.ValueString(),
			neon.TriggerID(prior.TriggerID.ValueString()),
			cfg,
		)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonTriggerModel(&plan, result.Trigger)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonTriggerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonTriggerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		return r.client.DeleteProjectBranchTrigger(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			neon.TriggerID(state.TriggerID.ValueString()),
		)
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			return nil
		},
		http.StatusConflict: func(_ context.Context) error {
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func buildTriggerCreateRequest(plan *neonTriggerResourceModel) neon.TriggerCreateRequest {
	cfg := neon.TriggerCreateRequest{
		Type: plan.Type.ValueString(),
	}

	typ := plan.Type.ValueString()
	switch typ {
	case "schedule":
		typeVal, _ := neon.NewScheduleTriggerCreateRequestType("schedule")
		cfg.ScheduleTriggerCreateRequest.Type = typeVal
		cfg.ScheduleTriggerCreateRequest.Name = plan.Name.ValueString()
		cfg.ScheduleTriggerCreateRequest.FunctionSlug = plan.FunctionSlug.ValueString()
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.ScheduleTriggerCreateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.ScheduleTriggerCreateRequest.Enabled = &en
		}
		if !plan.Cron.IsNull() && !plan.Cron.IsUnknown() {
			c := plan.Cron.ValueString()
			cfg.ScheduleTriggerCreateRequest.Schedule = neon.FunctionTriggerSchedule{Cron: c}
		}
	case "storage_object_created":
		typeVal, _ := neon.NewStorageObjectCreatedTriggerCreateRequestType("storage_object_created")
		cfg.StorageObjectCreatedTriggerCreateRequest.Type = typeVal
		cfg.StorageObjectCreatedTriggerCreateRequest.Name = plan.Name.ValueString()
		cfg.StorageObjectCreatedTriggerCreateRequest.FunctionSlug = plan.FunctionSlug.ValueString()
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.StorageObjectCreatedTriggerCreateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.StorageObjectCreatedTriggerCreateRequest.Enabled = &en
		}
		if !plan.BucketName.IsNull() && !plan.BucketName.IsUnknown() {
			bn := plan.BucketName.ValueString()
			soc := neon.FunctionTriggerStorageObjectCreated{BucketName: bn}
			if !plan.Prefix.IsNull() && !plan.Prefix.IsUnknown() {
				p := plan.Prefix.ValueString()
				soc.Prefix = &p
			}
			cfg.StorageObjectCreatedTriggerCreateRequest.StorageObjectCreated = soc
		}
	}
	return cfg
}

func buildTriggerUpdateRequest(plan *neonTriggerResourceModel) neon.TriggerUpdateRequest {
	cfg := neon.TriggerUpdateRequest{}

	typ := plan.Type.ValueString()
	cfg.Type = typ
	switch typ {
	case "schedule":
		typeVal, _ := neon.NewScheduleTriggerUpdateRequestType("schedule")
		cfg.ScheduleTriggerUpdateRequest.Type = typeVal
		name := plan.Name.ValueString()
		cfg.ScheduleTriggerUpdateRequest.Name = &name
		slug := plan.FunctionSlug.ValueString()
		cfg.ScheduleTriggerUpdateRequest.FunctionSlug = &slug
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.ScheduleTriggerUpdateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.ScheduleTriggerUpdateRequest.Enabled = &en
		}
		if !plan.Cron.IsNull() && !plan.Cron.IsUnknown() {
			c := plan.Cron.ValueString()
			cfg.ScheduleTriggerUpdateRequest.Schedule = &neon.FunctionTriggerSchedule{Cron: c}
		}
	case "storage_object_created":
		typeVal, _ := neon.NewStorageObjectCreatedTriggerUpdateRequestType("storage_object_created")
		cfg.StorageObjectCreatedTriggerUpdateRequest.Type = typeVal
		name := plan.Name.ValueString()
		cfg.StorageObjectCreatedTriggerUpdateRequest.Name = &name
		slug := plan.FunctionSlug.ValueString()
		cfg.StorageObjectCreatedTriggerUpdateRequest.FunctionSlug = &slug
		if !plan.FunctionPath.IsNull() && !plan.FunctionPath.IsUnknown() {
			fp := plan.FunctionPath.ValueString()
			cfg.StorageObjectCreatedTriggerUpdateRequest.FunctionPath = &fp
		}
		if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
			en := plan.Enabled.ValueBool()
			cfg.StorageObjectCreatedTriggerUpdateRequest.Enabled = &en
		}
		if !plan.BucketName.IsNull() && !plan.BucketName.IsUnknown() {
			bn := plan.BucketName.ValueString()
			soc := neon.FunctionTriggerStorageObjectCreated{BucketName: bn}
			if !plan.Prefix.IsNull() && !plan.Prefix.IsUnknown() {
				p := plan.Prefix.ValueString()
				soc.Prefix = &p
			}
			cfg.StorageObjectCreatedTriggerUpdateRequest.StorageObjectCreated = &soc
		}
	}
	return cfg
}

func setNeonTriggerModel(model *neonTriggerResourceModel, t neon.Trigger) {
	triggerID := triggerIDFromTrigger(t)
	model.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
		model.ProjectID.ValueString(), model.BranchID.ValueString(), triggerID))
	model.TriggerID = types.StringValue(triggerID)

	switch t.Type {
	case "schedule":
		setNeonTriggerModelFromSchedule(model, t.ScheduleTrigger)
	case "storage_object_created":
		setNeonTriggerModelFromStorage(model, t.StorageObjectCreatedTrigger)
	}
}

func triggerIDFromTrigger(t neon.Trigger) string {
	switch t.Type {
	case "schedule":
		return string(t.ScheduleTrigger.TriggerID)
	case "storage_object_created":
		return string(t.StorageObjectCreatedTrigger.TriggerID)
	}
	return ""
}

func setNeonTriggerModelFromSchedule(model *neonTriggerResourceModel, st neon.ScheduleTrigger) {
	model.Type = types.StringValue("schedule")
	model.Name = types.StringValue(st.Name)
	model.FunctionSlug = types.StringValue(st.FunctionSlug)
	model.FunctionPath = types.StringValue(st.FunctionPath)
	model.Enabled = types.BoolValue(st.Enabled)
	model.Inherited = types.BoolValue(st.Inherited)
	model.Version = types.Int64Value(st.Version)
	if st.Schedule.Cron != "" {
		model.Cron = types.StringValue(st.Schedule.Cron)
	} else {
		model.Cron = types.StringNull()
	}
	model.BucketName = types.StringNull()
	model.Prefix = types.StringNull()
}

func setNeonTriggerModelFromStorage(model *neonTriggerResourceModel, st neon.StorageObjectCreatedTrigger) {
	model.Type = types.StringValue("storage_object_created")
	model.Name = types.StringValue(st.Name)
	model.FunctionSlug = types.StringValue(st.FunctionSlug)
	model.FunctionPath = types.StringValue(st.FunctionPath)
	model.Enabled = types.BoolValue(st.Enabled)
	model.Inherited = types.BoolValue(st.Inherited)
	model.Version = types.Int64Value(st.Version)
	if st.StorageObjectCreated.BucketName != "" {
		model.BucketName = types.StringValue(st.StorageObjectCreated.BucketName)
	} else {
		model.BucketName = types.StringNull()
	}
	if st.StorageObjectCreated.Prefix != nil {
		model.Prefix = types.StringValue(*st.StorageObjectCreated.Prefix)
	} else {
		model.Prefix = types.StringNull()
	}
	model.Cron = types.StringNull()
}

type triggerTypeValidator struct{}

func (triggerTypeValidator) Description(_ context.Context) string {
	return "validates that the trigger type is `schedule` or `storage_object_created`"
}

func (v triggerTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (triggerTypeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if _, err := neon.NewScheduleTriggerType(s); err == nil {
		return
	}
	if _, err := neon.NewStorageObjectCreatedTriggerType(s); err == nil {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid trigger type",
		fmt.Sprintf("%q is not a valid trigger type; expected `schedule` or `storage_object_created`.", s),
	)
}
