//go:build subscription

package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

func TestAccSubscriptionSuspendTimeout(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run live subscription tests")
	}
	orgID := os.Getenv("ORG_ID")
	plan := os.Getenv("NEON_TEST_PLAN")
	if orgID == "" || os.Getenv("NEON_API_KEY") == "" {
		t.Fatal("ORG_ID and NEON_API_KEY are required")
	}
	switch plan {
	case "free", "launch", "scale":
	default:
		t.Fatal("set NEON_TEST_PLAN to free, launch, or scale")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}
	org, err := client.GetOrganization(orgID)
	if err != nil {
		t.Fatal(err)
	}
	if org.Plan != plan {
		t.Fatalf("expected a %s organization, got %s", plan, org.Plan)
	}

	name := newProjectName("tierTimeout")
	config := subscriptionTimeoutConfig(orgID, name, "")
	var projectID, endpointID string
	var initialTimeout neon.SuspendTimeoutSeconds
	initial := resource.TestStep{
		Config: config,
		Check: func(s *terraform.State) error {
			attrs := s.RootModule().Resources["neon_project.this"].Primary.Attributes
			projectID, endpointID = attrs["id"], attrs["primary_compute.id"]
			result, err := client.GetProjectEndpoint(projectID, endpointID)
			if err != nil {
				return err
			}
			initialTimeout = result.Endpoint.SuspendTimeoutSeconds
			if initialTimeout == -1 {
				return fmt.Errorf("new project already has scale-to-zero disabled")
			}
			return nil
		},
	}
	checkTimeout := func(want neon.SuspendTimeoutSeconds) error {
		result, err := client.GetProjectEndpoint(projectID, endpointID)
		if err != nil {
			return err
		}
		if got := result.Endpoint.SuspendTimeoutSeconds; got != want {
			return fmt.Errorf("endpoint suspend_timeout_seconds = %d, want %d", got, want)
		}
		return nil
	}

	update := resource.TestStep{
		Config: subscriptionTimeoutConfig(orgID, name, "suspend_timeout_seconds = -1"),
	}
	steps := []resource.TestStep{initial}
	if plan == "free" {
		update.ExpectError = regexp.MustCompile(`(?s)\[HTTP Code: 412\].*modifying the suspend interval is not\s+permitted on this account`)
		steps = append(steps, update, resource.TestStep{
			PreConfig: func() {
				if err := checkTimeout(initialTimeout); err != nil {
					t.Fatal(err)
				}
			},
			Config:   config,
			PlanOnly: true,
		})
	} else {
		update.Check = resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("neon_project.this", "primary_compute.suspend_timeout_seconds", "-1"),
			func(*terraform.State) error { return checkTimeout(-1) },
		)
		steps = append(steps, update)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps:                    steps,
	})
}

func subscriptionTimeoutConfig(orgID, name, timeoutSetting string) string {
	return fmt.Sprintf(`
resource "neon_project" "this" {
  name            = %q
  org_id          = %q
  region_id       = "aws-us-east-2"
  primary_compute = {
    %s
  }
}
`, name, orgID, timeoutSetting)
}
