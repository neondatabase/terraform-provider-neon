package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	testresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/require"
)

type entityNameInput struct {
	name         string
	resourceType string
	parent       string
	maxLength    int
}

func entityNameInputs() []entityNameInput {
	return []entityNameInput{
		{
			name:         "branch",
			resourceType: "neon_branch",
			maxLength:    256,
		},
		{
			name:         "endpoint",
			resourceType: "neon_endpoint",
			maxLength:    64,
		},
		{
			name:         "project_branch",
			resourceType: "neon_project",
			parent:       "branch",
			maxLength:    256,
		},
		{
			name:         "project_primary_compute",
			resourceType: "neon_project",
			parent:       "primary_compute",
			maxLength:    64,
		},
	}
}

type entityNameCase struct {
	name    string
	value   types.String
	omit    bool
	invalid bool
}

func entityNameCases(maxLength int) []entityNameCase {
	return []entityNameCase{
		{
			name:    "empty",
			value:   types.StringValue(""),
			invalid: true,
		},
		{
			name:  "ascii_min",
			value: types.StringValue("a"),
		},
		{
			name:  "ascii_max",
			value: types.StringValue(strings.Repeat("a", maxLength)),
		},
		{
			name:    "ascii_over",
			value:   types.StringValue(strings.Repeat("a", maxLength+1)),
			invalid: true,
		},
		{
			name:  "multibyte_min",
			value: types.StringValue("界"),
		},
		{
			name:  "multibyte_max",
			value: types.StringValue(strings.Repeat("界", maxLength)),
		},
		{
			name:    "multibyte_over",
			value:   types.StringValue(strings.Repeat("界", maxLength+1)),
			invalid: true,
		},
		{
			name:  "combining_min",
			value: types.StringValue("\u0305"),
		},
		{
			name:  "combining_max",
			value: types.StringValue(strings.Repeat("x\u0305", maxLength/2)),
		},
		{
			name:    "combining_over",
			value:   types.StringValue(strings.Repeat("x\u0305", maxLength/2) + "\u0305"),
			invalid: true,
		},
		{
			name:  "whitespace_min",
			value: types.StringValue(" "),
		},
		{
			name:  "whitespace_max",
			value: types.StringValue(strings.Repeat(" ", maxLength)),
		},
		{
			name:  "padded",
			value: types.StringValue(" padded "),
		},
		{
			name:  "omitted",
			value: types.StringNull(),
			omit:  true,
		},
		{
			name:  "null",
			value: types.StringNull(),
		},
		{
			name:  "dependency_unknown",
			value: types.StringUnknown(),
		},
	}
}

func (input entityNameInput) attribute(t *testing.T) (schema.StringAttribute, path.Path) {
	t.Helper()
	var r resource.Resource
	switch input.resourceType {
	case "neon_branch":
		r = NewNeonBranchResource()
	case "neon_endpoint":
		r = NewNeonEndpointResource()
	case "neon_project":
		r = NewNeonProjectResource()
	default:
		t.Fatalf("unsupported resource %s", input.resourceType)
	}
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	attributes := resp.Schema.Attributes
	p := path.Root("name")
	if input.parent != "" {
		attributes = attributes[input.parent].(schema.SingleNestedAttribute).Attributes
		p = path.Root(input.parent).AtName("name")
	}
	return attributes["name"].(schema.StringAttribute), p
}

func validateEntityName(attribute schema.StringAttribute, p path.Path, value types.String) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	for _, v := range attribute.Validators {
		var resp validator.StringResponse
		v.ValidateString(context.Background(), validator.StringRequest{
			Path:        p,
			ConfigValue: value,
		}, &resp)
		diagnostics.Append(resp.Diagnostics...)
	}
	return diagnostics
}

func TestBranchEndpointNameAttachedValidation(t *testing.T) {
	for _, input := range entityNameInputs() {
		t.Run(input.name, func(t *testing.T) {
			attribute, p := input.attribute(t)
			for _, tc := range entityNameCases(input.maxLength) {
				t.Run(tc.name, func(t *testing.T) {
					diagnostics := validateEntityName(attribute, p, tc.value)
					require.Equal(t, tc.invalid, diagnostics.HasError(), "%v", diagnostics)
					for _, d := range diagnostics {
						attributeDiagnostic, ok := d.(diag.DiagnosticWithPath)
						require.True(t, ok)
						require.True(t, p.Equal(attributeDiagnostic.Path()), "%s", attributeDiagnostic.Path())
					}
				})
			}
		})
	}
}

func entityNamePlanConfig(input entityNameInput, nameExpression, parentMode string) string {
	var body string
	switch input.resourceType {
	case "neon_branch":
		body = "project_id = \"test-project\"\n"
	case "neon_endpoint":
		body = "project_id = \"test-project\"\nbranch_id = \"br-test\"\n"
	case "neon_project":
		body = "name = \"test-project\"\nregion_id = \"aws-us-east-2\"\n"
	}
	name := ""
	if nameExpression != "" {
		name = "name = " + nameExpression + "\n"
	}
	if input.parent == "" {
		body += name
	} else {
		switch parentMode {
		case "omitted":
		case "null":
			body += input.parent + " = null\n"
		default:
			body += input.parent + " = {\n" + name + "}\n"
		}
	}
	return fmt.Sprintf(`provider "neon" {
  api_key = "test-api-key"
}

resource "terraform_data" "name" {
  input = "dependency-name"
}

resource %q "this" {
%s}
`, input.resourceType, body)
}

