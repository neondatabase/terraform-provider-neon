package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// Dropping either alias from create/update handling must fail these tests.
// State and the API must agree with the configured value, with no subsequent
// plan changes, including when a different field changes at the same time.
func TestProjectProtectionAliases(t *testing.T) {
	t.Parallel()
	type step struct {
		settings string
		want     bool
	}
	for _, tt := range []struct {
		name  string
		steps []step
	}{
		{"legacy create and updates", []step{
			{"default_branch_protected = true", true},
			{"default_branch_protected = false", false},
			{"default_branch_protected = true", true},
			{"", false},
		}},
		{"nested create and updates", []step{
			{"branch = { protected = true }", true},
			{"branch = { protected = false }", false},
			{"branch = { protected = true }", true},
			{"", false},
		}},
		{"matching aliases", []step{
			{"default_branch_protected = true\nbranch = { protected = true }", true},
			{"default_branch_protected = false\nbranch = { protected = false }", false},
		}},
		{"legacy with branch rename", []step{
			{"default_branch_protected = true\nbranch = { name = \"main\" }", true},
			{"default_branch_protected = true\nbranch = { name = \"renamed\" }", true},
			{"default_branch_protected = false\nbranch = { name = \"renamed\" }", false},
		}},
		{"neither configured defaults false", []step{
			{"", false},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &protectionAPI{branchName: "main"}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			var steps []resource.TestStep
			for _, step := range tt.steps {
				want := step.want
				steps = append(steps, resource.TestStep{
					Config: protectionConfig(step.settings),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("neon_project.this", "default_branch_protected", fmt.Sprint(want)),
						resource.TestCheckResourceAttr("neon_project.this", "branch.protected", fmt.Sprint(want)),
						func(_ *terraform.State) error {
							api.mu.Lock()
							defer api.mu.Unlock()
							if api.protected != want {
								return fmt.Errorf("API protection: want %t, got %t", want, api.protected)
							}
							return nil
						},
					),
				})
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps:                    steps,
			})
		})
	}
}

func TestProjectProtectionConflictingAliases(t *testing.T) {
	t.Parallel()
	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			t.Parallel()
			api := &protectionAPI{branchName: "main"}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{{
					Config:      protectionConfig(fmt.Sprintf("default_branch_protected = %t\nbranch = { protected = %t }", legacy, !legacy)),
					ExpectError: regexp.MustCompile("Conflicting branch protection"),
				}},
			})
			api.mu.Lock()
			defer api.mu.Unlock()
			require.Zero(t, api.creates, "conflicting input must fail before creation")
		})
	}
}

// Unknown configuration must remain unknown in the plan, not silently fall
// back to false or the previous value. Once values resolve, conflicts must
// still be rejected before any API call.
func TestProjectProtectionUnknownPlans(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		legacy any
		branch any
	}{
		{"legacy unknown", tftypes.UnknownValue, nil},
		{"nested unknown", nil, map[string]any{"protected": tftypes.UnknownValue}},
		{"branch object unknown", nil, tftypes.UnknownValue},
		{"both unknown", tftypes.UnknownValue, map[string]any{"protected": tftypes.UnknownValue}},
		{"legacy true and nested unknown", true, map[string]any{"protected": tftypes.UnknownValue}},
		{"legacy false and nested unknown", false, map[string]any{"protected": tftypes.UnknownValue}},
		{"legacy unknown and nested true", tftypes.UnknownValue, map[string]any{"protected": true}},
		{"legacy unknown and nested false", tftypes.UnknownValue, map[string]any{"protected": false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			server := NewServer("test")
			schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
			require.NoError(t, err)
			s := schemas.ResourceSchemas["neon_project"]
			ty := s.ValueType().(tftypes.Object)
			attrs := map[string]tftypes.Value{}
			for k, typ := range ty.AttributeTypes {
				attrs[k] = tftypes.NewValue(typ, nil)
			}
			attrs["name"] = tftypes.NewValue(tftypes.String, "protection")
			attrs["default_branch_protected"] = tftypes.NewValue(tftypes.Bool, tt.legacy)
			branchType := ty.AttributeTypes["branch"].(tftypes.Object)
			if fields, ok := tt.branch.(map[string]any); ok {
				branch := map[string]tftypes.Value{}
				for k, typ := range branchType.AttributeTypes {
					branch[k] = tftypes.NewValue(typ, fields[k])
				}
				attrs["branch"] = tftypes.NewValue(branchType, branch)
			} else {
				attrs["branch"] = tftypes.NewValue(branchType, tt.branch)
			}
			config, err := tfprotov6.NewDynamicValue(ty, tftypes.NewValue(ty, attrs))
			require.NoError(t, err)
			validation, err := server.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{
				TypeName: "neon_project", Config: &config,
			})
			require.NoError(t, err)
			for _, d := range validation.Diagnostics {
				require.NotEqual(t, tfprotov6.DiagnosticSeverityError, d.Severity, "%s: %s", d.Summary, d.Detail)
			}
			prior, err := tfprotov6.NewDynamicValue(ty, tftypes.NewValue(ty, nil))
			require.NoError(t, err)
			planned, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName: "neon_project", Config: &config, PriorState: &prior, ProposedNewState: &config,
			})
			require.NoError(t, err)
			for _, d := range planned.Diagnostics {
				require.NotEqual(t, tfprotov6.DiagnosticSeverityError, d.Severity, "%s: %s", d.Summary, d.Detail)
			}
			v, err := planned.PlannedState.Unmarshal(ty)
			require.NoError(t, err)
			var got map[string]tftypes.Value
			require.NoError(t, v.As(&got))
			if knownLegacy, ok := tt.legacy.(bool); ok {
				require.True(t, got["default_branch_protected"].Equal(tftypes.NewValue(tftypes.Bool, knownLegacy)),
					"explicit known legacy value must not change in the plan")
			} else {
				require.False(t, got["default_branch_protected"].IsKnown(), "legacy plan must stay unknown")
			}
			if fields, ok := tt.branch.(map[string]any); ok {
				var branchAttrs map[string]tftypes.Value
				require.NoError(t, got["branch"].As(&branchAttrs))
				require.True(t, branchAttrs["protected"].Equal(tftypes.NewValue(tftypes.Bool, fields["protected"])),
					"configured nested value must not change in the plan")
			}
		})
	}
}

