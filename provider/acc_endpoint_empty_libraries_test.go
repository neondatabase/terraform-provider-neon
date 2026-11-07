package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccEndpointEmptyLibraries checks unset -> [] on a new endpoint, then
// repeats [] to exercise refresh without changing existing project settings.
func TestAccEndpointEmptyLibraries(t *testing.T) {
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
	name := newProjectName("endpointLibrariesEmpty")
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

	ids := map[string]string{}
	checkAPI := func(state *terraform.State) error {
		for _, address := range []string{"neon_project.this", "neon_branch.this", "neon_endpoint.this"} {
			r, ok := state.RootModule().Resources[address]
			if !ok || r.Primary == nil {
				return fmt.Errorf("%s missing from state", address)
			}
			if id, seen := ids[address]; seen && id != r.Primary.ID {
				return fmt.Errorf("%s replaced: %s became %s", address, id, r.Primary.ID)
			}
			ids[address] = r.Primary.ID
			if address == "neon_project.this" {
				projectID = r.Primary.ID
			}
		}
		endpoint, err := client.GetProjectEndpoint(projectID, ids["neon_endpoint.this"])
		if err != nil {
			return fmt.Errorf("read endpoint %s: %w", ids["neon_endpoint.this"], err)
		}
		if preload := endpoint.Endpoint.Settings.PreloadLibraries; preload != nil && len(preload.EnabledLibraries) != 0 {
			return fmt.Errorf("API libraries must be empty, got %v", preload.EnabledLibraries)
		}
		return nil
	}
	var steps []resource.TestStep
	for i, libraries := range []string{"", "enabled_libraries = []", "enabled_libraries = []"} {
		want := "[]"
		if i == 0 {
			// A known initial null proves step 2 exercises the failing transition.
			want = "null"
		}
		steps = append(steps, resource.TestStep{
			Config: endpointLibrariesLiveConfig(name, orgID, libraries),
			Check: resource.ComposeAggregateTestCheckFunc(
				checkAPI,
				resource.TestCheckOutput("endpoint_libraries_json", want),
			),
		})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps:                    steps,
	})
}

func endpointLibrariesLiveConfig(name, orgID, libraries string) string {
	orgLine := ""
	if orgID != "" {
		orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
	}
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = %q%s
}
resource "neon_branch" "this" {
  project_id = neon_project.this.id
  name = "endpoint-libraries"
}
resource "neon_endpoint" "this" {
  project_id = neon_project.this.id
  branch_id = neon_branch.this.id
  type = "read_write"
  compute_provisioner = "k8s-neonvm"
  %s
}
output "endpoint_libraries_json" {
  value = jsonencode(neon_endpoint.this.enabled_libraries)
}
`, name, orgLine, libraries)
}
