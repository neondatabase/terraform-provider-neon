package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.Resource = (*jwksURLResource)(nil)
var _ resource.ResourceWithConfigure = (*jwksURLResource)(nil)

type jwksURLResource struct {
	client *neon.Client
}

type jwksURLResourceModel struct {
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	JWKSURL      types.String `tfsdk:"jwks_url"`
	ProviderName types.String `tfsdk:"provider_name"`
	RoleNames    []string     `tfsdk:"role_names"`
	BranchID     types.String `tfsdk:"branch_id"`
	JWTAudience  types.String `tfsdk:"jwt_audience"`
}

func NewJWKSURLResource() resource.Resource {
	return &jwksURLResource{}
}

func (r *jwksURLResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_jwks_url"
}

func (r *jwksURLResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Project JWKS URL. See details: https://neon.com/docs/data-api/custom-authentication-providers\n\n~>**WARNING** The resource does not support import.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The JWKS configuration ID.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "Project ID.",
			},
			"jwks_url": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The URL that lists the JWKS.",
			},
			"provider_name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The name of the authentication provider.",
			},
			"role_names": schema.ListAttribute{
				ElementType:   types.StringType,
				Required:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.RequiresReplace()},
				Validators: []validator.List{
					roleNamesLengthValidator{},
				},
				Description: "The roles the JWKS should be mapped to.",
			},
			"branch_id": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: requiresReplace,
				Description:   "Branch ID.",
			},
			"jwt_audience": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: requiresReplace,
				Description:   "The name of the required JWT Audience to be used.",
			},
		},
	}
}

type roleNamesLengthValidator struct{}

func (roleNamesLengthValidator) Description(context.Context) string {
	return "role_names must contain between 1 and 10 elements"
}

func (v roleNamesLengthValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v roleNamesLengthValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	length := len(req.ConfigValue.Elements())
	if length > 10 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid role_names",
			fmt.Sprintf("%s, got %d", v.Description(ctx), length),
		)
	}
}

func (r *jwksURLResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *jwksURLResource) ImportState(_ context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"the resource does not support import, please recreate it instead",
	)
}

func (r *jwksURLResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state jwksURLResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := neon.AddProjectJWKSRequest{
		JwksURL:      state.JWKSURL.ValueString(),
		ProviderName: state.ProviderName.ValueString(),
		RoleNames:    state.RoleNames,
	}
	if !state.BranchID.IsNull() && !state.BranchID.IsUnknown() && state.BranchID.ValueString() != "" {
		cfg.BranchID = pointer(state.BranchID.ValueString())
	}
	if !state.JWTAudience.IsNull() && !state.JWTAudience.IsUnknown() && state.JWTAudience.ValueString() != "" {
		cfg.JwtAudience = pointer(state.JWTAudience.ValueString())
	}

	var result neon.JWKSCreationOperation
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		var err error
		result, err = r.client.AddProjectJWKS(state.ProjectID.ValueString(), cfg)
		if err == nil {
			waitUnfinishedOperations(ctx, r.client, result.OperationsResponse.Operations)
		}
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(result.Jwks.ID)
	setJWKSModel(&state, result.JWKSResponse.Jwks, cfg.RoleNames)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *jwksURLResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state jwksURLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.ProjectJWKSResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		result, err = r.client.GetProjectJWKS(state.ProjectID.ValueString())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, jwks := range result.Jwks {
		if jwks.ID == state.ID.ValueString() {
			setJWKSModel(&state, jwks, nil)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	tflog.Debug(ctx, "JWKS not found, removing from state", map[string]any{
		"id":         state.ID.ValueString(),
		"project_id": state.ProjectID.ValueString(),
	})
	resp.State.RemoveResource(ctx)
}

func (r *jwksURLResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("JWKS URL Update Not Supported", "Changing the JWKS URL attributes requires replacing the resource.")
}

func (r *jwksURLResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state jwksURLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		_, err := r.client.DeleteProjectJWKS(state.ProjectID.ValueString(), state.ID.ValueString())
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func setJWKSModel(model *jwksURLResourceModel, value neon.JWKS, roleNames []string) {
	model.ID = types.StringValue(value.ID)
	model.ProjectID = types.StringValue(value.ProjectID)
	model.JWKSURL = types.StringValue(value.JwksURL)
	model.ProviderName = types.StringValue(value.ProviderName)
	if roleNames != nil {
		model.RoleNames = roleNames
	}
	if value.BranchID != nil {
		model.BranchID = types.StringValue(*value.BranchID)
	}
	if value.JwtAudience != nil {
		model.JWTAudience = types.StringValue(*value.JwtAudience)
	}
}
