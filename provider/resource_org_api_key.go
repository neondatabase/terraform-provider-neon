package provider

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.Resource = (*orgAPIKeyResource)(nil)
var _ resource.ResourceWithUpgradeState = (*orgAPIKeyResource)(nil)
var _ resource.ResourceWithConfigure = (*orgAPIKeyResource)(nil)

type orgAPIKeyResource struct {
	client *neon.Client
}

type orgAPIKeyResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	OrgID     types.String `tfsdk:"org_id"`
	ProjectID types.String `tfsdk:"project_id"`
	Key       types.String `tfsdk:"key"`
}

func NewOrgAPIKeyResource() resource.Resource {
	return &orgAPIKeyResource{}
}

func (r *orgAPIKeyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_org_api_key"
}

func (r *orgAPIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Version: 2,
		Description: `An org-specific key to access the Neon API.

~>**WARNING** The resource does not support import.
`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   "The API key ID.",
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The name of the API Key.",
			},
			"org_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The organisation ID.",
			},
			"project_id": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: requiresReplace,
				Description:   "The project ID to which this key will grant the access to.",
			},
			"key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "The generated 64-bit token required to access the Neon API.",
			},
		},
	}
}

func (r *orgAPIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *orgAPIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state orgAPIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createRequest := neon.OrgApiKeyCreateRequest{
		ApiKeyCreateRequest: neon.ApiKeyCreateRequest{KeyName: state.Name.ValueString()},
	}
	if !state.ProjectID.IsNull() && !state.ProjectID.IsUnknown() {
		projectID := state.ProjectID.ValueString()
		createRequest.ProjectID = &projectID
	}

	var result neon.OrgApiKeyCreateResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.CreateOrgApiKey(state.OrgID.ValueString(), createRequest)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(strconv.FormatInt(result.ID, 10))
	state.Key = types.StringValue(result.Key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgAPIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state orgAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var keys []neon.OrgApiKeysListResponseItem
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		keys, err = r.client.ListOrgApiKeys(state.OrgID.ValueString())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Parse API Key ID",
			fmt.Sprintf("The API key ID %q in the Terraform state is not a number: %s", state.ID.ValueString(), err))
		return
	}
	for _, key := range keys {
		if key.ID == id {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	tflog.Debug(ctx, "API key not found, removing from state", map[string]any{"id": state.ID.ValueString()})
	resp.State.RemoveResource(ctx)
}

func (r *orgAPIKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Organization API Key Update Not Supported",
		"Changing the API key attributes requires replacing the resource.")
}

func (r *orgAPIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state orgAPIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Parse API Key ID", err.Error())
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		_, err := r.client.RevokeOrgApiKey(state.OrgID.ValueString(), id)
		return err
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

// UpgradeState migrates state written by kislerdm/neon v0.18.0, the last Terraform Plugin SDK v2 release.
func (r *orgAPIKeyResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		1: legacyStateUpgrader(r.Schema, nil),
	}
}
