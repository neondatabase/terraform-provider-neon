package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccEphemeralConnectionURI(t *testing.T) {
	skipBelowTerraform(t, "1.10.0")
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	projectNamePrefix := "connectionURI"

	projectName := newProjectName(projectNamePrefix)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			// branch and endpoint specified
			{
				Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_connection_uri" "this" {
	project_id  = neon_project.this.id
	database    = neon_project.this.database_name
	role        = neon_project.this.database_user
	branch_id   = neon_project.this.branch.id
	endpoint_id = neon_project.this.primary_compute.id
    lifecycle {
		postcondition {
			condition     = self.uri != "" && replace(self.uri_pooler, "-pooler", "") == self.uri
			error_message = "URI is empty, or does not match URI pooler"
		}
	}
}
`, projectName),
			},
			// branch and endpoint not specified
			{
				Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_connection_uri" "this" {
	project_id  = neon_project.this.id
	database    = neon_project.this.database_name
	role        = neon_project.this.database_user
    lifecycle {
		postcondition {
			condition     = self.uri != "" && replace(self.uri_pooler, "-pooler", "") == self.uri
			error_message = "URI is empty, or does not match URI pooler"
		}
	}
}
`, projectName),
			},
			// branch specified, endpoint not specified
			{
				Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_connection_uri" "this" {
	project_id  = neon_project.this.id
	database    = neon_project.this.database_name
	role        = neon_project.this.database_user
	branch_id = neon_project.this.branch.id
    lifecycle {
		postcondition {
			condition     = self.uri != "" && replace(self.uri_pooler, "-pooler", "") == self.uri
			error_message = "URI is empty, or does not match URI pooler"
		}
	}
}
`, projectName),
			},
			// endpoint specified, branch not specified
			{
				Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_connection_uri" "this" {
	project_id  = neon_project.this.id
	database    = neon_project.this.database_name
	role        = neon_project.this.database_user
	endpoint_id = neon_project.this.primary_compute.id
    lifecycle {
		postcondition {
			condition     = self.uri != "" && replace(self.uri_pooler, "-pooler", "") == self.uri
			error_message = "URI is empty, or does not match URI pooler"
		}
	}
}
`, projectName),
			},
		},
	})
}
