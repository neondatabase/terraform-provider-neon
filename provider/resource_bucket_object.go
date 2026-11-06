package provider

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithConfigure = (*neonBucketObjectResource)(nil)
var _ resource.ResourceWithImportState = (*neonBucketObjectResource)(nil)
var _ resource.ResourceWithModifyPlan = (*neonBucketObjectResource)(nil)

type neonBucketObjectResource struct {
	client *providerAdapter
}

type neonBucketObjectResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	BranchID      types.String `tfsdk:"branch_id"`
	Bucket        types.String `tfsdk:"bucket"`
	Key           types.String `tfsdk:"key"`
	Content       types.String `tfsdk:"content"`
	ContentBase64 types.String `tfsdk:"content_base64"`
	Source        types.String `tfsdk:"source"`
	IsDirectory   types.Bool   `tfsdk:"is_directory"`
	ContentType   types.String `tfsdk:"content_type"`
	ETag          types.String `tfsdk:"etag"`
	ContentLength types.Int64  `tfsdk:"content_length"`
	Trigger       types.String `tfsdk:"trigger"`
}

func (v neonBucketObjectResourceModel) readContent() (body io.Reader, checksum string, err error) {
	if !v.IsDirectory.ValueBool() {
		var content []byte
		if !v.Content.IsNull() && !v.Content.IsUnknown() {
			content = []byte(v.Content.ValueString())
		}
		if !v.ContentBase64.IsNull() && !v.ContentBase64.IsUnknown() {
			var err error
			content, err = base64.StdEncoding.DecodeString(v.ContentBase64.ValueString())
			if err != nil {
				return nil, "", fmt.Errorf("unable to decode base64 content: %w", err)
			}
		}
		if !v.Source.IsNull() && !v.Source.IsUnknown() {
			var err error
			content, err = os.ReadFile(v.Source.ValueString())
			if err != nil {
				return nil, "", fmt.Errorf("unable to open source file: %w", err)
			}
		}
		body = bytes.NewReader(content)
		checksum = fmt.Sprintf("%x", md5.Sum(content))
	}
	return body, checksum, nil
}

func NewNeonBucketObjectResource() resource.Resource {
	return &neonBucketObjectResource{}
}

func (r *neonBucketObjectResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_bucket_object"
}

func (r *neonBucketObjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Manages an object in a Neon branchable object storage bucket.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				PlanModifiers: idPlanModifiers(),
				Computed:      true,
				Description:   `Object state ID: <project_id>/<branch_id>/<bucket>/<key>.`,
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon project ID",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon branch ID",
			},
			"bucket": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The Neon object storage bucket name",
			},
			"key": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplaceString,
				Description:   "The object key.",
			},
			"content": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: `The content of the object. 
Note that the value is stored in the Terraform plan and state; use "source" to keep file contents out of them.
It **conflicts** with "content_base64", "source".`,
			},
			"content_base64": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: `The base64-encoded content of the object. 
Note that the value is stored in the Terraform plan and state; use "source" to keep file contents out of them.
It **conflicts** with "content" and "source".`,
			},
			"source": schema.StringAttribute{
				Optional: true,
				Description: `The absolute path to the the object's content.
It **conflicts** with "content" and "content_base64".`,
			},
			"content_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The MIME type of the object.",
			},
			"etag": schema.StringAttribute{
				Computed:    true,
				Description: "The object's entity tag (content hash).",
			},
			"content_length": schema.Int64Attribute{
				Computed:    true,
				Description: "The object size in bytes.",
			},
			"trigger": schema.StringAttribute{
				Computed: true,
				Optional: true,
				Description: `The provider-computed md5 check sum of the object, or user-provided string 
that is used as a trigger to re-upload the object.`,
			},
			"is_directory": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
				Description: `Set to true to provision a "folder". Note that it **conflicts** with "content", "content_base64", "source".`,
			},
		},
	}
}

