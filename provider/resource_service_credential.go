package provider

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonServiceCredentialResource)(nil)
var _ resource.ResourceWithImportState = (*neonServiceCredentialResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonServiceCredentialResource)(nil)

type neonServiceCredentialResource struct {
	client *neon.Client
}

type neonServiceCredentialResourceModel struct {
	ID                types.String `tfsdk:"id"`
	ProjectID         types.String `tfsdk:"project_id"`
	BranchID          types.String `tfsdk:"branch_id"`
	Name              types.String `tfsdk:"name"`
	Scopes            []string     `tfsdk:"scopes"`
	TokenID           types.String `tfsdk:"token_id"`
	TokenIDShort      types.String `tfsdk:"token_id_short"`
	APIToken          types.String `tfsdk:"api_token"`
	S3SecretAccessKey types.String `tfsdk:"s3_secret_access_key"`
	CreatedAt         types.String `tfsdk:"created_at"`
	ExpiresAt         types.String `tfsdk:"expires_at"`
	IsExpired         types.Bool   `tfsdk:"is_expired"`
}

func NewNeonServiceCredentialResource() resource.Resource {
	return &neonServiceCredentialResource{}
}

func (r *neonServiceCredentialResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_service_credential"
}

func (r *neonServiceCredentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	requiresReplaceList := []planmodifier.List{listplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: "Manages a Neon scoped service credential.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The credential resource ID.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon branch ID.",
			},
			"name": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "A free-form customer label for the credential. It's an empty string if not set.",
			},
			"scopes": schema.ListAttribute{
				ElementType:   types.StringType,
				Required:      true,
				PlanModifiers: requiresReplaceList,
				Description:   "The capabilities granted to the credential.",
			},
			"token_id": schema.StringAttribute{
				Computed:    true,
				Description: "The opaque credential ID.",
			},
			"token_id_short": schema.StringAttribute{
				Computed:    true,
				Description: "The first 12 hexadecimal characters of the credential ID.",
			},
			"api_token": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "The API token for the credential.",
			},
			"s3_secret_access_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "The S3-compatible secret access key for the credential.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the credential was created.",
			},
			"expires_at": schema.StringAttribute{
				Computed:    true,
				Description: "When the credential expires.",
			},
			"is_expired": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the credential has expired.",
			},
		},
	}
}

func (r *neonServiceCredentialResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var prior neonServiceCredentialResourceModel
	var planned neonServiceCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if stringChanged(prior.ProjectID, planned.ProjectID) ||
		stringChanged(prior.BranchID, planned.BranchID) ||
		stringChanged(prior.Name, planned.Name) ||
		!slices.Equal(prior.Scopes, planned.Scopes) {
		resp.Diagnostics.AddError(
			"Neon Service Credential Update Not Supported",
			"Changing service credential attributes is not supported. Remove and recreate the resource instead.",
		)
	}
}

