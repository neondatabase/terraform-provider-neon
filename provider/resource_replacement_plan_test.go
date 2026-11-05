package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/require"
)

func TestImmutableResourceChangesRequireReplacementWithoutPlanErrors(t *testing.T) {
	tests := []struct {
		resourceType string
		field        string
		oldValue     any
		newValue     any
	}{
		{"neon_bucket", "project_id", "proj-1", "proj-2"},
		{"neon_bucket", "branch_id", "br-1", "br-2"},
		{"neon_bucket", "name", "bucket-a", "bucket-b"},
		{"neon_bucket", "access_level", "private", "public_read"},
		{"neon_service_credential", "project_id", "proj-1", "proj-2"},
		{"neon_service_credential", "branch_id", "br-1", "br-2"},
		{"neon_service_credential", "name", "credential-a", "credential-b"},
		{"neon_service_credential", "scopes", []any{"functions:invoke"}, []any{"functions:invoke", "sql:read"}},
	}

	for _, test := range tests {
		t.Run(test.resourceType+"/"+test.field, func(t *testing.T) {
			response, valueType := planReplacementResourceChange(t, test.resourceType, test.field, test.oldValue, test.newValue)
			assertNoPlanErrorDiagnostics(t, response.Diagnostics)
			require.Equal(t, []*tftypes.AttributePath{
				tftypes.NewAttributePath().WithAttributeName(test.field),
			}, response.RequiresReplace)
			plannedState, err := response.PlannedState.Unmarshal(valueType)
			require.NoError(t, err)
			var plannedAttributes map[string]tftypes.Value
			require.NoError(t, plannedState.As(&plannedAttributes))
			var plannedID string
			require.NoError(t, plannedAttributes["id"].As(&plannedID))
			require.Equal(t, "proj-1/br-1/item-1", plannedID)
		})
	}
}

func TestImmutableResourceUnchangedPlansDoNotRequireReplacement(t *testing.T) {
	for _, resourceType := range []string{"neon_bucket", "neon_service_credential"} {
		t.Run(resourceType, func(t *testing.T) {
			response, _ := planReplacementResourceChange(t, resourceType, "", nil, nil)
			assertNoPlanErrorDiagnostics(t, response.Diagnostics)
			require.Empty(t, response.RequiresReplace)
		})
	}
}

func TestImmutableResourceCreatePlansDoNotRequireReplacement(t *testing.T) {
	for _, resourceType := range []string{"neon_bucket", "neon_service_credential"} {
		t.Run(resourceType, func(t *testing.T) {
			ctx := context.Background()
			server := NewServer("unitTest")
			schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
			require.NoError(t, err)
			valueType := schemas.ResourceSchemas[resourceType].ValueType()
			config := replacementPlanConfig(resourceType)
			configValue := replacementPlanDynamicValue(t, valueType, config)

			response, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName:         resourceType,
				ProposedNewState: configValue,
				Config:           configValue,
			})
			require.NoError(t, err)
			assertNoPlanErrorDiagnostics(t, response.Diagnostics)
			require.Empty(t, response.RequiresReplace)
		})
	}
}

func TestImmutableResourceDirectUpdatesRemainRejected(t *testing.T) {
	tests := []struct {
		name   string
		update func(context.Context, resource.UpdateRequest, *resource.UpdateResponse)
		want   string
	}{
		{
			name: "bucket",
			update: func(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
				(&neonBucketResource{}).Update(ctx, req, resp)
			},
			want: "Neon Bucket Update Not Supported",
		},
		{
			name: "service credential",
			update: func(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
				(&neonServiceCredentialResource{}).Update(ctx, req, resp)
			},
			want: "Neon Credential Update Not Supported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &resource.UpdateResponse{}
			test.update(context.Background(), resource.UpdateRequest{}, response)
			require.Len(t, response.Diagnostics, 1)
			require.Equal(t, test.want, response.Diagnostics[0].Summary())
		})
	}
}

func planReplacementResourceChange(t *testing.T, resourceType, field string, oldValue, newValue any) (*tfprotov6.PlanResourceChangeResponse, tftypes.Type) {
	t.Helper()

	ctx := context.Background()
	server := NewServer("unitTest")
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	valueType := schemas.ResourceSchemas[resourceType].ValueType()
	prior := replacementPlanFixture(resourceType)
	config := replacementPlanConfig(resourceType)
	if field != "" {
		prior[field] = oldValue
		config[field] = newValue
	}
	proposed := make(map[string]any, len(prior))
	for name, value := range prior {
		proposed[name] = value
	}
	for name, value := range config {
		proposed[name] = value
	}

	response, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         resourceType,
		PriorState:       replacementPlanDynamicValue(t, valueType, prior),
		ProposedNewState: replacementPlanDynamicValue(t, valueType, proposed),
		Config:           replacementPlanDynamicValue(t, valueType, config),
	})
	require.NoError(t, err)
	return response, valueType
}

func replacementPlanDynamicValue(t *testing.T, valueType tftypes.Type, values map[string]any) *tfprotov6.DynamicValue {
	t.Helper()

	data, err := json.Marshal(values)
	require.NoError(t, err)
	value, err := tftypes.ValueFromJSONWithOpts(data, valueType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
	require.NoError(t, err)
	dynamic, err := tfprotov6.NewDynamicValue(valueType, value)
	require.NoError(t, err)
	return &dynamic
}

func assertNoPlanErrorDiagnostics(t *testing.T, diagnostics []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		require.NotEqual(t, tfprotov6.DiagnosticSeverityError, diagnostic.Severity, "%s: %s", diagnostic.Summary, diagnostic.Detail)
	}
}

func replacementPlanConfig(resourceType string) map[string]any {
	if resourceType == "neon_bucket" {
		return map[string]any{
			"project_id": "proj-1", "branch_id": "br-1", "name": "bucket-a", "access_level": "private",
		}
	}
	return map[string]any{
		"project_id": "proj-1", "branch_id": "br-1", "name": "credential-a", "scopes": []any{"functions:invoke"},
	}
}

func replacementPlanFixture(resourceType string) map[string]any {
	fixture := replacementPlanConfig(resourceType)
	fixture["id"] = "proj-1/br-1/item-1"
	if resourceType == "neon_bucket" {
		fixture["s3_endpoint"] = "https://s3.example"
		fixture["region"] = "aws-us-east-2"
		return fixture
	}
	fixture["token_id"] = "0123456789abcdef0123456789abcdef"
	fixture["token_id_short"] = "0123456789ab"
	fixture["api_token"] = "secret-token"
	fixture["s3_secret_access_key"] = "secret-s3"
	fixture["created_at"] = "2025-01-01T00:00:00Z"
	fixture["expires_at"] = nil
	fixture["is_expired"] = false
	return fixture
}
