package provider

import (
	"regexp"
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
