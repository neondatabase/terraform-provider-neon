package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testBranchInitSourceConfig(mode string) string {
	setting := ""
	if mode != "" {
		setting = fmt.Sprintf("init_source = %q", mode)
	}
	return fmt.Sprintf(`provider "neon" { api_key = "unused" }
resource "neon_branch" "this" {
  project_id = "cool-moon-42"
  parent_id  = "br-source"
  %s
}`, setting)
}

func TestBranchInitSourceBoundaries(t *testing.T) {
	unknown := strings.Replace(testBranchInitSourceConfig("parent-schema"), `"parent-schema"`, "terraform_data.mode.output", 1)
	unknown += "\n" + `resource "terraform_data" "mode" { input = "parent-schema" }`
	for _, tc := range []struct {
		name, config string
		invalid      bool
	}{
		{"unknown mode", unknown, false},
		{"project root read only", `provider "neon" { api_key = "unused" }
resource "neon_project" "this" {
 name = "example"
 branch = {init_source = "parent-schema"}
}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			step := resource.TestStep{Config: tc.config, PlanOnly: true, ExpectNonEmptyPlan: !tc.invalid}
			if tc.invalid {
				step.ExpectError = regexp.MustCompile(`(?is)(read.only|unconfigurable|cannot configure).*init_source`)
			}
			resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), Steps: []resource.TestStep{step}})
		})
	}
}

func TestBranchInitSourcePlan(t *testing.T) {
	for _, tc := range []struct{ name, mode string }{
		{"omitted", ""},
		{"parent-data", "parent-data"},
		{"parent-schema", "parent-schema"},
		{"schema-only", "schema-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{{
					Config:             testBranchInitSourceConfig(tc.mode),
					PlanOnly:           true,
					ExpectNonEmptyPlan: true,
				}},
			})
		})
	}
}
