package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestBranchRealtimeRejectsOriginWithPath(t *testing.T) {
	t.Parallel()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config: `
provider "neon" {
  api_key = "unused"
}

resource "neon_branch_realtime" "this" {
  project_id      = "cool-moon-42"
  branch_id       = "br-cool-moon-42"
  allowed_origins = ["https://app.example.com/callback"]
}
`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`must be \* or an http or https origin`),
		}},
	})
}

// Neon counts characters, so a 2,048-character origin whose UTF-8 encoding is longer than 2,048 bytes is valid.
func TestBranchRealtimeOriginLimitCountsCharacters(t *testing.T) {
	t.Parallel()

	origin := "https://" + strings.Repeat("a", 2039) + "é"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`provider "neon" {
  api_key = "unused"
}

resource "neon_branch_realtime" "this" {
  project_id      = "cool-moon-42"
  branch_id       = "br-cool-moon-42"
  allowed_origins = [%q]
}`, origin),
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}
