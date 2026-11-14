package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/stretchr/testify/require"
)

func realtimeSecretConfig(t *testing.T, r ephemeral.EphemeralResource) tfsdk.Config {
	t.Helper()
	var resp ephemeral.SchemaResponse
	r.Schema(context.Background(), ephemeral.SchemaRequest{}, &resp)
	require.True(t, resp.Schema.Attributes["secret"].IsSensitive())
	return tfsdk.Config{
		Schema: resp.Schema,
		Raw: dataAPIValue(resp.Schema.Type().TerraformType(context.Background()), map[string]any{
			"project_id": "pr/test",
			"branch_id":  "br/test",
		}),
	}
}
func TestEphemeralBranchRealtimeSecret(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		calls := 0
		r := NewNeonBranchRealtimeSecretEphemeralResource().(*neonEphemeralBranchRealtimeSecret)
		client := realtimeClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, "/api/v2/projects/pr%2Ftest/branches/br%2Ftest/realtime/secret", req.URL.EscapedPath())
			if calls == 1 {
				return realtimeHTTPResponse(404, `{
  "message": "not yet available"
}`)
			}
			return realtimeHTTPResponse(200, `{
  "secret": "fixture-only",
  "pending": false
}`)
		})
		var configured ephemeral.ConfigureResponse
		r.Configure(context.Background(), ephemeral.ConfigureRequest{
			ProviderData: &providerAdapter{
				sdk: client,
			},
		}, &configured)
		require.False(t, configured.Diagnostics.HasError())
		config := realtimeSecretConfig(t, r)
		resp := ephemeral.OpenResponse{
			Result: tfsdk.EphemeralResultData{
				Schema: config.Schema,
			},
		}
		r.Open(context.Background(), ephemeral.OpenRequest{
			Config: config,
		}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		require.Equal(t, 2, calls)
		var model neonEphemeralBranchRealtimeSecretModel
		require.False(t, resp.Result.Get(context.Background(), &model).HasError())
		require.True(t, model.Secret.ValueString() == "fixture-only", "secret mismatch (redacted)")
		require.Equal(t, false, model.Pending.ValueBool())
	})
	t.Run("pending", func(t *testing.T) {
		calls := 0
		r := NewNeonBranchRealtimeSecretEphemeralResource().(*neonEphemeralBranchRealtimeSecret)
		client := realtimeClient(t, func(req *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, "/api/v2/projects/pr%2Ftest/branches/br%2Ftest/realtime/secret", req.URL.EscapedPath())
			if calls == 1 {
				return realtimeHTTPResponse(404, `{
  "message": "not yet available"
}`)
			}
			return realtimeHTTPResponse(200, `{
  "secret": "fixture-only",
  "pending": true
}`)
		})
		var configured ephemeral.ConfigureResponse
		r.Configure(context.Background(), ephemeral.ConfigureRequest{
			ProviderData: &providerAdapter{
				sdk: client,
			},
		}, &configured)
		require.False(t, configured.Diagnostics.HasError())
		config := realtimeSecretConfig(t, r)
		resp := ephemeral.OpenResponse{
			Result: tfsdk.EphemeralResultData{
				Schema: config.Schema,
			},
		}
		r.Open(context.Background(), ephemeral.OpenRequest{
			Config: config,
		}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		require.Equal(t, 2, calls)
		var model neonEphemeralBranchRealtimeSecretModel
		require.False(t, resp.Result.Get(context.Background(), &model).HasError())
		require.True(t, model.Secret.ValueString() == "fixture-only", "secret mismatch (redacted)")
		require.Equal(t, true, model.Pending.ValueBool())
	})

	for _, tt := range []struct {
		name    string
		status  int
		message string
	}{
		{
			"404 exhaustion",
			404,
			"not yet available",
		},
		{
			"permission",
			403,
			"credential permission denied",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			r := NewNeonBranchRealtimeSecretEphemeralResource().(*neonEphemeralBranchRealtimeSecret)
			r.client = realtimeClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if tt.status == 404 {
					return realtimeHTTPResponse(404, `{
  "message": "not yet available"
}`)
				}
				return realtimeHTTPResponse(403, `{
  "message": "credential permission denied"
}`)
			})
			config := realtimeSecretConfig(t, r)
			resp := ephemeral.OpenResponse{
				Result: tfsdk.EphemeralResultData{
					Schema: config.Schema,
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			r.Open(ctx, ephemeral.OpenRequest{
				Config: config,
			}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			require.Contains(t, resp.Diagnostics.Errors()[0].Detail()+resp.Diagnostics.Errors()[0].Summary(), tt.message)
			require.Equal(t, 1, calls)
			require.False(t, resp.Result.Raw.IsKnown() && !resp.Result.Raw.IsNull())
		})
	}
	t.Run("cancel before attempt", func(t *testing.T) {
		r := NewNeonBranchRealtimeSecretEphemeralResource().(*neonEphemeralBranchRealtimeSecret)
		r.client = realtimeClient(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("unexpected request")
			return nil, nil
		})
		config := realtimeSecretConfig(t, r)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var resp ephemeral.OpenResponse
		r.Open(ctx, ephemeral.OpenRequest{
			Config: config,
		}, &resp)
		require.True(t, resp.Diagnostics.HasError())
	})
}

func TestEphemeralBranchRealtimeSecretConfigure(t *testing.T) {
	r := NewNeonBranchRealtimeSecretEphemeralResource().(*neonEphemeralBranchRealtimeSecret)
	for _, data := range []any{
		"wrong",
		&providerAdapter{},
		(*providerAdapter)(nil),
	} {
		var resp ephemeral.ConfigureResponse
		r.Configure(context.Background(), ephemeral.ConfigureRequest{
			ProviderData: data,
		}, &resp)
		require.True(t, resp.Diagnostics.HasError())
	}
	var opened ephemeral.OpenResponse
	r.Open(context.Background(), ephemeral.OpenRequest{}, &opened)
	require.True(t, opened.Diagnostics.HasError())
}

func TestEphemeralBranchRealtimeSecretDeadlineAfter404(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewNeonBranchRealtimeSecretEphemeralResource().(*neonEphemeralBranchRealtimeSecret)
	r.client = realtimeClient(t, func(*http.Request) (*http.Response, error) {
		cancel()
		return realtimeHTTPResponse(404, `{
  "message": "original unavailable diagnostic"
}`)
	})
	config := realtimeSecretConfig(t, r)
	var resp ephemeral.OpenResponse
	r.Open(ctx, ephemeral.OpenRequest{
		Config: config,
	}, &resp)
	require.Equal(t, 2, resp.Diagnostics.ErrorsCount())
	require.Contains(t, resp.Diagnostics.Errors()[0].Detail()+resp.Diagnostics.Errors()[0].Summary(), "original unavailable diagnostic")
}
