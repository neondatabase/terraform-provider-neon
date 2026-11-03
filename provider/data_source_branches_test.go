package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

const branchesAddress = "data.neon_branches.this"

const branchesOneResponse = `{
  "branches": [
    {
      "id": "br-first-123",
      "project_id": "response-project",
      "name": "main",
      "parent_id": "br-parent-789",
      "parent_lsn": "0/16B6C50",
      "parent_timestamp": "2026-01-01T00:00:00.123456Z",
      "logical_size": 1048577,
      "primary": true,
      "default": true,
      "protected": true,
      "current_state": "ready",
      "state_changed_at": "2026-01-01T00:00:00Z",
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "cpu_used_sec": 0,
      "active_time_seconds": 0,
      "compute_time_seconds": 0,
      "written_data_bytes": 0,
      "data_transfer_bytes": 0
    }
  ]
}`

const branchesTwoResponse = `{
  "branches": [
    {
      "id": "br-first-123",
      "project_id": "response-project",
      "name": "first",
      "parent_id": "",
      "parent_lsn": "",
      "logical_size": 0,
      "primary": false,
      "default": false,
      "protected": false,
      "current_state": "ready",
      "state_changed_at": "2026-01-01T00:00:00Z",
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "cpu_used_sec": 0,
      "active_time_seconds": 0,
      "compute_time_seconds": 0,
      "written_data_bytes": 0,
      "data_transfer_bytes": 0
    },
    {
      "id": "br-second-456",
      "project_id": "response-project",
      "name": "second",
      "default": true,
      "protected": false,
      "current_state": "ready",
      "state_changed_at": "2026-01-01T00:00:00Z",
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "cpu_used_sec": 0,
      "active_time_seconds": 0,
      "compute_time_seconds": 0,
      "written_data_bytes": 0,
      "data_transfer_bytes": 0
    }
  ]
}`

func TestBranchesDataSource(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name                    string
		projectID               string
		status                  int
		bodies                  []string
		check                   resource.TestCheckFunc
		wantError               string
		checkPrimaryDeprecation bool
	}{
		{
			name:      "missing project",
			projectID: "missing-project",
			status:    http.StatusNotFound,
			bodies: []string{`{
  "code": "NOT_FOUND",
  "message": "project not found"
}`},
			wantError: "[HTTP Code: 404][Error Code: NOT_FOUND] project not found",
		},
		{
			name:                    "one branch",
			projectID:               "cool-moon-42",
			status:                  http.StatusOK,
			bodies:                  []string{branchesOneResponse},
			checkPrimaryDeprecation: true,
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", "1",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.id", "br-first-123",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.name", "main",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_id", "br-parent-789",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.project_id", "response-project",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_lsn", "0/16B6C50",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_timestamp", "1767225600",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.logical_size", "1048577",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.primary", "true",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.default", "true",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.protected", "true",
				),
				resource.TestCheckOutput("null_parent_ids", ""),
				resource.TestCheckOutput("null_parent_lsn_ids", ""),
				resource.TestCheckOutput("null_size_ids", ""),
				resource.TestCheckOutput("null_primary_ids", ""),
			),
		},
		{
			name:      "two branches",
			projectID: "cool-moon-42",
			status:    http.StatusOK,
			bodies: []string{
				branchesTwoResponse,
				regexp.MustCompile(`"name": "second",`).ReplaceAllString(branchesTwoResponse, `"name": "second",
      "parent_id": null,
      "parent_lsn": null,
      "parent_timestamp": null,
      "logical_size": null,
      "primary": null,`),
			},
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", "2",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.id", "br-first-123",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.name", "first",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_id", "",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.project_id", "response-project",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_lsn", "",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.parent_timestamp", "0",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.logical_size", "0",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.primary", "false",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.default", "false",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.0.protected", "false",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.id", "br-second-456",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.name", "second",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.project_id", "response-project",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.parent_timestamp", "0",
				),
				resource.TestCheckOutput("null_parent_ids", "br-second-456"),
				resource.TestCheckOutput("null_parent_lsn_ids", "br-second-456"),
				resource.TestCheckOutput("null_size_ids", "br-second-456"),
				resource.TestCheckOutput("null_primary_ids", "br-second-456"),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.default", "true",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.1.protected", "false",
				),
			),
		},
		{
			name:      "no branches",
			projectID: "cool-moon-42",
			status:    http.StatusOK,
			bodies: []string{
				`{
  "branches": []
}`,
				`{
  "branches": null
}`,
				`{}`,
			},
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", "0",
				),
				resource.TestCheckOutput("branch_count", "0"),
			),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for i, body := range tt.bodies {
				t.Run(fmt.Sprintf("response %d", i+1), func(t *testing.T) {
					client := newBranchesTestClient(t, tt.projectID, tt.status, body)
					factories := newUnitTestProviderFactories(&providerAdapter{sdk: client})
					if tt.checkPrimaryDeprecation {
						server, err := factories["neon"]()
						require.NoError(t, err)
						response, err := server.GetProviderSchema(
							context.Background(),
							&tfprotov6.GetProviderSchemaRequest{},
						)
						require.NoError(t, err)
						require.Empty(t, response.Diagnostics)
						var primary *tfprotov6.SchemaAttribute
						for _, attribute := range response.DataSourceSchemas["neon_branches"].Block.Attributes {
							if attribute.Name != "branches" {
								continue
							}
							for _, field := range attribute.NestedType.Attributes {
								require.True(t, field.Computed, field.Name)
								require.False(t, field.Optional, field.Name)
								require.False(t, field.Required, field.Name)
								if field.Name == "primary" {
									primary = field
								}
							}
						}
						require.NotNil(t, primary)
						require.True(t, primary.Deprecated)
					}
					step := resource.TestStep{
						Config: branchesDataSourceConfig(tt.projectID, ""),
					}
					if tt.wantError != "" {
						step.ExpectError = regexp.MustCompile(regexp.QuoteMeta(tt.wantError))
					} else {
						step.Check = resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(
								branchesAddress, "project_id", "cool-moon-42",
							),
							resource.TestCheckResourceAttr(
								branchesAddress, "id", "cool-moon-42",
							),
							tt.check,
						)
					}
					resource.UnitTest(t, resource.TestCase{
						ProtoV6ProviderFactories: factories,
						Steps:                    []resource.TestStep{step},
					})
				})
			}
		})
	}
}

