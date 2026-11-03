package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestBucketDataSourceRegisteredWithFrameworkProvider(t *testing.T) {
	p := &Provider{}
	dataSources := p.DataSources(context.Background())

	var found datasource.DataSource
	var typeNames []string
	for _, factory := range dataSources {
		candidate := factory()
		var metadata datasource.MetadataResponse
		candidate.Metadata(context.Background(), datasource.MetadataRequest{}, &metadata)
		typeNames = append(typeNames, metadata.TypeName)
		if metadata.TypeName == "neon_bucket" {
			found = candidate
		}
	}

	require.NotNil(t, found, "neon_bucket must be registered as a Framework data source")
	require.NotContains(t, typeNames, "neon_branch_storage", "the old data source name must not remain as an alias")
	require.NotContains(t, typeNames, "neon_bucket_connection", "the old data source name must not remain as an alias")
}

func TestBucketDataSourceSchema(t *testing.T) {
	d := NewBucketDataSource()
	var response datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &response)

	require.Zero(t, response.Diagnostics.ErrorsCount())
	require.Empty(t, response.Schema.Blocks)
	for _, name := range []string{"project_id", "branch_id"} {
		require.Contains(t, response.Schema.Attributes, name)
		require.True(t, response.Schema.Attributes[name].IsRequired(), name)
	}
	for _, name := range []string{"id", "s3_endpoint", "region", "force_path_style"} {
		require.Contains(t, response.Schema.Attributes, name)
		require.True(t, response.Schema.Attributes[name].IsComputed(), name)
	}
	require.NotContains(t, response.Schema.Attributes, "enabled")
}

type branchDataSourceHTTPFixture struct {
	t      *testing.T
	path   string
	status int
	body   string
}

func (s branchDataSourceHTTPFixture) Do(req *http.Request) (*http.Response, error) {
	s.t.Helper()
	require.Equal(s.t, http.MethodGet, req.Method)
	require.Equal(s.t, s.path, req.URL.Path)
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func newBranchDataSourceTestClient(t *testing.T, path string, status int, body string) *neon.Client {
	t.Helper()
	client, err := neon.NewClient(neon.Config{
		Key:        "test",
		HTTPClient: branchDataSourceHTTPFixture{t: t, path: path, status: status, body: body},
	})
	require.NoError(t, err)
	return client
}

func newBucketDataSourceWithStub(t *testing.T, status int, body string) *neonBucketDataSource {
	t.Helper()
	client := newBranchDataSourceTestClient(t, "/api/v2/projects/cool-moon-42/branches/br-cool-moon-42/storage", status, body)
	d := NewBucketDataSource().(*neonBucketDataSource)
	var resp datasource.ConfigureResponse
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return d
}

func bucketConnectionTestSchema(t *testing.T) schema.Schema {
	t.Helper()
	d := NewBucketDataSource()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	require.Zero(t, resp.Diagnostics.ErrorsCount())
	return resp.Schema
}

func bucketConnectionReadConfig(t *testing.T, projectID, branchID string) tfsdk.Config {
	t.Helper()
	s := bucketConnectionTestSchema(t)
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	return tfsdk.Config{
		Schema: s,
		Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
			"id":               tftypes.NewValue(tftypes.String, nil),
			"project_id":       tftypes.NewValue(tftypes.String, projectID),
			"branch_id":        tftypes.NewValue(tftypes.String, branchID),
			"s3_endpoint":      tftypes.NewValue(tftypes.String, nil),
			"region":           tftypes.NewValue(tftypes.String, nil),
			"force_path_style": tftypes.NewValue(tftypes.Bool, nil),
		}),
	}
}

