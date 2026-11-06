package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// State captured from resources applied with kislerdm/neon v0.18.0; credentials are redacted.
var legacyV018States = []struct {
	typeName string
	version  int64
	state    string
}{
	{typeName: "neon_api_key", version: 1, state: `{"id": "1234", "key": "redacted", "name": "smoke"}`},
	{typeName: "neon_branch", version: 8, state: `{"id": "br-rough-wind-bdq2x30b", "logical_size": null, "name": "dev", "parent_id": "br-billowing-lake-bd9t282r", "parent_lsn": "0/19C1540", "parent_timestamp": null, "project_id": "quiet-dew-02055492", "protected": "no"}`},
	{typeName: "neon_database", version: 7, state: `{"branch_id": "br-rough-wind-bdq2x30b", "id": "quiet-dew-02055492/br-rough-wind-bdq2x30b/smoke_db", "name": "smoke_db", "owner_name": "smoke_role", "project_id": "quiet-dew-02055492"}`},
	{typeName: "neon_endpoint", version: 8, state: `{"autoscaling_limit_max_cu": 0.5, "autoscaling_limit_min_cu": 0.25, "branch_id": "br-rough-wind-bdq2x30b", "compute_provisioner": "k8s-neonvm", "disabled": false, "host": "ep-steep-hall-bdn09u1c.c-8.us-east-2.aws.neon.tech", "host_pooling": "ep-steep-hall-bdn09u1c-pooler.c-8.us-east-2.aws.neon.tech", "id": "ep-steep-hall-bdn09u1c", "name": null, "pg_settings": null, "project_id": "quiet-dew-02055492", "proxy_host": "c-8.us-east-2.aws.neon.tech", "region_id": "aws-us-east-2", "suspend_timeout_seconds": 0, "type": "read_write"}`},
	{typeName: "neon_jwks_url", version: 0, state: `{"branch_id": "", "id": "019330ba-c1b7-46a4-974c-75e787e50b87", "jwks_url": "https://www.googleapis.com/oauth2/v3/certs", "jwt_audience": "", "project_id": "quiet-dew-02055492", "provider_name": "smoke", "role_names": ["app_owner"]}`},
	{typeName: "neon_org_api_key", version: 1, state: `{"id": "5678", "key": "redacted", "name": "smoke", "org_id": "org-sparkling-term-69792114", "project_id": null}`},
	{typeName: "neon_project", version: 11, state: `{"allowed_ips": null, "allowed_ips_protected_branches_only": null, "autoscaling_limit_max_cu": 1, "autoscaling_limit_min_cu": 0.25, "block_public_connections": null, "block_vpc_connections": null, "branch": [{"database_name": "app", "id": "br-billowing-lake-bd9t282r", "name": "main", "role_name": "app_owner"}], "compute_provisioner": "k8s-neonvm", "connection_uri": "redacted", "connection_uri_pooler": "redacted", "database_host": "ep-mute-smoke-bd9jzxyt.c-8.us-east-2.aws.neon.tech", "database_host_pooler": "ep-mute-smoke-bd9jzxyt-pooler.c-8.us-east-2.aws.neon.tech", "database_name": "app", "database_password": "redacted", "database_user": "app_owner", "default_branch_id": "br-billowing-lake-bd9t282r", "default_branch_protected": false, "default_endpoint_id": "ep-mute-smoke-bd9jzxyt", "enable_logical_replication": "no", "hipaa": null, "history_retention_seconds": 21600, "id": "quiet-dew-02055492", "maintenance_window": [{"end_time": "09:00", "start_time": "08:00", "weekdays": [5]}], "name": "tf-smoke-upgrade-1793819311", "org_id": "org-sparkling-term-69792114", "pg_version": 18, "primary_compute": [{"autoscaling_limit_max_cu": 1, "autoscaling_limit_min_cu": 0.25, "id": "ep-mute-smoke-bd9jzxyt", "name": "", "suspend_timeout_seconds": 0}], "quota": [{"active_time_seconds": null, "compute_time_seconds": null, "data_transfer_bytes": null, "logical_size_bytes": null, "written_data_bytes": null}], "region_id": "aws-us-east-2", "store_password": "yes", "suspend_timeout_seconds": 0}`},
	{typeName: "neon_project_permission", version: 1, state: `{"grantee": "someone@example.com", "id": "perm-1", "project_id": "quiet-dew-02055492"}`},
	{typeName: "neon_role", version: 7, state: `{"branch_id": "br-rough-wind-bdq2x30b", "id": "quiet-dew-02055492/br-rough-wind-bdq2x30b/smoke_role", "name": "smoke_role", "password": "redacted", "project_id": "quiet-dew-02055492", "protected": false}`},
}

