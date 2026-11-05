package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestProjectPrimaryLibrariesLifecycle(t *testing.T) {
	t.Parallel()
	for _, shape := range []string{"empty list", "null list", "missing list", "null preload", "missing preload", "null settings", "missing settings"} {
		for _, createEmpty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/create_empty=%t", shape, createEmpty), func(t *testing.T) {
				t.Parallel()
				api := &primaryLibrariesAPI{
					endpoint: endpointLibrariesAPI{emptyShape: shape},
				}
				t.Cleanup(func() {
					if t.Failed() {
						api.mu.Lock()
						defer api.mu.Unlock()
						t.Logf("endpoint PATCH bodies: %v", api.endpointBodies)
					}
				})
				client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
				require.NoError(t, err)
				var steps []resource.TestStep
				if !createEmpty {
					want := "null"
					if shape == "empty list" {
						want = "[]"
					}
					steps = append(steps, resource.TestStep{
						Config: primaryLibrariesConfig(""),
						Check:  resource.TestCheckOutput("primary_libraries_json", want),
					})
				}
				empty := resource.TestStep{
					Config: primaryLibrariesConfig("primary_compute = { enabled_libraries = [] }"),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckOutput("primary_libraries_json", "[]"),
						func(_ *terraform.State) error {
							api.endpoint.mu.Lock()
							defer api.endpoint.mu.Unlock()
							if api.endpoint.patches != 0 {
								return fmt.Errorf("empty intent required no endpoint mutation, got %d PATCH requests", api.endpoint.patches)
							}
							return nil
						},
					),
				}
				steps = append(steps, empty, empty)
				resource.UnitTest(t, resource.TestCase{
					ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
					Steps:                    steps,
				})
			})
		}
	}
}

func TestProjectPrimaryLibrariesNonemptyTransitions(t *testing.T) {
	t.Parallel()
	api := &primaryLibrariesAPI{endpoint: endpointLibrariesAPI{emptyShape: "missing settings"}}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	var steps []resource.TestStep
	for _, tt := range []struct {
		libraries string
		patches   int
	}{
		{`["pg_stat_statements"]`, 1},
		{`["auto_explain"]`, 2},
		{`[]`, 3},
		{`[]`, 3},
	} {
		steps = append(steps, resource.TestStep{
			Config: primaryLibrariesConfig("primary_compute = { enabled_libraries = " + tt.libraries + " }"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("primary_libraries_json", tt.libraries),
				resource.TestCheckOutput("project_libraries_json", "null"),
				primaryLibrariesPatchCount(api, tt.patches),
			),
		})
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps:                    steps,
	})
}

func TestProjectPrimaryLibrariesUseDefaultsUnchanged(t *testing.T) {
	t.Parallel()
	for _, defaults := range []bool{false, true} {
		t.Run(strconv.FormatBool(defaults), func(t *testing.T) {
			t.Parallel()
			api := &primaryLibrariesAPI{endpoint: endpointLibrariesAPI{emptyShape: "missing settings"}}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			step := resource.TestStep{
				Config: primaryLibrariesConfig(fmt.Sprintf("primary_compute = { enabled_libraries = [], enabled_libraries_use_defaults = %t }", defaults)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckOutput("primary_libraries_json", "[]"),
					resource.TestCheckOutput("project_libraries_json", "null"),
					resource.TestCheckResourceAttr("neon_project.this", "primary_compute.enabled_libraries_use_defaults", strconv.FormatBool(defaults)),
					primaryLibrariesPatchCount(api, 1),
				),
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps:                    []resource.TestStep{step, step},
			})
		})
	}
}

func TestProjectPrimaryLibrariesDoNotMaskAPIContents(t *testing.T) {
	t.Parallel()
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial_nonempty=%t", initial), func(t *testing.T) {
			t.Parallel()
			api := &primaryLibrariesAPI{endpoint: endpointLibrariesAPI{emptyShape: "missing settings", ignoreClear: true}}
			var steps []resource.TestStep
			if initial {
				api.endpoint.libraries = []string{"pg_stat_statements"}
			} else {
				steps = append(steps, resource.TestStep{
					Config: primaryLibrariesConfig(`primary_compute = { enabled_libraries = ["pg_stat_statements"] }`),
				})
			}
			steps = append(steps, resource.TestStep{
				Config:      primaryLibrariesConfig("primary_compute = { enabled_libraries = [] }"),
				ExpectError: regexp.MustCompile("Provider produced inconsistent result after apply"),
			})
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps:                    steps,
			})
		})
	}
}

