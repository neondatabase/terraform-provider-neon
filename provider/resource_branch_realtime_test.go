package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

type realtimeHTTPFixture struct {
	do func(*http.Request) (*http.Response, error)
}

func (f realtimeHTTPFixture) Do(req *http.Request) (*http.Response, error) { return f.do(req) }
func realtimeHTTPResponse(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
}
func realtimeClient(t *testing.T, do func(*http.Request) (*http.Response, error)) *neon.Client {
	t.Helper()
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: realtimeHTTPFixture{do: do}})
	require.NoError(t, err)
	return client
}
func realtimeResource(t *testing.T, do func(*http.Request) (*http.Response, error)) *neonBranchRealtimeResource {
	t.Helper()
	r := NewNeonBranchRealtimeResource().(*neonBranchRealtimeResource)
	var resp resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &providerAdapter{sdk: realtimeClient(t, do)}}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return r
}
func realtimeSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	NewNeonBranchRealtimeResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}
func realtimeState(t *testing.T, values map[string]any) tfsdk.State {
	t.Helper()
	s := realtimeSchema(t)
	all := map[string]any{"project_id": "pr-test", "branch_id": "br-test", "id": "pr-test/br-test", "enabled": true}
	for k, v := range values {
		all[k] = v
	}
	return tfsdk.State{Schema: s, Raw: dataAPIValue(s.Type().TerraformType(context.Background()), all)}
}
func realtimeModel(t *testing.T, state tfsdk.State) neonBranchRealtimeModel {
	t.Helper()
	var model neonBranchRealtimeModel
	require.False(t, state.Get(context.Background(), &model).HasError())
	return model
}

func TestBranchRealtimeSchemaAndImport(t *testing.T) {
	s := realtimeSchema(t)
	require.Len(t, s.Attributes, 7)
	for _, name := range []string{"project_id", "branch_id"} {
		a := s.Attributes[name].(schema.StringAttribute)
		require.True(t, a.Required)
		require.Len(t, a.PlanModifiers, 1)
		state := realtimeState(t, nil)
		var modified planmodifier.StringResponse
		a.PlanModifiers[0].PlanModifyString(context.Background(), planmodifier.StringRequest{
			State: state, Plan: tfsdk.Plan(state), StateValue: types.StringValue("old"), PlanValue: types.StringValue("new"),
		}, &modified)
		require.True(t, modified.RequiresReplace, "%s must require replacement", name)
	}
	origins := s.Attributes["allowed_origins"].(schema.ListAttribute)
	require.True(t, origins.Optional)
	require.False(t, origins.Computed)
	for _, id := range []string{"pr/br", "missing", "/br", "pr/", "pr/br/extra", "pr//br"} {
		t.Run(id, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}}
			NewNeonBranchRealtimeResource().(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
			if id != "pr/br" {
				require.True(t, resp.Diagnostics.HasError())
				return
			}
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			m := realtimeModel(t, resp.State)
			require.Equal(t, id, m.ID.ValueString())
			require.True(t, m.AllowedOrigins.IsNull())
		})
	}
	for _, factory := range (&Provider{}).Resources(context.Background()) {
		var metadata resource.MetadataResponse
		factory().Metadata(context.Background(), resource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_branch_realtime" {
			return
		}
	}
	t.Fatal("resource is not registered")
}

