package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestEndpointLibrariesLifecycle(t *testing.T) {
	t.Parallel()
	type step struct {
		configured string
		want       string
		libraries  []string
		patches    int
	}
	for _, shape := range []string{"empty list", "null list", "missing list", "null preload", "missing preload", "null settings", "missing settings"} {
		initial := "null"
		if shape == "empty list" {
			initial = "[]"
		}
		for _, sequence := range []struct {
			name  string
			steps []step
		}{
			{"unset to empty and refresh", []step{
				{configured: "", want: initial},
				{configured: "enabled_libraries = []", want: "[]"},
				{configured: "enabled_libraries = []", want: "[]"},
			}},
			{"empty create and refresh", []step{
				{configured: "enabled_libraries = []", want: "[]"},
				{configured: "enabled_libraries = []", want: "[]"},
			}},
			{"nonempty edit and clear", []step{
				{configured: `enabled_libraries = ["pg_stat_statements"]`, want: `["pg_stat_statements"]`, libraries: []string{"pg_stat_statements"}, patches: 1},
				{configured: `enabled_libraries = ["auto_explain"]`, want: `["auto_explain"]`, libraries: []string{"auto_explain"}, patches: 2},
				{configured: "enabled_libraries = []", want: "[]", patches: 3},
				{configured: "enabled_libraries = []", want: "[]", patches: 3},
			}},
		} {
			t.Run(shape+"/"+sequence.name, func(t *testing.T) {
				t.Parallel()
				api := &endpointLibrariesAPI{emptyShape: shape}
				client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
				require.NoError(t, err)
				var steps []resource.TestStep
				for _, next := range sequence.steps {
					want := next
					steps = append(steps, resource.TestStep{
						Config: endpointLibrariesConfig(next.configured),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckOutput("libraries_json", next.want),
							func(_ *terraform.State) error {
								api.mu.Lock()
								defer api.mu.Unlock()
								if !slices.Equal(api.libraries, want.libraries) {
									return fmt.Errorf("API libraries: want %v, got %v", want.libraries, api.libraries)
								}
								if api.patches != want.patches {
									return fmt.Errorf("PATCH count: want %d, got %d", want.patches, api.patches)
								}
								return nil
							},
						),
					})
				}
				resource.UnitTest(t, resource.TestCase{
					ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
					Steps:                    steps,
				})
			})
		}
	}
}

func TestEndpointLibrariesDoNotMaskFailedClear(t *testing.T) {
	t.Parallel()
	api := &endpointLibrariesAPI{emptyShape: "missing settings", ignoreClear: true}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resource.TestStep{
			{Config: endpointLibrariesConfig(`enabled_libraries = ["pg_stat_statements"]`)},
			{Config: endpointLibrariesConfig("enabled_libraries = []"), ExpectError: regexp.MustCompile("Provider produced inconsistent result after apply")},
		},
	})
}

// The shared project handler must return the new list shape without issuing
// a PATCH when the API already has no libraries. This tests the handler, not
// the separate parent project's subsequent Read.
func TestEndpointLibrariesPrimaryComputeHandler(t *testing.T) {
	t.Parallel()
	api := &endpointLibrariesAPI{emptyShape: "missing settings"}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	ctx := context.Background()
	r := &neonProjectResource{}
	var configured fwresource.ConfigureResponse
	r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError(), "%v", configured.Diagnostics)
	prior := endpointLibrariesPrior(t, client)
	planned := prior
	planned.EnabledLibraries = types.ListValueMust(types.StringType, nil)
	var diagnostics diag.Diagnostics
	result := r.updatePrimaryCompute(ctx, &prior, &planned, &diagnostics)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	require.NotNil(t, result)
	require.True(t, result.EnabledLibraries.Equal(types.ListValueMust(types.StringType, nil)))
	require.Zero(t, api.patches)
}

