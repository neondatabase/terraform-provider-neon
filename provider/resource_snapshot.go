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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var (
	_ resource.Resource                = (*neonSnapshotResource)(nil)
	_ resource.ResourceWithConfigure   = (*neonSnapshotResource)(nil)
	_ resource.ResourceWithImportState = (*neonSnapshotResource)(nil)
)

type neonSnapshotResource struct {
	client *neon.Client
}

type neonSnapshotResourceModel struct {
	ID             types.String `tfsdk:"id"`
	ProjectID      types.String `tfsdk:"project_id"`
	BranchID       types.String `tfsdk:"branch_id"`
	SnapshotID     types.String `tfsdk:"snapshot_id"`
	Name           types.String `tfsdk:"name"`
	ExpiresAt      types.String `tfsdk:"expires_at"`
	Lsn            types.String `tfsdk:"lsn"`
	Timestamp      types.String `tfsdk:"timestamp"`
	SourceBranchID types.String `tfsdk:"source_branch_id"`
	CreatedAt      types.String `tfsdk:"created_at"`
	Manual         types.Bool   `tfsdk:"manual"`
	FullSize       types.Int64  `tfsdk:"full_size"`
	DiffSize       types.Int64  `tfsdk:"diff_size"`
}

func NewSnapshotResource() resource.Resource {
	return &neonSnapshotResource{}
}

func (r *neonSnapshotResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_snapshot"
}

func (r *neonSnapshotResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: "Manages a Neon snapshot.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "Composite ID of the form <project_id>/<snapshot_id>.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon branch ID to capture. Immutable after creation.",
			},
			"snapshot_id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "Server-generated snapshot identifier.",
			},
			"name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Human-readable label for the snapshot.",
			},
			"expires_at": schema.StringAttribute{
				Optional:    true,
				Description: "RFC 3339 timestamp when the snapshot expires. Omit or remove it to clear the expiration.",
			},
			"lsn": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: requiresReplace,
				Description:   "WAL position (LSN) at which to capture the snapshot, in Postgres LSN format.",
			},
			"timestamp": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: requiresReplace,
				Description:   "Point in time captured by the snapshot, in RFC 3339 format.",
			},
			"source_branch_id": schema.StringAttribute{
				Computed:    true,
				Description: "Branch from which this snapshot was created.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC 3339 timestamp at which the snapshot was created.",
			},
			"manual": schema.BoolAttribute{
				Computed:    true,
				Description: "True if the snapshot was created manually rather than by a schedule.",
			},
			"full_size": schema.Int64Attribute{
				Computed:    true,
				Description: "Full logical size of the snapshot in bytes at the time it was taken.",
			},
			"diff_size": schema.Int64Attribute{
				Computed:    true,
				Description: "Incremental Postgres storage size in bytes since the previous scheduled snapshot.",
			},
		},
	}
}

func (r *neonSnapshotResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonSnapshotResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	projectID, snapshotID, ok := strings.Cut(req.ID, "/")
	if !ok || projectID == "" || snapshotID == "" || strings.Contains(snapshotID, "/") {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Invalid Neon Snapshot Import ID",
			"Expected an import ID in the form <project_id>/<snapshot_id>.",
		)
		return
	}

	// Seed state with the parsed identity, then Read populates
	// branch_id, source_branch_id, name, expires_at, etc. Without
	// branch_id populated, the RequiresReplace plan modifier would
	// force replacement after import.
	state := neonSnapshotResourceModel{
		ID:         types.StringValue(req.ID),
		ProjectID:  types.StringValue(projectID),
		SnapshotID: types.StringValue(snapshotID),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.State = readResp.State
	resp.Diagnostics.Append(readResp.Diagnostics...)
}

