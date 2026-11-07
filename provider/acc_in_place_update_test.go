package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccInPlaceUpdateKeepsDependents renames a project, then a branch, and
// checks that nothing under them was recreated.
// A replaced role or database keeps the same composite ID, so the test
// compares created_at from the API as well as the IDs.
func TestAccInPlaceUpdateKeepsDependents(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}
	// Accounts that belong to an organization must set org_id on create.
	orgID := os.Getenv("ORG_ID")

	projectName := newProjectName("inPlace")
	renamedProject := newProjectName("inPlaceRenamed")

	var projectID string
	t.Cleanup(func() {
		if projectID == "" {
			projectID = findInPlaceProjectID(client, projectName, orgID)
		}
		if projectID == "" {
			projectID = findInPlaceProjectID(client, renamedProject, orgID)
		}
		if projectID != "" {
			_, _ = client.DeleteProject(projectID)
		}
	})

	var created map[string]string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// GIVEN a project with a branch, endpoint, role and database
				Config: newInPlaceUpdateConfig(projectName, "dev", orgID),
				Check: func(state *terraform.State) error {
					var err error
					if projectID, err = inPlaceAttr(state, "neon_project.this", "id"); err != nil {
						return err
					}
					created, err = readCreatedResources(client, state)
					return err
				},
			},
			{
				// WHEN the project is renamed
				// THEN nothing under it is recreated
				Config: newInPlaceUpdateConfig(renamedProject, "dev", orgID),
				Check: func(state *terraform.State) error {
					return checkNothingRecreated(client, state, created)
				},
			},
			{
				// WHEN the branch is renamed
				// THEN nothing on it is recreated
				Config: newInPlaceUpdateConfig(renamedProject, "dev-renamed", orgID),
				Check: func(state *terraform.State) error {
					return checkNothingRecreated(client, state, created)
				},
			},
		},
	})
}

func newInPlaceUpdateConfig(projectName, branchName, orgID string) string {
	orgLine := ""
	if orgID != "" {
		orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
	}
	return fmt.Sprintf(`
resource "neon_project" "this" {
  name = %q%s
}

resource "neon_branch" "this" {
  project_id = neon_project.this.id
  name       = %q
}

resource "neon_endpoint" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
  # Set explicitly: Create reads the config, so the schema default is lost.
  type = "read_write"
}

resource "neon_role" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
  name       = "in_place_role"

  # Neon creates roles through the branch's read-write endpoint.
  depends_on = [neon_endpoint.this]
}

resource "neon_database" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
  name       = "in_place_db"
  owner_name = neon_role.this.name
}
`, projectName, orgLine, branchName)
}

// readCreatedResources returns "<id> created <created_at>" for each resource
// under the project, read from the API.
func readCreatedResources(client *neon.Client, state *terraform.State) (map[string]string, error) {
	attr := func(address, name string) (string, error) { return inPlaceAttr(state, address, name) }

	projectID, err := attr("neon_project.this", "id")
	if err != nil {
		return nil, err
	}
	branchID, err := attr("neon_branch.this", "id")
	if err != nil {
		return nil, err
	}
	endpointID, err := attr("neon_endpoint.this", "id")
	if err != nil {
		return nil, err
	}
	roleName, err := attr("neon_role.this", "name")
	if err != nil {
		return nil, err
	}
	databaseName, err := attr("neon_database.this", "name")
	if err != nil {
		return nil, err
	}

	branch, err := client.GetProjectBranch(projectID, branchID)
	if err != nil {
		return nil, fmt.Errorf("read branch %s: %w", branchID, err)
	}
	endpoint, err := client.GetProjectEndpoint(projectID, endpointID)
	if err != nil {
		return nil, fmt.Errorf("read endpoint %s: %w", endpointID, err)
	}
	role, err := client.GetProjectBranchRole(projectID, branchID, roleName)
	if err != nil {
		return nil, fmt.Errorf("read role %s: %w", roleName, err)
	}
	database, err := client.GetProjectBranchDatabase(projectID, branchID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("read database %s: %w", databaseName, err)
	}

	format := func(id string, createdAt fmt.Stringer) string {
		return fmt.Sprintf("%s created %s", id, createdAt)
	}
	return map[string]string{
		"neon_branch.this":   format(branch.Branch.ID, branch.Branch.CreatedAt),
		"neon_endpoint.this": format(endpoint.Endpoint.ID, endpoint.Endpoint.CreatedAt),
		"neon_role.this":     format(branchID+"/"+role.Role.Name, role.Role.CreatedAt),
		"neon_database.this": format(branchID+"/"+database.Database.Name, database.Database.CreatedAt),
	}, nil
}

func checkNothingRecreated(client *neon.Client, state *terraform.State, before map[string]string) error {
	after, err := readCreatedResources(client, state)
	if err != nil {
		return err
	}
	for address, was := range before {
		if now := after[address]; now != was {
			return fmt.Errorf("%s was recreated: %s, now %s", address, was, now)
		}
	}
	return nil
}

// findInPlaceProjectID matches the full project name, which contains a UUID,
// so it never selects a project this test did not create.
func findInPlaceProjectID(client *neon.Client, name, orgID string) string {
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

func inPlaceAttr(state *terraform.State, address, attr string) (string, error) {
	r, ok := state.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s not found in state", address)
	}
	v := r.Primary.Attributes[attr]
	if v == "" {
		return "", fmt.Errorf("%s.%s is empty in state", address, attr)
	}
	return v, nil
}
