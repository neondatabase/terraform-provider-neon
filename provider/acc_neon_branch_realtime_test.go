//go:build acceptance

package provider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

type branchRealtimeAccIdentity struct{ projectID, branchID string }

func (i branchRealtimeAccIdentity) paths() (string, string) {
	return url.PathEscape(i.projectID), url.PathEscape(i.branchID)
}

func branchRealtimeAccPrerequisites(t *testing.T) (*neon.Client, string) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	orgID := os.Getenv("ORG_ID")
	if orgID == "" || os.Getenv("NEON_API_KEY") == "" {
		t.Skip("ORG_ID and NEON_API_KEY must be set")
	}
	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	require.NoError(t, err)
	return client, orgID
}
func branchRealtimeAccProject(t *testing.T, client *neon.Client, orgID string) string {
	t.Helper()
	name := newProjectName("branchRealtimeAcc")
	t.Cleanup(func() {
		response, err := client.ListProjects(nil, nil, &name, &orgID, nil, nil)
		if err != nil {
			t.Errorf("cleanup list: %v", err)
			return
		}
		for _, p := range response.Projects {
			if p.Name != name {
				continue
			}
			if _, err := client.DeleteProject(url.PathEscape(p.ID)); err != nil && !realtimeNotFound(err) {
				t.Errorf("cleanup project: %v", err)
			}
		}
	})
	return name
}
func newBranchRealtimeAccConfig(name, orgID, branch, options, extra string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
 name = %q
 org_id = %q
 region_id = "aws-us-east-2"
}
%s
resource "neon_branch_realtime" "this" {
 project_id = neon_project.this.id
 branch_id = %s
 %s
}
`, name, orgID, extra, branch, options)
}
func branchRealtimeAccBranches() string {
	return `resource "neon_branch" "first" {
 project_id = neon_project.this.id
 name = "realtime-first"
}
resource "neon_branch" "second" {
 project_id = neon_project.this.id
 name = "realtime-second"
}
`
}
func branchRealtimeAccCheck(client *neon.Client, identity *branchRealtimeAccIdentity) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources["neon_branch_realtime.this"]
		if !ok {
			return fmt.Errorf("Realtime resource missing from state")
		}
		attrs := r.Primary.Attributes
		i := branchRealtimeAccIdentity{attrs["project_id"], attrs["branch_id"]}
		if identity != nil {
			*identity = i
		}
		return branchRealtimeAccVerifyAttributes(client, attrs)
	}
}
func branchRealtimeAccVerifyAttributes(client *neon.Client, attrs map[string]string) error {
	i := branchRealtimeAccIdentity{attrs["project_id"], attrs["branch_id"]}
	if attrs["id"] != i.projectID+"/"+i.branchID {
		return fmt.Errorf("composite identity mismatch")
	}
	project, branch := i.paths()
	remote, err := client.GetProjectBranchRealtime(project, branch)
	if err != nil {
		return err
	}
	if !remote.Enabled || remote.Pending || attrs["enabled"] != "true" {
		return fmt.Errorf("Realtime not enabled and complete")
	}
	wantURL := ""
	if remote.InvocationURL != nil {
		wantURL = *remote.InvocationURL
	}
	if attrs["invocation_url"] != wantURL {
		return fmt.Errorf("invocation URL differs")
	}
	wantRevision := ""
	if remote.Revision != nil {
		wantRevision = strconv.Itoa(int(*remote.Revision))
	}
	if attrs["revision"] != wantRevision {
		return fmt.Errorf("revision differs")
	}
	if count, ok := attrs["allowed_origins.#"]; ok && count != "" {
		n, err := strconv.Atoi(count)
		if err != nil {
			return err
		}
		origins := make([]string, n)
		for j := range origins {
			origins[j] = attrs[fmt.Sprintf("allowed_origins.%d", j)]
		}
		if remote.AllowedOrigins == nil {
			return fmt.Errorf("backend omitted managed origins; independent policy verification unavailable")
		}
		if !reflect.DeepEqual(origins, remote.AllowedOrigins) && !(unrestrictedOrigins(origins) && unrestrictedOrigins(remote.AllowedOrigins)) {
			return fmt.Errorf("managed origins differ")
		}
	}
	return nil
}

func branchRealtimeAccAbsent(client *neon.Client, i branchRealtimeAccIdentity) error {
	project, branch := i.paths()
	remote, err := client.GetProjectBranchRealtime(project, branch)
	if err != nil && !realtimeNotFound(err) {
		return err
	}
	if err == nil && remote.Enabled {
		return fmt.Errorf("Realtime still enabled after disable")
	}
	_, err = client.GetProjectBranchRealtimeSecret(project, branch)
	if !realtimeNotFound(err) {
		return fmt.Errorf("expected secret absence after disable (response redacted)")
	}
	return nil
}
func branchRealtimeAccDestroy(client *neon.Client) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		for _, r := range state.RootModule().Resources {
			if r.Type == "neon_branch_realtime" {
				if err := branchRealtimeAccAbsent(client, branchRealtimeAccIdentity{r.Primary.Attributes["project_id"], r.Primary.Attributes["branch_id"]}); err != nil {
					return err
				}
			}
		}
		return nil
	}
}
func branchRealtimeAccDisable(t *testing.T, client *neon.Client, i branchRealtimeAccIdentity) {
	t.Helper()
	project, branch := i.paths()
	err := client.DisableProjectBranchRealtime(project, branch)
	if !realtimeNotFound(err) {
		require.NoError(t, err)
	}
	ctx, cancel := realtimeContext(context.Background())
	defer cancel()
	require.NoError(t, realtimePoll(ctx, realtimePollInterval, func(context.Context) (bool, error) {
		remote, err := client.GetProjectBranchRealtime(project, branch)
		if realtimeNotFound(err) {
			t.Log("Disabled GET shape: 404")
			return true, nil
		}
		if err == nil && !remote.Enabled {
			t.Logf("Disabled GET shape: 200, pending=%t", remote.Pending)
		}
		return err == nil && !remote.Enabled, err
	}))
	require.NoError(t, branchRealtimeAccAbsent(client, i))
}
func TestAccNeonBranchRealtime(t *testing.T) {
	client, orgID := branchRealtimeAccPrerequisites(t)
	t.Run("default branch origins boundaries import and no-op", func(t *testing.T) {
		name := branchRealtimeAccProject(t, client, orgID)
		var identity branchRealtimeAccIdentity
		config := func(options string) string {
			return newBranchRealtimeAccConfig(name, orgID, "neon_project.this.branch.id", options, "")
		}
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
			{Config: config(`allowed_origins = ["https://first.example"]`), Check: branchRealtimeAccCheck(client, nil)},
			{Config: config(`allowed_origins = ["https://second.example"]`), Check: branchRealtimeAccCheck(client, nil)},
			{Config: config(`allowed_origins = []`), Check: branchRealtimeAccCheck(client, nil)},
			{Config: config(`allowed_origins = ["*"]`), Check: branchRealtimeAccCheck(client, &identity)},
			{Config: config(""), Check: resource.ComposeTestCheckFunc(branchRealtimeAccCheck(client, nil),
				resource.TestCheckNoResourceAttr("neon_branch_realtime.this", "allowed_origins.#"),
				func(*terraform.State) error {
					p, b := identity.paths()
					remote, err := client.GetProjectBranchRealtime(p, b)
					if err != nil {
						return err
					}
					if remote.AllowedOrigins == nil || !unrestrictedOrigins(remote.AllowedOrigins) {
						return fmt.Errorf("relinquishing origins changed the previous unrestricted policy")
					}
					return nil
				})},
			{Config: config(""), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{ResourceName: "neon_branch_realtime.this", ImportState: true, ImportStateVerify: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected one imported Realtime resource")
					}
					attrs := states[0].Attributes
					if attrs["allowed_origins.#"] != "" {
						return fmt.Errorf("imported origins must remain unmanaged")
					}
					return branchRealtimeAccVerifyAttributes(client, attrs)
				}},
			{Config: config(""), PlanOnly: true, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{Config: config(`allowed_origins = ["https://managed.example"]`), Check: branchRealtimeAccCheck(client, nil)},
		}})
	})
	t.Run("arbitrary branch replacement and old scope disable", func(t *testing.T) {
		name := branchRealtimeAccProject(t, client, orgID)
		var old branchRealtimeAccIdentity
		config := func(branch string) string {
			return newBranchRealtimeAccConfig(name, orgID, branch, `allowed_origins = []`, branchRealtimeAccBranches())
		}
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
			{Config: config("neon_branch.first.id"), Check: branchRealtimeAccCheck(client, &old)},
			{Config: config("neon_branch.second.id"), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("neon_branch_realtime.this", plancheck.ResourceActionDestroyBeforeCreate)}}, Check: resource.ComposeTestCheckFunc(branchRealtimeAccCheck(client, nil), func(*terraform.State) error { return branchRealtimeAccAbsent(client, old) })},
		}})
	})
	t.Run("project identity replacement", func(t *testing.T) {
		name := branchRealtimeAccProject(t, client, orgID)
		other := branchRealtimeAccProject(t, client, orgID)
		var old branchRealtimeAccIdentity
		extra := fmt.Sprintf("resource \"neon_project\" \"other\" {\n name = %q\n org_id = %q\n region_id = \"aws-us-east-2\"\n}\n", other, orgID)
		original := newBranchRealtimeAccConfig(name, orgID, "neon_project.this.branch.id", "", extra)
		changed := newBranchRealtimeAccConfig(name, orgID, "neon_project.other.branch.id", "", extra)
		// Change both scope components consistently: branch IDs are project-scoped.
		changed = regexp.MustCompile(`project_id = neon_project.this.id`).ReplaceAllString(changed, "project_id = neon_project.other.id")
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
			{Config: original, Check: branchRealtimeAccCheck(client, &old)},
			{Config: changed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("neon_branch_realtime.this", plancheck.ResourceActionDestroyBeforeCreate)}}, Check: resource.ComposeTestCheckFunc(branchRealtimeAccCheck(client, nil), func(*terraform.State) error { return branchRealtimeAccAbsent(client, old) })},
		}})
	})
	for _, branchDeletion := range []bool{false, true} {
		t.Run(map[bool]string{true: "external branch deletion", false: "external disable and already absent destroy"}[branchDeletion], func(t *testing.T) {
			name := branchRealtimeAccProject(t, client, orgID)
			var identity branchRealtimeAccIdentity
			config := newBranchRealtimeAccConfig(name, orgID, "neon_branch.first.id", "", branchRealtimeAccBranches())
			remove := func() {
				if branchDeletion {
					p, b := identity.paths()
					_, err := client.DeleteProjectBranch(p, b)
					require.NoError(t, err)
				} else {
					branchRealtimeAccDisable(t, client, identity)
				}
			}
			resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
				{Config: config, Check: branchRealtimeAccCheck(client, &identity)},
				{PreConfig: remove, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources["neon_branch_realtime.this"]; ok {
						return fmt.Errorf("absent Realtime remains in state")
					}
					return branchRealtimeAccAbsent(client, identity)
				}},
				{Config: config, Check: branchRealtimeAccCheck(client, &identity)},
				{PreConfig: remove, Config: config, Destroy: true},
			}})
		})
	}
	t.Run("server invalid origin and failed apply recovery", func(t *testing.T) {
		name := branchRealtimeAccProject(t, client, orgID)
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
			{Config: newBranchRealtimeAccConfig(name, orgID, "neon_project.this.branch.id", `allowed_origins = ["not-an-origin"]`, ""), ExpectError: regexp.MustCompile(`\[HTTP Code: (400|422)\]`)},
			{Config: newBranchRealtimeAccConfig(name, orgID, "neon_project.this.branch.id", "", ""), Check: branchRealtimeAccCheck(client, nil)},
		}})
	})
	t.Run("inherited adoption and destructive child disable", func(t *testing.T) {
		name := branchRealtimeAccProject(t, client, orgID)
		var child branchRealtimeAccIdentity
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
			{Config: newBranchRealtimeInheritanceConfig(name, orgID, false), Check: func(state *terraform.State) error {
				attrs := state.RootModule().Resources["neon_branch.child"].Primary.Attributes
				child = branchRealtimeAccIdentity{attrs["project_id"], attrs["id"]}
				p, b := child.paths()
				remote, err := client.GetProjectBranchRealtime(p, b)
				if err != nil {
					return err
				}
				if !remote.Enabled {
					return errors.New("child did not inherit enabled Realtime")
				}
				return nil
			}},
			{Config: newBranchRealtimeInheritanceConfig(name, orgID, true), Check: branchRealtimeAccCheck(client, nil)},
			{Config: newBranchRealtimeInheritanceConfig(name, orgID, false), Check: func(*terraform.State) error { return branchRealtimeAccAbsent(client, child) }},
		}})
	})
}
func newBranchRealtimeInheritanceConfig(name, orgID string, adopt bool) string {
	config := fmt.Sprintf(`resource "neon_project" "this" {
 name = %q
 org_id = %q
 region_id = "aws-us-east-2"
}
resource "neon_branch_realtime" "parent" {
 project_id = neon_project.this.id
 branch_id = neon_project.this.branch.id
 allowed_origins = ["*"]
}
resource "neon_branch" "child" {
 project_id = neon_project.this.id
 parent_id = neon_project.this.branch.id
 name = "inherited-realtime"
 depends_on = [neon_branch_realtime.parent]
}
`, name, orgID)
	if adopt {
		config += `resource "neon_branch_realtime" "this" {
 project_id = neon_project.this.id
 branch_id = neon_branch.child.id
 allowed_origins = ["https://child.example"]
}
`
	}
	return config
}
