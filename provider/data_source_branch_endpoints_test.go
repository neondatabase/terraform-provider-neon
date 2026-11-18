package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/require"
)

func TestBranchEndpointsDataSource(t *testing.T) {
	t.Parallel()

	const address = "data.neon_branch_endpoints.this"

	for _, tt := range []struct {
		name      string
		projectID string
		branchID  string
		status    int
		body      string
		check     resource.TestCheckFunc
		wantError string
	}{
		{
			name:      "existing project with missing branch",
			projectID: "cool-moon-42",
			branchID:  "br-missing-test-00000000",
			status:    http.StatusNotFound,
			body: `{
  "code": "NOT_FOUND",
  "message": "branch not found"
}`,
			wantError: "[HTTP Code: 404][Error Code: NOT_FOUND] branch not found",
		},
		{
			name:      "missing project",
			projectID: "missing-project",
			branchID:  "br-cool-moon-42",
			status:    http.StatusNotFound,
			body: `{
  "code": "NOT_FOUND",
  "message": "project not found"
}`,
			wantError: "[HTTP Code: 404][Error Code: NOT_FOUND] project not found",
		},
		{
			name:      "existing branch with one endpoint",
			projectID: "cool-moon-42",
			branchID:  "br-cool-moon-42",
			status:    http.StatusOK,
			body: `{
  "endpoints": [
    {
      "id": "ep-first-123",
      "project_id": "response-project",
      "branch_id": "response-branch",
      "host": "ep-first-123.us-east-2.aws.neon.tech",
      "type": "read_write",
      "region_id": "aws-us-east-2",
      "proxy_host": "us-east-2.aws.neon.tech",
      "name": "primary compute",
      "autoscaling_limit_min_cu": 0.5,
      "autoscaling_limit_max_cu": 4,
      "suspend_timeout_seconds": 300,
      "current_state": "active",
      "pooler_enabled": true,
      "pooler_mode": "transaction",
      "disabled": false,
      "passwordless_access": false,
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "settings": {},
      "provisioner": "k8s-pod"
    }
  ]
}`,
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					address, "endpoints.#", "1",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.id", "ep-first-123",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.host", "ep-first-123.us-east-2.aws.neon.tech",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.host_pooling", "ep-first-123-pooler.us-east-2.aws.neon.tech",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.type", "read_write",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.region_id", "aws-us-east-2",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.proxy_host", "us-east-2.aws.neon.tech",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.name", "primary compute",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.autoscaling_limit_min_cu", "0.5",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.autoscaling_limit_max_cu", "4",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.suspend_timeout_seconds", "300",
				),
			),
		},
		{
			name:      "existing branch with two endpoints",
			projectID: "cool-moon-42",
			branchID:  "br-cool-moon-42",
			status:    http.StatusOK,
			body: `{
  "endpoints": [
    {
      "id": "ep-first-123",
      "project_id": "cool-moon-42",
      "branch_id": "br-cool-moon-42",
      "host": "ep-first-123.us-east-2.aws.neon.tech",
      "type": "read_write",
      "region_id": "aws-us-east-2",
      "proxy_host": "us-east-2.aws.neon.tech",
      "autoscaling_limit_min_cu": 0.5,
      "autoscaling_limit_max_cu": 4,
      "suspend_timeout_seconds": 300,
      "current_state": "active",
      "pooler_enabled": true,
      "pooler_mode": "transaction",
      "disabled": false,
      "passwordless_access": false,
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "settings": {},
      "provisioner": "k8s-pod"
    },
    {
      "id": "ep-second-456",
      "project_id": "cool-moon-42",
      "branch_id": "br-cool-moon-42",
      "host": "ep-second-456.us-east-2.aws.neon.tech",
      "type": "read_only",
      "region_id": "aws-us-east-2",
      "proxy_host": "us-east-2.aws.neon.tech",
      "name": "",
      "autoscaling_limit_min_cu": 0.25,
      "autoscaling_limit_max_cu": 2,
      "suspend_timeout_seconds": -1,
      "current_state": "idle",
      "pooler_enabled": true,
      "pooler_mode": "transaction",
      "disabled": false,
      "passwordless_access": false,
      "creation_source": "console",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "settings": {},
      "provisioner": "k8s-pod"
    }
  ]
}`,
			check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(
					address, "endpoints.#", "2",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.id", "ep-first-123",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.0.name", "",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.id", "ep-second-456",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.host", "ep-second-456.us-east-2.aws.neon.tech",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.host_pooling", "ep-second-456-pooler.us-east-2.aws.neon.tech",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.type", "read_only",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.region_id", "aws-us-east-2",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.proxy_host", "us-east-2.aws.neon.tech",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.name", "",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.autoscaling_limit_min_cu", "0.25",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.autoscaling_limit_max_cu", "2",
				),
				resource.TestCheckResourceAttr(
					address, "endpoints.1.suspend_timeout_seconds", "-1",
				),
			),
		},
		{
			name:      "existing branch with no endpoints",
			projectID: "cool-moon-42",
			branchID:  "br-cool-moon-42",
			status:    http.StatusOK,
			body: `{
  "endpoints": []
}`,
			check: resource.TestCheckResourceAttr(
				address, "endpoints.#", "0",
			),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newBranchDataSourceTestClient(t,
				fmt.Sprintf("/api/v2/projects/%s/branches/%s/endpoints", tt.projectID, tt.branchID),
				tt.status,
				tt.body,
			)
			step := resource.TestStep{
				Config: branchEndpointsDataSourceConfig(tt.projectID, tt.branchID),
			}
			if tt.wantError != "" {
				step.ExpectError = regexp.MustCompile(regexp.QuoteMeta(tt.wantError))
			} else {
				step.Check = resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(
						address, "id", "cool-moon-42/br-cool-moon-42",
					),
					resource.TestCheckResourceAttr(
						address, "project_id", "cool-moon-42",
					),
					resource.TestCheckResourceAttr(
						address, "branch_id", "br-cool-moon-42",
					),
					tt.check,
				)
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
					sdk: client,
				}),
				Steps: []resource.TestStep{step},
			})
		})
	}
}