func TestBranchRealtimeOriginsRequests(t *testing.T) {
	for _, tt := range []struct {
		name    string
		origins any
		json    string
	}{
		{"omitted", nil, `{}`}, {"empty", []string{}, `{"allowed_origins":[]}`},
		{"wildcard", []string{"*"}, `{"allowed_origins":["*"]}`}, {"configured", []string{"https://example.test"}, `{"allowed_origins":["https://example.test"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/api/v2/projects/pr-test/branches/br-test/realtime", req.URL.EscapedPath())
				if req.Method == http.MethodPost {
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.JSONEq(t, tt.json, string(body))
					return realtimeHTTPResponse(202, "")
				}
				return realtimeHTTPResponse(200, `{"enabled":true,"pending":false,"allowed_origins":["*"],"invocation_url":"https://realtime.test","revision":3}`)
			})
			state := realtimeState(t, map[string]any{"allowed_origins": tt.origins})
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, 2, calls)
			m := realtimeModel(t, resp.State)
			require.Equal(t, "pr-test/br-test", m.ID.ValueString())
			require.Equal(t, int64(3), m.Revision.ValueInt64())
			require.True(t, m.AllowedOrigins.Equal(realtimeModel(t, state).AllowedOrigins))
		})
	}
	t.Run("unknown fails before mutation", func(t *testing.T) {
		r := realtimeResource(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected mutation"); return nil, nil })
		state := realtimeState(t, map[string]any{"allowed_origins": tftypes.UnknownValue})
		resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
		require.True(t, resp.Diagnostics.HasError())
	})
}

func TestBranchRealtimeRead(t *testing.T) {
	for _, tt := range []struct {
		name             string
		status           int
		body             string
		prior            any
		want             any
		removed, warning bool
	}{
		{name: "404", status: 404, body: `{"message":"gone"}`, removed: true},
		{name: "disabled pending", status: 200, body: `{"enabled":false,"pending":true}`, removed: true},
		{name: "unmanaged", status: 200, body: `{"enabled":true,"allowed_origins":["*"]}`},
		{name: "pending refresh", status: 200, body: `{"enabled":true,"pending":true,"allowed_origins":["https://new.test"]}`, prior: []string{"https://old.test"}, want: []string{"https://new.test"}},
		{name: "empty equivalent", status: 200, body: `{"enabled":true,"allowed_origins":["*"]}`, prior: []string{}, want: []string{}},
		{name: "wildcard equivalent", status: 200, body: `{"enabled":true,"allowed_origins":[]}`, prior: []string{"*"}, want: []string{"*"}},
		{name: "absent observation", status: 200, body: `{"enabled":true}`, prior: []string{"https://old.test"}, want: []string{"https://old.test"}, warning: true},
		{name: "null observations", status: 200, body: `{"enabled":true,"allowed_origins":null,"invocation_url":null,"revision":null}`, prior: []string{"https://old.test"}, want: []string{"https://old.test"}, warning: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := realtimeResource(t, func(*http.Request) (*http.Response, error) { return realtimeHTTPResponse(tt.status, tt.body) })
			state := realtimeState(t, map[string]any{"allowed_origins": tt.prior})
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, tt.removed, resp.State.Raw.IsNull())
			if tt.removed {
				return
			}
			m := realtimeModel(t, resp.State)
			expected := realtimeModel(t, realtimeState(t, map[string]any{"allowed_origins": tt.want}))
			require.True(t, m.AllowedOrigins.Equal(expected.AllowedOrigins))
			require.True(t, m.InvocationURL.IsNull())
			require.True(t, m.Revision.IsNull())
			require.Equal(t, tt.warning, resp.Diagnostics.WarningsCount() > 0)
		})
	}
}

func TestBranchRealtimePartialStateAndDelete(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		success bool
	}{
		{"ready", 200, `{"enabled":true,"pending":false}`, true},
		{"disabled", 200, `{"enabled":false,"pending":false}`, false},
		{"get failure", 403, `{"message":"forbidden"}`, false},
		{"get 404", 404, `{"message":"gone"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost {
					return realtimeHTTPResponse(202, "")
				}
				return realtimeHTTPResponse(tt.status, tt.body)
			})
			state := realtimeState(t, nil)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.Equal(t, !tt.success, resp.Diagnostics.HasError())
			m := realtimeModel(t, resp.State)
			require.Equal(t, "pr-test/br-test", m.ID.ValueString())
			if !tt.success {
				require.True(t, m.Enabled.IsNull())
				require.True(t, m.InvocationURL.IsNull())
				require.True(t, m.Revision.IsNull())
			}
		})
	}
	for _, status := range []int{404, 202} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method == http.MethodDelete {
					return realtimeHTTPResponse(status, `{"message":"gone"}`)
				}
				return realtimeHTTPResponse(200, `{"enabled":false,"pending":true}`)
			})
			state := realtimeState(t, nil)
			resp := resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			if status == 404 {
				require.Equal(t, 1, calls)
			} else {
				require.Equal(t, 2, calls)
			}
		})
	}
	t.Run("pending timeout retains identity", func(t *testing.T) {
		calls := 0
		r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method == http.MethodPost {
				return realtimeHTTPResponse(202, "")
			}
			return realtimeHTTPResponse(200, `{"enabled":true,"pending":true}`)
		})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		state := realtimeState(t, nil)
		resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
		require.True(t, resp.Diagnostics.HasError())
		require.Equal(t, 2, calls)
		require.Equal(t, "pr-test/br-test", realtimeModel(t, resp.State).ID.ValueString())
	})
}

