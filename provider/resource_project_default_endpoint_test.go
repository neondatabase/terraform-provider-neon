package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// Disabling an endpoint only restricts connections, so a disabled primary
// compute is still the project's default endpoint.
func TestProjectReadKeepsDisabledPrimaryCompute(t *testing.T) {
	t.Parallel()

	state, diags := readProjectWithEndpoints(t, `[
    {"id": "ep-1", "project_id": "p-1", "branch_id": "br-1", "type": "read_write",
     "host": "ep-1.example", "disabled": true, "created_at": "2026-01-01T00:00:00Z"}
  ]`)
	require.False(t, diags.HasError(), "%v", diags)
	require.Equal(t, "ep-1", state.PrimaryCompute.ID.ValueString())
	require.True(t, state.PrimaryCompute.Disabled.ValueBool())
}

// Without a read-write endpoint on the default branch, Read must report an
// error that says so instead of crashing the provider.
func TestProjectReadReportsMissingPrimaryCompute(t *testing.T) {
	t.Parallel()

	for name, endpoints := range map[string]string{
		"no endpoints": `[]`,
		"only a read-only endpoint": `[
    {"id": "ep-ro", "project_id": "p-1", "branch_id": "br-1", "type": "read_only",
     "host": "ep-ro.example", "created_at": "2026-01-01T00:00:00Z"}
  ]`,
		"read-write endpoint on another branch": `[
    {"id": "ep-2", "project_id": "p-1", "branch_id": "br-2", "type": "read_write",
     "host": "ep-2.example", "created_at": "2026-01-01T00:00:00Z"}
  ]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := readProjectWithEndpoints(t, endpoints)
			require.True(t, diags.HasError(), "expected an error")
			require.Contains(t, diags.Errors()[0].Detail(),
				"project p-1 has no read-write compute endpoint on its default branch br-1")
		})
	}
}

// readProjectWithEndpoints runs Read for project p-1, whose default branch is
// br-1, against a mock API that lists the given endpoints.
func readProjectWithEndpoints(t *testing.T, endpoints string) (neonProjectResourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()

	api := projectReadFixture{
		"/api/v2/projects/p-1": `{"project": {"id": "p-1", "name": "read", ` +
			`"region_id": "aws-us-east-2", "pg_version": 17, "org_id": "org-1", ` +
			`"provisioner": "k8s-neonvm", "settings": {}}}`,
		"/api/v2/projects/p-1/branches": `{"branches": [{"id": "br-1", "project_id": "p-1", ` +
			`"name": "main", "default": true, "protected": false}]}`,
		"/api/v2/projects/p-1/endpoints": `{"endpoints": ` + endpoints + `}`,
		"/api/v2/projects/p-1/branches/br-1/databases": `{"databases": [` +
			`{"branch_id": "br-1", "name": "neondb", "owner_name": "neondb_owner"}]}`,
	}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)

	r := &neonProjectResource{}
	var configured resource.ConfigureResponse
	r.Configure(ctx, resource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError(), "%v", configured.Diagnostics)

	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	prior := tfsdk.State{Schema: schema.Schema}
	require.False(t, prior.Set(ctx, neonProjectResourceModel{
		ID:               types.StringValue("p-1"),
		Name:             types.StringValue("read"),
		AllowedIPs:       types.ListNull(types.StringType),
		EnabledLibraries: types.ListNull(types.StringType),
	}).HasError())

	resp := resource.ReadResponse{State: prior}
	r.Read(ctx, resource.ReadRequest{State: prior}, &resp)

	var state neonProjectResourceModel
	if !resp.Diagnostics.HasError() {
		require.False(t, resp.State.Get(ctx, &state).HasError())
	}
	return state, resp.Diagnostics
}

// projectReadFixture answers GET requests by path.
type projectReadFixture map[string]string

func (f projectReadFixture) Do(req *http.Request) (*http.Response, error) {
	body, ok := f[req.URL.Path]
	if !ok || req.Method != http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"unexpected call: ` + req.Method + " " + req.URL.Path + `"}`)),
		}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}