func branchEndpointsDataSourceConfig(projectID, branchID string) string {
	return fmt.Sprintf(`
data "neon_branch_endpoints" "this" {
  project_id = %q
  branch_id  = %q
}
`, projectID, branchID)
}

func TestBranchEndpointsLookupIDs(t *testing.T) {
	const unknown = "terraform_data.scope.output"
	minimum := strconv.Quote("-")
	maximum := strconv.Quote(strings.Repeat("a", 57) + "-42")
	for _, tc := range []struct {
		name       string
		project    string
		branch     string
		diagnostic string
	}{
		{
			name:       "malformed project with unknown branch",
			project:    `"Upper"`,
			branch:     unknown,
			diagnostic: "Invalid project_id",
		},
		{
			name:       "malformed branch with unknown project",
			project:    unknown,
			branch:     `"branch_id"`,
			diagnostic: "Invalid branch_id",
		},
		{
			name:       "empty project",
			project:    `""`,
			branch:     minimum,
			diagnostic: "Invalid project_id",
		},
		{
			name:       "overlong branch",
			project:    maximum,
			branch:     strconv.Quote(strings.Repeat("a", 61)),
			diagnostic: "Invalid branch_id",
		},
		{
			name:    "scope boundaries",
			project: minimum,
			branch:  maximum,
		},
		{
			name:       "required null project",
			project:    "null",
			branch:     maximum,
			diagnostic: "Missing Configuration for Required Attribute|Missing required argument",
		},
		{
			name:    "unknown scope",
			project: unknown,
			branch:  unknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := resource.TestStep{
				Config:             branchEndpointsLookupPlanConfig(tc.project, tc.branch),
				PlanOnly:           true,
				ExpectNonEmptyPlan: tc.diagnostic == "",
			}
			if tc.diagnostic != "" {
				step.ExpectError = regexp.MustCompile(tc.diagnostic)
			}
			if tc.project == unknown && tc.branch == unknown {
				const address = "data.neon_branch_endpoints.this"
				step.ConfigPlanChecks.PostApplyPreRefresh = []plancheck.PlanCheck{
					plancheck.ExpectUnknownValue(address, tfjsonpath.New("project_id")),
					plancheck.ExpectUnknownValue(address, tfjsonpath.New("branch_id")),
				}
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps:                    []resource.TestStep{step},
			})
		})
	}
}

func branchEndpointsLookupPlanConfig(project, branch string) string {
	return fmt.Sprintf(`
provider "neon" { api_key = "unused" }
resource "terraform_data" "scope" { input = "example" }

data "neon_branch_endpoints" "this" {
  project_id = %s
  branch_id  = %s
  depends_on = [terraform_data.scope]
}
`, project, branch)
}

func TestBranchEndpointsScopeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/branch-endpoints-scope.json")
	require.NoError(t, err)
	var contract struct {
		Patterns map[string]string `json:"patterns"`
	}
	require.NoError(t, json.Unmarshal(data, &contract))
	require.Equal(t, map[string]string{
		"project_id": endpointListScopePattern.String(),
		"branch_id":  endpointListScopePattern.String(),
	}, contract.Patterns)
}
