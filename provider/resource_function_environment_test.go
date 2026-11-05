package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// Native map decoding is intentional: the historical schema warning claimed
// that known nonempty maps could not cross this boundary.
func TestFunctionEnvironmentPlanDecoding(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  types.Map
		want map[string]string
	}{
		{name: "null", env: types.MapNull(types.StringType)},
		{
			name: "empty", env: types.MapValueMust(types.StringType, map[string]attr.Value{}),
			want: map[string]string{},
		},
		{
			name: "nonempty",
			env: types.MapValueMust(types.StringType, map[string]attr.Value{
				"FOO": types.StringValue("fixture-value"),
			}),
			want: map[string]string{"FOO": "fixture-value"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := functionEnvironmentPlan(t, tc.env)
			var model neonFunctionResourceModel
			require.False(t, plan.Get(context.Background(), &model).HasError())
			require.Equal(t, tc.want, model.EnvironmentVariables)
		})
	}
}

// The API exposes names only. Clearing the input map while mapping either
// Create or Read would lose values that cannot be recovered from the API.
func TestFunctionEnvironmentCreateAndReadPreserveValues(t *testing.T) {
	ctx := context.Background()
	api := &functionEnvironmentHTTP{names: []string{"EMPTY", "FOO", "QUOTED"}}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	r := &neonFunctionResource{client: client}
	env := types.MapValueMust(types.StringType, map[string]attr.Value{
		"FOO":    types.StringValue("fixture-value"),
		"EMPTY":  types.StringValue(""),
		"QUOTED": types.StringValue("a\"b\nc=✓"),
	})
	plan := functionEnvironmentPlan(t, env)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &created)
	require.False(t, created.Diagnostics.HasError(), "%v", created.Diagnostics)

	got := api.deployment()
	require.True(t, got.environmentPresent)
	require.Equal(t, map[string]string{
		"FOO": "fixture-value", "EMPTY": "", "QUOTED": "a\"b\nc=✓",
	}, got.environment)
	require.Equal(t, "nodejs24", got.runtime)
	zip, err := os.ReadFile("testdata/function/function.zip")
	require.NoError(t, err)
	require.Equal(t, zip, got.zip)
	assertFunctionEnvironmentState(t, created.State, env, []string{"EMPTY", "FOO", "QUOTED"})

	read := resource.ReadResponse{State: created.State}
	r.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	require.False(t, read.Diagnostics.HasError(), "%v", read.Diagnostics)
	assertFunctionEnvironmentState(t, read.State, env, []string{"EMPTY", "FOO", "QUOTED"})
	require.Equal(t, []string{"POST", "GET", "GET"}, api.methods())
}

func TestFunctionEnvironmentTerraform(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   map[string]string
	}{
		{name: "omitted"},
		{name: "empty", config: "environment_variables = {}"},
		{
			name: "nonempty", config: `environment_variables = { FOO = "fixture-value" }`,
			want: map[string]string{"FOO": "fixture-value"},
		},
		{
			name: "numeric coercion", config: `environment_variables = { FOO = 1, BAR = "baz" }`,
			want: map[string]string{"FOO": "1", "BAR": "baz"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &functionEnvironmentHTTP{names: slices.Sorted(maps.Keys(tc.want))}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			zipPath, err := filepath.Abs("testdata/function/function.zip")
			require.NoError(t, err)
			config := fmt.Sprintf(`resource "neon_function" "this" {
  project_id    = "p-1"
  branch_id     = "br-1"
  slug          = "hello"
  name          = "hello"
  runtime       = "nodejs24"
  zip_file_path = %q
  %s
}`, zipPath, tc.config)
			resourcetest.UnitTest(t, resourcetest.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resourcetest.TestStep{{
					Config: config,
					Check: func(state *terraform.State) error {
						got := api.deployment()
						if !maps.Equal(got.environment, tc.want) || got.environmentPresent != (len(tc.want) > 0) {
							return fmt.Errorf("deployment environment differs from configured fixture")
						}
						fn, ok := state.RootModule().Resources["neon_function.this"]
						if !ok || fn.Primary == nil {
							return fmt.Errorf("function missing from Terraform state")
						}
						for name, value := range tc.want {
							if fn.Primary.Attributes["environment_variables."+name] != value {
								return fmt.Errorf("configured environment missing from Terraform state")
							}
						}
						return nil
					},
				}},
			})
			require.Contains(t, api.methods(), "DELETE", "Terraform destroy did not reach the mock API")
		})
	}
}

