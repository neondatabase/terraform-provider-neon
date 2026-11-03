package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
)

func TestAccEphemeralRole(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "ephemeralRole"
	t.Cleanup(func() {
		resp, _ := client.ListProjects(nil, nil, &projectNamePrefix, nil, nil, nil)
		for _, project := range resp.Projects {
			_, _ = client.DeleteProject(project.ID)
		}
	})

	t.Run("returns a password for an existing role", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

resource "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	name       = "foo"
}

ephemeral "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	name       = neon_role.this.name
    lifecycle {
		postcondition {
			condition     = self.password != ""
			error_message = "Password is empty"
		}
	}
}
`, projectName),
				},
			},
		})
	})

	t.Run("returns the error when the role does not exist", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	name       = "missing"
}
`, projectName),
					ExpectError: regexp.MustCompile("role not found"),
				},
			},
		})
	})

	t.Run("returns the error when the branch does not exist", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = "br-foo"
	name       = neon_project.this.database_user
}
`, projectName),
					ExpectError: regexp.MustCompile("branch not found"),
				},
			},
		})
	})

	t.Run("returns the error when the project does not exist", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`
resource "neon_project" "this" {
	name = %q
}

ephemeral "neon_role" "this" {
	project_id = "pr-foo"
	branch_id  = "br-foo"
	name       = neon_project.this.database_user
}
`, projectName),
					ExpectError: regexp.MustCompile("project not found"),
				},
			},
		})
	})
}
