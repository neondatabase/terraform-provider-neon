package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/require"
)

func TestAIGatewayDataSourceRegisteredWithFrameworkProvider(t *testing.T) {
	p := &Provider{}
	dataSources := p.DataSources(context.Background())

	var found datasource.DataSource
	for _, factory := range dataSources {
		candidate := factory()
		var metadata datasource.MetadataResponse
		candidate.Metadata(context.Background(), datasource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_ai_gateway" {
			found = candidate
			break
		}
	}

	require.NotNil(t, found, "neon_ai_gateway must be registered as a Framework data source")
}

func TestAIGatewayDataSourceSchema(t *testing.T) {
	d := NewAIGatewayDataSource()
	var response datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &response)

	require.Zero(t, response.Diagnostics.ErrorsCount())
	require.Empty(t, response.Schema.Blocks)
	for _, name := range []string{"project_id", "branch_id"} {
		require.Contains(t, response.Schema.Attributes, name)
		require.True(t, response.Schema.Attributes[name].IsRequired(), name)
	}
	for _, name := range []string{"id", "base_url"} {
		require.Contains(t, response.Schema.Attributes, name)
		require.True(t, response.Schema.Attributes[name].IsComputed(), name)
	}
	require.NotContains(t, response.Schema.Attributes, "enabled")
}

func aiGatewayTestSchema(t *testing.T) schema.Schema {
	t.Helper()
	d := NewAIGatewayDataSource()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return resp.Schema
}

func aiGatewayReadConfig(t *testing.T, projectID, branchID string) tfsdk.Config {
	t.Helper()
	s := aiGatewayTestSchema(t)
	return tfsdk.Config{
		Schema: s,
		Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), map[string]tftypes.Value{
			"id":         tftypes.NewValue(tftypes.String, nil),
			"project_id": tftypes.NewValue(tftypes.String, projectID),
			"branch_id":  tftypes.NewValue(tftypes.String, branchID),
			"base_url":   tftypes.NewValue(tftypes.String, nil),
		}),
	}
}

func newAIGatewayDataSourceWithStub(t *testing.T, path string, status int, body string) *neonAIGatewayDataSource {
	t.Helper()
	client := newBranchDataSourceTestClient(t, path, status, body)
	d := NewAIGatewayDataSource().(*neonAIGatewayDataSource)
	var resp datasource.ConfigureResponse
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return d
}

func TestAIGatewayDataSourceReadSuccess(t *testing.T) {
	d := newAIGatewayDataSourceWithStub(t,
		"/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/ai_gateway",
		http.StatusOK, `{
			"enabled": true,
			"base_url": "https://br-cool-moon-42.ai-gateway.neon.tech/v1"
		}`)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: aiGatewayTestSchema(t)}}
	d.Read(context.Background(),
		datasource.ReadRequest{Config: aiGatewayReadConfig(t, "cool-moon-42", "br-cool-moon-42")}, resp)

	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var got neonAIGatewayDataSourceModel
	stateDiags := resp.State.Get(context.Background(), &got)
	require.False(t, stateDiags.HasError(), "%v", stateDiags)
	require.Equal(t, "cool-moon-42/br-cool-moon-42", got.ID.ValueString())
	require.Equal(t, "cool-moon-42", got.ProjectID.ValueString())
	require.Equal(t, "br-cool-moon-42", got.BranchID.ValueString())
	require.Equal(t, "https://br-cool-moon-42.ai-gateway.neon.tech/v1", got.BaseURL.ValueString())
}

func TestAIGatewayDataSourceReadErrors(t *testing.T) {
	for _, tt := range []struct {
		name       string
		branchID   string
		path       string
		status     int
		body       string
		wantDetail string
	}{
		{
			name:       "not found",
			branchID:   "br-cool-moon-42",
			path:       "/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/ai_gateway",
			status:     http.StatusNotFound,
			body:       `{"code":"NOT_FOUND","message":"not found"}`,
			wantDetail: "[HTTP Code: 404][Error Code: NOT_FOUND] not found",
		},
		{
			name:       "server rejects input",
			branchID:   "not-a-branch",
			path:       "/api/v2/projects/cool-moon-42/branches/not-a-branch/ai_gateway",
			status:     http.StatusBadRequest,
			body:       `{"code":"INVALID_INPUT","message":"upstream rejected request"}`,
			wantDetail: "[HTTP Code: 400][Error Code: INVALID_INPUT] upstream rejected request",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := newAIGatewayDataSourceWithStub(t, tt.path, tt.status, tt.body)
			resp := &datasource.ReadResponse{State: tfsdk.State{Schema: aiGatewayTestSchema(t)}}
			d.Read(context.Background(),
				datasource.ReadRequest{Config: aiGatewayReadConfig(t, "cool-moon-42", tt.branchID)}, resp)

			require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
			require.Equal(t, "Neon API request failed", resp.Diagnostics[0].Summary())
			require.Equal(t, tt.wantDetail, resp.Diagnostics[0].Detail())
			require.True(t, resp.State.Raw.IsNull(), "failed reads must not populate state")
		})
	}
}

func TestAIGatewayDataSourceReadWithoutConfigure(t *testing.T) {
	d := NewAIGatewayDataSource()
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: aiGatewayTestSchema(t)}}
	d.Read(context.Background(),
		datasource.ReadRequest{Config: aiGatewayReadConfig(t, "cool-moon-42", "br-cool-moon-42")}, resp)

	require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
	require.Equal(t, "SDK is not configured", resp.Diagnostics[0].Summary())
	require.Equal(t, "The Neon provider client is unavailable.", resp.Diagnostics[0].Detail())
}
