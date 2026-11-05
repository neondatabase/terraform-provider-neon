package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	resourcetest "github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

const triggerImportID = "p-1/br-1/trig-1"

func TestTriggerImportMapsBranchLocalTriggers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantName   string
		wantType   string
		wantCron   string
		wantBucket string
		wantPrefix *string
	}{
		{
			name: "schedule", wantName: "weekday",
			body:     `{"trigger":{"type":"schedule","trigger_id":"trig-1","name":"weekday","function_slug":"worker","function_path":"/run","enabled":true,"version":7,"inherited":false,"schedule":{"cron":"15 3 * * 1"}}}`,
			wantType: "schedule", wantCron: "15 3 * * 1",
		},
		{
			name: "storage with absent prefix", wantName: "uploads",
			body:     `{"trigger":{"type":"storage_object_created","trigger_id":"trig-1","name":"uploads","function_slug":"worker","function_path":"/handle","enabled":false,"version":9,"inherited":false,"storage_object_created":{"bucket_name":"assets"}}}`,
			wantType: "storage_object_created", wantBucket: "assets",
		},
		{
			name: "storage with nonempty prefix", wantName: "uploads",
			body:     `{"trigger":{"type":"storage_object_created","trigger_id":"trig-1","name":"uploads","function_slug":"worker","function_path":"/handle","enabled":false,"version":9,"inherited":false,"storage_object_created":{"bucket_name":"assets","prefix":"incoming/"}}}`,
			wantType: "storage_object_created", wantBucket: "assets", wantPrefix: stringPointer("incoming/"),
		},
		{
			name: "storage with explicit empty prefix", wantName: "uploads",
			body:     `{"trigger":{"type":"storage_object_created","trigger_id":"trig-1","name":"uploads","function_slug":"worker","function_path":"/handle","enabled":false,"version":9,"inherited":false,"storage_object_created":{"bucket_name":"assets","prefix":""}}}`,
			wantType: "storage_object_created", wantBucket: "assets", wantPrefix: stringPointer(""),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &triggerImportHTTP{body: tc.body}
			state, diagnostics := importTrigger(t, context.Background(), triggerImportID, api)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Equal(t, triggerImportID, state.ID.ValueString())
			require.Equal(t, "p-1", state.ProjectID.ValueString())
			require.Equal(t, "br-1", state.BranchID.ValueString())
			require.Equal(t, "trig-1", state.TriggerID.ValueString())
			require.Equal(t, tc.wantType, state.Type.ValueString())
			require.Equal(t, tc.wantName, state.Name.ValueString())
			if tc.wantType == "schedule" {
				require.Equal(t, "worker", state.FunctionSlug.ValueString())
				require.Equal(t, "/run", state.FunctionPath.ValueString())
				require.True(t, state.Enabled.ValueBool())
				require.Equal(t, int64(7), state.Version.ValueInt64())
				require.False(t, state.Inherited.ValueBool())
				require.Equal(t, tc.wantCron, state.Cron.ValueString())
				require.True(t, state.BucketName.IsNull())
				require.True(t, state.Prefix.IsNull())
			} else {
				require.Equal(t, "worker", state.FunctionSlug.ValueString())
				require.Equal(t, "/handle", state.FunctionPath.ValueString())
				require.False(t, state.Enabled.ValueBool())
				require.Equal(t, int64(9), state.Version.ValueInt64())
				require.False(t, state.Inherited.ValueBool())
				require.Equal(t, tc.wantBucket, state.BucketName.ValueString())
				require.True(t, state.Cron.IsNull())
				if tc.wantPrefix == nil {
					require.True(t, state.Prefix.IsNull())
				} else {
					require.False(t, state.Prefix.IsNull())
					require.Equal(t, *tc.wantPrefix, state.Prefix.ValueString())
				}
			}
			method, route, escaped := api.snapshot()
			require.Equal(t, 1, api.callCount(), "import must only perform one direct lookup")
			require.Equal(t, http.MethodGet, method)
			require.Equal(t, "/api/v2/projects/p-1/branches/br-1/triggers/trig-1", route)
			require.Equal(t, route, escaped)
		})
	}
}

