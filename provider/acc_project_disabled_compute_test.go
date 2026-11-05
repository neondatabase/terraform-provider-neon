package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccProjectDisabledPrimaryCompute disables the primary compute through
// neon_project and checks the provider keeps tracking it.
func TestAccProjectDisabledPrimaryCompute(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	// Accounts that belong to an organization must set org_id on create.
	orgID := os.Getenv("ORG_ID")
	projectName := newProjectName("disabledCompute")

	var projectID string
	t.Cleanup(func() {
		if projectID == "" {
			projectID = findDisabledComputeProjectID(client, projectName, orgID)
		}
		if projectID != "" {
			_, _ = client.DeleteProject(projectID)
		}
	})

	config := func(primaryCompute string) string {
		orgLine := ""
		if orgID != "" {
			orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
		}
		return fmt.Sprintf(`
resource "neon_project" "this" {
  name = %q%s
%s
}
`, projectName, orgLine, primaryCompute)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// GIVEN a project
				Config: config(""),
				Check: func(state *terraform.State) error {
					projectID = state.RootModule().Resources["neon_project.this"].Primary.ID
					return nil
				},
			},
			{
				// WHEN its primary compute is disabled
				Config: config("  primary_compute = {\n    disabled = true\n  }"),
				// THEN the provider keeps tracking it as disabled
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neon_project.this", "primary_compute.disabled", "true"),
					func(state *terraform.State) error {
						attrs := state.RootModule().Resources["neon_project.this"].Primary.Attributes
						resp, err := client.GetProjectEndpoint(attrs["id"], attrs["primary_compute.id"])
						if err != nil {
							return fmt.Errorf("read endpoint %s: %w", attrs["primary_compute.id"], err)
						}
						if !resp.Endpoint.Disabled {
							return fmt.Errorf("API endpoint %s is not disabled", resp.Endpoint.ID)
						}
						return nil
					},
				),
			},
		},
	})
}

// findDisabledComputeProjectID matches the full project name, which contains
// a UUID, so it never selects a project this test did not create.
func findDisabledComputeProjectID(client *neon.Client, name, orgID string) string {
	var org *string
	if orgID != "" {
		org = &orgID
	}
	resp, err := client.ListProjects(nil, nil, &name, org, nil, nil)
	if err != nil {
		return ""
	}
	for _, project := range resp.Projects {
		if project.Name == name {
			return project.ID
		}
	}
	return ""
}