func TestBranchEndpointNamePlan(t *testing.T) {
	for _, input := range entityNameInputs() {
		t.Run(input.name, func(t *testing.T) {
			for _, tc := range entityNameCases(input.maxLength) {
				t.Run(tc.name, func(t *testing.T) {
					expression := fmt.Sprintf("%q", tc.value.ValueString())
					if tc.omit {
						expression = ""
					} else if tc.value.IsNull() {
						expression = "null"
					} else if tc.value.IsUnknown() {
						expression = "terraform_data.name.output"
					}
					p := tfjsonpath.New("name")
					diagnosticPath := "name"
					if input.parent != "" {
						p = tfjsonpath.New(input.parent).AtMapKey("name")
						diagnosticPath = input.parent + ".name"
					}
					var expectError *regexp.Regexp
					var checks []plancheck.PlanCheck
					if tc.invalid {
						expectError = regexp.MustCompile(fmt.Sprintf(
							`(?s)Attribute %s UTF-8 character count must be between 1 and\s+%d`,
							regexp.QuoteMeta(diagnosticPath),
							input.maxLength,
						))
					} else {
						address := input.resourceType + ".this"
						checks = append(checks, plancheck.ExpectResourceAction(address, plancheck.ResourceActionCreate))
						if tc.value.IsNull() || tc.value.IsUnknown() {
							checks = append(checks, plancheck.ExpectUnknownValue(address, p))
						} else {
							checks = append(checks, plancheck.ExpectKnownValue(
								address,
								p,
								knownvalue.StringExact(tc.value.ValueString()),
							))
						}
					}
					testresource.UnitTest(t, testresource.TestCase{
						ProtoV6ProviderFactories: newProviderFactories(),
						Steps: []testresource.TestStep{
							{
								Config:             entityNamePlanConfig(input, expression, ""),
								PlanOnly:           true,
								ExpectNonEmptyPlan: !tc.invalid,
								ExpectError:        expectError,
								ConfigPlanChecks: testresource.ConfigPlanChecks{
									PostApplyPreRefresh: checks,
								},
							},
						},
					})
				})
			}
			if input.parent != "" {
				for _, mode := range []string{"omitted", "null"} {
					t.Run("parent_"+mode, func(t *testing.T) {
						testresource.UnitTest(t, testresource.TestCase{
							ProtoV6ProviderFactories: newProviderFactories(),
							Steps: []testresource.TestStep{
								{
									Config:             entityNamePlanConfig(input, "", mode),
									PlanOnly:           true,
									ExpectNonEmptyPlan: true,
									ConfigPlanChecks: testresource.ConfigPlanChecks{
										PostApplyPreRefresh: []plancheck.PlanCheck{
											plancheck.ExpectUnknownValue("neon_project.this", tfjsonpath.New(input.parent)),
										},
									},
								},
							},
						})
					})
				}
			}
		})
	}
}

func TestBranchEndpointNameRequestContract(t *testing.T) {
	data, err := os.ReadFile("testdata/branch_endpoint_name_contract.json")
	require.NoError(t, err)
	var fixture struct {
		Requests []struct {
			Schema    string `json:"schema"`
			Input     string `json:"input"`
			MinLength int    `json:"minLength"`
			MaxLength int    `json:"maxLength"`
		} `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	for _, request := range fixture.Requests {
		t.Run(request.Schema, func(t *testing.T) {
			for _, input := range entityNameInputs() {
				matches := input.resourceType == "neon_"+request.Input || input.parent == request.Input ||
					(request.Input == "endpoint" && input.parent == "primary_compute")
				if !matches {
					continue
				}
				t.Run(input.name, func(t *testing.T) {
					attribute, p := input.attribute(t)
					for _, size := range []int{request.MinLength - 1, request.MinLength, request.MaxLength, request.MaxLength + 1} {
						diagnostics := validateEntityName(attribute, p, types.StringValue(strings.Repeat("界", size)))
						require.Equal(t,
							size < request.MinLength || size > request.MaxLength,
							diagnostics.HasError(),
							"size %d: %v", size, diagnostics,
						)
					}
				})
			}
		})
	}
}

func TestBranchEndpointNameReadOnlyConsumers(t *testing.T) {
	ctx := context.Background()
	var project datasource.SchemaResponse
	NewProjectDataSource().Schema(ctx, datasource.SchemaRequest{}, &project)
	require.False(t, project.Diagnostics.HasError(), "%v", project.Diagnostics)
	for _, parent := range []string{"branch", "primary_compute"} {
		t.Run("project_"+parent, func(t *testing.T) {
			attribute := project.Schema.Attributes[parent].(dsschema.SingleNestedAttribute).Attributes["name"].(dsschema.StringAttribute)
			require.True(t, attribute.Computed)
			require.False(t, attribute.Optional || attribute.Required)
			require.Empty(t, attribute.Validators)
		})
	}
	var branches datasource.SchemaResponse
	NewBranchesDataSource().Schema(ctx, datasource.SchemaRequest{}, &branches)
	require.False(t, branches.Diagnostics.HasError(), "%v", branches.Diagnostics)
	attribute := branches.Schema.Attributes["branches"].(dsschema.ListNestedAttribute).NestedObject.Attributes["name"].(dsschema.StringAttribute)
	require.True(t, attribute.Computed)
	require.False(t, attribute.Optional || attribute.Required)
	require.Empty(t, attribute.Validators)
}
