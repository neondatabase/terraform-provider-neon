package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.ResourceWithImportState = (*customBranchDomainResource)(nil)
var _ resource.ResourceWithConfigure = (*customBranchDomainResource)(nil)

type customBranchDomainResource struct{ client *neonClient }
type customBranchDomainModel struct {
	ID            types.String `tfsdk:"id"`
	ProjectID     types.String `tfsdk:"project_id"`
	BranchID      types.String `tfsdk:"branch_id"`
	Domain        types.String `tfsdk:"domain"`
	EntityID      types.String `tfsdk:"entity_id"`
	EntityType    types.String `tfsdk:"entity_type"`
	CnameTarget   types.String `tfsdk:"cname_target"`
	Status        types.String `tfsdk:"status"`
	DNSStatus     types.String `tfsdk:"dns_status"`
	BindingStatus types.String `tfsdk:"binding_status"`
	StatusReason  types.String `tfsdk:"status_reason"`
}

func NewCustomBranchDomainResource() resource.Resource { return &customBranchDomainResource{} }
func (r *customBranchDomainResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_custom_branch_domain"
}
func (r *customBranchDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureBackend(req, resp)
}
func (r *customBranchDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	domain := backendIdentityAttribute("Lowercase fully qualified custom domain, without a trailing dot. Changing the binding replaces this resource.")
	domain.Validators = []validator.String{backendStringValidator{"domain"}}
	entity := backendIdentityAttribute("Function slug within the branch, typically neon_function.api.slug.")
	entity.Validators = []validator.String{backendStringValidator{"slug"}}
	entityType := backendIdentityAttribute("Target entity type. Currently function.")
	entityType.Required = false
	entityType.Optional = true
	entityType.Computed = true
	entityType.Default = stringdefault.StaticString("function")
	entityType.Validators = []validator.String{backendStringValidator{"entity_type"}}
	resp.Schema = schema.Schema{Description: "Registers a branch-scoped custom domain targeting a Neon Function. Creation returns before DNS/certificate activation, so DNS can depend on cname_target without a dependency cycle.", Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:    true,
			Description: "Composite ID: <project_id>/<branch_id>/<domain>.",
		},
		"project_id":  backendIdentityAttribute("Neon project ID."),
		"branch_id":   backendIdentityAttribute("Neon branch ID."),
		"domain":      domain,
		"entity_id":   entity,
		"entity_type": entityType,
		"cname_target": schema.StringAttribute{
			Computed:    true,
			Description: "Exact CNAME hostname returned by Neon. Empty if this region has no custom domain front door; never synthesized from the function URL.",
		},
		"status": schema.StringAttribute{
			Computed:    true,
			Description: "Current domain validity, commonly pending, active or error.",
		},
		"dns_status": schema.StringAttribute{
			Computed:    true,
			Description: "DNS/CAA status returned by Neon.",
		},
		"binding_status": schema.StringAttribute{
			Computed:    true,
			Description: "Neon routing status returned by Neon.",
		},
		"status_reason": schema.StringAttribute{
			Computed:    true,
			Description: "Machine-readable reason for a non-active status.",
		},
	}}
}

func setCustomBranchDomain(model *customBranchDomainModel, domain neon.CustomDomain) {
	model.ID = types.StringValue(strings.Join([]string{model.ProjectID.ValueString(), model.BranchID.ValueString(), domain.Domain}, "/"))
	model.Domain = types.StringValue(domain.Domain)
	model.EntityID = types.StringValue(domain.EntityID)
	model.EntityType = types.StringValue(domain.EntityType)
	model.CnameTarget = types.StringValue(domain.CnameTarget)
	model.Status = types.StringPointerValue(domain.Status)
	model.DNSStatus = types.StringPointerValue(domain.DnsStatus)
	model.BindingStatus = types.StringPointerValue(domain.BindingStatus)
	model.StatusReason = types.StringPointerValue(domain.StatusReason)
}

func (r *customBranchDomainResource) find(ctx context.Context, model customBranchDomainModel) (neon.CustomDomain, bool, error) {
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err != nil {
		return neon.CustomDomain{}, false, err
	}
	var cursor *string
	seen := map[string]bool{}
	for {
		// The generated SDK does not URL-encode query parameters.
		var encodedCursor *string
		if cursor != nil {
			value := url.QueryEscape(*cursor)
			encodedCursor = &value
		}
		page, err := client.ListProjectBranchCustomDomains(url.PathEscape(model.ProjectID.ValueString()), url.PathEscape(model.BranchID.ValueString()), encodedCursor, nil)
		if apiNotFound(err) {
			return neon.CustomDomain{}, false, nil
		}
		if err != nil {
			return neon.CustomDomain{}, false, err
		}
		for _, domain := range page.CustomDomains {
			if domain.Domain == model.Domain.ValueString() {
				return domain, true, nil
			}
		}
		if page.Pagination == nil || page.Pagination.Next == nil || *page.Pagination.Next == "" {
			return neon.CustomDomain{}, false, nil
		}
		cursor = page.Pagination.Next
		if seen[*cursor] {
			return neon.CustomDomain{}, false, fmt.Errorf("repeated pagination cursor when reading custom domains")
		}
		seen[*cursor] = true
	}
}

func (r *customBranchDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customBranchDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err != nil {
		resp.Diagnostics.AddError("Unable to Configure Neon Client", err.Error())
		return
	}
	domain, err := client.RegisterProjectBranchCustomDomain(url.PathEscape(plan.ProjectID.ValueString()), url.PathEscape(plan.BranchID.ValueString()), neon.CustomDomainRegisterRequest{Domain: plan.Domain.ValueString(), EntityID: plan.EntityID.ValueString(), EntityType: plan.EntityType.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Register Custom Domain", err.Error())
		return
	}
	// The registration response properties are optional in the OpenAPI schema.
	if domain.Domain == "" {
		domain.Domain = plan.Domain.ValueString()
	}
	if domain.EntityID == "" {
		domain.EntityID = plan.EntityID.ValueString()
	}
	if domain.EntityType == "" {
		domain.EntityType = plan.EntityType.ValueString()
	}
	setCustomBranchDomain(&plan, domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *customBranchDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customBranchDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	domain, found, err := r.find(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Custom Domain", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	setCustomBranchDomain(&state, domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *customBranchDomainResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected Custom Domain Update", "All custom domain binding inputs require replacement.")
}

func (r *customBranchDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customBranchDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "Configure the Neon provider.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client, closeBodies, err := r.client.scopedSDK(ctx)
	defer closeBodies()
	if err == nil {
		err = client.DeleteProjectBranchCustomDomain(url.PathEscape(state.ProjectID.ValueString()), url.PathEscape(state.BranchID.ValueString()), url.PathEscape(state.Domain.ValueString()))
	}
	if err != nil && !apiNotFound(err) {
		resp.Diagnostics.AddError("Unable to Delete Custom Domain", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *customBranchDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitBackendID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Custom Domain Import ID", err.Error())
		return
	}
	state := customBranchDomainModel{ID: types.StringValue(req.ID), ProjectID: types.StringValue(parts[0]), BranchID: types.StringValue(parts[1]), Domain: types.StringValue(parts[2])}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
