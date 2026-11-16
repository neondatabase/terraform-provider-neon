package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccProjectKeepsLogicalReplicationEnabledOutsideTerraform covers Neon Realtime, the console, and the CLI turning on
// logical replication, which can't be turned off again, for a project whose configuration omits the attribute.
func TestAccProjectKeepsLogicalReplicationEnabledOutsideTerraform(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`
resource "neon_project" "this" {
  name = %q
}
`, newProjectName("keepLogicalReplication"))

	var projectID string
	sameProject := func(state *terraform.State) error {
		if got := state.RootModule().Resources["neon_project.this"].Primary.ID; got != projectID {
			return fmt.Errorf("project %s was replaced by %s", projectID, got)
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// GIVEN a project whose configuration omits enable_logical_replication
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "false"),
					func(state *terraform.State) error {
						projectID = state.RootModule().Resources["neon_project.this"].Primary.ID
						return nil
					},
				),
			},
			{
				// WHEN logical replication is enabled outside Terraform
				PreConfig: func() {
					enabled := true
					if _, err := client.UpdateProject(projectID, neon.ProjectUpdateRequest{Project: neon.ProjectUpdateRequestProject{
						Settings: &neon.ProjectSettingsData{EnableLogicalReplication: &enabled},
					}}); err != nil {
						t.Fatal(err)
					}
				},
				// THEN the plan keeps the project
				Config:   config,
				PlanOnly: true,
			},
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "true"),
					sameProject,
				),
			},
		},
	})
}
