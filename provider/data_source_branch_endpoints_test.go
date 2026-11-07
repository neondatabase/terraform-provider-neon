package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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