func functionEnvironmentPlan(t *testing.T, env types.Map) tfsdk.Plan {
	t.Helper()
	ctx := context.Background()
	var schema resource.SchemaResponse
	(&neonFunctionResource{}).Schema(ctx, resource.SchemaRequest{}, &schema)
	plan := tfsdk.Plan{Schema: schema.Schema}
	model := neonFunctionResourceModel{
		ProjectID: types.StringValue("p-1"), BranchID: types.StringValue("br-1"),
		Slug: types.StringValue("hello"), Runtime: types.StringValue("nodejs24"),
		Name: types.StringValue("hello"), ZipFilePath: types.StringValue("testdata/function/function.zip"),
		EnvironmentVariableNames: types.ListNull(types.StringType),
	}
	require.False(t, plan.Set(ctx, &model).HasError())
	require.False(t, plan.SetAttribute(ctx, path.Root("environment_variables"), env).HasError())
	return plan
}

func assertFunctionEnvironmentState(t *testing.T, state tfsdk.State, want types.Map, names []string) {
	t.Helper()
	ctx := context.Background()
	var env types.Map
	require.False(t, state.GetAttribute(ctx, path.Root("environment_variables"), &env).HasError())
	require.True(t, want.Equal(env), "configured map must survive API responses containing names only")
	var gotNames []string
	require.False(t, state.GetAttribute(ctx, path.Root("environment_variable_names"), &gotNames).HasError())
	require.Equal(t, names, gotNames)
}

type functionEnvironmentDeployment struct {
	environment        map[string]string
	environmentPresent bool
	runtime            string
	zip                []byte
}

type functionEnvironmentHTTP struct {
	mu      sync.Mutex
	names   []string
	request functionEnvironmentDeployment
	calls   []string
	deleted bool
}

func (f *functionEnvironmentHTTP) deployment() functionEnvironmentDeployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	got := f.request
	got.environment = maps.Clone(got.environment)
	got.zip = slices.Clone(got.zip)
	return got
}

func (f *functionEnvironmentHTTP) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *functionEnvironmentHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req.Method)
	const base = "/api/v2/projects/p-1/branches/br-1/functions/hello"
	status := http.StatusOK
	body := `{}`
	deployment := neon.NeonFunctionDeployment{
		ID: 1, CreatedAt: "2026-01-01T00:00:00Z", Environment: f.names,
		Runtime: "nodejs24", MemoryMib: 256, Status: neon.NeonFunctionDeploymentStatusCompleted,
	}
	switch {
	case req.Method == http.MethodPost && req.URL.Path == base+"/deployments":
		if err := f.recordDeployment(req); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(neon.NeonFunctionDeploymentResponse{Deployment: deployment})
		if err != nil {
			return nil, err
		}
		body = string(encoded)
	case req.Method == http.MethodGet && req.URL.Path == base:
		if f.deleted {
			status, body = http.StatusNotFound, `{"message":"function not found"}`
			break
		}
		encoded, err := json.Marshal(neon.NeonFunctionResponse{Function: neon.NeonFunction{
			ID: "fn-1", Slug: "hello", Name: "hello", CreatedAt: "2026-01-01T00:00:00Z",
			InvocationURL:     "https://fixture.invalid/",
			CurrentDeployment: &neon.NeonFunctionCurrentDeployment{NeonFunctionDeployment: deployment},
			ActiveDeployment:  &neon.NeonFunctionActiveDeployment{NeonFunctionDeployment: deployment},
		}})
		if err != nil {
			return nil, err
		}
		body = string(encoded)
	case req.Method == http.MethodDelete && req.URL.Path == base:
		f.deleted = true
	default:
		return nil, fmt.Errorf("unexpected function request: %s %s", req.Method, req.URL.Path)
	}
	return &http.Response{
		StatusCode: status, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": {"application/json"}}, Request: req,
	}, nil
}

func (f *functionEnvironmentHTTP) recordDeployment(req *http.Request) error {
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		return fmt.Errorf("parse deployment content type: %w", err)
	}
	if mediaType != "multipart/form-data" || params["boundary"] == "" {
		return fmt.Errorf("deployment must use multipart/form-data with a boundary")
	}
	reader := multipart.NewReader(req.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read deployment part: %w", err)
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return fmt.Errorf("read deployment value: %w", err)
		}
		switch part.FormName() {
		case "environment":
			f.request.environmentPresent = true
			if err := json.Unmarshal(value, &f.request.environment); err != nil {
				return fmt.Errorf("decode deployment environment: %w", err)
			}
		case "runtime":
			f.request.runtime = string(value)
		case "zip":
			f.request.zip = value
		default:
			return fmt.Errorf("unexpected deployment part %q", part.FormName())
		}
	}
}
