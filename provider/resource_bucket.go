package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonBucketResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonBucketResource)(nil)
var _ resource.ResourceWithImportState = (*neonBucketResource)(nil)

type neonBucketResource struct {
	client *neon.Client
}

type neonBucketResourceModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	BranchID    types.String `tfsdk:"branch_id"`
	Name        types.String `tfsdk:"name"`
	AccessLevel types.String `tfsdk:"access_level"`
	S3Endpoint  types.String `tfsdk:"s3_endpoint"`
	Region      types.String `tfsdk:"region"`
}

func NewNeonBucketResource() resource.Resource {
	return &neonBucketResource{}
}

func (r *neonBucketResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_bucket"
}

func (r *neonBucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: "Creates and manages a branchable object storage bucket on a Neon branch.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The bucket resource ID.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon project ID",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon branch ID",
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The bucket name.",
			},
			"access_level": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: requiresReplace,
				Default:       stringdefault.StaticString(neon.BucketAccessLevelPrivate.String()),
				Validators: []validator.String{
					bucketAccessLevelValidator{},
				},
				Description: "Access level for the bucket. " +
					"Defaults to `private`. " +
					"Set to `public_read` to allow anonymous `GetObject`/`HeadObject` on objects in this bucket.",
			},
			"s3_endpoint": schema.StringAttribute{
				Computed:    true,
				Description: "The S3-compatible endpoint URL for this branch.",
			},
			"region": schema.StringAttribute{
				Computed:    true,
				Description: "The AWS region for this branch's object storage.",
			},
		},
	}
}

type bucketAccessLevelValidator struct{}

func (bucketAccessLevelValidator) Description(context.Context) string {
	return "validates that the bucket access level is supported by the Neon API"
}

func (v bucketAccessLevelValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (bucketAccessLevelValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	_, err := neon.NewBucketAccessLevel(req.ConfigValue.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid bucket access level",
			err.Error(),
		)
	}
}

func (r *neonBucketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *neonBucketResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var prior neonBucketResourceModel
	var planned neonBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if stringChanged(prior.ProjectID, planned.ProjectID) ||
		stringChanged(prior.BranchID, planned.BranchID) ||
		stringChanged(prior.Name, planned.Name) ||
		stringChanged(prior.AccessLevel, planned.AccessLevel) {
		resp.Diagnostics.AddError(
			"Neon Bucket Update Not Supported",
			"Changing bucket attributes is not supported. Remove and recreate the resource instead.",
		)
	}
}

