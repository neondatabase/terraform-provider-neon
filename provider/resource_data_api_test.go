package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	resourcetest "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/stretchr/testify/require"
)

func TestDataAPIAuthProviderValidation(t *testing.T) {
	authProviderPath := tfjsonpath.New("auth_provider")
	for _, tt := range []struct {
		name        string
		config      string
		expectError bool
		planCheck   plancheck.PlanCheck
	}{
		{
			name:        "reject invalid auth provider",
			config:      dataAPIAuthProviderConfig(`auth_provider = "invalid-auth-provider"`),
			expectError: true,
		},
		{
			name:        "reject empty auth provider",
			config:      dataAPIAuthProviderConfig(`auth_provider = ""`),
			expectError: true,
		},
		{
			name:      "accept neon_auth",
			config:    dataAPIAuthProviderConfig(`auth_provider = "neon_auth"`),
			planCheck: plancheck.ExpectKnownValue("neon_data_api.this", authProviderPath, knownvalue.StringExact("neon_auth")),
		},
		{
			name:      "accept external",
			config:    dataAPIAuthProviderConfig(`auth_provider = "external"`),
			planCheck: plancheck.ExpectKnownValue("neon_data_api.this", authProviderPath, knownvalue.StringExact("external")),
		},
		{
			name:      "preserve SDK accepted quoted auth provider",
			config:    dataAPIAuthProviderConfig(`auth_provider = "\"neon_auth\""`),
			planCheck: plancheck.ExpectKnownValue("neon_data_api.this", authProviderPath, knownvalue.StringExact(`"neon_auth"`)),
		},
		{
			name:      "preserve omitted auth provider",
			config:    dataAPIAuthProviderConfig(""),
			planCheck: plancheck.ExpectKnownValue("neon_data_api.this", authProviderPath, knownvalue.Null()),
		},
		{
			name:      "preserve null auth provider",
			config:    dataAPIAuthProviderConfig(`auth_provider = null`),
			planCheck: plancheck.ExpectKnownValue("neon_data_api.this", authProviderPath, knownvalue.Null()),
		},
		{
			name:      "defer dependency produced unknown auth provider",
			config:    dataAPIUnknownAuthProviderConfig(),
			planCheck: plancheck.ExpectUnknownValue("neon_data_api.this", authProviderPath),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			step := resourcetest.TestStep{
				Config:             tt.config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: !tt.expectError,
			}
			if tt.expectError {
				step.ExpectError = regexp.MustCompile(`(?s)auth_provider.*must be one of: neon_auth, external`)
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

func dataAPIAuthProviderConfig(authProviderAttribute string) string {
	return fmt.Sprintf(`provider "neon" {
  api_key = "test-api-key"
}

resource "neon_data_api" "this" {
  project_id    = "project-id"
  branch_id     = "br-id"
  database_name = "neondb"
  %s
}
`, authProviderAttribute)
}

func dataAPIUnknownAuthProviderConfig() string {
	return dataAPIAuthProviderConfig(`auth_provider = terraform_data.auth_provider.output == "neon_auth" ? "neon_auth" : "external"`) + `
resource "terraform_data" "auth_provider" {
  input = "neon_auth"
}
`
}

func dataAPITestResource(t *testing.T) resource.Resource {
	t.Helper()
	for _, factory := range (&Provider{}).Resources(context.Background()) {
		r := factory()
		var metadata resource.MetadataResponse
		r.Metadata(context.Background(), resource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_data_api" {
			return r
		}
	}
	t.Fatal("neon_data_api is not registered")
	return nil
}

func dataAPISchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	dataAPITestResource(t).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return resp.Schema
}

func dataAPIValue(typ tftypes.Type, value any) tftypes.Value {
	if object, ok := typ.(tftypes.Object); ok && value != nil && value != tftypes.UnknownValue {
		supplied := value.(map[string]any)
		values := make(map[string]tftypes.Value, len(object.AttributeTypes))
		for name, attributeType := range object.AttributeTypes {
			values[name] = dataAPIValue(attributeType, supplied[name])
		}
		return tftypes.NewValue(typ, values)
	}
	if list, ok := typ.(tftypes.List); ok && value != nil && value != tftypes.UnknownValue {
		var values []tftypes.Value
		for _, item := range value.([]string) {
			values = append(values, tftypes.NewValue(list.ElementType, item))
		}
		return tftypes.NewValue(typ, values)
	}
	return tftypes.NewValue(typ, value)
}

func dataAPIState(t *testing.T, overrides map[string]any) tfsdk.State {
	t.Helper()
	s := dataAPISchema(t)
	values := map[string]any{"id": "pr-test/br-test/neondb", "project_id": "pr-test", "branch_id": "br-test", "database_name": "neondb", "url": "https://endpoint.test/rest/v1"}
	for k, v := range overrides {
		values[k] = v
	}
	return tfsdk.State{Schema: s, Raw: dataAPIValue(s.Type().TerraformType(context.Background()), values)}
}

func dataAPIString(t *testing.T, state tfsdk.State, name string) string {
	t.Helper()
	var value types.String
	diags := state.GetAttribute(context.Background(), path.Root(name), &value)
	require.False(t, diags.HasError(), "%v", diags)
	return value.ValueString()
}

func TestDataAPIImport(t *testing.T) {
	for _, id := range []string{"pr-test/br-test/neondb", "pr-test/br-test/db/with/slash", "missing", "pr-test//neondb", "/br-test/neondb", "pr-test/br-test/"} {
		t.Run(id, func(t *testing.T) {
			r := dataAPITestResource(t)
			s := dataAPISchema(t)
			resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}}
			r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
			if strings.Count(id, "/") < 2 || strings.Contains(id, "//") || strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
				require.True(t, resp.Diagnostics.HasError())
				return
			}
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, id, dataAPIString(t, resp.State, "id"))
			require.Equal(t, strings.SplitN(id, "/", 3)[2], dataAPIString(t, resp.State, "database_name"))
			var settings types.Object
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings"), &settings).HasError())
			require.True(t, settings.IsNull())
		})
	}
}

func TestDataAPISettingsRemovalReplacement(t *testing.T) {
	for _, tt := range []struct {
		name       string
		prior      any
		configured any
		replace    bool
	}{
		{name: "remove object", prior: map[string]any{"db_max_rows": float64(10)}, replace: true},
		{name: "remove member", prior: map[string]any{"db_max_rows": float64(10), "db_anon_role": "guest"}, configured: map[string]any{"db_anon_role": "guest"}, replace: true},
		{name: "change value", prior: map[string]any{"db_max_rows": float64(10)}, configured: map[string]any{"db_max_rows": float64(20)}},
		{name: "unknown value", prior: map[string]any{"db_max_rows": float64(10)}, configured: map[string]any{"db_max_rows": tftypes.UnknownValue}},
		{name: "add object", configured: map[string]any{"db_max_rows": float64(10)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := dataAPITestResource(t)
			state := dataAPIState(t, map[string]any{"settings": tt.prior})
			planned := dataAPIState(t, map[string]any{"settings": tt.configured})
			req := resource.ModifyPlanRequest{State: state, Plan: tfsdk.Plan(planned), Config: tfsdk.Config(planned)}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}
			r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(), req, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, tt.replace, len(resp.RequiresReplace) > 0)
		})
	}
}