func TestBranchesDataSourceFiltering(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name         string
		filterConfig string
		query        url.Values
		body         string
		wantCount    string
		wantNull     string
		wantFilter   string
	}{
		{
			name: "unset filter",
			query: url.Values{
				"sort_by":    {"name"},
				"sort_order": {"asc"},
			},
			body:      branchesOneResponse,
			wantCount: "1",
			wantNull:  "true",
		},
		{
			name: "null filter",
			query: url.Values{
				"sort_by":    {"name"},
				"sort_order": {"asc"},
			},
			filterConfig: `filter = null`,
			body:         branchesOneResponse,
			wantCount:    "1",
			wantNull:     "true",
		},
		{
			name: "empty filter",
			query: url.Values{
				"sort_by":    {"name"},
				"sort_order": {"asc"},
			},
			filterConfig: `filter = ""`,
			body:         branchesOneResponse,
			wantCount:    "1",
			wantNull:     "false",
			wantFilter:   "",
		},
		{
			name:         "partial name search",
			filterConfig: `filter = "mai"`,
			query: url.Values{
				"search":     {"mai"},
				"sort_by":    {"name"},
				"sort_order": {"asc"},
			},
			body:       branchesOneResponse,
			wantCount:  "1",
			wantNull:   "false",
			wantFilter: "mai",
		},
		{
			name:         "partial ID search",
			filterConfig: `filter = "br-first"`,
			query: url.Values{
				"search":     {"br-first"},
				"sort_by":    {"name"},
				"sort_order": {"asc"},
			},
			body:       branchesOneResponse,
			wantCount:  "1",
			wantNull:   "false",
			wantFilter: "br-first",
		},
		{
			name:         "reserved search characters preserve state",
			filterConfig: `filter = "  unmatched+filter&value/one=two  "`,
			query: url.Values{
				"search":     {"  unmatched+filter&value/one=two  "},
				"sort_by":    {"name"},
				"sort_order": {"asc"},
			},
			body: `{
  "branches": []
}`,
			wantCount:  "0",
			wantNull:   "false",
			wantFilter: "  unmatched+filter&value/one=two  ",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &branchesPaginationHTTPFixture{
				t: t,
				pages: []branchesPage{
					{
						query:  tt.query,
						status: http.StatusOK,
						body:   tt.body,
					},
				},
			}
			client, err := neon.NewClient(neon.Config{
				Key:        "test",
				HTTPClient: fixture,
			})
			require.NoError(t, err)
			checks := []resource.TestCheckFunc{
				resource.TestCheckResourceAttr(
					branchesAddress, "id", "cool-moon-42",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "project_id", "cool-moon-42",
				),
				resource.TestCheckResourceAttr(
					branchesAddress, "branches.#", tt.wantCount,
				),
				resource.TestCheckOutput("filter_is_null", tt.wantNull),
			}
			if tt.wantNull == "false" {
				checks = append(checks, resource.TestCheckResourceAttr(
					branchesAddress, "filter", tt.wantFilter,
				))
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{
						Config: branchesDataSourceConfig("cool-moon-42", tt.filterConfig) + `
output "filter_is_null" {
  value = data.neon_branches.this.filter == null
}
`,
						Check: resource.ComposeAggregateTestCheckFunc(checks...),
					},
				},
			})
		})
	}
}

