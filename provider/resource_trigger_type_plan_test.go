package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/require"
)

func TestTriggerTypeChangeRequiresReplacement(t *testing.T) {
	tests := []struct {
		name        string
		priorType   string
		plannedType string
		priorCron   any
		plannedCron any
		priorBucket any
		planBucket  any
		wantReplace []*tftypes.AttributePath
	}{
		{
			name:        "schedule_to_storage_object_created",
			priorType:   "schedule",
			plannedType: "storage_object_created",
			priorCron:   "0 3 * * *",
			planBucket:  "uploads",
			wantReplace: []*tftypes.AttributePath{
				tftypes.NewAttributePath().WithAttributeName("type"),
			},
		},
		{
			name:        "storage_object_created_to_schedule",
			priorType:   "storage_object_created",
			plannedType: "schedule",
			plannedCron: "0 3 * * *",
			priorBucket: "uploads",
			wantReplace: []*tftypes.AttributePath{
				tftypes.NewAttributePath().WithAttributeName("type"),
			},
		},
		{
			name:        "schedule_cron_update",
			priorType:   "schedule",
			plannedType: "schedule",
			priorCron:   "0 3 * * *",
			plannedCron: "0 4 * * *",
			wantReplace: []*tftypes.AttributePath{},
		},
		{
			name:        "unchanged_schedule",
			priorType:   "schedule",
			plannedType: "schedule",
			priorCron:   "0 3 * * *",
			plannedCron: "0 3 * * *",
			wantReplace: []*tftypes.AttributePath{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			server := NewServer("unitTest")
			schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
			require.NoError(t, err)
			resourceSchema, ok := schemas.ResourceSchemas["neon_trigger"]
			require.True(t, ok, "neon_trigger schema missing")
			valueType := resourceSchema.ValueType()

			prior := map[string]any{
				"id": "proj-1/br-1/trig-1", "project_id": "proj-1", "branch_id": "br-1",
				"name": "nightly", "type": test.priorType, "function_slug": "worker",
				"function_path": "/", "enabled": true, "cron": test.priorCron,
				"bucket_name": test.priorBucket, "prefix": nil, "trigger_id": "trig-1",
				"version": int64(1), "inherited": false,
			}
			config := map[string]any{
				"project_id": "proj-1", "branch_id": "br-1", "name": "nightly",
				"type": test.plannedType, "function_slug": "worker", "function_path": "/",
				"enabled": true, "cron": test.plannedCron, "bucket_name": test.planBucket,
				"prefix": nil,
			}
			proposed := make(map[string]any, len(prior))
			for name, value := range prior {
				proposed[name] = value
			}
			for name, value := range config {
				proposed[name] = value
			}

			toDynamic := func(value map[string]any) *tfprotov6.DynamicValue {
				data, marshalErr := json.Marshal(value)
				require.NoError(t, marshalErr)
				terraformValue, parseErr := tftypes.ValueFromJSONWithOpts(
					data,
					valueType,
					tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true},
				)
				require.NoError(t, parseErr)
				dynamicValue, dynamicErr := tfprotov6.NewDynamicValue(valueType, terraformValue)
				require.NoError(t, dynamicErr)
				return &dynamicValue
			}

			response, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName:         "neon_trigger",
				PriorState:       toDynamic(prior),
				ProposedNewState: toDynamic(proposed),
				Config:           toDynamic(config),
			})
			require.NoError(t, err)
			for _, diagnostic := range response.Diagnostics {
				require.NotEqual(
					t,
					tfprotov6.DiagnosticSeverityError,
					diagnostic.Severity,
					"%s: %s",
					diagnostic.Summary,
					diagnostic.Detail,
				)
			}

			paths := make([]*tftypes.AttributePath, 0, len(response.RequiresReplace))
			paths = append(paths, response.RequiresReplace...)
			require.Equal(t, test.wantReplace, paths, "Terraform protocol replacement paths")

			plannedState, err := response.PlannedState.Unmarshal(valueType)
			require.NoError(t, err)
			var plannedAttrs map[string]tftypes.Value
			require.NoError(t, plannedState.As(&plannedAttrs))
			var plannedID string
			require.NoError(t, plannedAttrs["id"].As(&plannedID))
			require.Equal(t, "proj-1/br-1/trig-1", plannedID, "planning must preserve existing trigger ID")
		})
	}
}