// A null plan must not cause actual API contents to be replaced with null.
func TestEndpointLibrariesNullPlanKeepsAPIContents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := &endpointLibrariesAPI{emptyShape: "missing settings"}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	r := &neonEndpointResource{}
	var configured fwresource.ConfigureResponse
	r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError())
	var schema fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schema)
	prior := endpointLibrariesPrior(t, client)
	planned := prior
	planned.EnabledLibraries = types.ListNull(types.StringType)
	api.libraries = []string{"pg_stat_statements"}
	state := tfsdk.State{Schema: schema.Schema}
	require.False(t, state.Set(ctx, &prior).HasError())
	plan := tfsdk.Plan{Schema: schema.Schema}
	require.False(t, plan.Set(ctx, &planned).HasError())
	response := fwresource.UpdateResponse{State: state}
	r.Update(ctx, fwresource.UpdateRequest{State: state, Plan: plan}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	var got neonEndpointResourceModel
	require.False(t, response.State.Get(ctx, &got).HasError())
	require.True(t, got.EnabledLibraries.Equal(types.ListValueMust(types.StringType, []attr.Value{types.StringValue("pg_stat_statements")})))
	require.Zero(t, api.patches)
}

// A transient PATCH payload must not invent empty intent for an originally
// null plan when the final GET reports that no libraries remain.
func TestEndpointLibrariesOriginalPlanSurvivesPatchMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := &endpointLibrariesAPI{emptyShape: "missing settings", patchOnlyLibraries: []string{"auto_explain"}}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	r := &neonEndpointResource{}
	var configured fwresource.ConfigureResponse
	r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError())
	var schema fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schema)
	prior := endpointLibrariesPrior(t, client)
	planned := prior
	planned.Name = types.StringValue("renamed")
	state := tfsdk.State{Schema: schema.Schema}
	require.False(t, state.Set(ctx, &prior).HasError())
	plan := tfsdk.Plan{Schema: schema.Schema}
	require.False(t, plan.Set(ctx, &planned).HasError())
	response := fwresource.UpdateResponse{State: state}
	r.Update(ctx, fwresource.UpdateRequest{State: state, Plan: plan}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	var got neonEndpointResourceModel
	require.False(t, response.State.Get(ctx, &got).HasError())
	require.True(t, got.EnabledLibraries.IsNull(), "intermediate PATCH contents invented empty intent")
	require.Equal(t, 1, api.patches)
}

func TestEndpointLibrariesMapperIntent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		prior types.List
		null  bool
	}{
		{"null", types.ListNull(types.StringType), true},
		{"unknown", types.ListUnknown(types.StringType), true},
		{"empty", types.ListValueMust(types.StringType, nil), false},
		{"removed contents", types.ListValueMust(types.StringType, []attr.Value{types.StringValue("auto_explain")}), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := neonEndpointResourceModel{EnabledLibraries: tt.prior}
			var diagnostics diag.Diagnostics
			model.inferAttr(context.Background(), neon.Endpoint{}, &diagnostics)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Equal(t, tt.null, model.EnabledLibraries.IsNull())
			require.False(t, model.EnabledLibraries.IsUnknown())
			require.Empty(t, model.EnabledLibraries.Elements())
		})
	}
}

func endpointLibrariesPrior(t *testing.T, client *neon.Client) neonEndpointResourceModel {
	t.Helper()
	endpoint, err := client.GetProjectEndpoint("p-1", "ep-1")
	require.NoError(t, err)
	var prior neonEndpointResourceModel
	var diagnostics diag.Diagnostics
	prior.inferAttr(context.Background(), endpoint.Endpoint, &diagnostics)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	return prior
}

func endpointLibrariesConfig(settings string) string {
	return fmt.Sprintf(`resource "neon_endpoint" "this" {
  project_id = "p-1"
  branch_id = "br-1"
  # Defaults are explicit to avoid the unrelated N2 bug on head.
  type = "read_write"
  compute_provisioner = "k8s-neonvm"
  %s
}
output "libraries_json" {
  value = jsonencode(neon_endpoint.this.enabled_libraries)
}
`, settings)
}

// Requests mutate the following GET responses. A rejected or ignored write
// therefore cannot satisfy a later check merely because a fixture says so.
type endpointLibrariesAPI struct {
	mu                 sync.Mutex
	emptyShape         string
	libraries          []string
	useDefaults        *bool
	patches            int
	ignoreClear        bool
	patchOnlyLibraries []string
}

