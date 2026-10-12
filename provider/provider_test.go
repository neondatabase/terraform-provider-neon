//go:build !acceptance
// +build !acceptance

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/stretchr/testify/require"
)

func TestProviderAPIKeySensitive(t *testing.T) {
	t.Parallel()

	server, err := NewServer("test")
	require.NoError(t, err)

	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Diagnostics)
	require.NotNil(t, resp.Provider)
	require.NotNil(t, resp.Provider.Block)

	// Check the schema served to Terraform, including SDK/Framework mux compatibility.
	for _, attr := range resp.Provider.Block.Attributes {
		if attr.Name == "api_key" {
			require.True(t, attr.Sensitive, "provider api_key must be sensitive")
			return
		}
	}
	t.Fatal("provider schema does not contain api_key")
}
