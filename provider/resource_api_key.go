package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.Resource = (*apiKeyResource)(nil)
var _ resource.ResourceWithUpgradeState = (*apiKeyResource)(nil)
var _ resource.ResourceWithConfigure = (*apiKeyResource)(nil)

type apiKeyResource struct {
	client *neon.Client
}

type apiKeyResourceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Key  types.String `tfsdk:"key"`
}

func NewAPIKeyResource() resource.Resource {
	return &apiKeyResource{}
}

func (r *apiKeyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_api_key"
}

func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 2,
		Description: `A key to access the Neon API.

~>**WARNING** The resource does not support import.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The API key ID.",
			},
			"name": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Description: "The name of the API Key.",
			},
			"key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "The generated 64-bit token required to access the Neon API.",
			},
		},
	}
}

func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.ApiKeyCreateResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.CreateApiKey(neon.ApiKeyCreateRequest{KeyName: state.Name.ValueString()})
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(strconv.FormatInt(result.ID, 10))
	state.Key = types.StringValue(result.Key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var keys []neon.ApiKeysListResponseItem
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		keys, err = r.client.ListApiKeys()
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, key := range keys {
		if key.Name == state.Name.ValueString() {
			state.ID = types.StringValue(strconv.FormatInt(key.ID, 10))
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	tflog.Debug(ctx, "API key not found, removing from state", map[string]any{"name": state.Name.ValueString()})
	resp.State.RemoveResource(ctx)
}

func (r *apiKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("API Key Update Not Supported",
		"Changing the API key name requires replacing the resource.")
}

func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state apiKeyResourceModel
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
		_, err := r.client.RevokeApiKey(id)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

// UpgradeState migrates state written by kislerdm/neon v0.18.0, the last Terraform Plugin SDK v2 release.
func (r *apiKeyResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		1: legacyStateUpgrader(r.Schema, nil),
	}
}