func TestBranchesDataSourcePagination(t *testing.T) {
	t.Parallel()
	firstPage := strings.TrimSuffix(branchesTwoResponse, "\n}") + `,
  "pagination": {
    "next": "next+page/with=cursor&value",
    "sort_by": "name",
    "sort_order": "asc"
  }
}`
	lastPage := strings.NewReplacer(
		"br-first-123", "br-third-789",
		`"name": "main"`, `"name": "third"`,
		`"primary": true`, `"primary": false`,
		`"default": true`, `"default": false`,
	).Replace(branchesOneResponse)
	middlePage := strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": {
    "next": "last-page"
  }
}`
	nextQuery := url.Values{
		"cursor":     {"next+page/with=cursor&value"},
		"sort_by":    {"name"},
		"sort_order": {"asc"},
	}
	filteredNextQuery := url.Values{
		"search":     {"br-"},
		"cursor":     {"next+page/with=cursor&value"},
		"sort_by":    {"name"},
		"sort_order": {"asc"},
	}
	lastQuery := url.Values{
		"cursor":     {"last-page"},
		"sort_by":    {"name"},
		"sort_order": {"asc"},
	}
	for _, tt := range []struct {
		name         string
		filterConfig string
		pages        []branchesPage
		wantError    string
	}{
		{
			name: "three pages request name ascending",
			pages: []branchesPage{
				{
					query: url.Values{
						"sort_by":    {"name"},
						"sort_order": {"asc"},
					},
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body:   middlePage,
				},
				{
					query:  lastQuery,
					status: http.StatusOK,
					body: `{
  "branches": []
}`,
				},
			},
		},
		{
			name:         "filtered later page retry adds no duplicate branches",
			filterConfig: `filter = "br-"`,
			pages: []branchesPage{
				{
					query: url.Values{
						"search":     {"br-"},
						"sort_by":    {"name"},
						"sort_order": {"asc"},
					},
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  filteredNextQuery,
					status: http.StatusTooManyRequests,
					body: `{
  "code": "RATE_LIMITED",
  "message": "retry this page"
}`,
				},
				{
					query:  filteredNextQuery,
					status: http.StatusOK,
					body:   lastPage,
				},
			},
		},
		{
			name: "later page failure rejects partial results",
			pages: []branchesPage{
				{
					query: url.Values{
						"sort_by":    {"name"},
						"sort_order": {"asc"},
					},
					status: http.StatusOK,
					body: strings.Replace(firstPage,
						`"next": "next+page/with=cursor&value"`,
						`"next": ""`,
						1,
					),
				},
				{
					query: url.Values{
						"cursor":     {""},
						"sort_by":    {"name"},
						"sort_order": {"asc"},
					},
					status: http.StatusForbidden,
					body: `{
  "code": "FORBIDDEN",
  "message": "page access denied"
}`,
				},
			},
			wantError: "[HTTP Code: 403][Error Code: FORBIDDEN] page access denied",
		},
		{
			name: "null pagination ends traversal",
			pages: []branchesPage{
				{
					query: url.Values{
						"sort_by":    {"name"},
						"sort_order": {"asc"},
					},
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body: strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": null
}`,
				},
			},
		},
		{
			name: "absent next cursor ends traversal",
			pages: []branchesPage{
				{
					query: url.Values{
						"sort_by":    {"name"},
						"sort_order": {"asc"},
					},
					status: http.StatusOK,
					body:   firstPage,
				},
				{
					query:  nextQuery,
					status: http.StatusOK,
					body: strings.TrimSuffix(lastPage, "\n}") + `,
  "pagination": {}
}`,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &branchesPaginationHTTPFixture{
				t:     t,
				pages: tt.pages,
			}
			client, err := neon.NewClient(neon.Config{
				Key:        "test",
				HTTPClient: fixture,
			})
			require.NoError(t, err)
			step := resource.TestStep{
				Config: branchesDataSourceConfig("cool-moon-42", tt.filterConfig),
			}
			if tt.wantError != "" {
				step.ExpectError = regexp.MustCompile(regexp.QuoteMeta(tt.wantError))
			} else {
				step.Check = resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						branchesAddress, "id", "cool-moon-42",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "project_id", "cool-moon-42",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.#", "3",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.0.id", "br-first-123",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.1.id", "br-second-456",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.id", "br-third-789",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.name", "third",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.logical_size", "1048577",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.primary", "false",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.default", "false",
					),
					resource.TestCheckResourceAttr(
						branchesAddress, "branches.2.protected", "true",
					),
					resource.TestCheckOutput("branch_count", "3"),
				)
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps:                    []resource.TestStep{step},
			})
			fixture.mu.Lock()
			requests := fixture.requests
			fixture.mu.Unlock()
			require.Positive(t, requests)
			require.Zero(t, requests%len(tt.pages), "each read must finish its expected page sequence")
		})
	}
}