func TestBranchRealtimeUpdateRelinquishAndEscaping(t *testing.T) {
	posts, gets := 0, 0
	r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/v2/projects/pr%2Ftest/branches/br%2Ftest/realtime", req.URL.EscapedPath())
		if req.Method == http.MethodGet {
			gets++
		}
		if req.Method == http.MethodPost {
			posts++
			var body map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			require.NotContains(t, body, "allowed_origins")
			return realtimeHTTPResponse(202, "")
		}
		return realtimeHTTPResponse(200, `{"enabled":true,"allowed_origins":["https://old.test"]}`)
	})
	// Prior state manages origins and the plan drops them, so Update must relinquish management.
	prior := realtimeState(t, map[string]any{
		"project_id": "pr/test", "branch_id": "br/test", "allowed_origins": []string{"https://old.test"},
	})
	plan := realtimeState(t, map[string]any{"project_id": "pr/test", "branch_id": "br/test"})
	resp := resource.UpdateResponse{State: prior}
	r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(plan), State: prior}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	require.Equal(t, 1, posts)
	require.GreaterOrEqual(t, gets, 1)
	require.True(t, realtimeModel(t, resp.State).AllowedOrigins.IsNull())
}

func TestBranchRealtimeConfigure(t *testing.T) {
	r := NewNeonBranchRealtimeResource().(*neonBranchRealtimeResource)
	for _, data := range []any{"wrong", &providerAdapter{}, (*providerAdapter)(nil)} {
		var resp resource.ConfigureResponse
		r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: data}, &resp)
		require.True(t, resp.Diagnostics.HasError())
	}
	var resp resource.ReadResponse
	r.Read(context.Background(), resource.ReadRequest{}, &resp)
	require.True(t, resp.Diagnostics.HasError())
}

func TestBranchRealtimeEnablePendingAndFailures(t *testing.T) {
	t.Run("pending then ready", func(t *testing.T) {
		calls := 0
		r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method == http.MethodPost {
				return realtimeHTTPResponse(202, "")
			}
			if calls == 2 {
				return realtimeHTTPResponse(200, `{"enabled":true,"pending":true}`)
			}
			return realtimeHTTPResponse(200, `{"enabled":true,"pending":false}`)
		})
		state := realtimeState(t, nil)
		resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
		require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
		require.Equal(t, 3, calls)
	})
	t.Run("enable 404", func(t *testing.T) {
		calls := 0
		r := realtimeResource(t, func(*http.Request) (*http.Response, error) {
			calls++
			return realtimeHTTPResponse(404, `{"message":"missing branch"}`)
		})
		state := realtimeState(t, nil)
		resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
		require.True(t, resp.Diagnostics.HasError())
		require.Equal(t, 1, calls)
	})
	t.Run("cancellation after accepted enable retains state", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := realtimeResource(t, func(*http.Request) (*http.Response, error) { cancel(); return realtimeHTTPResponse(202, "") })
		state := realtimeState(t, nil)
		resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
		require.True(t, resp.Diagnostics.HasError())
		require.Equal(t, "pr-test/br-test", realtimeModel(t, resp.State).ID.ValueString())
	})
	t.Run("update failure retains new planned policy and identity", func(t *testing.T) {
		r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodPost {
				return realtimeHTTPResponse(202, "")
			}
			return realtimeHTTPResponse(403, `{"message":"read denied"}`)
		})
		state := realtimeState(t, map[string]any{"allowed_origins": []string{"*"}})
		resp := resource.UpdateResponse{State: state}
		r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(state), State: state}, &resp)
		require.True(t, resp.Diagnostics.HasError())
		require.Equal(t, "pr-test/br-test", realtimeModel(t, resp.State).ID.ValueString())
	})
	t.Run("read retry shares caller deadline", func(t *testing.T) {
		calls := 0
		r := realtimeResource(t, func(*http.Request) (*http.Response, error) {
			calls++
			return realtimeHTTPResponse(503, `{"message":"busy"}`)
		})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		state := realtimeState(t, nil)
		resp := resource.ReadResponse{State: state}
		r.Read(ctx, resource.ReadRequest{State: state}, &resp)
		require.True(t, resp.Diagnostics.HasError())
		require.Equal(t, 1, calls)
		require.False(t, resp.State.Raw.IsNull())
	})
}

func TestBranchRealtimeDisablePolling(t *testing.T) {
	for _, status := range []int{200, 404, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			r := realtimeResource(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method == http.MethodDelete {
					return realtimeHTTPResponse(202, "")
				}
				if calls == 2 {
					return realtimeHTTPResponse(200, `{"enabled":true,"pending":true}`)
				}
				return realtimeHTTPResponse(status, `{"enabled":false,"pending":true,"message":"final response"}`)
			})
			state := realtimeState(t, nil)
			resp := resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
			require.Equal(t, status == 403, resp.Diagnostics.HasError())
			require.Equal(t, 3, calls)
		})
	}
}
