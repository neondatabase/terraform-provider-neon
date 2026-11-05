package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	resourcetest "github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestFunctionCreateConfiguredName(t *testing.T) {
	for _, tc := range []struct {
		name         string
		planned      types.String
		returnedName string
		patchStatus  int
		patchError   error
		cancelPatch  bool
		wantName     string
		wantPatch    bool
		wantError    string
	}{
		{
			name: "different name", planned: types.StringValue("bar"),
			wantName: "bar", wantPatch: true,
		},
		{name: "omitted name", planned: types.StringNull(), wantName: "hello"},
		{name: "unknown name", planned: types.StringUnknown(), wantName: "hello"},
		{name: "matching name", planned: types.StringValue("hello"), wantName: "hello"},
		{
			name: "API metadata remains authoritative", planned: types.StringValue("bar"),
			returnedName: "canonical", wantName: "canonical", wantPatch: true,
		},
		{
			name: "rename rejected", planned: types.StringValue("bar"),
			patchStatus: 400, wantName: "hello", wantPatch: true, wantError: "fixture rename denied",
		},
		{
			name: "rename forbidden", planned: types.StringValue("bar"),
			patchStatus: 403, wantName: "hello", wantPatch: true, wantError: "fixture rename denied",
		},
		{
			name: "rename transport failure", planned: types.StringValue("bar"),
			patchError: errors.New("fixture connection refused"), wantName: "hello",
			wantPatch: true, wantError: "fixture connection refused",
		},
		{
			name: "rename retry canceled", planned: types.StringValue("bar"),
			patchStatus: 503, cancelPatch: true, wantName: "hello",
			wantPatch: true, wantError: "context canceled",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			api := &functionNameHTTP{
				base:         &functionEnvironmentHTTP{names: []string{"FOO"}},
				returnedName: tc.returnedName, patchStatus: tc.patchStatus, patchError: tc.patchError,
			}
			if tc.cancelPatch {
				api.onPatch = cancel
			}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			env := types.MapValueMust(types.StringType, map[string]attr.Value{
				"FOO": types.StringValue("fixture-value"),
			})
			plan := functionEnvironmentPlan(t, env)
			require.False(t, plan.SetAttribute(ctx, path.Root("name"), tc.planned).HasError())
			response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
			(&neonFunctionResource{client: client}).Create(ctx, resource.CreateRequest{Plan: plan}, &response)
			if tc.wantError == "" {
				require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			} else {
				require.True(t, response.Diagnostics.HasError(), "rename failure must not look successful")
				require.Contains(t, fmt.Sprint(response.Diagnostics), tc.wantError)
			}

			// A failed rename must retain the observed identity, not discard a
			// successfully deployed function or save the unaccepted planned name.
			var state neonFunctionResourceModel
			require.False(t, response.State.Get(context.Background(), &state).HasError())
			require.Equal(t, "p-1/br-1/hello", state.ID.ValueString())
			require.Equal(t, tc.wantName, state.Name.ValueString())
			require.Equal(t, "hello", state.Slug.ValueString())
			require.Equal(t, "nodejs24", state.Runtime.ValueString())
			require.Equal(t, "testdata/function/function.zip", state.ZipFilePath.ValueString())
			assertFunctionEnvironmentState(t, response.State, env, []string{"FOO"})
			calls, patches := api.snapshot()
			wantCalls := []string{"POST", "GET"}
			if tc.wantPatch {
				wantCalls = append(wantCalls, "PATCH")
				require.Equal(t, []map[string]string{{"name": "bar"}}, patches)
			} else {
				require.Empty(t, patches)
			}
			require.Equal(t, wantCalls, calls, "no rollback delete or duplicate deployment")
			if tc.wantPatch && tc.wantError == "" {
				require.Equal(t, "https://renamed.invalid/", state.InvocationURL.ValueString())
				require.Equal(t, int64(2), state.CurrentDeploymentID.ValueInt64())
			} else {
				require.Equal(t, "https://fixture.invalid/", state.InvocationURL.ValueString())
				require.Equal(t, int64(1), state.CurrentDeploymentID.ValueInt64())
			}
		})
	}
}

func TestFunctionCreateConfiguredNameTerraform(t *testing.T) {
	api := &functionNameHTTP{base: &functionEnvironmentHTTP{}}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	resourcetest.UnitTest(t, resourcetest.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resourcetest.TestStep{{
			Config: functionNameConfig(t),
			Check:  resourcetest.TestCheckResourceAttr("neon_function.this", "name", "bar"),
		}},
	})
	calls, patches := api.snapshot()
	require.Equal(t, []map[string]string{{"name": "bar"}}, patches)
	require.Contains(t, calls, "DELETE", "Terraform destroy must use the saved identity")
}