func TestBucketDataSourceReadSuccess(t *testing.T) {
	d := newBucketDataSourceWithStub(t, http.StatusOK, `{
		"enabled": true,
		"force_path_style": true,
		"region": "us-east-2",
		"s3_endpoint": "https://br-cool-moon-42.storage.c-2.local.neon.build"
	}`)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: bucketConnectionTestSchema(t)}}

	d.Read(context.Background(),
		datasource.ReadRequest{Config: bucketConnectionReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.Equal(t, 0, resp.Diagnostics.ErrorsCount(), "diagnostics: %v", resp.Diagnostics)
	var got neonBucketDataSourceModel
	stateDiags := resp.State.Get(context.Background(), &got)
	require.Equal(t, 0, len(stateDiags), "state diagnostics: %v", stateDiags)

	require.Equal(t, "cool-moon-42/br-cool-moon-42", got.ID.ValueString())
	require.Equal(t, "cool-moon-42", got.ProjectID.ValueString())
	require.Equal(t, "br-cool-moon-42", got.BranchID.ValueString())
	require.Equal(t, "https://br-cool-moon-42.storage.c-2.local.neon.build", got.S3Endpoint.ValueString())
	require.Equal(t, "us-east-2", got.Region.ValueString())
	require.True(t, got.ForcePathStyle.ValueBool())
}

func TestBucketDataSourceReadNotFound(t *testing.T) {
	d := newBucketDataSourceWithStub(t, http.StatusNotFound, `{
		"code": "STORAGE_NOT_ENABLED",
		"message": "not found"
	}`)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: bucketConnectionTestSchema(t)}}

	d.Read(context.Background(),
		datasource.ReadRequest{Config: bucketConnectionReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
	require.Equal(t, "Neon API request failed", resp.Diagnostics[0].Summary())
	require.Equal(t, "[HTTP Code: 404][Error Code: STORAGE_NOT_ENABLED] not found", resp.Diagnostics[0].Detail())
}

func TestBucketDataSourceReadAPIError(t *testing.T) {
	d := newBucketDataSourceWithStub(t, http.StatusBadRequest, `{
		"code": "INVALID_INPUT",
		"message": "upstream rejected request"
	}`)

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: bucketConnectionTestSchema(t)}}

	d.Read(context.Background(),
		datasource.ReadRequest{Config: bucketConnectionReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.NotZero(t, resp.Diagnostics.ErrorsCount(), "expected an actionable diagnostic for non-2xx API error")
	combined := ""
	for _, diag := range resp.Diagnostics {
		combined += diag.Summary() + ": " + diag.Detail() + "\n"
	}
	require.Contains(t, combined, "INVALID_INPUT")
	require.Equal(t, "[HTTP Code: 400][Error Code: INVALID_INPUT] upstream rejected request", resp.Diagnostics[0].Detail())
}

func TestBucketDataSourceReadWithoutConfigure(t *testing.T) {
	d := &neonBucketDataSource{}
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: bucketConnectionTestSchema(t)}}

	d.Read(context.Background(),
		datasource.ReadRequest{Config: bucketConnectionReadConfig(t, "cool-moon-42", "br-cool-moon-42")},
		resp)

	require.Equal(t, 1, resp.Diagnostics.ErrorsCount())
	require.Equal(t, "SDK is not configured", resp.Diagnostics[0].Summary())
	require.Equal(t, "The Neon provider client is unavailable.", resp.Diagnostics[0].Detail())
}

func TestBucketDataSourceConfigureWithConfiguredProvider(t *testing.T) {
	ctx := context.Background()
	p := &Provider{version: "test"}
	var schemaResp frameworkprovider.SchemaResponse
	p.Schema(ctx, frameworkprovider.SchemaRequest{}, &schemaResp)
	var configureResp frameworkprovider.ConfigureResponse
	p.Configure(ctx, frameworkprovider.ConfigureRequest{
		TerraformVersion: "test",
		Config: tfsdk.Config{
			Schema: schemaResp.Schema,
			Raw: tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), map[string]tftypes.Value{
				"api_key": tftypes.NewValue(tftypes.String, "test"),
			}),
		},
	}, &configureResp)
	require.False(t, configureResp.Diagnostics.HasError(), "%v", configureResp.Diagnostics)

	adapter, ok := configureResp.DataSourceData.(*providerAdapter)
	require.True(t, ok, "provider must supply DataSourceData")
	require.NotNil(t, adapter.sdk)

	d := NewBucketDataSource().(*neonBucketDataSource)
	var dataSourceConfigureResp datasource.ConfigureResponse
	d.Configure(ctx, datasource.ConfigureRequest{ProviderData: configureResp.DataSourceData}, &dataSourceConfigureResp)
	require.False(t, dataSourceConfigureResp.Diagnostics.HasError(), "%v", dataSourceConfigureResp.Diagnostics)
	require.Same(t, adapter.sdk, d.client)
}