func TestBranchesDataSourceConfiguredOutputs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		output string
	}{
		{
			name:   "former name input",
			output: `name = "main"`,
		},
		{
			name:   "id",
			output: `id = "configured-id"`,
		},
		{
			name:   "branches assignment",
			output: `branches = []`,
		},
		{
			name:   "branches block",
			output: `branches { name = "configured-name" }`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newBranchesTestClient(t, "cool-moon-42", http.StatusOK, branchesOneResponse)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`
data "neon_branches" "this" {
  project_id = "cool-moon-42"
  %s
}
`, tt.output),
						ExpectError: regexp.MustCompile(`(?i)(computed|unconfigurable|read.only|not expected|invalid or unknown key)`),
					},
				},
			})
		})
	}
}

func branchesDataSourceConfig(projectID, filterConfig string) string {
	return fmt.Sprintf(`
data "neon_branches" "this" {
  project_id = %q
  %s
}

output "branch_count" {
  value = length(data.neon_branches.this.branches)
}

output "null_parent_ids" {
  value = join(",", [for branch in data.neon_branches.this.branches : branch.id if branch.parent_id == null])
}

output "null_parent_lsn_ids" {
  value = join(",", [for branch in data.neon_branches.this.branches : branch.id if branch.parent_lsn == null])
}

output "null_size_ids" {
  value = join(",", [for branch in data.neon_branches.this.branches : branch.id if branch.logical_size == null])
}

output "null_primary_ids" {
  value = join(",", [for branch in data.neon_branches.this.branches : branch.id if branch.primary == null])
}
`, projectID, filterConfig)
}

type branchesPage struct {
	query  url.Values
	status int
	body   string
}

type branchesPaginationHTTPFixture struct {
	t        *testing.T
	pages    []branchesPage
	mu       sync.Mutex
	requests int
}

func (f *branchesPaginationHTTPFixture) Do(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	page := f.pages[f.requests%len(f.pages)]
	f.requests++
	query := page.query
	if query == nil {
		query = url.Values{}
	}
	if !reflect.DeepEqual(query, req.URL.Query()) {
		return nil, fmt.Errorf("unexpected branches query: got %v, want %v", req.URL.Query(), query)
	}
	return branchDataSourceHTTPFixture{
		t:      f.t,
		path:   "/api/v2/projects/cool-moon-42/branches",
		status: page.status,
		body:   page.body,
	}.Do(req)
}

type branchesHTTPFixture struct {
	branchDataSourceHTTPFixture
}

func (f branchesHTTPFixture) Do(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	query := url.Values{
		"sort_by":    {"name"},
		"sort_order": {"asc"},
	}
	if !reflect.DeepEqual(query, req.URL.Query()) {
		f.t.Errorf("unexpected branches query: got %v, want %v", req.URL.Query(), query)
	}
	return f.branchDataSourceHTTPFixture.Do(req)
}

func newBranchesTestClient(t *testing.T, projectID string, status int, body string) *neon.Client {
	t.Helper()
	client, err := neon.NewClient(neon.Config{
		Key: "test",
		HTTPClient: branchesHTTPFixture{
			branchDataSourceHTTPFixture: branchDataSourceHTTPFixture{
				t:      t,
				path:   "/api/v2/projects/" + projectID + "/branches",
				status: status,
				body:   body,
			},
		},
	})
	require.NoError(t, err)
	return client
}
