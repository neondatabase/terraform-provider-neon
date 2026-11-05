package provider

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

// Exercise both aliases against the API, not just the saved state. The final
// step unprotects the branch so Terraform can delete the isolated project.
func TestAccProjectProtectionAliases(t *testing.T) {
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
	orgID := os.Getenv("ORG_ID")
	projectName := newProjectName("protectionAliases")
	var projectID string

	t.Cleanup(func() {
		if projectID == "" {
			var org *string
			if orgID != "" {
				org = &orgID
			}
			projects, err := client.ListProjects(nil, nil, &projectName, org, nil, nil)
			if err != nil {
				t.Errorf("cleanup: find project %q: %v", projectName, err)
				return
			}
			for _, project := range projects.Projects {
				if project.Name == projectName {
					projectID = project.ID
				}
			}
		}
		if projectID == "" {
			return
		}
		// A failure after protection is enabled must not leave a protected
		// test project behind. Only inspect the exact project this test owns.
		branches, err := client.ListProjectBranches(projectID, nil, nil, nil, nil, nil, nil)
		if err != nil {
			if apiErr, ok := err.(neon.Error); ok && apiErr.HTTPCode == 404 {
				return
			}
			t.Errorf("cleanup: list branches in project %s: %v", projectID, err)
			return
		}
		for _, branch := range branches.Branches {
			if !branch.Protected {
				continue
			}
			unprotected := false
			_, err := client.UpdateProjectBranch(projectID, branch.ID, neon.BranchUpdateRequest{
				Branch: neon.BranchUpdateRequestBranch{Protected: &unprotected},
			})
			if err != nil {
				t.Errorf("cleanup: unprotect branch %s in project %s: %v", branch.ID, projectID, err)
				return
			}
		}
		if _, err := client.DeleteProject(projectID); err != nil {
			if apiErr, ok := err.(neon.Error); ok && apiErr.HTTPCode == 404 {
				return
			}
			t.Errorf("cleanup: delete project %s: %v", projectID, err)
		}
	})

	orgLine := ""
	if orgID != "" {
		orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
	}
	var steps []resource.TestStep
	for _, step := range []struct {
		settings string
		want     bool
	}{
		{"default_branch_protected = true", true},
		{"default_branch_protected = false", false},
		{"branch = { protected = true }", true},
		{"branch = { protected = false }", false},
		{"default_branch_protected = true\nbranch = { protected = true }", true},
		{"", false},
	} {
		want := step.want
		steps = append(steps, resource.TestStep{
			Config: projectProtectionLiveConfig(projectName, orgLine, step.settings),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("neon_project.this", "default_branch_protected", strconv.FormatBool(want)),
				resource.TestCheckResourceAttr("neon_project.this", "branch.protected", strconv.FormatBool(want)),
				func(state *terraform.State) error {
					r, ok := state.RootModule().Resources["neon_project.this"]
					if !ok || r.Primary == nil {
						return fmt.Errorf("neon_project.this missing from state")
					}
					if projectID != "" && projectID != r.Primary.ID {
						return fmt.Errorf("project was replaced: %s became %s", projectID, r.Primary.ID)
					}
					projectID = r.Primary.ID
					branchID := r.Primary.Attributes["branch.id"]
					branch, err := client.GetProjectBranch(projectID, branchID)
					if err != nil {
						return fmt.Errorf("read branch %s: %w", branchID, err)
					}
					if branch.Branch.Protected != want {
						return fmt.Errorf("API branch %s: protected=%t, want %t", branchID, branch.Branch.Protected, want)
					}
					return nil
				},
			),
		})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), Steps: steps})
}

func projectProtectionLiveConfig(projectName, orgLine, settings string) string {
	return fmt.Sprintf("resource \"neon_project\" \"this\" {\n  name = %q%s\n%s\n}\n", projectName, orgLine, settings)
}
