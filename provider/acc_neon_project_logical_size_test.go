package provider

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	neon "github.com/kislerdm/neon-sdk-go"
)

// TestAccProjectUpdateKeepsPlannedLogicalSize calls Update directly, because Terraform refreshes before every
// acceptance test plan and the branch's logical size can't be made to change between that refresh and the apply.
func TestAccProjectUpdateKeepsPlannedLogicalSize(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	ctx := context.Background()
	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}
	name := newProjectName("plannedLogicalSize")
	cfg := neon.ProjectCreateRequest{Project: neon.ProjectCreateRequestProject{Name: &name}}
	if orgID := os.Getenv("ORG_ID"); orgID != "" {
		cfg.Project.OrgID = &orgID
	}
	created, err := client.CreateProject(cfg)
	if err != nil {
		t.Fatal(err)
	}
	projectID := created.ProjectResponse.Project.ID
	t.Cleanup(func() { _, _ = client.DeleteProject(projectID) })
	if err := waitUnfinishedOperations(ctx, client, created.OperationsResponse.Operations); err != nil {
		t.Fatal(err)
	}

	r := neonProjectResource{client: client}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	sch := schemaResp.Schema
	empty := tftypes.NewValue(sch.Type().TerraformType(ctx), nil)

	// GIVEN the project's state as Terraform imports it
	importResp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch, Raw: empty}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: projectID}, &importResp)
	if importResp.Diagnostics.HasError() {
		t.Fatal(importResp.Diagnostics)
	}

	// AND a plan that renames it and carries a logical size Neon no longer reports
	const plannedSize = int64(1)
	newName := name + "-renamed"
	plan := tfsdk.Plan{Schema: sch, Raw: importResp.State.Raw.Copy()}
	diags := plan.SetAttribute(ctx, path.Root("name"), newName)
	diags.Append(plan.SetAttribute(ctx, path.Root("branch").AtName("logical_size"), plannedSize)...)
	config := tfsdk.Plan{Schema: sch, Raw: empty}
	diags.Append(config.Set(ctx, &neonProjectResourceModel{
		Name:             types.StringValue(newName),
		AllowedIPs:       types.ListNull(types.StringType),
		EnabledLibraries: types.ListNull(types.StringType),
	})...)
	if diags.HasError() {
		t.Fatal(diags)
	}

	// WHEN Update applies the plan
	updateResp := resource.UpdateResponse{State: importResp.State}
	r.Update(ctx, resource.UpdateRequest{
		Config: tfsdk.Config{Schema: sch, Raw: config.Raw},
		Plan:   plan,
		State:  importResp.State,
	}, &updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatal(updateResp.Diagnostics)
	}

	// THEN the new state keeps the planned size, as Terraform requires
	var got types.Int64
	if diags := updateResp.State.GetAttribute(ctx, path.Root("branch").AtName("logical_size"), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.ValueInt64() != plannedSize || got.IsNull() {
		t.Errorf("branch.logical_size after apply = %s, want the planned %d", got, plannedSize)
	}
}
