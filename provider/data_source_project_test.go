package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/require"
)

func TestProjectLookupIDValidation(t *testing.T) {
	data, err := os.ReadFile("testdata/project-lookup-id.json")
	require.NoError(t, err)
	var contract struct {
		Pattern string `json:"pattern"`
	}
	require.NoError(t, json.Unmarshal(data, &contract))
	require.Equal(t, contract.Pattern, projectLookupIDPattern.String(), "lookup validator must match the reviewed getProject contract")
	pattern := regexp.MustCompile(contract.Pattern)
	for _, tc := range []struct {
		name       string
		expression string
	}{
		{
			name:       "empty",
			expression: `""`,
		},
		{
			name:       "uppercase",
			expression: `"Cool-moon-42"`,
		},
		{
			name:       "underscore",
			expression: `"cool_moon"`,
		},
		{
			name:       "non-ASCII",
			expression: `"café"`,
		},
		{
			name:       "too long",
			expression: strconv.Quote(strings.Repeat("a", 61)),
		},
		{
			name:       "one character",
			expression: `"0"`,
		},
		{
			name:       "maximum",
			expression: strconv.Quote(strings.Repeat("a", 57) + "-42"),
		},
		{
			name:       "required null",
			expression: "null",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wantError *regexp.Regexp
			if tc.expression == "null" {
				wantError = regexp.MustCompile("Missing Configuration for Required Attribute|Missing required argument")
			} else {
				id, err := strconv.Unquote(tc.expression)
				require.NoError(t, err)
				if !pattern.MatchString(id) {
					wantError = regexp.MustCompile("lowercase ASCII letters, digits, or hyphens")
				}
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{{
					Config:             projectLookupIDConfig(tc.expression),
					PlanOnly:           true,
					ExpectNonEmptyPlan: wantError == nil,
					ExpectError:        wantError,
				}},
			})
		})
	}

	t.Run("dependency-produced ID", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config:             projectLookupIDConfig("terraform_data.project_id.output"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectUnknownValue("data.neon_project.lookup", tfjsonpath.New("id")),
					},
				},
			}},
		})
	})
}

func projectLookupIDConfig(expression string) string {
	return fmt.Sprintf(`
provider "neon" { api_key = "unused" }

resource "terraform_data" "project_id" { input = "cool-moon-42" }

data "neon_project" "lookup" {
  id         = %s
  depends_on = [terraform_data.project_id]
}
`, expression)
}