func primaryLibrariesPatchCount(api *primaryLibrariesAPI, want int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		api.endpoint.mu.Lock()
		defer api.endpoint.mu.Unlock()
		if api.endpoint.patches != want {
			return fmt.Errorf("endpoint PATCH count: want %d, got %d", want, api.endpoint.patches)
		}
		return nil
	}
}

func primaryLibrariesConfig(settings string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = "libraries"
  %s
}
output "primary_libraries_json" {
  value = jsonencode(neon_project.this.primary_compute.enabled_libraries)
}
output "project_libraries_json" {
  value = jsonencode(neon_project.this.enabled_libraries)
}
`, settings)
}

// Terraform and the resource lifecycle remain real. Project metadata and the
// stateful endpoint service share one HTTP boundary, and invalid endpoint
// requests fail instead of receiving a canned success response.
type primaryLibrariesAPI struct {
	project          projectLibrariesAPI
	endpoint         endpointLibrariesAPI
	mu               sync.Mutex
	endpointBodies   []string
	getOnlyLibraries []string
}

func (f *primaryLibrariesAPI) Do(req *http.Request) (*http.Response, error) {
	const base = "/api/v2/projects/p-1"
	if req.URL.Path == base+"/endpoints/ep-1" {
		if req.Method == http.MethodPatch {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			f.mu.Lock()
			f.endpointBodies = append(f.endpointBodies, string(body))
			f.mu.Unlock()
			var request struct {
				Endpoint struct {
					BranchID *string `json:"branch_id"`
				} `json:"endpoint"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				return nil, err
			}
			if request.Endpoint.BranchID != nil && *request.Endpoint.BranchID != "br-1" {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body: io.NopCloser(strings.NewReader(fmt.Sprintf(
						`{"message":%q}`, fmt.Sprintf("primary compute PATCH sent branch_id=%q; branch_id is read-only", *request.Endpoint.BranchID),
					))),
				}, nil
			}
			req.Body = io.NopCloser(strings.NewReader(string(body)))
		}
		response, err := f.endpoint.Do(req)
		if err != nil || req.Method != http.MethodGet || len(f.getOnlyLibraries) == 0 {
			return response, err
		}
		defer response.Body.Close()
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			return nil, err
		}
		var endpoint map[string]any
		if err := json.Unmarshal(payload["endpoint"], &endpoint); err != nil {
			return nil, err
		}
		endpoint["settings"] = map[string]any{"preload_libraries": map[string]any{"enabled_libraries": f.getOnlyLibraries}}
		payload["endpoint"], err = json.Marshal(endpoint)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: response.StatusCode, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	}
	response, err := f.project.Do(req)
	if err != nil {
		return nil, err
	}
	if req.Method != http.MethodPost && (req.Method != http.MethodGet || req.URL.Path != base+"/endpoints") {
		return response, nil
	}
	defer response.Body.Close()
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	get := req.Clone(req.Context())
	url := *req.URL
	url.Path = base + "/endpoints/ep-1"
	get.URL = &url
	get.Method = http.MethodGet
	get.Body = nil
	endpointResponse, err := f.endpoint.Do(get)
	if err != nil {
		return nil, err
	}
	defer endpointResponse.Body.Close()
	var endpoint struct {
		Endpoint json.RawMessage `json:"endpoint"`
	}
	if err := json.NewDecoder(endpointResponse.Body).Decode(&endpoint); err != nil {
		return nil, err
	}
	endpoints, err := json.Marshal([]json.RawMessage{endpoint.Endpoint})
	if err != nil {
		return nil, err
	}
	payload["endpoints"] = endpoints
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: response.StatusCode, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}
