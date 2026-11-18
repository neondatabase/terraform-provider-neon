package provider

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/echoprovider"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	neon "github.com/kislerdm/neon-sdk-go"
)

func TestAccBranchRealtime(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}

	const address = "neon_branch_realtime.this"
	projectName := newProjectName("branchRealtime")
	project := fmt.Sprintf(`
resource "neon_project" "this" {
  name      = %q
  region_id = "aws-us-east-2"

  enable_logical_replication = true
}
`, projectName)
	withRealtime := func(origins string) string {
		return project + fmt.Sprintf(`
resource "neon_branch_realtime" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.default_branch_id
  %s
}
`, origins)
	}
	var projectID, branchID string
	remoteRealtime := func(state *terraform.State) (neon.Realtime, error) {
		attrs := state.RootModule().Resources["neon_project.this"].Primary.Attributes
		projectID, branchID = attrs["id"], attrs["default_branch_id"]
		return client.GetProjectBranchRealtime(projectID, branchID)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// GIVEN a project, WHEN Realtime is enabled with one allowed origin
				Config: withRealtime(`allowed_origins = ["https://app.example.com"]`),
				// THEN Neon reports it enabled with that origin
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "allowed_origins.#", "1"),
					resource.TestCheckResourceAttr(address, "allowed_origins.0", "https://app.example.com"),
					resource.TestMatchResourceAttr(address, "invocation_url", regexp.MustCompile(`^https://`)),
					func(state *terraform.State) error {
						remote, err := remoteRealtime(state)
						if err != nil {
							return err
						}
						if !remote.Enabled || len(remote.AllowedOrigins) != 1 {
							return fmt.Errorf("unexpected Realtime state in Neon: %+v", remote)
						}
						return nil
					},
				),
			},
			{
				// WHEN it is imported THEN the state matches
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// WHEN the origins are an empty list THEN any origin may connect
				Config: withRealtime(`allowed_origins = []`),
				Check:  resource.TestCheckResourceAttr(address, "allowed_origins.#", "0"),
			},
			{
				// WHEN the origins are ["*"], which Neon stores as an empty list, THEN the plan stays empty after apply
				Config: withRealtime(`allowed_origins = ["*"]`),
				Check:  resource.TestCheckResourceAttr(address, "allowed_origins.0", "*"),
			},
			{
				// WHEN the origins are omitted THEN the branch keeps its current origins
				Config:   withRealtime(""),
				PlanOnly: true,
			},
			{
				// WHEN Realtime is disabled outside Terraform THEN the plan enables it again
				PreConfig: func() {
					if err := client.DisableProjectBranchRealtime(projectID, branchID); err != nil {
						t.Fatal(err)
					}
				},
				Config:             withRealtime(`allowed_origins = ["*"]`),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: withRealtime(`allowed_origins = ["*"]`),
			},
			{
				// WHEN the resource is removed THEN Realtime is disabled on the branch
				Config: project,
				Check: func(state *terraform.State) error {
					remote, err := remoteRealtime(state)
					if err != nil {
						return err
					}
					if remote.Enabled || remote.Pending {
						return fmt.Errorf("realtime still enabled or pending in Neon: %+v", remote)
					}
					return nil
				},
			},
		},
	})
}

func TestAccEphemeralBranchRealtimeSecret(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}
	factories := newProviderFactories()
	factories["echo"] = echoprovider.NewProviderServer()
	var projectID, branchID, previousSecret string

	projectName := newProjectName("realtimeSecret")
	project := fmt.Sprintf(`
resource "neon_project" "this" {
  name      = %q
  region_id = "aws-us-east-2"

  enable_logical_replication = true
}
`, projectName)
	realtime := `
resource "neon_branch_realtime" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.default_branch_id
}
`
	secret := `
ephemeral "neon_branch_realtime_secret" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.default_branch_id
  lifecycle {
    postcondition {
      condition     = startswith(self.secret, "nrt_live_")
      error_message = "unexpected Realtime secret format"
    }
  }
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_10_0)},
		Steps: []resource.TestStep{
			{
				// GIVEN a branch without Realtime, WHEN the secret is read THEN the error says Realtime is off
				Config:      project + secret,
				ExpectError: regexp.MustCompile(`Neon Realtime not enabled`),
			},
			{
				// WHEN Realtime is enabled
				Config: project + realtime,
				Check: func(state *terraform.State) error {
					attrs := state.RootModule().Resources["neon_branch_realtime.this"].Primary.Attributes
					projectID, branchID = attrs["project_id"], attrs["branch_id"]
					return nil
				},
			},
			{
				// THEN the secret can be read
				Config: project + realtime + secret,
			},
			{
				// WHEN the secret is rotated and read before the rotation settles
				PreConfig: func() {
					current, err := client.GetProjectBranchRealtimeSecret(projectID, branchID)
					if err != nil {
						t.Fatal(err)
					}
					previousSecret = current.Secret
					if err := client.RotateProjectBranchRealtimeSecret(projectID, branchID); err != nil {
						t.Fatal(err)
					}
				},
				Config: project + realtime + secret + `
provider "echo" {
  data = ephemeral.neon_branch_realtime_secret.this.secret
}

resource "echo" "secret" {}
`,
				// THEN the ephemeral resource returns the new secret
				Check: resource.TestCheckResourceAttrWith("echo.secret", "data", func(got string) error {
					settled, err := client.GetProjectBranchRealtimeSecret(projectID, branchID)
					if err != nil {
						return err
					}
					if settled.Pending || got != settled.Secret || got == previousSecret {
						return errors.New("the ephemeral resource did not return the rotated secret")
					}
					return nil
				}),
			},
			{
				// The final destroy can disable Realtime before it configures the echo provider, which reads the secret.
				Config: project + realtime,
			},
		},
	})
}
