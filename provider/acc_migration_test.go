package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

// TestMigrationFromKislerdm test migration from kislerdm/neon to neondatabase/neon given a fixed stack
func TestMigrationFromKislerdm(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	t.Run("from v0.18.0", func(t *testing.T) {
		projectName := "migration0180"

		configFrom := fmt.Sprintf(`resource "neon_project" "_" {
  name = %q

  autoscaling_limit_min_cu = 0.25
  autoscaling_limit_max_cu = 1
  suspend_timeout_seconds  = 600

  primary_compute {
        autoscaling_limit_min_cu = 0.5
		autoscaling_limit_max_cu = 2
		suspend_timeout_seconds  = 1200
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

resource "neon_endpoint" "_" {
 project_id = neon_project._.id
 branch_id  = neon_project._.default_branch_id
 name       = "test"
 type       = "read_only"
}

resource "neon_branch" "_" {
 project_id   = neon_project._.id
 parent_id    = neon_project._.default_branch_id
 name         = "test"
 protected    = "yes"
}
`, projectName)

		configTo := fmt.Sprintf(`resource "neon_project" "_" {
  name = %q

  database_name = "default"
  database_user = "default"

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
    protected = true
  }

  allowed_ips = ["0.0.0.0/0"]
}

resource "neon_endpoint" "_" {
 project_id = neon_project._.id
 branch_id  = neon_project._.branch.id
 name       = "test"
 type       = "read_only"
}

resource "neon_branch" "_" {
 project_id   = neon_project._.id
 parent_id    = neon_project._.branch.id
 name         = "test"
 protected    = true
}
`, projectName)

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
						ProtoV6ProviderFactories: newProviderFactories(),
						Config:                   configTo,
					},
					{
						ProtoV6ProviderFactories: newProviderFactories(),
						Config: fmt.Sprintf(`resource "neon_project" "_" {
  name = %q

  database_name = "default"
  database_user = "default"

  autoscaling_limit_min_cu = 0.25
  autoscaling_limit_max_cu = 1
  suspend_timeout_seconds  = 600
  primary_compute = {
        autoscaling_limit_min_cu = 0.5
		autoscaling_limit_max_cu = 2
		suspend_timeout_seconds  = 1200
  }

  branch = {
	name = "default"
  }

  allowed_ips = ["0.0.0.0/0"]
}

resource "neon_endpoint" "_" {
 project_id = neon_project._.id
 branch_id  = neon_project._.branch.id
 name       = "test"
 type       = "read_only"
}

resource "neon_branch" "_" {
 project_id   = neon_project._.id
 parent_id    = neon_project._.branch.id
 name         = "test"
}
`, projectName),
					},
				},
			},
		)
	})
}