func (r *neonSnapshotResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonSnapshotResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var (
		name      *string
		expiresAt *string
		lsn       *string
		timestamp *string
	)
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() && plan.Name.ValueString() != "" {
		v := plan.Name.ValueString()
		name = &v
	}
	if !plan.ExpiresAt.IsNull() && !plan.ExpiresAt.IsUnknown() && plan.ExpiresAt.ValueString() != "" {
		v := plan.ExpiresAt.ValueString()
		expiresAt = &v
	}
	if !plan.Lsn.IsNull() && !plan.Lsn.IsUnknown() && plan.Lsn.ValueString() != "" {
		v := plan.Lsn.ValueString()
		lsn = &v
	}
	if !plan.Timestamp.IsNull() && !plan.Timestamp.IsUnknown() && plan.Timestamp.ValueString() != "" {
		v := plan.Timestamp.ValueString()
		timestamp = &v
	}

	var created neon.Snapshot
	resp.Diagnostics.Append(
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				rsp, err := r.client.CreateSnapshot(
					plan.ProjectID.ValueString(),
					plan.BranchID.ValueString(),
					lsn, timestamp, name, expiresAt,
				)
				if err != nil {
					return err
				}
				created = rsp.Snapshot
				return nil
			}, nil,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonSnapshotModel(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonSnapshotResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonSnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The Neon Go SDK has no single-snapshot getter, so Read lists the project's snapshots and filters by ID.
	var found *neon.Snapshot
	resp.Diagnostics.Append(
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				rsp, err := r.client.ListSnapshots(state.ProjectID.ValueString())
				if err != nil {
					return err
				}
				for i := range rsp.Snapshots {
					if rsp.Snapshots[i].ID == state.SnapshotID.ValueString() {
						s := rsp.Snapshots[i]
						found = &s
						return nil
					}
				}
				// Snapshot not in list: treat as drift-by-deletion so
				// out-of-band deletes do not produce a hard error. The
				// Neon REST API returns 200 with an empty list for
				// "snapshot not found" rather than 404, so the Go SDK
				// does not surface an HTTP-status error here; we have to
				// detect absence in-process.
				resp.State.RemoveResource(ctx)
				return nil
			},
			map[int]func(context.Context) error{
				http.StatusNotFound: func(_ context.Context) error {
					resp.State.RemoveResource(ctx)
					return nil
				},
			},
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	if found == nil {
		// Read already removed the resource; nothing more to do.
		return
	}

	setNeonSnapshotModel(&state, *found)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonSnapshotResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan, state neonSnapshotResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update := neon.SnapshotUpdateRequest{
		Snapshot: neon.SnapshotUpdateRequestSnapshot{},
	}

	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		update.Snapshot.Name = &v
	}

	switch {
	case plan.ExpiresAt.IsNull():
		// The API clears the expiration when it receives the zero time.
		zero := time.Time{}
		update.Snapshot.ExpiresAt = &zero
	case plan.ExpiresAt.IsUnknown():
	default:
		parsed, err := time.Parse(time.RFC3339, plan.ExpiresAt.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("expires_at"),
				"Invalid expires_at",
				fmt.Sprintf("Expected RFC 3339 timestamp: %s", err),
			)
			return
		}
		update.Snapshot.ExpiresAt = &parsed
	}

	var updated neon.Snapshot
	resp.Diagnostics.Append(
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				rsp, err := r.client.UpdateSnapshot(
					state.ProjectID.ValueString(),
					state.SnapshotID.ValueString(),
					update,
				)
				if err != nil {
					return err
				}
				updated = rsp.Snapshot
				return nil
			}, nil,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ExpiresAt = plan.ExpiresAt
	setNeonSnapshotModel(&state, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonSnapshotResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonSnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(
		projectReadiness.Retry(ctx,
			func(ctx context.Context) error {
				_, err := r.client.DeleteSnapshot(
					state.ProjectID.ValueString(),
					state.SnapshotID.ValueString(),
				)
				return err
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

func setNeonSnapshotModel(model *neonSnapshotResourceModel, snap neon.Snapshot) {
	model.ID = types.StringValue(snapshotCompositeID(model.ProjectID.ValueString(), snap.ID))
	model.SnapshotID = types.StringValue(snap.ID)
	if snap.Name != "" {
		model.Name = types.StringValue(snap.Name)
	}
	if snap.SourceBranchID != nil {
		model.SourceBranchID = types.StringValue(*snap.SourceBranchID)
		// BranchID is the input field (the branch we captured from).
		// After Read, we derive it from the snapshot's source branch
		// so post-import plans do not see branch_id as missing and
		// trigger RequiresReplace replacement. Preserve an
		// already-populated BranchID if present (e.g. after Create).
		if model.BranchID.IsNull() || model.BranchID.IsUnknown() {
			model.BranchID = types.StringValue(*snap.SourceBranchID)
		}
	} else {
		model.SourceBranchID = types.StringNull()
	}
	if snap.CreatedAt != "" {
		model.CreatedAt = types.StringValue(snap.CreatedAt)
	} else {
		model.CreatedAt = types.StringNull()
	}
	if snap.Manual != nil {
		model.Manual = types.BoolValue(*snap.Manual)
	} else {
		model.Manual = types.BoolNull()
	}
	if snap.FullSize != nil {
		model.FullSize = types.Int64Value(*snap.FullSize)
	} else {
		model.FullSize = types.Int64Null()
	}
	if snap.DiffSize != nil {
		model.DiffSize = types.Int64Value(*snap.DiffSize)
	} else {
		model.DiffSize = types.Int64Null()
	}
	if snap.ExpiresAt == nil {
		model.ExpiresAt = types.StringNull()
	} else if !sameInstant(model.ExpiresAt, *snap.ExpiresAt) {
		// Neon returns UTC; a configured value with another offset that names the same instant stays as written,
		// here and for timestamp below.
		model.ExpiresAt = types.StringValue(*snap.ExpiresAt)
	}
	if snap.Lsn != nil {
		model.Lsn = types.StringValue(*snap.Lsn)
	} else {
		model.Lsn = types.StringNull()
	}
	if snap.Timestamp == nil {
		model.Timestamp = types.StringNull()
	} else if !sameInstant(model.Timestamp, *snap.Timestamp) {
		model.Timestamp = types.StringValue(*snap.Timestamp)
	}
}

func snapshotCompositeID(projectID, snapID string) string {
	return fmt.Sprintf("%s/%s", projectID, snapID)
}

func sameInstant(configured types.String, rfc3339 string) bool {
	if configured.IsNull() || configured.IsUnknown() {
		return false
	}
	a, errA := time.Parse(time.RFC3339, configured.ValueString())
	b, errB := time.Parse(time.RFC3339, rfc3339)
	return errA == nil && errB == nil && a.Equal(b)
}
