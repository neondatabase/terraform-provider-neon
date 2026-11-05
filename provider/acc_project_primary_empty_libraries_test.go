package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccProjectPrimaryEmptyLibraries checks initial empty intent and the
// unset-to-empty transition on separate disposable projects.
func TestAccProjectPrimaryEmptyLibraries(t *testing.T) {
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
	for _, createEmpty := range []bool{false, true} {
		t.Run(fmt.Sprintf("create_empty=%t", createEmpty), func(t *testing.T) {
			runProjectPrimaryEmptyLibrariesLive(t, client, os.Getenv("ORG_ID"), createEmpty)
		})
	}
}

func runProjectPrimaryEmptyLibrariesLive(t *testing.T, client *neon.Client, orgID string, createEmpty bool) {
	t.Helper()
	name := newProjectName("primaryLibrariesEmpty")
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
		t.Logf("cleanup isolated project: %s (%s)", projectID, name)
		if _, err := client.DeleteProject(projectID); err != nil {
			if apiErr, ok := err.(neon.Error); ok && apiErr.HTTPCode == 404 {
				return
			}
			t.Errorf("cleanup: delete project %s: %v", projectID, err)
		}
	})

	ids := map[string]string{}
	checkAPI := func(state *terraform.State) error {
		r, ok := state.RootModule().Resources["neon_project.this"]
		if !ok || r.Primary == nil {
			return fmt.Errorf("neon_project.this missing from state")
		}
		current := map[string]string{
			"project": r.Primary.ID,
			"branch":  r.Primary.Attributes["branch.id"],
			"compute": r.Primary.Attributes["primary_compute.id"],
		}
		for name, id := range current {
			if id == "" {
				return fmt.Errorf("%s ID missing from state", name)
			}
			if old, seen := ids[name]; seen && old != id {
				return fmt.Errorf("%s replaced: %s became %s", name, old, id)
			}
			ids[name] = id
		}
		if projectID == "" {
			t.Logf("isolated project: %s (%s)", current["project"], name)
		}
		projectID = current["project"]
		endpoint, err := client.GetProjectEndpoint(projectID, ids["compute"])
		if err != nil {
			return fmt.Errorf("read primary endpoint %s: %w", ids["compute"], err)
		}
		if endpoint.Endpoint.BranchID != ids["branch"] {
			return fmt.Errorf("primary endpoint moved: want branch %s, got %s", ids["branch"], endpoint.Endpoint.BranchID)
		}
		if preload := endpoint.Endpoint.Settings.PreloadLibraries; preload != nil && len(preload.EnabledLibraries) != 0 {
			return fmt.Errorf("primary API libraries must be empty, got %v", preload.EnabledLibraries)
		}
		return nil
	}
	var steps []resource.TestStep
	if !createEmpty {
		steps = append(steps, resource.TestStep{
			Config: projectPrimaryEmptyLibrariesLiveConfig(name, orgID, false),
			Check: resource.ComposeAggregateTestCheckFunc(
				checkAPI,
				resource.TestCheckOutput("primary_libraries_json", "null"),
			),
		})
	}
	empty := resource.TestStep{
		Config: projectPrimaryEmptyLibrariesLiveConfig(name, orgID, true),
		Check: resource.ComposeAggregateTestCheckFunc(
			checkAPI,
			resource.TestCheckOutput("primary_libraries_json", "[]"),
		),
	}
	steps = append(steps, empty, empty)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps:                    steps,
	})
}

func projectPrimaryEmptyLibrariesLiveConfig(name, orgID string, empty bool) string {
	settings := ""
	if orgID != "" {
		settings += fmt.Sprintf("\n  org_id = %q", orgID)
	}
	if empty {
		settings += "\n  primary_compute = { enabled_libraries = [] }"
	}
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = %q%s
}
output "primary_libraries_json" {
  value = jsonencode(neon_project.this.primary_compute.enabled_libraries)
}
`, name, settings)
}
