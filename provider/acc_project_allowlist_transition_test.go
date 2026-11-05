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

// Switching between omitted and empty allowlists must not lose the configured
// list shape. Only an isolated project's already empty allowlist is exercised;
// no real connection restriction or nonempty allowlist is configured here.
func TestAccProjectAllowlistEmptyTransition(t *testing.T) {
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
	projectName := newProjectName("allowlistEmpty")
	var projectID string

	t.Cleanup(func() {
		if projectID == "" {
			var org *string
			if orgID != "" {
				org = &orgID
			}
			projects, err := client.ListProjects(nil, nil, &projectName, org, nil, nil)
			if err != nil {
				t.Errorf("cleanup: find test project %q: %v", projectName, err)
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
		if _, err := client.DeleteProject(projectID); err != nil {
			if apiErr, ok := err.(neon.Error); ok && apiErr.HTTPCode == 404 {
				return
			}
			t.Errorf("cleanup: delete test project %s: %v", projectID, err)
		}
	})

	var steps []resource.TestStep
	for _, explicitEmpty := range []bool{false, true, false, true} {
		explicit := explicitEmpty
		steps = append(steps, resource.TestStep{
			Config: allowlistEmptyLiveConfig(projectName, orgID, explicit),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("allowlist_is_null", strconv.FormatBool(!explicit)),
				func(state *terraform.State) error {
					r, ok := state.RootModule().Resources["neon_project.this"]
					if !ok || r.Primary == nil {
						return fmt.Errorf("neon_project.this missing from state")
					}
					if projectID != "" && r.Primary.ID != projectID {
						return fmt.Errorf("project replaced: %s became %s", projectID, r.Primary.ID)
					}
					projectID = r.Primary.ID
					project, err := client.GetProject(projectID)
					if err != nil {
						return fmt.Errorf("read test project %s: %w", projectID, err)
					}
					if settings := project.Project.Settings; settings != nil && settings.AllowedIps != nil {
						if len(settings.AllowedIps.Ips) != 0 {
							return fmt.Errorf("API allowlist is not empty: %v", settings.AllowedIps.Ips)
						}
					}
					return nil
				},
			),
		})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), Steps: steps})
}

func allowlistEmptyLiveConfig(name, orgID string, explicitEmpty bool) string {
	orgLine := ""
	if orgID != "" {
		orgLine = fmt.Sprintf("\n  org_id = %q", orgID)
	}
	allowlistLine := ""
	if explicitEmpty {
		allowlistLine = "\n  allowed_ips = []"
	}
	return fmt.Sprintf(`resource "neon_project" "this" {
  name = %q%s%s
}
output "allowlist_is_null" {
  value = neon_project.this.allowed_ips == null
}
`, name, orgLine, allowlistLine)
}
