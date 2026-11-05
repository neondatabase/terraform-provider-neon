package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// Create and reread one isolated project with an explicit empty library list.
// This does not change the org defaults or any existing project's settings.
func TestAccProjectEmptyLibraries(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	key := os.Getenv("NEON_API_KEY")
	if key == "" {
		t.Fatal("NEON_API_KEY must be set")
	}
	client, err := neon.NewClient(neon.Config{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	name := newProjectName("librariesEmpty")
	orgID := os.Getenv("ORG_ID")
	var projectID string

	t.Cleanup(func() {
		if projectID == "" {
			var org *string
			if orgID != "" {
				org = &orgID
			}
			projects, err := client.ListProjects(nil, nil, &name, org, nil, nil)
			if err != nil {
				t.Errorf("cleanup: find project %q: %v", name, err)
				return
			}
			for _, project := range projects.Projects {
				if project.Name == name {
					projectID = project.ID
				}
			}
		}
		if projectID == "" {
			return
		}
		if _, err := client.DeleteProject(projectID); err != nil {
			if apiErr, ok := err.(neon.Error); ok && apiErr.HTTPCode == 404 {
				return
			}
			t.Errorf("cleanup: delete project %s: %v", projectID, err)
		}
	})

	step := resource.TestStep{
		Config: emptyLibrariesLiveConfig(name, orgID),
		Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckOutput("libraries_json", "[]"),
			func(state *terraform.State) error {
				r, ok := state.RootModule().Resources["neon_project.this"]
				if !ok || r.Primary == nil {
					return fmt.Errorf("neon_project.this missing from state")
				}
				if projectID != "" && projectID != r.Primary.ID {
					return fmt.Errorf("project replaced: %s became %s", projectID, r.Primary.ID)
				}
				projectID = r.Primary.ID
				project, err := client.GetProject(projectID)
				if err != nil {
					return fmt.Errorf("read project %s: %w", projectID, err)
				}
				if settings := project.Project.Settings; settings != nil && settings.PreloadLibraries != nil {
					if len(settings.PreloadLibraries.EnabledLibraries) != 0 {
						return fmt.Errorf("API library list is not empty: %v", settings.PreloadLibraries.EnabledLibraries)
					}
				}
				return nil
			},
		),
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps:                    []resource.TestStep{step, step},
	})
}

func emptyLibrariesLiveConfig(name, orgID string) string {
	orgLine := ""
	if orgID != "" {
		orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
	}
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = %q%s
  enabled_libraries = []
}
output "libraries_json" {
  value = jsonencode(neon_project.this.enabled_libraries)
}
`, name, orgLine)
}