func (r *neonServiceCredentialResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonServiceCredentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.Split(req.ID, "/")
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Expected an import ID in the form <project_id>/<branch_id>/<token_id>.",
		)
		return
	}
	projectID := els[0]
	branchID := els[1]
	tokenID := els[2]

	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var result neon.ListCredentialsResponse
	resp.Diagnostics.Append(projectReadiness.Retry(func(_ context.Context) error {
		var err error
		result, err = r.client.ListCredentials(projectID, branchID)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var metadata *neon.CredentialMeta
	for _, credential := range result.Credentials {
		if tokenID == credential.TokenID {
			metadata = &credential
			break
		}
	}

	if metadata == nil {
		resp.Diagnostics.AddError(
			"Service Credential Not Found",
			fmt.Sprintf("The service credential %q was not found on branch %q.", tokenID, branchID),
		)
		return
	}

	state := neonServiceCredentialResourceModel{
		ID:        types.StringValue(req.ID),
		ProjectID: types.StringValue(projectID),
		BranchID:  types.StringValue(branchID),
		TokenID:   types.StringValue(tokenID),
	}
	setServiceCredentialMetadata(&state, *metadata)

	var secret neon.CredentialSecret
	var revealNotFound bool
	var revealUnavailable bool
	resp.Diagnostics.Append(projectReadiness.RetryWithFallback(func(_ context.Context) error {
		var err error
		secret, err = r.client.RevealCredential(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			state.TokenID.ValueString(),
		)
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			revealNotFound = true
			return nil
		},
		http.StatusConflict: func(_ context.Context) error {
			revealUnavailable = true
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	if revealNotFound {
		resp.Diagnostics.AddWarning(
			"Credential Secret Not Found",
			"This credential was not found. Rotate it outside Terraform to obtain new secret material.",
		)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	if revealUnavailable {
		resp.Diagnostics.AddWarning(
			"Credential Secret Not Available",
			"This credential has no recoverable secret. Rotate it outside Terraform to obtain new secret material.",
		)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	state.APIToken = types.StringValue(secret.APIToken)
	state.S3SecretAccessKey = types.StringValue(secret.S3SecretAccessKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonServiceCredentialResource) Create(ctx context.Context, req resource.CreateRequest,
	resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonServiceCredentialResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	scopes := make([]neon.CredentialScope, 0, len(state.Scopes))
	for _, value := range state.Scopes {
		scope, err := neon.NewCredentialScope(value)
		if err != nil {
			resp.Diagnostics.AddError("Invalid Credential Scope", err.Error())
			return
		}
		scopes = append(scopes, scope)
	}

	cfg := neon.CreateCredentialRequest{
		PrincipalType: neon.CreateCredentialRequestPrincipalTypeUser,
		Scopes:        scopes,
	}
	if !state.Name.IsNull() && !state.Name.IsUnknown() {
		name := state.Name.ValueString()
		cfg.Name = &name
	}

	var result neon.CreateCredentialResponse
	resp.Diagnostics.Append(projectReadiness.Retry(func(_ context.Context) error {
		var err error
		result, err = r.client.CreateCredential(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			cfg,
		)
		return err
	}, ctx)...)
	if resp.Diagnostics.HasError() {
		return
	}

	setServiceCredentialModel(&state, result)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonServiceCredentialResource) Read(ctx context.Context, req resource.ReadRequest,
	resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonServiceCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.ListCredentialsResponse
	resp.Diagnostics.Append(projectReadiness.RetryWithFallback(func(_ context.Context) error {
		var err error
		result, err = r.client.ListCredentials(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
		)
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			resp.State.RemoveResource(ctx)
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	var metadata *neon.CredentialMeta
	for _, credential := range result.Credentials {
		if state.TokenID.ValueString() == credential.TokenID {
			metadata = &credential
			break
		}
	}
	if metadata == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if metadata.RevokedAt != nil {
		resp.Diagnostics.AddWarning("Credential Revoked", "This credential has been revoked.")
		resp.State.RemoveResource(ctx)
		return
	}

	setServiceCredentialMetadata(&state, *metadata)

	var secret neon.CredentialSecret
	var revealNotFound bool
	var revealUnavailable bool
	resp.Diagnostics.Append(projectReadiness.RetryWithFallback(func(_ context.Context) error {
		var err error
		secret, err = r.client.RevealCredential(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			state.TokenID.ValueString(),
		)
		return err
	}, ctx, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			revealNotFound = true
			return nil
		},
		http.StatusConflict: func(_ context.Context) error {
			revealUnavailable = true
			return nil
		},
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	if revealNotFound {
		resp.Diagnostics.AddWarning(
			"Credential Secret Not Found",
			"This credential was not found. Rotate it outside Terraform to obtain new secret material.",
		)
		if !state.IsExpired.ValueBool() {
			resp.Diagnostics.AddWarning(
				"Credential Secret Expired",
				"This credential has expired, it's being removed from the state.",
			)
			resp.State.RemoveResource(ctx)
			return
		}
		state.APIToken = types.StringNull()
		state.S3SecretAccessKey = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	if revealUnavailable {
		resp.Diagnostics.AddWarning(
			"Credential Secret Not Available",
			"This credential has no recoverable secret. Rotate it outside Terraform to obtain new secret material.",
		)
		state.APIToken = types.StringNull()
		state.S3SecretAccessKey = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	state.APIToken = types.StringValue(secret.APIToken)
	state.S3SecretAccessKey = types.StringValue(secret.S3SecretAccessKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonServiceCredentialResource) Update(ctx context.Context, req resource.UpdateRequest,
	resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Neon Credential Update Not Supported",
		"Changing credential attributes requires replacing the resource.",
	)
}

func (r *neonServiceCredentialResource) Delete(ctx context.Context, req resource.DeleteRequest,
	resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonServiceCredentialResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.RetryWithFallback(func(_ context.Context) error {
		return r.client.RevokeCredential(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			state.TokenID.ValueString(),
		)
	}, ctx, map[int]func(context.Context) error{
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

func setServiceCredentialModel(model *neonServiceCredentialResourceModel, result neon.CreateCredentialResponse) {
	model.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
		model.ProjectID.ValueString(), result.BranchID, result.TokenID))
	model.BranchID = types.StringValue(result.BranchID)
	model.TokenID = types.StringValue(result.TokenID)
	model.TokenIDShort = types.StringValue(result.TokenIDShort)
	model.APIToken = types.StringValue(result.APIToken)
	model.S3SecretAccessKey = types.StringValue(result.S3SecretAccessKey)
	model.CreatedAt = types.StringValue(result.CreatedAt.Format(time.RFC3339))
	model.Scopes = grantedCredentialScopes(result.Scopes)
	if result.Name == nil {
		model.Name = types.StringValue("")
	} else {
		model.Name = types.StringValue(*result.Name)
	}
	setCredentialExpiration(model, result.ExpiresAt)
}

func setServiceCredentialMetadata(model *neonServiceCredentialResourceModel, metadata neon.CredentialMeta) {
	model.TokenID = types.StringValue(metadata.TokenID)
	model.TokenIDShort = types.StringValue(metadata.TokenIDShort)
	if metadata.BranchID != nil {
		model.BranchID = types.StringValue(*metadata.BranchID)
	}
	model.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
		model.ProjectID.ValueString(), model.BranchID.ValueString(), metadata.TokenID))
	if metadata.Name == nil {
		model.Name = types.StringValue("")
	} else {
		model.Name = types.StringValue(*metadata.Name)
	}
	model.CreatedAt = types.StringValue(metadata.CreatedAt.Format(time.RFC3339))
	model.Scopes = grantedCredentialScopes(metadata.Scopes)
	setCredentialExpiration(model, metadata.ExpiresAt)
}

func setCredentialExpiration(model *neonServiceCredentialResourceModel, expiresAt *time.Time) {
	if expiresAt == nil {
		model.ExpiresAt = types.StringNull()
		model.IsExpired = types.BoolValue(false)
		return
	}
	model.ExpiresAt = types.StringValue(expiresAt.Format(time.RFC3339))
	model.IsExpired = types.BoolValue(isCredentialExpired(expiresAt))
}

func isCredentialExpired(expiresAt *time.Time) bool {
	return expiresAt != nil && !expiresAt.After(time.Now())
}

func grantedCredentialScopes(scopes []neon.GrantedCredentialScope) []string {
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		result = append(result, scope.String())
	}
	return result
}
