package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestProjectPrimaryLibrariesMapperIntent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		prior    *neonEndpointResourceModel
		wantNull bool
	}{
		{"missing compute", nil, true},
		{"null", &neonEndpointResourceModel{EnabledLibraries: types.ListNull(types.StringType)}, true},
		{"unknown", &neonEndpointResourceModel{EnabledLibraries: types.ListUnknown(types.StringType)}, true},
		{"empty", &neonEndpointResourceModel{EnabledLibraries: types.ListValueMust(types.StringType, nil)}, false},
		{"removed contents", &neonEndpointResourceModel{EnabledLibraries: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("auto_explain")})}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.prior != nil {
				tt.prior.ID = types.StringValue("old-id")
				tt.prior.Host = types.StringValue("old.example")
				tt.prior.Name = types.StringValue("old-name")
				tt.prior.EnabledLibrariesUseDefaults = types.BoolValue(true)
			}
			model := neonProjectResourceModel{PrimaryCompute: tt.prior}
			var diagnostics diag.Diagnostics
			model.inferAttr(context.Background(), neon.Project{}, neon.Branch{}, neon.Endpoint{
				ID: "ep-1", ProjectID: "p-1", BranchID: "br-1", Host: "ep-1.example",
			}, neon.Database{}, &diagnostics)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.NotNil(t, model.PrimaryCompute)
			require.Equal(t, tt.wantNull, model.PrimaryCompute.EnabledLibraries.IsNull())
			require.False(t, model.PrimaryCompute.EnabledLibraries.IsUnknown())
			require.Empty(t, model.PrimaryCompute.EnabledLibraries.Elements())
			require.Equal(t, "ep-1", model.PrimaryCompute.ID.ValueString())
			require.Equal(t, "ep-1.example", model.PrimaryCompute.Host.ValueString())
			require.True(t, model.PrimaryCompute.Name.IsNull(), "old name must not survive missing API value")
			require.True(t, model.PrimaryCompute.EnabledLibrariesUseDefaults.IsNull(), "use_defaults must still come from API")
		})
	}
}

// Delegated endpoint Read can observe a different snapshot from the final
// project Read. The parent's original nested plan is the intent source.
func TestProjectPrimaryLibrariesUpdateIntent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name         string
		priorEmpty   bool
		plan         types.List
		intermediate []string
		final        []string
		want         types.List
	}{
		{
			name: "original null survives delegated response",
			plan: types.ListNull(types.StringType), intermediate: []string{"auto_explain"},
			want: types.ListNull(types.StringType),
		},
		{
			name: "actual API contents override null plan",
			plan: types.ListNull(types.StringType), final: []string{"pg_stat_statements"},
			want: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("pg_stat_statements")}),
		},
		{
			name: "unknown does not inherit prior empty intent", priorEmpty: true,
			plan: types.ListUnknown(types.StringType), want: types.ListNull(types.StringType),
		},
		{
			name: "null does not inherit prior empty intent", priorEmpty: true,
			plan: types.ListNull(types.StringType), want: types.ListNull(types.StringType),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			api := &primaryLibrariesAPI{endpoint: endpointLibrariesAPI{emptyShape: "missing settings"}}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			r := &neonProjectResource{}
			var configured fwresource.ConfigureResponse
			r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
			require.False(t, configured.Diagnostics.HasError())
			var schema fwresource.SchemaResponse
			r.Schema(ctx, fwresource.SchemaRequest{}, &schema)
			project, err := client.GetProject("p-1")
			require.NoError(t, err)
			endpoint, err := client.GetProjectEndpoint("p-1", "ep-1")
			require.NoError(t, err)
			var prior neonProjectResourceModel
			var diagnostics diag.Diagnostics
			prior.inferAttr(ctx, project.Project, neon.Branch{
				ID: "br-1", ProjectID: "p-1", Name: "main", Default: true,
			}, endpoint.Endpoint, neon.Database{
				Name: "neondb", OwnerName: "neondb_owner", BranchID: "br-1",
			}, &diagnostics)
			require.False(t, diagnostics.HasError())
			if tt.priorEmpty {
				prior.PrimaryCompute.EnabledLibraries = types.ListValueMust(types.StringType, nil)
			}
			state := tfsdk.State{Schema: schema.Schema}
			require.False(t, state.Set(ctx, &prior).HasError())
			planned := prior
			nested := *prior.PrimaryCompute
			nested.EnabledLibraries = tt.plan
			planned.PrimaryCompute = &nested
			plan := tfsdk.Plan{Schema: schema.Schema}
			require.False(t, plan.Set(ctx, &planned).HasError())
			api.getOnlyLibraries = tt.intermediate
			api.endpoint.libraries = tt.final
			response := fwresource.UpdateResponse{State: state}
			r.Update(ctx, fwresource.UpdateRequest{
				State: state, Plan: plan, Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw},
			}, &response)
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			var got neonProjectResourceModel
			require.False(t, response.State.Get(ctx, &got).HasError())
			require.NotNil(t, got.PrimaryCompute)
			require.True(t, got.PrimaryCompute.EnabledLibraries.Equal(tt.want), "got %v, want %v", got.PrimaryCompute.EnabledLibraries, tt.want)
			require.Zero(t, api.endpoint.patches)
		})
	}
}
