package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

type initSourceFixture struct{ mode string }

func (f initSourceFixture) Do(req *http.Request) (*http.Response, error) {
	path := "/api/v2/projects/cool-moon-42/branches"
	reported := f.mode
	if reported == "" {
		reported = "parent-data"
	}
	branch := map[string]any{"id": "br-result", "project_id": "cool-moon-42", "name": "test", "init_source": reported}
	if reported != "schema-only" {
		branch["parent_id"] = "br-source"
	}
	switch req.Method {
	case http.MethodPost:
		var payload struct{ Branch map[string]any }
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			return nil, err
		}
		got, ok := payload.Branch["init_source"]
		if ok != (f.mode != "") || (ok && got != f.mode) || payload.Branch["parent_id"] != "br-source" {
			return nil, fmt.Errorf("unexpected branch create payload: %v", payload.Branch)
		}
	case http.MethodGet, http.MethodDelete:
		path += "/br-result"
	default:
		return nil, fmt.Errorf("unexpected method %s", req.Method)
	}
	if req.URL.Path != path {
		return nil, fmt.Errorf("unexpected path %s", req.URL.Path)
	}
	data, err := json.Marshal(map[string]any{"branch": branch, "operations": []any{}})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: http.Header{"Content-Type": {"application/json"}}}, nil
}

func TestBranchInitSourceLifecycle(t *testing.T) {
	const addr = "neon_branch.this"
	for _, tc := range []struct{ name, mode, want string }{{"omitted", "", "parent-data"}, {"child", "parent-schema", "parent-schema"}, {"root", "schema-only", "schema-only"}, {"explicit data", "parent-data", "parent-data"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, err := neon.NewClient(neon.Config{Key: "unused", HTTPClient: initSourceFixture{tc.mode}})
			require.NoError(t, err)
			cfg := testBranchInitSourceConfig(tc.mode)
			other := "parent-schema"
			if tc.mode == other {
				other = "parent-data"
			}
			importStep := resource.TestStep{ResourceName: addr, ImportState: true, ImportStateId: "cool-moon-42/br-result", ImportStateVerify: true}
			if tc.mode == "schema-only" {
				importStep.ImportStateVerifyIgnore = []string{"parent_id"}
			}
			steps := []resource.TestStep{
				{Config: cfg, Check: resource.TestCheckResourceAttr(addr, "init_source", tc.want)},
				{Config: testBranchInitSourceConfig(""), PlanOnly: true},
				importStep,
				{Config: testBranchInitSourceConfig(other), PlanOnly: true, ExpectNonEmptyPlan: true, ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionReplace)},
				}},
			}
			resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}), Steps: steps})
		})
	}
}

func TestBranchInitSourceExcludesImport(t *testing.T) {
	t.Parallel()
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), Steps: []resource.TestStep{{Config: testBranchInitSourceConfig("import"), PlanOnly: true, ExpectError: regexp.MustCompile(`value must be one of`)}}})
}
