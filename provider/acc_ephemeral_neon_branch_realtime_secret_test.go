//go:build acceptance

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/stretchr/testify/require"
)

type branchRealtimeSecretAbsentPlanCheck struct{ secret *string }

func (c branchRealtimeSecretAbsentPlanCheck) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	encoded, err := json.Marshal(req.Plan)
	if err != nil {
		resp.Error = fmt.Errorf("cannot inspect plan (redacted)")
		return
	}
	if *c.secret != "" && bytes.Contains(encoded, []byte(*c.secret)) {
		resp.Error = fmt.Errorf("ephemeral secret found in persistent plan (redacted)")
	}
}
func TestAccEphemeralNeonBranchRealtimeSecret(t *testing.T) {
	client, orgID := branchRealtimeAccPrerequisites(t)
	name := branchRealtimeAccProject(t, client, orgID)
	var identity branchRealtimeAccIdentity
	var expected string
	config := newBranchRealtimeAccConfig(name, orgID, "neon_project.this.branch.id", "", "")
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), TerraformVersionChecks: []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_10_0)}, CheckDestroy: branchRealtimeAccDestroy(client), Steps: []resource.TestStep{
		{Config: config, Check: branchRealtimeAccCheck(client, &identity)},
		{PreConfig: func() {
			p, b := identity.paths()
			remote, err := client.GetProjectBranchRealtimeSecret(p, b)
			require.True(t, err == nil, "independent secret read failed (redacted)")
			expected = remote.Secret
			require.True(t, expected != "", "independent secret empty (redacted)")
			// Ephemeral variables are process-local and excluded from Terraform plan/state.
			t.Setenv("TF_VAR_expected_secret", expected)
			t.Setenv("TF_VAR_expected_pending", fmt.Sprint(remote.Pending))
		}, Config: newBranchRealtimeSecretAccConfig(config), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{branchRealtimeSecretAbsentPlanCheck{secret: &expected}}}, Check: func(state *terraform.State) error {
			encoded, err := json.Marshal(state)
			if err != nil {
				return fmt.Errorf("cannot inspect persistent state (redacted)")
			}
			if bytes.Contains(encoded, []byte(expected)) {
				return fmt.Errorf("ephemeral secret found in persistent state (redacted)")
			}
			return branchRealtimeAccCheck(client, nil)(state)
		}},
	}})
}
func newBranchRealtimeSecretAccConfig(base string) string {
	return base + `
variable "expected_secret" {
 type = string
 ephemeral = true
 sensitive = true
}
variable "expected_pending" {
 type = bool
 ephemeral = true
}
ephemeral "neon_branch_realtime_secret" "this" {
 project_id = neon_project.this.id
 branch_id = neon_project.this.branch.id
 depends_on = [neon_branch_realtime.this]
 lifecycle {
  postcondition {
   condition = self.secret == var.expected_secret && self.pending == var.expected_pending
   error_message = "Independent Realtime secret comparison failed (redacted)."
  }
 }
}
`
}
