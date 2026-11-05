package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/require"
)

// Other resources reference these attributes in RequiresReplace attributes.
// If an in-place update plans them as unknown, Terraform replaces those
// resources even though the values never change.
func TestInPlaceUpdateKeepsReferencedAttributes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		typeName string
		prior    string
		// config lists the attributes set in the configuration.
		config map[string]any
		// wantKept lists attributes that must keep their prior value.
		wantKept []string
	}{
		{
			name:     "project rename",
			typeName: "neon_project",
			prior:    planProjectState,
			config:   map[string]any{"name": "renamed", "org_id": "org-1"},
			wantKept: []string{
				"id", "default_branch_id", "default_endpoint_id",
				"database_host", "database_host_pooler", "database_name", "database_user",
			},
		},
		{
			name:     "branch rename",
			typeName: "neon_branch",
			prior:    planBranchState,
			config: map[string]any{
				"project_id": "proj-1", "parent_id": "br-main-1", "name": "dev-renamed",
			},
			wantKept: []string{"id"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			planned, prior := planInPlaceUpdate(t, tt.typeName, tt.prior, tt.config)
			for _, name := range tt.wantKept {
				require.True(t, planned[name].IsKnown(), "%s is planned as unknown", name)
				require.True(t, planned[name].Equal(prior[name]),
					"%s: planned %s, prior %s", name, planned[name], prior[name])
			}
		})
	}
}

// Resource IDs never change on an in-place update, so every id must keep its
// prior state value. The exceptions are IDs built from attributes that can
// change in place.
func TestResourceIDsKeepPriorStateOnUpdate(t *testing.T) {
	t.Parallel()

	idChangesInPlace := map[string]string{
		"neon_database":               "id includes name, which can change in place",
		"neon_branch_backup_schedule": "id is project_id/branch_id, which can change in place",
	}
	keepState := fmt.Sprintf("%T", stringplanmodifier.UseStateForUnknown())

	ctx := context.Background()
	var missing []string
	for _, newResource := range (&Provider{}).Resources(ctx) {
		r := newResource()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "neon"}, &meta)
		if _, ok := idChangesInPlace[meta.TypeName]; ok {
			continue
		}

		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		id, ok := resp.Schema.Attributes["id"].(schema.StringAttribute)
		require.True(t, ok, "%s: id is not a string attribute", meta.TypeName)

		found := false
		for _, m := range id.PlanModifiers {
			if fmt.Sprintf("%T", m) == keepState {
				found = true
			}
		}
		if !found {
			missing = append(missing, meta.TypeName)
		}
	}
	require.Empty(t, missing, "id has no UseStateForUnknown plan modifier")
}

const planProjectState = `{"allowed_ips_protected_branches_only":false,` +
	`"autoscaling_limit_max_cu":2,"autoscaling_limit_min_cu":0.25,` +
	`"block_public_connections":false,"block_vpc_connections":false,` +
	`"branch":{"default":true,"id":"br-main-1","logical_size":30,"name":"main",` +
	`"parent_id":"","parent_lsn":"","parent_timestamp":0,"project_id":"proj-1",` +
	`"protected":false},"compute_provisioner":"k8s-neonvm",` +
	`"database_host":"ep-main-1.example","database_host_pooler":"ep-main-1-pooler.example",` +
	`"database_name":"neondb","database_user":"neondb_owner","default_branch_id":"br-main-1",` +
	`"default_branch_protected":false,"default_endpoint_id":"ep-main-1",` +
	`"enable_logical_replication":false,"hipaa":false,"history_retention_seconds":86400,` +
	`"id":"proj-1","maintenance_window":{"weekdays":[1],"start_time":"01:00","end_time":"02:00"},` +
	`"name":"orig","org_id":"org-1","pg_version":17,"primary_compute":{` +
	`"autoscaling_limit_max_cu":2,"autoscaling_limit_min_cu":0.25,"branch_id":"br-main-1",` +
	`"compute_provisioner":"k8s-neonvm","disabled":false,"enabled_libraries":null,` +
	`"enabled_libraries_use_defaults":null,"host":"ep-main-1.example",` +
	`"host_pooler":"ep-main-1-pooler.example","id":"ep-main-1","name":"",` +
	`"passwordless_access":true,"project_id":"proj-1","region_id":"aws-us-east-2",` +
	`"suspend_timeout_seconds":0,"type":"read_write"},"quota":{"active_time_seconds":0,` +
	`"compute_time_seconds":0,"data_transfer_bytes":0,"logical_size_bytes":0,` +
	`"written_data_bytes":0},"region_id":"aws-us-east-2","store_password":true,` +
	`"suspend_timeout_seconds":0}`

const planBranchState = `{"default":false,"id":"br-dev-1","logical_size":30,"name":"dev",` +
	`"parent_id":"br-main-1","parent_lsn":"0/1","parent_timestamp":1,` +
	`"project_id":"proj-1","protected":false}`

// planInPlaceUpdate calls PlanResourceChange the way Terraform does for an
// in-place update, and returns the planned and prior top-level attributes.
func planInPlaceUpdate(
	t *testing.T, typeName, priorJSON string, config map[string]any,
) (map[string]tftypes.Value, map[string]tftypes.Value) {
	t.Helper()
	ctx := context.Background()
	server := NewServer("unitTest")

	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	resourceSchema, ok := schemas.ResourceSchemas[typeName]
	require.True(t, ok, typeName)
	valueType := resourceSchema.ValueType()

	var prior map[string]any
	require.NoError(t, json.Unmarshal([]byte(priorJSON), &prior))

	// Terraform proposes the config value for configured attributes, keeps the
	// prior value for unset computed attributes, and null for the rest.
	configState := map[string]any{}
	proposed := map[string]any{}
	for _, attr := range resourceSchema.Block.Attributes {
		v, configured := config[attr.Name]
		switch {
		case configured:
			configState[attr.Name] = v
			proposed[attr.Name] = v
		case attr.Computed:
			proposed[attr.Name] = prior[attr.Name]
		}
	}

	toDynamic := func(m map[string]any) *tfprotov6.DynamicValue {
		data, err := json.Marshal(m)
		require.NoError(t, err)
		value, err := tftypes.ValueFromJSONWithOpts(data, valueType,
			tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
		require.NoError(t, err)
		dv, err := tfprotov6.NewDynamicValue(valueType, value)
		require.NoError(t, err)
		return &dv
	}

	resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         typeName,
		PriorState:       toDynamic(prior),
		ProposedNewState: toDynamic(proposed),
		Config:           toDynamic(configState),
	})
	require.NoError(t, err)
	for _, d := range resp.Diagnostics {
		require.NotEqual(t, tfprotov6.DiagnosticSeverityError, d.Severity, "%s: %s", d.Summary, d.Detail)
	}

	planned, err := resp.PlannedState.Unmarshal(valueType)
	require.NoError(t, err)
	var plannedAttrs map[string]tftypes.Value
	require.NoError(t, planned.As(&plannedAttrs))

	priorValue, err := toDynamic(prior).Unmarshal(valueType)
	require.NoError(t, err)
	var priorAttrs map[string]tftypes.Value
	require.NoError(t, priorValue.As(&priorAttrs))

	// The test is only meaningful if the provider planned an in-place change.
	require.False(t, planned.Equal(priorValue), "no change was planned")
	return plannedAttrs, priorAttrs
}
