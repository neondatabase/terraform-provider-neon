package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	frameworkresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/require"
)

func apiKeyNameResources() map[string]func() frameworkresource.Resource {
	return map[string]func() frameworkresource.Resource{
		"neon_api_key":     NewAPIKeyResource,
		"neon_org_api_key": NewOrgAPIKeyResource,
	}
}

func apiKeyNameCases() []struct {
	name    string
	value   string
	invalid bool
} {
	return []struct {
		name    string
		value   string
		invalid bool
	}{
		{
			name:  "short",
			value: "test-api-key",
		},
		{
			name:  "ASCII 64",
			value: strings.Repeat("a", 64),
		},
		{
			name:    "ASCII 65",
			value:   strings.Repeat("a", 65),
			invalid: true,
		},
		{
			name:  "multi-byte 64",
			value: strings.Repeat("界", 64),
		},
		{
			name:    "multi-byte 65",
			value:   strings.Repeat("界", 65),
			invalid: true,
		},
		{
			name:  "combining code points 64",
			value: strings.Repeat("q\u0301", 32),
		},
		{
			name:    "combining code points 65",
			value:   strings.Repeat("q\u0301", 32) + "\u0301",
			invalid: true,
		},
		{
			name:  "empty",
			value: "",
		},
		{
			name:  "whitespace 64",
			value: strings.Repeat(" ", 64),
		},
		{
			name:    "whitespace 65",
			value:   strings.Repeat(" ", 65),
			invalid: true,
		},
		{
			name:  "padded",
			value: "  test key \t ",
		},
	}
}

func TestAPIKeyNameValidationContract(t *testing.T) {
	data, err := os.ReadFile("testdata/api-key-name-validation-contract.json")
	require.NoError(t, err)
	var contract struct {
		Property struct {
			Type      string
			MaxLength int
			MinLength *int
		}
		Bindings []struct {
			Resource  string
			Attribute string
		}
	}
	require.NoError(t, json.Unmarshal(data, &contract))
	require.Equal(t, "string", contract.Property.Type)
	require.Equal(t, 64, contract.Property.MaxLength)
	require.Nil(t, contract.Property.MinLength)
	require.Len(t, contract.Bindings, 2)

	for _, binding := range contract.Bindings {
		t.Run(binding.Resource, func(t *testing.T) {
			constructor, ok := apiKeyNameResources()[binding.Resource]
			require.True(t, ok)
			var response frameworkresource.SchemaResponse
			constructor().Schema(context.Background(), frameworkresource.SchemaRequest{}, &response)
			require.False(t, response.Diagnostics.HasError(), response.Diagnostics)
			attribute, ok := response.Schema.Attributes[binding.Attribute].(schema.StringAttribute)
			require.True(t, ok)

			for _, tc := range apiKeyNameCases() {
				t.Run(tc.name, func(t *testing.T) {
					diagnostics := validateAPIKeyName(attribute, types.StringValue(tc.value))
					require.Equal(t, tc.invalid, diagnostics.HasError(), diagnostics)
					if tc.invalid {
						require.Len(t, diagnostics, 1)
						attributeDiagnostic, ok := diagnostics[0].(diag.DiagnosticWithPath)
						require.True(t, ok)
						require.True(t, attributeDiagnostic.Path().Equal(path.Root("name")))
						require.Contains(t, diagnostics[0].Detail(), "must be at most 64")
						require.NotContains(t, diagnostics[0].Detail(), tc.value)
					}
				})
			}
			for name, value := range map[string]types.String{
				"null defers to Required": types.StringNull(),
				"unknown defers":          types.StringUnknown(),
			} {
				t.Run(name, func(t *testing.T) {
					require.Empty(t, validateAPIKeyName(attribute, value))
				})
			}
		})
	}
}

func validateAPIKeyName(attribute schema.StringAttribute, value types.String) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	for _, attached := range attribute.Validators {
		var response validator.StringResponse
		attached.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("name"),
			ConfigValue: value,
		}, &response)
		diagnostics.Append(response.Diagnostics...)
	}
	return diagnostics
}

func TestAPIKeyNameValidationPlan(t *testing.T) {
	for resourceType := range apiKeyNameResources() {
		t.Run(resourceType, func(t *testing.T) {
			for _, tc := range apiKeyNameCases() {
				t.Run(tc.name, func(t *testing.T) {
					step := resource.TestStep{
						Config:             apiKeyNamePlanConfig(resourceType, fmt.Sprintf("name = %q", tc.value)),
						PlanOnly:           true,
						ExpectNonEmptyPlan: !tc.invalid,
					}
					if tc.invalid {
						step.ExpectError = regexp.MustCompile(`Attribute name UTF-8 character count must be at most 64`)
					} else {
						step.ConfigPlanChecks.PostApplyPreRefresh = []plancheck.PlanCheck{
							plancheck.ExpectKnownValue(resourceType+".this", tfjsonpath.New("name"), knownvalue.StringExact(tc.value)),
						}
					}
					resource.UnitTest(t, resource.TestCase{
						ProtoV6ProviderFactories: newProviderFactories(),
						Steps:                    []resource.TestStep{step},
					})
				})
			}
			for name, assignment := range map[string]string{
				"required null":    "name = null",
				"required omitted": "",
			} {
				t.Run(name, func(t *testing.T) {
					resource.UnitTest(t, resource.TestCase{
						ProtoV6ProviderFactories: newProviderFactories(),
						Steps: []resource.TestStep{
							{
								Config:      apiKeyNamePlanConfig(resourceType, assignment),
								PlanOnly:    true,
								ExpectError: regexp.MustCompile(`(?s)(Missing Configuration for Required Attribute|Missing required argument).*name`),
							},
						},
					})
				})
			}
			t.Run("dependency-produced unknown", func(t *testing.T) {
				resource.UnitTest(t, resource.TestCase{
					ProtoV6ProviderFactories: newProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: apiKeyNamePlanConfig(resourceType, "name = terraform_data.name.output") + `
resource "terraform_data" "name" {
  input = "not available until apply"
}
`,
							PlanOnly:           true,
							ExpectNonEmptyPlan: true,
							ConfigPlanChecks: resource.ConfigPlanChecks{
								PostApplyPreRefresh: []plancheck.PlanCheck{
									plancheck.ExpectUnknownValue(resourceType+".this", tfjsonpath.New("name")),
								},
							},
						},
					},
				})
			})
		})
	}
}

func apiKeyNamePlanConfig(resourceType, assignment string) string {
	orgID := ""
	if resourceType == "neon_org_api_key" {
		orgID = `org_id = "test-org"`
	}
	return fmt.Sprintf(`provider "neon" {
  api_key = "test-api-key"
}

resource %q "this" {
  %s
  %s
}
`, resourceType, assignment, orgID)
}
