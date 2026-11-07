package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

// TestMigrationFromKislerdm tests migration from kislerdm/neon to neondatabase/neon given a fixed stack.
func TestMigrationFromKislerdm(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	assert.NoError(t, err)

	t.Run("v0.18.0:neon_project", func(t *testing.T) {
		var (
			projectID  string
			endpointID string
			branchID   string
		)

		projectName := "migration0180Project"

		configFrom := fmt.Sprintf(`resource "neon_project" "_" {
  name = %q

  autoscaling_limit_min_cu = 0.25
  autoscaling_limit_max_cu = 1
  suspend_timeout_seconds  = 600

  primary_compute {
        autoscaling_limit_min_cu = 0.5
		autoscaling_limit_max_cu = 2
		suspend_timeout_seconds  = 1200
		name                     = "default"
  }

  branch {
	name          = "default"
	database_name = "baz"
    role_name     = "qux"
  }
  default_branch_protected = true

  maintenance_window {
    weekdays   = [6, 7]
    start_time = "07:00"
    end_time   = "08:00"
  }

  allowed_ips = ["0.0.0.0/0"]
}
`, projectName)

		var configTo = func(protected bool) string {
			return fmt.Sprintf(`resource "neon_project" "_" {
  name = %q

  database_name = "baz"
  database_user = "qux"

  autoscaling_limit_min_cu = 0.25
  autoscaling_limit_max_cu = 1
  suspend_timeout_seconds  = 600
  primary_compute = {
        autoscaling_limit_min_cu = 0.5
		autoscaling_limit_max_cu = 2
		suspend_timeout_seconds  = 1200
  }

  branch = {
	name      = "default"
    protected = %v
  }

  maintenance_window = {
    weekdays   = [6, 7]
    start_time = "07:00"
    end_time   = "08:00"
  }

  allowed_ips = ["0.0.0.0/0"]
}
`, projectName, protected)
		}

		resource.Test(
			t, resource.TestCase{
				Steps: []resource.TestStep{
					{
						ExternalProviders: map[string]resource.ExternalProvider{
							"neon": {
								Source:            "kislerdm/neon",
								VersionConstraint: "0.18.0",
							},
						},
						Config: configFrom,
					},
					{
						// read ids of provisioned resources to ensure that no resource were recreated as
						// the result of migration
						PreConfig: func() {
							pr, err := client.ListProjects(nil, nil, &projectName, nil, nil,
								nil)
							if err != nil {
								panic(err)
							}
							projectID = pr.Projects[0].ID

							e, err := client.ListProjectEndpoints(projectID)
							if err != nil {
								panic(err)
							}
							endpointID = e.Endpoints[0].ID

							branches, err := client.ListProjectBranches(projectID, nil, nil, nil,
								nil, nil, nil)
							if err != nil {
								panic(err)
							}
							branchID = branches.Branches[0].ID
						},
						ProtoV6ProviderFactories: newProviderFactories(),
						Config:                   configTo(true),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project._", "name", projectName),

							resource.TestCheckResourceAttr("neon_project._", "database_name", "baz"),
							resource.TestCheckResourceAttr("neon_project._", "database_user", "qux"),

							resource.TestCheckResourceAttr("neon_project._", "branch.name", "default"),
							resource.TestCheckResourceAttr("neon_project._", "branch.protected", "true"),

							resource.TestCheckResourceAttr("neon_project._", "autoscaling_limit_min_cu", "0.25"),
							resource.TestCheckResourceAttr("neon_project._", "autoscaling_limit_max_cu", "1"),
							resource.TestCheckResourceAttr("neon_project._", "suspend_timeout_seconds", "600"),

							resource.TestCheckResourceAttr("neon_project._", "primary_compute.autoscaling_limit_min_cu", "0.5"),
							resource.TestCheckResourceAttr("neon_project._", "primary_compute.autoscaling_limit_max_cu", "2"),
							resource.TestCheckResourceAttr("neon_project._", "primary_compute.suspend_timeout_seconds", "1200"),

							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.weekdays.#", "2"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.weekdays.0", "6"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.weekdays.1", "7"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.start_time", "07:00"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.end_time", "08:00"),

							resource.TestCheckResourceAttr("neon_project._", "allowed_ips.#", "1"),
							resource.TestCheckResourceAttr("neon_project._", "allowed_ips.0", "0.0.0.0/0"),

							func(state *terraform.State) error {
								p, ok := state.RootModule().Resources["neon_project._"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if p.Primary.ID != projectID {
									return fmt.Errorf("project was replaced")
								}

								if p.Primary.Attributes["branch.id"] != branchID {
									return fmt.Errorf("default branch was replaced")
								}

								if p.Primary.Attributes["primary_compute.id"] != endpointID {
									return fmt.Errorf("default endpoint was replaced")
								}

								return nil
							},
						),
					},
					{
						ProtoV6ProviderFactories: newProviderFactories(),
						Config:                   configTo(true),
						PlanOnly:                 true,
					},
					// unprotect the default branch to delete the resources in the end of the test
					{
						ProtoV6ProviderFactories: newProviderFactories(),
						Config:                   configTo(false),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project._", "name", projectName),

							resource.TestCheckResourceAttr("neon_project._", "database_name", "baz"),
							resource.TestCheckResourceAttr("neon_project._", "database_user", "qux"),

							resource.TestCheckResourceAttr("neon_project._", "branch.name", "default"),
							resource.TestCheckResourceAttr("neon_project._", "branch.protected", "false"),

							resource.TestCheckResourceAttr("neon_project._", "autoscaling_limit_min_cu", "0.25"),
							resource.TestCheckResourceAttr("neon_project._", "autoscaling_limit_max_cu", "1"),
							resource.TestCheckResourceAttr("neon_project._", "suspend_timeout_seconds", "600"),

							resource.TestCheckResourceAttr("neon_project._", "primary_compute.autoscaling_limit_min_cu", "0.5"),
							resource.TestCheckResourceAttr("neon_project._", "primary_compute.autoscaling_limit_max_cu", "2"),
							resource.TestCheckResourceAttr("neon_project._", "primary_compute.suspend_timeout_seconds", "1200"),

							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.weekdays.#", "2"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.weekdays.0", "6"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.weekdays.1", "7"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.start_time", "07:00"),
							resource.TestCheckResourceAttr("neon_project._", "maintenance_window.end_time", "08:00"),

							resource.TestCheckResourceAttr("neon_project._", "allowed_ips.#", "1"),
							resource.TestCheckResourceAttr("neon_project._", "allowed_ips.0", "0.0.0.0/0"),
						),
					},
				},
			},
		)
	})
}
