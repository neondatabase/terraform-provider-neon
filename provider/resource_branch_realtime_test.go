package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/stretchr/testify/require"
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

func TestBranchRealtimeOriginLength(t *testing.T) {
	data, err := os.ReadFile("testdata/realtime-origin-length.json")
	require.NoError(t, err)
	var contract struct {
		MinLength int `json:"minLength"`
		MaxLength int `json:"maxLength"`
	}
	require.NoError(t, json.Unmarshal(data, &contract))
	const prefix = "https://"
	require.Greater(t, contract.MaxLength, len(prefix))
	hostLength := contract.MaxLength - len(prefix)
	for _, tc := range []struct {
		name             string
		originExpression string
		overLimit        bool
	}{
		{
			name:             "ASCII maximum",
			originExpression: fmt.Sprintf("%q", prefix+strings.Repeat("a", hostLength)),
		},
		{
			name:             "Unicode maximum",
			originExpression: fmt.Sprintf("%q", prefix+strings.Repeat("界", hostLength)),
		},
		{
			name:             "Unicode over maximum",
			originExpression: fmt.Sprintf("%q", prefix+strings.Repeat("界", hostLength+1)),
			overLimit:        true,
		},
		{
			name:             "unknown origin",
			originExpression: "terraform_data.origin.output",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := resource.TestStep{
				Config:             branchRealtimeOriginConfig(tc.originExpression),
				PlanOnly:           true,
				ExpectNonEmptyPlan: !tc.overLimit,
			}
			if tc.overLimit {
				step.ExpectError = regexp.MustCompile(fmt.Sprintf(
					`(?s)allowed_origins.*character count must be between %d and\s+%d\b`, contract.MinLength, contract.MaxLength,
				))
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps:                    []resource.TestStep{step},
			})
		})
	}
}

func branchRealtimeOriginConfig(originExpression string) string {
	return fmt.Sprintf(`
provider "neon" { api_key = "unused" }
resource "terraform_data" "origin" { input = "https://example.com" }
resource "neon_branch_realtime" "this" {
  project_id = "cool-moon-42"
  branch_id = "br-cool-moon-42"
  allowed_origins = [%s]
}
`, originExpression)
}