func (f *endpointLibrariesAPI) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const base = "/api/v2/projects/p-1/endpoints"
	switch {
	case req.Method == http.MethodPost && req.URL.Path == base:
	case req.Method == http.MethodGet && req.URL.Path == base+"/ep-1":
	case req.Method == http.MethodDelete && req.URL.Path == base+"/ep-1":
	case req.Method == http.MethodPatch && req.URL.Path == base+"/ep-1":
		var body struct {
			Endpoint struct {
				Settings struct {
					Preload json.RawMessage `json:"preload_libraries"`
				} `json:"settings"`
			} `json:"endpoint"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		f.patches++
		if len(body.Endpoint.Settings.Preload) != 0 {
			var preload map[string]json.RawMessage
			if err := json.Unmarshal(body.Endpoint.Settings.Preload, &preload); err != nil {
				return nil, err
			}
			if libs, ok := preload["enabled_libraries"]; ok {
				if string(libs) == "null" {
					return nil, fmt.Errorf("enabled_libraries must be an array, not null")
				}
				var values []string
				if err := json.Unmarshal(libs, &values); err != nil {
					return nil, err
				}
				if !f.ignoreClear || len(values) > 0 {
					f.libraries = values
				}
			}
			if defaults, ok := preload["use_defaults"]; ok {
				if err := json.Unmarshal(defaults, &f.useDefaults); err != nil {
					return nil, err
				}
			}
		}
	default:
		return nil, fmt.Errorf("unexpected API request: %s %s", req.Method, req.URL.Path)
	}
	endpoint := map[string]any{
		"id": "ep-1", "project_id": "p-1", "branch_id": "br-1", "type": "read_write",
		"region_id": "aws-us-east-2", "host": "ep-1.example", "provisioner": "k8s-neonvm",
		"autoscaling_limit_min_cu": 0.25, "autoscaling_limit_max_cu": 2, "suspend_timeout_seconds": 0,
		"disabled": false, "passwordless_access": true,
	}
	libraries := f.libraries
	if req.Method == http.MethodPatch && len(f.patchOnlyLibraries) > 0 {
		libraries = f.patchOnlyLibraries
	}
	var settings map[string]any
	if len(libraries) > 0 {
		settings = map[string]any{"preload_libraries": map[string]any{"enabled_libraries": libraries}}
	} else {
		switch f.emptyShape {
		case "empty list":
			settings = map[string]any{"preload_libraries": map[string]any{"enabled_libraries": []string{}}}
		case "null list":
			settings = map[string]any{"preload_libraries": map[string]any{"enabled_libraries": nil}}
		case "missing list":
			settings = map[string]any{"preload_libraries": map[string]any{}}
		case "null preload":
			settings = map[string]any{"preload_libraries": nil}
		case "missing preload":
			settings = map[string]any{}
		case "null settings", "missing settings":
		default:
			return nil, fmt.Errorf("unknown response shape: %s", f.emptyShape)
		}
	}
	if f.useDefaults != nil {
		if settings == nil {
			settings = map[string]any{}
		}
		preload, ok := settings["preload_libraries"].(map[string]any)
		if !ok {
			preload = map[string]any{}
			settings["preload_libraries"] = preload
		}
		preload["use_defaults"] = f.useDefaults
	}
	if f.emptyShape != "missing settings" || len(libraries) > 0 || f.useDefaults != nil {
		endpoint["settings"] = settings
	}
	data, err := json.Marshal(map[string]any{"endpoint": endpoint, "operations": []any{}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}

func TestEndpointLibrariesUseDefaultsUnchanged(t *testing.T) {
	t.Parallel()
	for _, useDefaults := range []bool{false, true} {
		t.Run(strconv.FormatBool(useDefaults), func(t *testing.T) {
			t.Parallel()
			api := &endpointLibrariesAPI{emptyShape: "missing settings"}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			config := endpointLibrariesConfig(fmt.Sprintf("enabled_libraries = []\nenabled_libraries_use_defaults = %t", useDefaults))
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{Config: config, Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckOutput("libraries_json", "[]"),
						resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries_use_defaults", strconv.FormatBool(useDefaults)),
					)},
					{Config: config, Check: func(_ *terraform.State) error {
						api.mu.Lock()
						defer api.mu.Unlock()
						if api.useDefaults == nil || *api.useDefaults != useDefaults || api.patches != 1 {
							return fmt.Errorf("API defaults/PATCH count: got %v/%d, want %t/1", api.useDefaults, api.patches, useDefaults)
						}
						return nil
					}},
				},
			})
		})
	}
}