func (r *neonBucketObjectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected providerAdapter, got an unexpected type.")
		return
	}
	r.client = client
}

func (r *neonBucketObjectResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || req.Config.Raw.IsNull() {
		return
	}

	var planned neonBucketObjectResourceModel
	var config neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planned)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Values supplied by other resources are unknown until apply; Terraform plans again then with known values.
	if config.Content.IsUnknown() || config.ContentBase64.IsUnknown() || config.Source.IsUnknown() {
		return
	}

	contentDefined := !config.Content.IsNull() && !config.Content.IsUnknown()
	contentBase64Defined := !config.ContentBase64.IsNull() && !config.ContentBase64.IsUnknown()
	sourceDefined := !config.Source.IsNull() && !config.Source.IsUnknown()
	isDir := !config.IsDirectory.IsNull() && !config.IsDirectory.IsUnknown() && config.IsDirectory.ValueBool()
	if config.IsDirectory.IsNull() || config.IsDirectory.IsUnknown() {
		isDir = !planned.IsDirectory.IsNull() && !planned.IsDirectory.IsUnknown() && planned.IsDirectory.ValueBool()
	}

	switch {
	case contentDefined && contentBase64Defined && sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content", "content_base64" and "source" cannot be specified together`)
	case contentDefined && contentBase64Defined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content" and "content_base64" cannot be specified together`)
	case contentDefined && sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content" and "source" cannot be specified together`)
	case contentBase64Defined && sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`"content_base64" and "source" cannot be specified together`)
	case isDir && (contentDefined || contentBase64Defined || sourceDefined):
		resp.Diagnostics.AddError("conflicting configuration",
			`"is_directory" cannot be specified with "content", "content_base64" or "source"`)
	case !isDir && !contentDefined && !contentBase64Defined && !sourceDefined:
		resp.Diagnostics.AddError("conflicting configuration",
			`either of the attributes must be provided: 
"is_directory", or "content", or "content_base64" or "source"`)
	}

	if isDir && !planned.ContentType.IsNull() && !config.ContentType.IsNull() {
		resp.Diagnostics.AddError("conflicting configuration",
			`"content_type" cannot be specified when "is_directory" is true"`)
	}

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *neonBucketObjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, checksum, err := plan.readContent()
	if err != nil {
		resp.Diagnostics.AddError("unable to read content", err.Error())
		return
	}
	if plan.Trigger.IsNull() || plan.Trigger.IsUnknown() {
		plan.Trigger = types.StringValue(checksum)
		if body == nil {
			plan.Trigger = types.StringNull()
		}
	}

	projectID := plan.ProjectID.ValueString()
	branchID := plan.BranchID.ValueString()
	bucket := plan.Bucket.ValueString()
	key := plan.Key.ValueString()
	if plan.IsDirectory.ValueBool() {
		key = folderObjectKey(key)
	}

	etag, contentLength, contentType, err := upload(ctx, r.client.sdk, r.client.httpClient,
		plan.ContentType.ValueStringPointer(), body, projectID, branchID, bucket, key)
	if err != nil {
		resp.Diagnostics.AddError("unable to upload bucket object", err.Error())
		return
	}

	plan.ID = types.StringValue(bucketObjectID(projectID, branchID, bucket, plan.Key.ValueString()))
	plan.ETag = types.StringValue(etag)
	plan.ContentLength = types.Int64Value(contentLength)
	plan.ContentType = types.StringValue(contentType)
	if plan.IsDirectory.ValueBool() {
		plan.ContentType = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonBucketObjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := state.Key.ValueString()
	if state.IsDirectory.ValueBool() {
		key = folderObjectKey(key)
	}

	headers, err := readObjectHeaders(ctx, r.client.sdk, state.ProjectID.ValueString(), state.BranchID.ValueString(),
		state.Bucket.ValueString(), key)
	switch {
	case err == nil:
	case isNotFoundError(err):
		resp.State.RemoveResource(ctx)
		return
	case errors.As(err, &objectNotFoundError{}):
		resp.State.RemoveResource(ctx)
		return
	default:
		resp.Diagnostics.Append(newDiagnostics(err)...)
		return
	}

	state.ContentLength = types.Int64Value(headers.Size)
	state.ETag = types.StringValue(headers.Etag)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *neonBucketObjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, checksum, err := plan.readContent()
	if err != nil {
		resp.Diagnostics.AddError("unable to read content", err.Error())
		return
	}

	projectID := plan.ProjectID.ValueString()
	branchID := plan.BranchID.ValueString()
	bucket := plan.Bucket.ValueString()
	key := plan.Key.ValueString()
	if plan.IsDirectory.ValueBool() {
		key = folderObjectKey(key)
	}

	etag, contentLength, contentType, err := upload(ctx, r.client.sdk, r.client.httpClient,
		plan.ContentType.ValueStringPointer(), body, projectID, branchID, bucket, key)
	if err != nil {
		resp.Diagnostics.AddError("unable to upload bucket object", err.Error())
		return
	}
	if plan.Trigger.IsNull() || plan.Trigger.IsUnknown() {
		plan.Trigger = types.StringValue(checksum)
		if body == nil {
			plan.Trigger = types.StringNull()
		}
	}

	plan.ID = types.StringValue(bucketObjectID(projectID, branchID, bucket, plan.Key.ValueString()))
	plan.ETag = types.StringValue(etag)
	plan.ContentLength = types.Int64Value(contentLength)
	plan.ContentType = types.StringValue(contentType)
	if plan.IsDirectory.ValueBool() {
		plan.ContentType = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonBucketObjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	var state neonBucketObjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := state.Key.ValueString()
	if state.IsDirectory.ValueBool() {
		key = folderObjectKey(key)
	}
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		return r.client.sdk.DeleteProjectBranchBucketObject(
			state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			state.Bucket.ValueString(),
			url.PathEscape(key),
		)
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *neonBucketObjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 4)
	if len(els) != 4 {
		resp.Diagnostics.AddError("Invalid Neon Bucket Object Import ID",
			"Expected <project_id>/<branch_id>/<bucket>/<key>.")
		return
	}
	state := neonBucketObjectResourceModel{
		ID:            types.StringValue(req.ID),
		ProjectID:     types.StringValue(els[0]),
		BranchID:      types.StringValue(els[1]),
		Bucket:        types.StringValue(els[2]),
		Key:           types.StringValue(els[3]),
		Content:       types.StringNull(),
		ContentBase64: types.StringNull(),
		Trigger:       types.StringNull(),
		ContentType:   types.StringNull(),
	}
	if state.Source.IsUnknown() {
		state.Source = types.StringNull()
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}
	key := state.Key.ValueString()
	if state.IsDirectory.ValueBool() {
		key = folderObjectKey(key)
	}
	meta, err := readObjectHeaders(ctx, r.client.sdk,
		state.ProjectID.ValueString(), state.BranchID.ValueString(), state.Bucket.ValueString(), key,
	)
	if err != nil {
		resp.Diagnostics.AddError("error reading object meta", err.Error())
		return
	}
	state.ContentLength = types.Int64Value(meta.Size)
	state.ETag = types.StringValue(meta.Etag)

	var presignResp neon.PresignResponse
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(ctx context.Context) error {
		var err error
		presignResp, err = r.client.sdk.PresignProjectBranchBucketObject(state.ProjectID.ValueString(),
			state.BranchID.ValueString(),
			state.Bucket.ValueString(),
			key, neon.PresignRequest{Operation: neon.PresignRequestOperationDownload},
		)
		return err
	}, nil)...)
	if resp.Diagnostics.HasError() {
		return
	}
	contentType, ok := presignResp.Headers["Content-Type"]
	if !ok {
		contentType, ok = presignResp.Headers["content-type"]
	}
	if ok {
		state.ContentType = types.StringValue(fmt.Sprint(contentType))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func upload(ctx context.Context, sdk *neon.Client, httpClient httpClient, contentType *string, body io.Reader,
	projectID string, branchID string, bucket string, key string) (etag string, contentLength int64,
	contentTypeResp string, err error) {
	// set an arbitrary long expiration time to ensure that the large object can be uploaded
	var expTime int64 = 3600
	var presignResp neon.PresignResponse
	if err := projectReadiness.Do(ctx, func(ctx context.Context) error {
		var err error
		presignResp, err = sdk.PresignProjectBranchBucketObject(
			projectID, branchID, bucket, url.PathEscape(key), neon.PresignRequest{
				ContentType:      contentType,
				ExpiresInSeconds: &expTime,
				Operation:        neon.PresignRequestOperationUpload,
			})
		return err
	}, nil); err != nil {
		return "", 0, "",
			fmt.Errorf("unable to generate presign URL to upload to bucket %s/%s/%s: %w",
				projectID, branchID, bucket, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, presignResp.Method, presignResp.URL, body)
	if err != nil {
		return "", 0, "", fmt.Errorf("unable to create HTTP request: %w", err)
	}

	for k, v := range presignResp.Headers {
		httpReq.Header.Set(k, fmt.Sprint(v))
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", 0, "",
			fmt.Errorf("unable to upload object %q to bucket %s/%s/%s: %w", key, projectID, branchID, bucket, err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return "", 0, "",
			fmt.Errorf("unable to upload object %q to bucket %s/%s/%s: HTTP %d, Resp: %s",
				key, projectID, branchID, bucket, httpResp.StatusCode, respBody)
	}

	meta, err := readObjectHeaders(ctx, sdk, projectID, branchID, bucket, key)
	if err != nil {
		return "", 0, "",
			fmt.Errorf("unable to read object metadata: %w", err)
	}

	return meta.Etag, meta.Size, httpReq.Header.Get("Content-Type"), nil
}

func readObjectHeaders(ctx context.Context, sdk *neon.Client, projectID string, branchID string, bucket string,
	key string) (neon.BucketObject, error) {
	var cursor, prefix *string
	// Object keys use "/" as their separator on every OS, and keep repeated or dot segments as is.
	if i := strings.LastIndex(key, "/"); i > 0 {
		prefixTmp := key[:i]
		prefix = &prefixTmp
	}
	for {
		var resp neon.BucketObjectsListResponse
		if err := projectReadiness.Do(ctx, func(_ context.Context) error {
			var err error
			resp, err = sdk.ListProjectBranchBucketObjects(projectID, branchID, bucket, prefix, nil, cursor,
				nil)
			return err
		}, nil); err != nil {
			return neon.BucketObject{}, err
		}
		for _, obj := range resp.Objects {
			if key == obj.Key {
				return obj, nil
			}
		}
		if !resp.IsTruncated || resp.NextCursor == nil {
			break
		}
		cursor = resp.NextCursor
	}
	return neon.BucketObject{}, objectNotFoundError{
		key:       key,
		projectID: projectID,
		branchID:  branchID,
		bucket:    bucket,
	}
}

type objectNotFoundError struct {
	key       string
	projectID string
	branchID  string
	bucket    string
}

func (o objectNotFoundError) Error() string {
	return fmt.Sprintf("object %q not found in the projectID/branchID/bucket %s/%s/%s", o.key, o.projectID,
		o.branchID, o.bucket)
}

func folderObjectKey(key string) string {
	return strings.TrimSuffix(key, "/") + "/.emptyFolderPlaceholder"
}

// bucketObjectID is the <project_id>/<branch_id>/<bucket>/<key> state ID; the key keeps its own slashes.
func bucketObjectID(projectID, branchID, bucket, key string) string {
	return strings.Join([]string{projectID, branchID, bucket, key}, "/")
}