// A conflict that only resolves at apply must fail before creating a project.
func TestProjectProtectionApplyRejectsConflictsBeforeCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	api := &protectionAPI{branchName: "main"}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	r := &neonProjectResource{}
	var configured fwresource.ConfigureResponse
	r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &configured)
	require.False(t, configured.Diagnostics.HasError())
	var schema fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schema)
	raw, err := tftypes.ValueFromJSONWithOpts([]byte(`{"name":"protection","default_branch_protected":true,"branch":{"protected":false}}`),
		schema.Schema.Type().TerraformType(ctx), tftypes.ValueFromJSONOpts{})
	require.NoError(t, err)
	resp := fwresource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
	r.Create(ctx, fwresource.CreateRequest{Config: tfsdk.Config{Raw: raw, Schema: schema.Schema}}, &resp)
	require.True(t, resp.Diagnostics.HasError())
	api.mu.Lock()
	defer api.mu.Unlock()
	require.Zero(t, api.creates, "resolved conflict must not create a project")
}

func protectionConfig(settings string) string {
	return fmt.Sprintf("resource \"neon_project\" \"this\" {\nname = \"protection\"\n%s\n}\n", settings)
}

// Only the network boundary is faked. GETs reflect the actual protection
// changes sent by the provider, so an ignored PATCH cannot pass the checks.
type protectionAPI struct {
	mu         sync.Mutex
	protected  bool
	branchName string
	creates    int
}

func (f *protectionAPI) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Method == http.MethodPost && req.URL.Path == "/api/v2/projects" {
		f.creates++
	} else if req.Method == http.MethodPatch && req.URL.Path == "/api/v2/projects/p-1/branches/br-1" {
		var body struct {
			Branch struct {
				Protected *bool   `json:"protected"`
				Name      *string `json:"name"`
			} `json:"branch"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		if body.Branch.Protected != nil {
			f.protected = *body.Branch.Protected
		}
		if body.Branch.Name != nil {
			f.branchName = *body.Branch.Name
		}
	} else if req.Method != http.MethodGet && (req.Method != http.MethodDelete || req.URL.Path != "/api/v2/projects/p-1") {
		return nil, fmt.Errorf("unexpected API request: %s %s", req.Method, req.URL.Path)
	}
	branch := map[string]any{"id": "br-1", "project_id": "p-1", "name": f.branchName, "default": true, "protected": f.protected}
	response := map[string]any{
		"project": map[string]any{"id": "p-1", "name": "protection", "org_id": "org-1", "region_id": "aws-us-east-2", "pg_version": 17, "provisioner": "k8s-neonvm", "store_passwords": true, "history_retention_seconds": 86400, "settings": map[string]any{}, "default_endpoint_settings": map[string]any{"suspend_timeout_seconds": 0}},
		"branch":  branch, "branches": []any{branch},
		"endpoints":  []any{map[string]any{"id": "ep-1", "project_id": "p-1", "branch_id": "br-1", "type": "read_write", "region_id": "aws-us-east-2", "host": "ep-1.example", "provisioner": "k8s-neonvm", "settings": map[string]any{}, "autoscaling_limit_min_cu": 0.25, "autoscaling_limit_max_cu": 2}},
		"databases":  []any{map[string]any{"branch_id": "br-1", "name": "neondb", "owner_name": "neondb_owner"}},
		"operations": []any{},
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}
