package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/require"
)

// SDK v2 could store an omitted Optional list as []; keeping that value makes
// the first framework plan differ from configuration that still omits it.
func TestProjectUpgradeNormalizesLegacyEmptyAllowlist(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		field    string
		wantNull bool
		wantIPs  []string
	}{
		{name: "missing", wantNull: true},
		{name: "null", field: `,"allowed_ips":null`, wantNull: true},
		{name: "legacy empty", field: `,"allowed_ips":[]`, wantNull: true},
		{name: "configured address", field: `,"allowed_ips":["203.0.113.10"]`, wantIPs: []string{"203.0.113.10"}},
		{name: "configured addresses", field: `,"allowed_ips":["203.0.113.10","198.51.100.0/24"]`, wantIPs: []string{"203.0.113.10", "198.51.100.0/24"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			server := NewServer("unitTest")
			schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
			require.NoError(t, err)
			response, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
				TypeName: "neon_project",
				Version:  11,
				RawState: &tfprotov6.RawState{JSON: []byte(fmt.Sprintf(
					`{"id":"proj-1","allowed_ips_protected_branches_only":"yes"%s}`, tt.field,
				))},
			})
			require.NoError(t, err)
			require.Empty(t, response.Diagnostics)
			require.NotNil(t, response.UpgradedState)
			value, err := response.UpgradedState.Unmarshal(schemas.ResourceSchemas["neon_project"].ValueType())
			require.NoError(t, err)
			var state map[string]tftypes.Value
			require.NoError(t, value.As(&state))
			var protected bool
			require.NoError(t, state["allowed_ips_protected_branches_only"].As(&protected))
			require.True(t, protected, "normalization must retain existing flag conversion")
			ips := state["allowed_ips"]
			require.Equal(t, tt.wantNull, ips.IsNull(), "allowed_ips null intent")
			if tt.wantNull {
				return
			}
			var elements []tftypes.Value
			require.NoError(t, ips.As(&elements))
			var got []string
			for _, element := range elements {
				var ip string
				require.NoError(t, element.As(&ip))
				got = append(got, ip)
			}
			require.Equal(t, tt.wantIPs, got, "configured addresses must survive migration")
		})
	}
}