func TestTriggerImportTerraformProtocol(t *testing.T) {
	trigger := `{"type":"schedule","trigger_id":"trig-1","name":"weekday","function_slug":"worker","function_path":"/run","enabled":true,"version":7,"inherited":false,"schedule":{"cron":"15 3 * * 1"}}`
	api := &triggerImportHTTP{body: `{"trigger":` + trigger + `}`, listBody: `{"triggers":[` + trigger + `]}`}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	resourcetest.UnitTest(t, resourcetest.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resourcetest.TestStep{
			{
				Config: `resource "neon_trigger" "this" {
  project_id    = "p-1"
  branch_id     = "br-1"
  type          = "schedule"
  name          = "weekday"
  function_slug = "worker"
  function_path = "/run"
  enabled       = true
  cron          = "15 3 * * 1"
}`,
			},
			{
				Config: `resource "neon_trigger" "this" {
  project_id    = "p-1"
  branch_id     = "br-1"
  type          = "schedule"
  name          = "weekday"
  function_slug = "worker"
  function_path = "/run"
  enabled       = true
  cron          = "15 3 * * 1"
}`,
				ImportState:       true,
				ResourceName:      "neon_trigger.this",
				ImportStateId:     triggerImportID,
				ImportStateVerify: true,
			}},
	})
	require.GreaterOrEqual(t, api.callCount(), 1)
}

func TestTriggerImportRejectsInvalidIDBeforeHTTP(t *testing.T) {
	for _, id := range []string{
		"", "/br-1/trig-1", "p-1//trig-1", "p-1/br-1/", "p-1/br-1/trig-1/extra", "p-1/br-1",
		"p-%/br-1/trig-1", "p-1/br-%/trig-1", "p-1/br-1/trig-%",
		"p-1/br-1/trig-1?query=1", "p-1/br-1/trig-1#fragment",
		"p-1/br%2Fother/trig-1", "p-1/./trig-1", "p-1/../trig-1",
	} {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			api := &triggerImportHTTP{body: `{}`}
			var diagnostics diag.Diagnostics
			require.NotPanics(t, func() {
				_, diagnostics = importTrigger(t, context.Background(), id, api)
			})
			require.True(t, diagnostics.HasError())
			require.Contains(t, fmt.Sprint(diagnostics), "Expected an import ID in the form <project_id>/<branch_id>/<trigger_id>")
			require.Equal(t, 0, api.callCount())
		})
	}
}

func TestTriggerImportReportsErrorsWithoutSavingState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		id         string
		status     int
		body       string
		transport  error
		cancelCall bool
		want       string
	}{
		{name: "missing client", id: triggerImportID, want: "Client Not Configured"},
		{name: "not found", id: triggerImportID, status: http.StatusNotFound, body: `{"message":"missing"}`, want: "Trigger Not Found"},
		{name: "backend response", id: triggerImportID, status: http.StatusServiceUnavailable, body: `{"message":"fixture backend unavailable"}`, want: "fixture backend unavailable"},
		{name: "unauthorized", id: triggerImportID, status: http.StatusUnauthorized, body: `{"message":"fixture authentication required"}`, want: "fixture authentication required"},
		{name: "forbidden", id: triggerImportID, status: http.StatusForbidden, body: `{"message":"fixture access denied"}`, want: "fixture access denied"},
		{name: "missing trigger metadata", id: triggerImportID, body: `{}`, want: "empty trigger_id"},
		{name: "transport failure", id: triggerImportID, transport: errors.New("fixture connection refused"), want: "fixture connection refused"},
		{name: "canceled context", id: triggerImportID, cancelCall: true, want: "context canceled"},
		{name: "empty trigger identity", id: triggerImportID, body: `{"trigger":{"type":"schedule","trigger_id":"","name":"weekday","function_slug":"worker","function_path":"/","enabled":true,"version":1,"inherited":false,"schedule":{"cron":"* * * * *"}}}`, want: "empty trigger_id"},
		{name: "mismatched trigger identity", id: triggerImportID, body: `{"trigger":{"type":"schedule","trigger_id":"other","name":"weekday","function_slug":"worker","function_path":"/","enabled":true,"version":1,"inherited":false,"schedule":{"cron":"* * * * *"}}}`, want: "does not match requested trigger ID"},
		{name: "unsupported discriminator", id: triggerImportID, body: `{"trigger":{"type":"unknown","trigger_id":"trig-1"}}`, want: "unknown discriminator value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &triggerImportHTTP{status: tc.status, body: tc.body, transport: tc.transport}
			ctx, cancel := context.WithCancel(context.Background())
			if tc.cancelCall {
				cancel()
			}
			defer cancel()
			_, response, diagnostics := importTriggerResponse(t, ctx, tc.id, api, tc.name != "missing client")
			require.True(t, diagnostics.HasError(), "failed import must not look successful")
			require.Contains(t, fmt.Sprint(diagnostics), tc.want)
			require.True(t, response.State.Raw.IsNull(), "failed import must not save partial state")
			if tc.cancelCall {
				require.Equal(t, 0, api.callCount(), "canceled import must not call the API")
			}
		})
	}
}

