package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccEndpointCreateWithDefaults creates an endpoint without type or
// compute_provisioner, and checks the schema defaults reach state and the API.
func TestAccEndpointCreateWithDefaults(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	// Accounts that belong to an organization must set org_id on create.
	orgID := os.Getenv("ORG_ID")
	projectName := newProjectName("endpointDefaults")

	var projectID string
	t.Cleanup(func() {
		if projectID == "" {
			projectID = findEndpointDefaultsProjectID(client, projectName, orgID)
		}
		if projectID != "" {
			_, _ = client.DeleteProject(projectID)
		}
	})

	orgLine := ""
	if orgID != "" {
		orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			// GIVEN an endpoint with type and compute_provisioner unset
			Config: fmt.Sprintf(`
resource "neon_project" "this" {
  name = %q%s
}

resource "neon_branch" "this" {
  project_id = neon_project.this.id
  name       = "dev"
}

resource "neon_endpoint" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
}
`, projectName, orgLine),
			// THEN create succeeds and the defaults reach state and the API
			Check: resource.ComposeAggregateTestCheckFunc(
				func(state *terraform.State) error {
					projectID = state.RootModule().Resources["neon_project.this"].Primary.ID
					return nil
				},
				resource.TestCheckResourceAttr("neon_endpoint.this", "type", "read_write"),
				resource.TestCheckResourceAttr("neon_endpoint.this", "compute_provisioner", "k8s-neonvm"),
				func(state *terraform.State) error {
					ep := state.RootModule().Resources["neon_endpoint.this"].Primary
					resp, err := client.GetProjectEndpoint(ep.Attributes["project_id"], ep.ID)
					if err != nil {
						return fmt.Errorf("read endpoint %s: %w", ep.ID, err)
					}
					if got := resp.Endpoint.Type.String(); got != "read_write" {
						return fmt.Errorf("API endpoint type: want read_write, got %q", got)
					}
					if got := string(resp.Endpoint.Provisioner); got != "k8s-neonvm" {
						return fmt.Errorf("API endpoint provisioner: want k8s-neonvm, got %q", got)
					}
					return nil
				},
			),
		}},
	})
}

// findEndpointDefaultsProjectID matches the full project name, which contains
// a UUID, so it never selects a project this test did not create.
func findEndpointDefaultsProjectID(client *neon.Client, name, orgID string) string {
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