func TestUpgradeResourceState_v018(t *testing.T) {
	ctx := context.Background()
	server := NewServer("test")

	schemaResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}

	upgraded := map[string]map[string]tftypes.Value{}
	for _, tc := range legacyV018States {
		t.Run(tc.typeName, func(t *testing.T) {
			resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
				TypeName: tc.typeName,
				Version:  tc.version,
				RawState: &tfprotov6.RawState{JSON: []byte(tc.state)},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range resp.Diagnostics {
				t.Errorf("%s: %s", d.Summary, d.Detail)
			}
			if t.Failed() {
				return
			}

			typ := schemaResp.ResourceSchemas[tc.typeName].ValueType()
			val, err := resp.UpgradedState.Unmarshal(typ)
			if err != nil {
				t.Fatal(err)
			}
			var attrs map[string]tftypes.Value
			if err := val.As(&attrs); err != nil {
				t.Fatal(err)
			}
			upgraded[tc.typeName] = attrs
		})
	}

	assertBool := func(t *testing.T, v tftypes.Value, want bool) {
		t.Helper()
		var got bool
		if err := v.As(&got); err != nil || got != want {
			t.Errorf("got %v (err %v), want %v", v, err, want)
		}
	}

	t.Run("neon_project blocks and flags", func(t *testing.T) {
		p := upgraded["neon_project"]
		var branch map[string]tftypes.Value
		if err := p["branch"].As(&branch); err != nil {
			t.Fatalf("branch is not an object: %v", err)
		}
		var id string
		if err := branch["id"].As(&id); err != nil || id != "br-billowing-lake-bd9t282r" {
			t.Errorf("branch.id = %q (err %v)", id, err)
		}
		assertBool(t, p["enable_logical_replication"], false)
		assertBool(t, p["store_password"], true)
		if !p["hipaa"].IsNull() {
			t.Errorf("hipaa = %v, want null", p["hipaa"])
		}
	})

	t.Run("neon_branch protected", func(t *testing.T) {
		assertBool(t, upgraded["neon_branch"]["protected"], false)
	})

	t.Run("neon_jwks_url empty strings", func(t *testing.T) {
		for _, k := range []string{"branch_id", "jwt_audience"} {
			if v := upgraded["neon_jwks_url"][k]; !v.IsNull() {
				t.Errorf("%s = %v, want null", k, v)
			}
		}
	})
}

// Neon no longer provisions k8s-pod, so only projects created by earlier versions carry it in state.
func TestProjectComputeProvisionerKeepsStateWhenOmitted(t *testing.T) {
	var schemaResp resource.SchemaResponse
	neonProjectResource{}.Schema(context.Background(), resource.SchemaRequest{}, &schemaResp)
	attr, ok := schemaResp.Schema.Attributes["compute_provisioner"].(schema.StringAttribute)
	if !ok {
		t.Fatal("compute_provisioner is not a string attribute")
	}
	if attr.Default != nil {
		t.Fatal("a default would replace every project whose state holds another provisioner")
	}

	raw := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	req := planmodifier.StringRequest{
		Path:        path.Root("compute_provisioner"),
		ConfigValue: types.StringNull(),
		StateValue:  types.StringValue("k8s-pod"),
		PlanValue:   types.StringUnknown(),
		Plan:        tfsdk.Plan{Raw: raw},
		State:       tfsdk.State{Raw: raw},
	}
	resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
	for _, modifier := range attr.PlanModifiers {
		req.PlanValue = resp.PlanValue
		modifier.PlanModifyString(context.Background(), req, &resp)
	}
	if resp.PlanValue.ValueString() != "k8s-pod" || resp.RequiresReplace {
		t.Errorf("plan = %s, requires replace = %t; want k8s-pod kept without replacement", resp.PlanValue, resp.RequiresReplace)
	}
}

// A failed rollout cannot be produced against the live API on demand, so the plan is checked through the protocol.
func TestFunctionPlanRedeploysPendingUpload(t *testing.T) {
	ctx := context.Background()
	server := NewServer("test")
	schemaResp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	typ := schemaResp.ResourceSchemas["neon_function"].ValueType()
	value := func(computed string) *tfprotov6.DynamicValue {
		return &tfprotov6.DynamicValue{JSON: []byte(`{"project_id": "p", "branch_id": "br", "slug": "hello",
			"runtime": "nodejs24", "name": "hello", "zip_file_path": "/tmp/function.zip", "source_code_hash": null,
			"environment_variables": {"FOO": "1"}, ` + computed + `}`)}
	}
	prior := value(`"id": "p/br/hello", "created_at": "2026-11-06T00:00:00Z", "invocation_url": "https://hello.test",
		"current_deployment_id": 7, "environment_variable_names": ["FOO"]`)
	config := value(`"id": null, "created_at": null, "invocation_url": null, "current_deployment_id": null,
		"environment_variable_names": null`)
	pending, err := json.Marshal(map[string][]byte{pendingUploadPrivateKey: []byte(`["BAR"]`)})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name         string
		private      []byte
		wantRedeploy bool
	}{
		{name: "no pending upload", private: nil, wantRedeploy: false},
		{name: "pending upload", private: pending, wantRedeploy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName:         "neon_function",
				PriorState:       prior,
				ProposedNewState: prior,
				Config:           config,
				PriorPrivate:     tc.private,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range resp.Diagnostics {
				t.Fatalf("%s: %s", d.Summary, d.Detail)
			}
			planned, err := resp.PlannedState.Unmarshal(typ)
			if err != nil {
				t.Fatal(err)
			}
			var attrs map[string]tftypes.Value
			if err := planned.As(&attrs); err != nil {
				t.Fatal(err)
			}
			if redeploy := !attrs["current_deployment_id"].IsKnown(); redeploy != tc.wantRedeploy {
				t.Errorf("plan redeploys = %t, want %t", redeploy, tc.wantRedeploy)
			}
		})
	}
}
