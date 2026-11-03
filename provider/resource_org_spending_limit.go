package provider

import (
	"context"
	"fmt"
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

var _ resource.Resource = (*neonOrgSpendingLimitResource)(nil)
var _ resource.ResourceWithConfigure = (*neonOrgSpendingLimitResource)(nil)
var _ resource.ResourceWithImportState = (*neonOrgSpendingLimitResource)(nil)

type neonOrgSpendingLimitResource struct {
	client *neon.Client
}

type neonOrgSpendingLimitResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	OrgID              types.String `tfsdk:"org_id"`
	SpendingLimitCents types.Int64  `tfsdk:"spending_limit_cents"`
}

func NewNeonOrgSpendingLimitResource() resource.Resource {
	return &neonOrgSpendingLimitResource{}
}

func (r *neonOrgSpendingLimitResource) Metadata(
	_ context.Context,
	_ resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = "neon_org_spending_limit"
}

func (r *neonOrgSpendingLimitResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages an organization's monthly spending limit in cents.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The organization ID used as the resource identity.",
			},
			"org_id": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Description: "The Neon organization ID.",
			},
			"spending_limit_cents": schema.Int64Attribute{
				Required: true,
				Validators: []validator.Int64{
					positiveSpendingLimitValidator{},
				},
				Description: "Monthly spending alert limit in cents. The limit does not suspend computes.",
			},
		},
	}
}

type positiveSpendingLimitValidator struct{}

func (positiveSpendingLimitValidator) Description(context.Context) string {
	return "validates that the spending limit is positive"
}

func (v positiveSpendingLimitValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (positiveSpendingLimitValidator) ValidateInt64(
	_ context.Context,
	req validator.Int64Request,
	resp *validator.Int64Response,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.ConfigValue.ValueInt64() < 1 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid spending limit",
			"spending_limit_cents must be greater than zero.",
		)
	}
}

func (r *neonOrgSpendingLimitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
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

func (r *neonOrgSpendingLimitResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonOrgSpendingLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.SpendingLimitResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.SetOrganizationSpendingLimit(
			plan.OrgID.ValueString(),
			neon.SpendingLimitUpdateRequest{
				SpendingLimitCents: plan.SpendingLimitCents.ValueInt64(),
			},
		)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(spendingLimitResourceID(plan.OrgID))
	plan.SpendingLimitCents = types.Int64Value(result.SpendingLimitCents)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonOrgSpendingLimitResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonOrgSpendingLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.SpendingLimitResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.GetOrganizationSpendingLimit(state.OrgID.ValueString())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if spendingLimitIsAbsent(result.SpendingLimitCents) {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(spendingLimitResourceID(state.OrgID))
	state.SpendingLimitCents = types.Int64Value(result.SpendingLimitCents)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonOrgSpendingLimitResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonOrgSpendingLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.SpendingLimitResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.SetOrganizationSpendingLimit(
			plan.OrgID.ValueString(),
			neon.SpendingLimitUpdateRequest{
				SpendingLimitCents: plan.SpendingLimitCents.ValueInt64(),
			},
		)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(spendingLimitResourceID(plan.OrgID))
	plan.SpendingLimitCents = types.Int64Value(result.SpendingLimitCents)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonOrgSpendingLimitResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonOrgSpendingLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		_, err := r.client.DeleteOrganizationSpendingLimit(state.OrgID.ValueString())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *neonOrgSpendingLimitResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	if req.ID == "" || strings.Contains(req.ID, "/") {
		resp.Diagnostics.AddAttributeError(
			path.Root("id"),
			"Invalid organization spending limit import ID",
			"Expected an organization ID in the form <org_id>.",
		)
		return
	}

	state := neonOrgSpendingLimitResourceModel{
		ID:    types.StringValue(req.ID),
		OrgID: types.StringValue(req.ID),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readResp := resource.ReadResponse{State: resp.State}
	r.Read(ctx, resource.ReadRequest{State: resp.State}, &readResp)
	resp.Diagnostics.Append(readResp.Diagnostics...)
}

func spendingLimitResourceID(orgID types.String) string {
	return orgID.ValueString()
}

func spendingLimitIsAbsent(cents int64) bool {
	return cents == 0
}

func (r *neonOrgSpendingLimitResource) String() string {
	return fmt.Sprintf("%T", r)
}