func TestTriggerImportRejectsInheritedTriggers(t *testing.T) {
	for _, tc := range []struct {
		kind string
		body string
	}{
		{kind: "schedule", body: `{"trigger":{"type":"schedule","trigger_id":"trig-1","name":"weekday","function_slug":"worker","function_path":"/","enabled":true,"version":1,"inherited":true,"schedule":{"cron":"* * * * *"}}}`},
		{kind: "storage", body: `{"trigger":{"type":"storage_object_created","trigger_id":"trig-1","name":"uploads","function_slug":"worker","function_path":"/","enabled":true,"version":1,"inherited":true,"storage_object_created":{"bucket_name":"assets"}}}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			api := &triggerImportHTTP{body: tc.body}
			_, response, diagnostics := importTriggerResponse(t, context.Background(), triggerImportID, api, true)
			require.True(t, diagnostics.HasError(), "inherited triggers must not be imported")
			require.Contains(t, fmt.Sprint(diagnostics), "inherited")
			require.True(t, response.State.Raw.IsNull(), "inherited trigger must not save state")
		})
	}
}

func importTrigger(t *testing.T, ctx context.Context, id string, api *triggerImportHTTP) (neonTriggerResourceModel, diag.Diagnostics) {
	t.Helper()
	_, response, diagnostics := importTriggerResponse(t, ctx, id, api, true)
	var state neonTriggerResourceModel
	if !diagnostics.HasError() {
		require.False(t, response.State.Get(context.Background(), &state).HasError())
	}
	return state, diagnostics
}

func importTriggerResponse(t *testing.T, ctx context.Context, id string, api *triggerImportHTTP, configured bool) (*neonTriggerResource, resource.ImportStateResponse, diag.Diagnostics) {
	t.Helper()
	var schemaResponse resource.SchemaResponse
	r := &neonTriggerResource{}
	if configured {
		client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
		require.NoError(t, err)
		r.client = client
	}
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
	response := resource.ImportStateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: id}, &response)
	return r, response, response.Diagnostics
}

type triggerImportHTTP struct {
	status    int
	body      string
	listBody  string
	transport error

	mu      sync.Mutex
	calls   int
	method  string
	route   string
	escaped string
}

func (f *triggerImportHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls++
	f.method = req.Method
	f.route = req.URL.Path
	f.escaped = req.URL.EscapedPath()
	f.mu.Unlock()
	const base = "/api/v2/projects/p-1/branches/br-1/triggers"
	valid := req.Method == http.MethodGet && req.URL.Path == base+"/trig-1"
	if f.listBody != "" {
		valid = valid || req.Method == http.MethodGet && req.URL.Path == base ||
			req.Method == http.MethodPost && req.URL.Path == base ||
			req.Method == http.MethodDelete && req.URL.Path == base+"/trig-1"
	}
	if !valid {
		return nil, fmt.Errorf("unexpected trigger import fixture request: %s %s", req.Method, req.URL.Path)
	}
	if f.transport != nil {
		return nil, f.transport
	}
	body := f.body
	if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/triggers") && f.listBody != "" {
		body = f.listBody
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Request:    req,
	}, nil
}

func (f *triggerImportHTTP) snapshot() (string, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.method, f.route, f.escaped
}

func (f *triggerImportHTTP) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func stringPointer(value string) *string {
	return &value
}
