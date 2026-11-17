package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	resourcetest "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/require"
)

func TestServiceCredentialScopeValidation(t *testing.T) {
	for _, tt := range []struct {
		name        string
		scopes      string
		dependency  bool
		expectError string
		planCheck   plancheck.PlanCheck
	}{
		{
			name:        "reject empty list",
			scopes:      `[]`,
			expectError: `(?s)scopes.*list must contain at least 1 elements and at most 16\s+elements`,
		},
		{
			name:        "reject seventeen scopes",
			scopes:      serviceCredentialRepeatedScopes(17),
			expectError: `(?s)scopes.*list must contain at least 1 elements and at most 16\s+elements`,
		},
		{
			name:        "reject invalid scope",
			scopes:      `["invalid-scope"]`,
			expectError: `(?s)scopes.*must be one of: storage:read, storage:write, ai_gateway:invoke,\s+functions:invoke`,
		},
		{
			name:        "reject response only scope",
			scopes:      `["telemetry:write"]`,
			expectError: `(?s)scopes.*must be one of: storage:read, storage:write, ai_gateway:invoke,\s+functions:invoke`,
		},
		{
			name:      "accept storage read and minimum size",
			scopes:    `["storage:read"]`,
			planCheck: serviceCredentialKnownScopes("storage:read"),
		},
		{
			name:      "accept storage write",
			scopes:    `["storage:write"]`,
			planCheck: serviceCredentialKnownScopes("storage:write"),
		},
		{
			name:      "accept ai gateway invoke",
			scopes:    `["ai_gateway:invoke"]`,
			planCheck: serviceCredentialKnownScopes("ai_gateway:invoke"),
		},
		{
			name:      "accept functions invoke",
			scopes:    `["functions:invoke"]`,
			planCheck: serviceCredentialKnownScopes("functions:invoke"),
		},
		{
			name:   "accept sixteen scopes including duplicates",
			scopes: serviceCredentialRepeatedScopes(16),
			planCheck: plancheck.ExpectKnownValue("neon_service_credential.this", tfjsonpath.New("scopes"),
				knownvalue.ListSizeExact(16)),
		},
		{
			name:        "reject required null",
			scopes:      `null`,
			expectError: `(?s)(Missing Configuration for Required Attribute|Missing required argument).*scopes`,
		},
		{
			name:       "defer unknown entire list",
			scopes:     `split(",", terraform_data.scope.output)`,
			dependency: true,
			planCheck:  plancheck.ExpectUnknownValue("neon_service_credential.this", tfjsonpath.New("scopes")),
		},
		{
			name:       "defer unknown element",
			scopes:     `[terraform_data.scope.output == "storage:read" ? "storage:read" : "storage:write"]`,
			dependency: true,
			planCheck:  plancheck.ExpectUnknownValue("neon_service_credential.this", tfjsonpath.New("scopes").AtSliceIndex(0)),
		},
		{
			name:        "reject known invalid sibling of unknown element",
			scopes:      `[terraform_data.scope.output == "storage:read" ? "storage:read" : "storage:write", "invalid-scope"]`,
			dependency:  true,
			expectError: `(?s)scopes.*must be one of: storage:read, storage:write, ai_gateway:invoke,\s+functions:invoke`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := serviceCredentialScopesConfig(tt.scopes)
			if tt.dependency {
				config += serviceCredentialScopeDependencyConfig()
			}
			step := resourcetest.TestStep{
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: tt.expectError == "",
			}
			if tt.expectError != "" {
				step.ExpectError = regexp.MustCompile(tt.expectError)
			} else {
				step.ConfigPlanChecks.PostApplyPreRefresh = []plancheck.PlanCheck{tt.planCheck}
			}
			resourcetest.UnitTest(t, resourcetest.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps:                    []resourcetest.TestStep{step},
			})
		})
	}
}

func serviceCredentialKnownScopes(scope string) plancheck.PlanCheck {
	return plancheck.ExpectKnownValue("neon_service_credential.this", tfjsonpath.New("scopes"),
		knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact(scope)}))
}

func serviceCredentialRepeatedScopes(count int) string {
	return "[" + strings.TrimSuffix(strings.Repeat(`"storage:read",`, count), ",") + "]"
}

func serviceCredentialScopesConfig(scopes string) string {
	return fmt.Sprintf(`provider "neon" {
  api_key = "test-api-key"
}

resource "neon_service_credential" "this" {
  project_id = "project-id"
  branch_id  = "br-id"
  scopes     = %s
}
`, scopes)
}

func serviceCredentialScopeDependencyConfig() string {
	return `
resource "terraform_data" "scope" {
  input = "storage:read"
}
`
}

func TestServiceCredentialScopeValidatorDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name      string
		value     types.List
		errorPath *path.Path
	}{
		{
			name:  "skip null collection for required schema handling",
			value: types.ListNull(types.StringType),
		},
		{
			name:  "skip unknown collection",
			value: types.ListUnknown(types.StringType),
		},
		{
			name: "skip unknown element",
			value: types.ListValueMust(types.StringType, []attr.Value{
				types.StringUnknown(),
			}),
		},
		{
			name: "report invalid element beside unknown at exact index",
			value: types.ListValueMust(types.StringType, []attr.Value{
				types.StringUnknown(),
				types.StringValue("invalid-scope"),
			}),
			errorPath: serviceCredentialScopePath(1),
		},
		{
			name:      "report empty list at attribute",
			value:     types.ListValueMust(types.StringType, []attr.Value{}),
			errorPath: serviceCredentialScopePath(-1),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var schemaResp resource.SchemaResponse
			NewNeonServiceCredentialResource().Schema(ctx, resource.SchemaRequest{}, &schemaResp)
			require.False(t, schemaResp.Diagnostics.HasError(), "%v", schemaResp.Diagnostics)
			scopes := schemaResp.Schema.Attributes["scopes"].(schema.ListAttribute)
			var resp validator.ListResponse
			for _, v := range scopes.Validators {
				v.ValidateList(ctx, validator.ListRequest{
					Path:        path.Root("scopes"),
					ConfigValue: tt.value,
				}, &resp)
			}
			if tt.errorPath == nil {
				require.Empty(t, resp.Diagnostics)
				return
			}
			require.Len(t, resp.Diagnostics, 1)
			require.Equal(t, diag.SeverityError, resp.Diagnostics[0].Severity())
			diagnostic, ok := resp.Diagnostics[0].(diag.DiagnosticWithPath)
			require.True(t, ok)
			require.True(t, tt.errorPath.Equal(diagnostic.Path()), "%s", diagnostic.Path())
			require.NotContains(t, diagnostic.Detail(), "invalid-scope")
		})
	}
}

func serviceCredentialScopePath(index int) *path.Path {
	p := path.Root("scopes")
	if index >= 0 {
		p = p.AtListIndex(index)
	}
	return &p
}
