package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// newAIGatewayConfig returns the HCL for a TestStep: a Neon project plus a
// neon_ai_gateway data source. branchIDExpr is inserted raw into the HCL, so
// callers pass either a Terraform expression (e.g.
// `neon_project.this.branch.id`) for the happy path or a quoted
// UUID literal for the unavailable-scenario TestStep.
func newAIGatewayConfig(projectName, orgID, branchIDExpr string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

data "neon_ai_gateway" "this" {
  project_id = neon_project.this.id
  branch_id  = %s
}
`, projectName, orgID, branchIDExpr)
}

func TestAIGatewayDataSource(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	require.NoError(t, err)

	projectNamePrefix := "aiGatewayDataSource"

	projectName := newProjectName(projectNamePrefix)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Happy path: data source reads the branch's AI Gateway
				// base URL via the SDK; state is compared with a direct
				// GetProjectBranchAiGateway call to confirm the framework
				// data source actually received the SDK client.
				Config: newAIGatewayConfig(projectName, orgID, "neon_project.this.branch.id"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.neon_ai_gateway.this", "id"),
					resource.TestCheckResourceAttrSet("data.neon_ai_gateway.this", "base_url"),
					func(state *terraform.State) error {
						project := state.RootModule().Resources["neon_project.this"]
						gatewayState := state.RootModule().Resources["data.neon_ai_gateway.this"]
						gateway, err := client.GetProjectBranchAiGateway(project.Primary.ID, project.Primary.Attributes["branch.id"])
						if err != nil {
							return err
						}
						if gatewayState.Primary.Attributes["base_url"] != gateway.BaseURL {
							return fmt.Errorf("base_url mismatch: Terraform=%q API=%q", gatewayState.Primary.Attributes["base_url"], gateway.BaseURL)
						}
						return nil
					},
				),
			},
			{
				Config:      newAIGatewayConfig(projectName, orgID, `"00000000-0000-0000-0000-000000000000"`),
				ExpectError: regexp.MustCompile("(Neon resource not found|Neon rejected the request|AI Gateway unavailable on this branch)"),
			},
		},
	})
}
