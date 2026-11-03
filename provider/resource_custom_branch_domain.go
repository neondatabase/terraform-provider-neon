package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
)

var _ resource.Resource = (*neonCustomBranchDomain)(nil)
var _ resource.ResourceWithConfigure = (*neonCustomBranchDomain)(nil)
var _ resource.ResourceWithImportState = (*neonCustomBranchDomain)(nil)

type neonCustomBranchDomain struct {
	client *neon.Client
}

type neonCustomBranchDomainModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	BranchID    types.String `tfsdk:"branch_id"`
	Domain      types.String `tfsdk:"domain"`
	EntityID    types.String `tfsdk:"entity_id"`
	EntityType  types.String `tfsdk:"entity_type"`
	CnameTarget types.String `tfsdk:"cname_target"`
}

func NewNeonCustomBranchDomainResource() resource.Resource {
	return &neonCustomBranchDomain{}
}

func (r *neonCustomBranchDomain) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "neon_custom_branch_domain"
}

func (r *neonCustomBranchDomain) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		Description: `Manages custom domains for a project's branch.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The custom branch domain ID.",
			},
			"project_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The Neon project ID.",
			},
			"branch_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The branch ID.",
			},
			"domain": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The custom domain.",
			},
			"entity_type": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description:   "The kind of branch entity the domain targets. Possible values: \"function\".",
			},
			"entity_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: requiresReplace,
				Description: "The target entity's identifier within the branch. " +
					"For `function` this is the function slug (which must already exist on the branch).",
			},
			"cname_target": schema.StringAttribute{
				Computed: true,
				Description: "The CNAME target provided by Neon to configure DNS for the custom domain to point to the " +
					"resource, e.g., Neon function.",
			},
		},
	}
}

func (r *neonCustomBranchDomain) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*providerAdapter)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *providerAdapter, got an unexpected type.")
		return
	}
	if client.sdk == nil {
		resp.Diagnostics.AddError("SDK is not configured", "")
		return
	}
	r.client = client.sdk
}

func (r *neonCustomBranchDomain) ImportState(ctx context.Context, req resource.ImportStateRequest,
	resp *resource.ImportStateResponse) {
	els := strings.SplitN(req.ID, "/", 3)
	if len(els) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Custom Branch Domain Import ID",
			"Expected an import ID in the form <project_id>/<branch_id>/<domain>.",
		)
		return
	}

	state := neonCustomBranchDomainModel{
		ID:        types.StringValue(req.ID),
		ProjectID: types.StringValue(els[0]),
		BranchID:  types.StringValue(els[1]),
		Domain:    types.StringValue(els[2]),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := fundCustomBranchDomain(ctx, r.client, state.ProjectID.ValueString(), state.BranchID.ValueString(),
		state.Domain.ValueString())
	switch {
	case err == nil:
		state.EntityID = types.StringValue(domain.EntityID)
		state.EntityType = types.StringValue(domain.EntityType)
		state.CnameTarget = types.StringValue(domain.CnameTarget)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	case errors.As(err, &domainNotFoundErr{}):
		resp.Diagnostics.AddError(err.Error(), "")

	default:
		resp.Diagnostics.AddError("Could not read the custom domain", err.Error())
	}
}

func (r *neonCustomBranchDomain) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var plan neonCustomBranchDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var respData neon.CustomDomain
	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		var err error
		respData, err = r.client.RegisterProjectBranchCustomDomain(plan.ProjectID.ValueString(), plan.BranchID.ValueString(),
			neon.CustomDomainRegisterRequest{
				Domain:     plan.Domain.ValueString(),
				EntityID:   plan.EntityID.ValueString(),
				EntityType: plan.EntityType.ValueString(),
			},
		)
		return err
	}, nil)...)

	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", plan.ProjectID.ValueString(),
		plan.BranchID.ValueString(), plan.Domain.ValueString()))
	plan.CnameTarget = types.StringValue(respData.CnameTarget)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *neonCustomBranchDomain) Update(_ context.Context, _ resource.UpdateRequest, _ *resource.UpdateResponse) {
}

func (r *neonCustomBranchDomain) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonCustomBranchDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := fundCustomBranchDomain(ctx, r.client, state.ProjectID.ValueString(), state.BranchID.ValueString(),
		state.Domain.ValueString())
	switch {
	case err == nil:
		state.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", state.ProjectID.ValueString(),
			state.BranchID.ValueString(), state.Domain.ValueString()))
		state.EntityID = types.StringValue(domain.EntityID)
		state.EntityType = types.StringValue(domain.EntityType)
		state.CnameTarget = types.StringValue(domain.CnameTarget)

	case errors.As(err, &domainNotFoundErr{}):
		resp.Diagnostics.AddWarning(err.Error(), "")
		resp.State.RemoveResource(ctx)

	default:
		resp.Diagnostics.AddError("Could not read the custom domain", err.Error())
	}
}

type domainNotFoundErr struct {
	Domain    string
	ProjectID string
	BranchID  string
}

func (d domainNotFoundErr) Error() string {
	return fmt.Sprintf("domain %q not found in project %s, branch %s", d.Domain, d.ProjectID, d.BranchID)
}

func fundCustomBranchDomain(ctx context.Context, client *neon.Client, projectID, branchID, domain string) (
	neon.CustomDomain, error) {
	var o neon.CustomDomain
	err := projectReadiness.Do(ctx, func(_ context.Context) error {
		var cursor *string
		for {
			l, err := client.ListProjectBranchCustomDomains(projectID, branchID, cursor, nil)
			if err != nil {
				return err
			}

			for _, el := range l.CustomDomains {
				if domain == el.Domain {
					o = el
					break
				}
			}
			if o.Domain != "" {
				break
			}
			if l.CursorPaginationResponse.Pagination == nil {
				break
			}
			cursor = l.CursorPaginationResponse.Pagination.Next
			if cursor == nil {
				break
			}
		}
		return nil
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error {
			return domainNotFoundErr{Domain: domain, ProjectID: projectID, BranchID: branchID}
		},
	})

	if err != nil {
		return neon.CustomDomain{}, err
	}

	if o.Domain == "" {
		return neon.CustomDomain{}, domainNotFoundErr{Domain: domain, ProjectID: projectID, BranchID: branchID}
	}
	return o, nil
}

func (r *neonCustomBranchDomain) Delete(ctx context.Context, req resource.DeleteRequest,
	resp *resource.DeleteResponse) {
	if r.client == nil {
		resp.Diagnostics.AddError("Client Not Configured", "The Neon provider client is not configured.")
		return
	}

	var state neonCustomBranchDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(projectReadiness.Retry(ctx, func(_ context.Context) error {
		return r.client.DeleteProjectBranchCustomDomain(state.ProjectID.ValueString(), state.BranchID.ValueString(),
			state.Domain.ValueString())
	}, map[int]func(context.Context) error{
		http.StatusNotFound: func(_ context.Context) error { return nil },
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.State.RemoveResource(ctx)
}
