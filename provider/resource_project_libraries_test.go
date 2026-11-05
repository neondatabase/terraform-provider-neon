package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// Dropping a configured [] during Create or Read produces an inconsistent
// apply. Each configuration is applied twice so the saved empty/null intent
// must also survive a subsequent refresh without an update request.
func TestProjectLibrariesCreateAndRefresh(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name     string
		settings string
	}{
		{"empty list", `{"preload_libraries":{"enabled_libraries":[]}}`},
		{"null list", `{"preload_libraries":{"enabled_libraries":null}}`},
		{"missing list", `{"preload_libraries":{}}`},
		{"null preload libraries", `{"preload_libraries":null}`},
		{"missing preload libraries", `{}`},
		{"null settings", `null`},
		{"missing settings", ""},
	} {
		for _, configured := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/configured=%t", shape.name, configured), func(t *testing.T) {
				t.Parallel()
				api := &projectLibrariesAPI{settings: shape.settings}
				config, want := "", "null"
				if configured {
					config, want = "enabled_libraries = []", "[]"
				}
				runProjectLibrariesLifecycle(t, api, config, want)
			})
		}
	}
	for _, configured := range []bool{true, false} {
		t.Run(fmt.Sprintf("API nonempty/configured=%t", configured), func(t *testing.T) {
			t.Parallel()
			api := &projectLibrariesAPI{settings: `{"preload_libraries":{"enabled_libraries":["pg_stat_statements"],"use_defaults":true}}`}
			config := "enabled_libraries_use_defaults = true"
			if configured {
				config += "\nenabled_libraries = [\"pg_stat_statements\"]"
			}
			runProjectLibrariesLifecycle(t, api, config, `["pg_stat_statements"]`)
		})
	}
}

// A nonempty API result must not be replaced with the configured empty list.
func TestProjectLibrariesDoNotHideUnexpectedAPIContents(t *testing.T) {
	t.Parallel()
	api := &projectLibrariesAPI{settings: `{"preload_libraries":{"enabled_libraries":["pg_stat_statements"]}}`}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resource.TestStep{{
			Config:      projectLibrariesConfig("enabled_libraries = []"),
			ExpectError: regexp.MustCompile("Provider produced inconsistent result after apply"),
		}},
	})
}

// Unknown prior values are not explicit empty intent. A vanished API list is
// empty, rather than retaining stale nonempty contents from prior state.
func TestProjectLibrariesReadIntent(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		prior types.List
		null  bool
	}{
		{"null remains null", types.ListNull(types.StringType), true},
		{"unknown becomes null", types.ListUnknown(types.StringType), true},
		{"empty stays empty", types.ListValueMust(types.StringType, nil), false},
		{"removed contents become empty", types.ListValueMust(types.StringType,
			[]attr.Value{types.StringValue("pg_stat_statements")}), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := neonProjectResourceModel{
				AllowedIPs:       types.ListNull(types.StringType),
				EnabledLibraries: tt.prior,
			}
			var diagnostics diag.Diagnostics
			model.inferAttr(context.Background(), neon.Project{}, neon.Branch{}, neon.Endpoint{}, neon.Database{}, &diagnostics)
			require.False(t, diagnostics.HasError(), "%v", diagnostics)
			require.Equal(t, tt.null, model.EnabledLibraries.IsNull())
			require.False(t, model.EnabledLibraries.IsUnknown())
			require.Empty(t, model.EnabledLibraries.Elements())
		})
	}
}

func runProjectLibrariesLifecycle(t *testing.T, api *projectLibrariesAPI, settings, want string) {
	t.Helper()
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	step := resource.TestStep{
		Config: projectLibrariesConfig(settings),
		Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckOutput("libraries_json", want),
			func(_ *terraform.State) error {
				api.mu.Lock()
				defer api.mu.Unlock()
				if api.patches != 0 {
					return fmt.Errorf("identical config caused %d unexpected PATCH requests", api.patches)
				}
				return nil
			},
		),
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps:                    []resource.TestStep{step, step},
	})
}

func projectLibrariesConfig(settings string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = "libraries"
  %s
}
output "libraries_json" {
  value = jsonencode(neon_project.this.enabled_libraries)
}
`, settings)
}

// Only the HTTP boundary is replaced. Terraform and the provider still plan,
// create, refresh, and destroy the resource. Unexpected mutations fail instead
// of getting a generic successful response.
type projectLibrariesAPI struct {
	settings string
	mu       sync.Mutex
	patches  int
}

func (f *projectLibrariesAPI) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const base = "/api/v2/projects/p-1"
	switch {
	case req.Method == http.MethodPost && req.URL.Path == "/api/v2/projects":
	case req.Method == http.MethodGet:
		switch req.URL.Path {
		case base, base + "/branches", base + "/endpoints", base + "/branches/br-1/databases":
		default:
			return nil, fmt.Errorf("unexpected GET %s", req.URL.Path)
		}
	case req.Method == http.MethodDelete && req.URL.Path == base:
	case req.Method == http.MethodPatch:
		f.patches++
		return nil, fmt.Errorf("unexpected PATCH %s", req.URL.Path)
	default:
		return nil, fmt.Errorf("unexpected API call: %s %s", req.Method, req.URL.Path)
	}

	project := map[string]any{
		"id": "p-1", "name": "libraries", "org_id": "org-1", "region_id": "aws-us-east-2",
		"pg_version": 17, "provisioner": "k8s-neonvm", "store_passwords": true,
		"history_retention_seconds": 86400,
		"default_endpoint_settings": map[string]any{"suspend_timeout_seconds": 0},
	}
	if f.settings != "" {
		project["settings"] = json.RawMessage(f.settings)
	}
	branch := map[string]any{"id": "br-1", "project_id": "p-1", "name": "main", "default": true, "protected": false}
	response := map[string]any{
		"project": project, "branch": branch, "branches": []any{branch},
		"endpoints": []any{map[string]any{
			"id": "ep-1", "project_id": "p-1", "branch_id": "br-1", "type": "read_write",
			"region_id": "aws-us-east-2", "host": "ep-1.example", "provisioner": "k8s-neonvm",
			"settings": map[string]any{}, "autoscaling_limit_min_cu": 0.25, "autoscaling_limit_max_cu": 2,
		}},
		"databases":  []any{map[string]any{"branch_id": "br-1", "name": "neondb", "owner_name": "neondb_owner"}},
		"operations": []any{},
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}