func TestFunctionCreateNameFailureTerraformRetainsIdentity(t *testing.T) {
	api := &functionNameHTTP{base: &functionEnvironmentHTTP{}, patchStatus: 400}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	resourcetest.UnitTest(t, resourcetest.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resourcetest.TestStep{{
			Config:      functionNameConfig(t),
			ExpectError: regexp.MustCompile("fixture rename denied"),
		}},
	})
	calls, patches := api.snapshot()
	require.Equal(t, []map[string]string{{"name": "bar"}}, patches)
	require.Equal(t, 1, countFunctionMethod(calls, "POST"), "no duplicate deployment on rename error")
	require.Equal(t, 1, countFunctionMethod(calls, "DELETE"), "Terraform must retain failed-create identity for cleanup")
}

func functionNameConfig(t *testing.T) string {
	t.Helper()
	zipPath, err := filepath.Abs("testdata/function/function.zip")
	require.NoError(t, err)
	return fmt.Sprintf(`resource "neon_function" "this" {
  project_id    = "p-1"
  branch_id     = "br-1"
  slug          = "hello"
  name          = "bar"
  runtime       = "nodejs24"
  zip_file_path = %q
}`, zipPath)
}

func countFunctionMethod(calls []string, method string) int {
	var count int
	for _, call := range calls {
		if call == method {
			count++
		}
	}
	return count
}

type functionNameHTTP struct {
	base         *functionEnvironmentHTTP
	returnedName string
	patchStatus  int
	patchError   error
	onPatch      func()

	mu      sync.Mutex
	calls   []string
	patches []map[string]string
	renamed bool
	name    string
}

func (f *functionNameHTTP) snapshot() ([]string, []map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	patches := make([]map[string]string, len(f.patches))
	for i, patch := range f.patches {
		patches[i] = maps.Clone(patch)
	}
	return slices.Clone(f.calls), patches
}

func (f *functionNameHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req.Method)
	if req.Method == http.MethodPatch {
		if req.URL.Path != "/api/v2/projects/p-1/branches/br-1/functions/hello" {
			return nil, fmt.Errorf("unexpected rename URL: %s", req.URL.Path)
		}
		var patch map[string]string
		if err := json.NewDecoder(req.Body).Decode(&patch); err != nil {
			return nil, fmt.Errorf("decode function rename: %w", err)
		}
		f.patches = append(f.patches, patch)
		if f.onPatch != nil {
			f.onPatch()
		}
		if f.patchError != nil {
			return nil, f.patchError
		}
		if f.patchStatus != 0 {
			return &http.Response{
				StatusCode: f.patchStatus, Body: io.NopCloser(strings.NewReader(`{"message":"fixture rename denied"}`)),
				Header: make(http.Header), Request: req,
			}, nil
		}
		f.renamed = true
		f.name = patch["name"]
		if f.returnedName != "" {
			f.name = f.returnedName
		}
		return f.renamedResponse(req)
	}
	response, err := f.base.Do(req)
	if err != nil || req.Method != http.MethodGet || !f.renamed || response.StatusCode != http.StatusOK {
		return response, err
	}
	if err := response.Body.Close(); err != nil {
		return nil, err
	}
	return f.renamedResponse(req)
}

func (f *functionNameHTTP) renamedResponse(req *http.Request) (*http.Response, error) {
	deployment := neon.NeonFunctionDeployment{
		ID: 2, CreatedAt: "2026-01-01T00:00:00Z", Environment: f.base.names,
		Runtime: "nodejs24", MemoryMib: 256, Status: neon.NeonFunctionDeploymentStatusCompleted,
	}
	encoded, err := json.Marshal(neon.NeonFunctionResponse{Function: neon.NeonFunction{
		ID: "fn-1", Slug: "hello", Name: f.name, CreatedAt: "2026-01-01T00:00:00Z",
		InvocationURL:     "https://renamed.invalid/",
		CurrentDeployment: &neon.NeonFunctionCurrentDeployment{NeonFunctionDeployment: deployment},
		ActiveDeployment:  &neon.NeonFunctionActiveDeployment{NeonFunctionDeployment: deployment},
	}})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(encoded))),
		Header: http.Header{"Content-Type": {"application/json"}}, Request: req,
	}, nil
}
