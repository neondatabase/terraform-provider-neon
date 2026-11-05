package provider

import (
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

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestProjectAllowlistTransitions(t *testing.T) {
	t.Parallel()
	type step struct {
		configured string
		wantNull   bool
		wantIPs    []string
		patches    int
	}
	sequences := []struct {
		name  string
		steps []step
	}{
		{"unset to empty and back", []step{
			{"", true, nil, 0},
			{"allowed_ips = []", false, nil, 0},
			{"", true, nil, 0},
			{"allowed_ips = []", false, nil, 0},
		}},
		{"initial empty and refresh", []step{
			{"allowed_ips = []", false, nil, 0},
			{"allowed_ips = []", false, nil, 0},
			{"", true, nil, 0},
		}},
		{"nonempty changes and clearing", []step{
			{`allowed_ips = ["203.0.113.1", "203.0.113.2"]`, false, []string{"203.0.113.1", "203.0.113.2"}, 0},
			{`allowed_ips = ["203.0.113.2"]`, false, []string{"203.0.113.2"}, 1},
			{"allowed_ips = []", false, nil, 2},
			{"", true, nil, 2},
			{`allowed_ips = ["203.0.113.1"]`, false, []string{"203.0.113.1"}, 3},
			{"", true, nil, 4},
		}},
	}
	for _, shape := range []string{"empty ips", "null ips", "omitted allowlist", "null settings", "omitted settings"} {
		for _, sequence := range sequences {
			t.Run(shape+"/"+sequence.name, func(t *testing.T) {
				t.Parallel()
				api := &allowlistAPI{emptyShape: shape}
				client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
				require.NoError(t, err)
				var steps []resource.TestStep
				for _, next := range sequence.steps {
					want := next
					steps = append(steps, resource.TestStep{
						Config: allowlistTestConfig(next.configured),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckOutput("allowlist_is_null", strconv.FormatBool(next.wantNull)),
							func(_ *terraform.State) error {
								api.mu.Lock()
								defer api.mu.Unlock()
								if !slices.Equal(api.ips, want.wantIPs) {
									return fmt.Errorf("API allowlist: want %v, got %v", want.wantIPs, api.ips)
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

// An ignored API update must remain visible as an inconsistent apply result.
// Copying the planned list over a nonempty API result would hide this failure.
func TestProjectAllowlistDoesNotMaskFailedClearing(t *testing.T) {
	t.Parallel()
	for _, removed := range []string{"", "allowed_ips = []"} {
		t.Run(fmt.Sprintf("remove with %q", removed), func(t *testing.T) {
			t.Parallel()
			api := &allowlistAPI{emptyShape: "empty ips", ignoreUpdates: true}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{Config: allowlistTestConfig(`allowed_ips = ["203.0.113.1"]`)},
					{Config: allowlistTestConfig(removed), ExpectError: regexp.MustCompile("Provider produced inconsistent result after apply")},
				},
			})
		})
	}
}

func allowlistTestConfig(settings string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = "allowlist"
  %s
}
output "allowlist_is_null" {
  value = neon_project.this.allowed_ips == null
}
`, settings)
}

// The API cannot distinguish omitted and explicitly empty IP lists. Responses
// cover all empty representations; writes change subsequent GET results.
type allowlistAPI struct {
	mu            sync.Mutex
	ips           []string
	patches       int
	emptyShape    string
	ignoreUpdates bool
}

func (f *allowlistAPI) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	projectPath := "/api/v2/projects/p-1"
	if req.Method == http.MethodPost && req.URL.Path == "/api/v2/projects" ||
		req.Method == http.MethodPatch && req.URL.Path == projectPath {
		var body struct {
			Project struct {
				Settings struct {
					AllowedIPs json.RawMessage `json:"allowed_ips"`
				} `json:"settings"`
			} `json:"project"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		if req.Method == http.MethodPatch {
			f.patches++
		}
		if len(body.Project.Settings.AllowedIPs) != 0 {
			var allowlist map[string]json.RawMessage
			if err := json.Unmarshal(body.Project.Settings.AllowedIPs, &allowlist); err != nil {
				return nil, err
			}
			if raw, ok := allowlist["ips"]; ok {
				if string(raw) == "null" {
					return nil, fmt.Errorf("ips must be an array, not null")
				}
				var ips []string
				if err := json.Unmarshal(raw, &ips); err != nil {
					return nil, err
				}
				if !f.ignoreUpdates || req.Method == http.MethodPost {
					f.ips = ips
				}
			}
		}
	} else if req.Method == http.MethodGet {
		switch req.URL.Path {
		case projectPath, projectPath + "/branches", projectPath + "/endpoints",
			projectPath + "/endpoints/ep-1", projectPath + "/branches/br-1/databases":
		default:
			return nil, fmt.Errorf("unexpected GET path: %s", req.URL.Path)
		}
	} else if req.Method != http.MethodDelete || req.URL.Path != projectPath {
		return nil, fmt.Errorf("unexpected API call: %s %s", req.Method, req.URL.Path)
	}

	project := map[string]any{
		"id": "p-1", "name": "allowlist", "org_id": "org-1", "region_id": "aws-us-east-2",
		"pg_version": 17, "provisioner": "k8s-neonvm", "store_passwords": true,
		"history_retention_seconds": 86400,
		"default_endpoint_settings": map[string]any{"suspend_timeout_seconds": 0},
	}
	if len(f.ips) > 0 {
		project["settings"] = map[string]any{"allowed_ips": map[string]any{"ips": f.ips}}
	} else {
		switch f.emptyShape {
		case "empty ips":
			project["settings"] = map[string]any{"allowed_ips": map[string]any{"ips": []string{}}}
		case "null ips":
			project["settings"] = map[string]any{"allowed_ips": map[string]any{"ips": nil}}
		case "omitted allowlist":
			project["settings"] = map[string]any{}
		case "null settings":
			project["settings"] = nil
		case "omitted settings":
		default:
			return nil, fmt.Errorf("unknown empty response shape: %s", f.emptyShape)
		}
	}
	branch := map[string]any{"id": "br-1", "project_id": "p-1", "name": "main", "default": true, "protected": false}
	endpoint := map[string]any{
		"id": "ep-1", "project_id": "p-1", "branch_id": "br-1", "type": "read_write",
		"region_id": "aws-us-east-2", "host": "ep-1.example", "provisioner": "k8s-neonvm",
		"settings": map[string]any{}, "autoscaling_limit_min_cu": 0.25, "autoscaling_limit_max_cu": 2,
	}
	response := map[string]any{
		"project": project, "branch": branch, "branches": []any{branch},
		"endpoint": endpoint, "endpoints": []any{endpoint},
		"databases":  []any{map[string]any{"branch_id": "br-1", "name": "neondb", "owner_name": "neondb_owner"}},
		"operations": []any{},
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}