func (r *neonBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.Split(req.ID, "/")
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Neon Bucket Import ID",
			"Expected an import ID in the form <project_id>/<branch_id>/<bucket_name>.",
		)
		return
	}

	projectID := els[0]
	branchID := els[1]
	bucketName := els[2]

	var branchStorageResp neon.BranchStorage
	resp.Diagnostics.Append(
		projectReadiness.RetryWithFallback(
			func(ctx context.Context) error {
				var err error
				branchStorageResp, err = r.client.GetProjectBranchStorage(projectID, branchID)
				return err
			}, ctx, map[int]func(context.Context) error{
				http.StatusNotFound: func(_ context.Context) error {
					resp.Diagnostics.AddError(
						"Bucket Not Found",
						"Branchable-storage is not enabled.",
					)
					return nil
				},
			},
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	var result neon.BucketsListResponse
	resp.Diagnostics.Append(
		projectReadiness.Retry(func(ctx context.Context) error {
			var err error
			result, err = r.client.ListProjectBranchBuckets(
				projectID,
				branchID,
			)
			return err

		}, ctx)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, bucket := range result.Buckets {
		if bucket.Name == bucketName {
			state := neonBucketResourceModel{
				ID:        types.StringValue(req.ID),
				ProjectID: types.StringValue(projectID),
				BranchID:  types.StringValue(branchID),
				Name:      types.StringValue(bucketName),
			}
			setNeonBucketModel(&state, bucket, branchStorageResp)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.Diagnostics.AddError(
		"Bucket Not Found",
		"The requested bucket was not found.",
	)
}

func (r *neonBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var branchStorageResp neon.BranchStorage
	resp.Diagnostics.Append(
		projectReadiness.Retry(
			func(ctx context.Context) error {
				var err error
				branchStorageResp, err = r.client.GetProjectBranchStorage(state.ProjectID.ValueString(),
					state.BranchID.ValueString())
				return err
			}, ctx,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	if !branchStorageResp.Enabled {
		resp.Diagnostics.AddError("Branch Storage Not Enabled", "The branch storage is not enabled.")
		return
	}

	accessLevel, err := neon.NewBucketCreateRequestAccessLevel(state.AccessLevel.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid Bucket Access Level", err.Error())
		return
	}

	var result neon.BucketResponse
	resp.Diagnostics.Append(
		projectReadiness.Retry(
			func(ctx context.Context) error {
				result, err = r.client.CreateProjectBranchBucket(
					state.ProjectID.ValueString(),
					state.BranchID.ValueString(),
					neon.BucketCreateRequest{
						Name:        state.Name.ValueString(),
						AccessLevel: &accessLevel,
					},
				)
				return err
			}, ctx)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	setNeonBucketModel(&state, result.Bucket, branchStorageResp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var branchStorageResp neon.BranchStorage
	resp.Diagnostics.Append(
		projectReadiness.Retry(
			func(ctx context.Context) error {
				var err error
				branchStorageResp, err = r.client.GetProjectBranchStorage(state.ProjectID.ValueString(),
					state.BranchID.ValueString())
				return err
			}, ctx,
		)...,
	)
	if resp.Diagnostics.HasError() {
		return
	}

	if !branchStorageResp.Enabled {
		resp.Diagnostics.AddError("Branch Storage Not Enabled", "The branch storage is not enabled.")
		return
	}

	var result neon.BucketsListResponse
	resp.Diagnostics.Append(
		projectReadiness.RetryWithFallback(func(ctx context.Context) error {
			var err error
			result, err = r.client.ListProjectBranchBuckets(
				state.ProjectID.ValueString(),
				state.BranchID.ValueString(),
			)
			return err

		}, ctx, map[int]func(context.Context) error{
			http.StatusNotFound: func(_ context.Context) error {
				return nil
			},
		})...,
	)

	if resp.Diagnostics.HasError() {
		return
	}

	for _, bucket := range result.Buckets {
		if bucket.Name == state.Name.ValueString() {
			setNeonBucketModel(&state, bucket, branchStorageResp)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *neonBucketResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Neon Bucket Update Not Supported",
		"Changing bucket attributes requires replacing the resource.",
	)
}

func (r *neonBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(
		projectReadiness.RetryWithFallback(func(ctx context.Context) error {
			return r.client.DeleteProjectBranchBucket(
				state.ProjectID.ValueString(),
				state.BranchID.ValueString(),
				state.Name.ValueString(),
			)
		}, ctx,
			map[int]func(context.Context) error{
				http.StatusNotFound: func(_ context.Context) error {
					return nil
				},
			},
		)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}

func setNeonBucketModel(model *neonBucketResourceModel, bucket neon.Bucket, branchStorageResp neon.BranchStorage) {
	model.ID = types.StringValue(neonBucketID(model.ProjectID.ValueString(), model.BranchID.ValueString(), bucket.Name))
	model.Name = types.StringValue(bucket.Name)
	model.AccessLevel = types.StringValue(bucket.AccessLevel.String())
	model.S3Endpoint = types.StringValue(branchStorageResp.S3Endpoint)
	model.Region = types.StringValue(branchStorageResp.Region)
}

func neonBucketID(projectID, branchID, bucketName string) string {
	return fmt.Sprintf("%s/%s/%s", projectID, branchID, bucketName)
}
