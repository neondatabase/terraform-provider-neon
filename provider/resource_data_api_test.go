package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func dataAPITestResource(t *testing.T) resource.Resource {
	t.Helper()
	for _, factory := range (&Provider{}).Resources(context.Background()) {
		r := factory()
		var metadata resource.MetadataResponse
		r.Metadata(context.Background(), resource.MetadataRequest{}, &metadata)
		if metadata.TypeName == "neon_data_api" {
			return r
		}
	}
	t.Fatal("neon_data_api is not registered")
	return nil
}

func TestDataAPIRegistration(t *testing.T) {
	r := dataAPITestResource(t)
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	for _, name := range []string{"project_id", "branch_id", "database_name"} {
		require.True(t, resp.Schema.Attributes[name].IsRequired(), name)
	}
	for _, name := range []string{"id", "url"} {
		require.True(t, resp.Schema.Attributes[name].IsComputed(), name)
	}
	require.NotContains(t, resp.Schema.Attributes, "status")
	require.NotContains(t, resp.Schema.Attributes, "available_schemas")
}

type dataAPIHTTPFixture func(*http.Request) (*http.Response, error)

func (f dataAPIHTTPFixture) Do(req *http.Request) (*http.Response, error) { return f(req) }

type dataAPITestCall struct {
	method  string
	status  int
	body    string
	payload map[string]any
	err     error
}

func dataAPIConfiguredResource(t *testing.T, calls ...dataAPITestCall) resource.Resource {
	t.Helper()
	r := dataAPITestResource(t)
	index := 0
	client, err := neon.NewClient(neon.Config{Key: "fixture-key", HTTPClient: dataAPIHTTPFixture(func(req *http.Request) (*http.Response, error) {
		require.Less(t, index, len(calls), "unexpected SDK call")
		call := calls[index]
		index++
		require.Equal(t, call.method, req.Method)
		require.Equal(t, "/api/v2/projects/pr-test/branches/br-test/data-api/neondb", req.URL.Path)
		if call.payload != nil {
			var body map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			require.Equal(t, call.payload, body)
		}
		if call.err != nil {
			return nil, call.err
		}
		return &http.Response{StatusCode: call.status, Body: io.NopCloser(strings.NewReader(call.body))}, nil
	})})
	require.NoError(t, err)
	var resp resource.ConfigureResponse
	r.(resource.ResourceWithConfigure).Configure(context.Background(), resource.ConfigureRequest{ProviderData: &providerAdapter{sdk: client}}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	t.Cleanup(func() { require.Equal(t, len(calls), index, "missing SDK call") })
	return r
}

func dataAPISchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	dataAPITestResource(t).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	return resp.Schema
}

func dataAPIValue(typ tftypes.Type, value any) tftypes.Value {
	if object, ok := typ.(tftypes.Object); ok && value != nil && value != tftypes.UnknownValue {
		supplied := value.(map[string]any)
		values := make(map[string]tftypes.Value, len(object.AttributeTypes))
		for name, attributeType := range object.AttributeTypes {
			values[name] = dataAPIValue(attributeType, supplied[name])
		}
		return tftypes.NewValue(typ, values)
	}
	if list, ok := typ.(tftypes.List); ok && value != nil && value != tftypes.UnknownValue {
		var values []tftypes.Value
		for _, item := range value.([]string) {
			values = append(values, tftypes.NewValue(list.ElementType, item))
		}
		return tftypes.NewValue(typ, values)
	}
	return tftypes.NewValue(typ, value)
}

func dataAPIState(t *testing.T, overrides map[string]any) tfsdk.State {
	t.Helper()
	s := dataAPISchema(t)
	values := map[string]any{"id": "pr-test/br-test/neondb", "project_id": "pr-test", "branch_id": "br-test", "database_name": "neondb", "url": "https://endpoint.test/rest/v1"}
	for k, v := range overrides {
		values[k] = v
	}
	return tfsdk.State{Schema: s, Raw: dataAPIValue(s.Type().TerraformType(context.Background()), values)}
}

func dataAPIString(t *testing.T, state tfsdk.State, name string) string {
	t.Helper()
	var value types.String
	diags := state.GetAttribute(context.Background(), path.Root(name), &value)
	require.False(t, diags.HasError(), "%v", diags)
	return value.ValueString()
}

const dataAPIGetBody = `{"url":"https://endpoint.test/rest/v1","status":"active","settings":{"db_aggregates_enabled":false,"db_max_rows":0,"db_schemas":["public"],"server_timing_enabled":false},"available_schemas":["public","auth"]}`

func TestDataAPICreatePayloadAndState(t *testing.T) {
	settings := map[string]any{"db_aggregates_enabled": false, "db_anon_role": "guest", "db_extra_search_path": "extensions", "db_max_rows": float64(0), "db_schemas": []string{"public"}, "jwt_cache_max_lifetime": float64(0), "jwt_role_claim_key": ".role", "openapi_mode": "disabled", "server_cors_allowed_origins": "https://app.test", "server_timing_enabled": false}
	payloadSettings := make(map[string]any)
	for k, v := range settings {
		payloadSettings[k] = v
	}
	payloadSettings["db_schemas"] = []any{"public"}
	r := dataAPIConfiguredResource(t,
		dataAPITestCall{method: "POST", status: 201, body: `{"url":"https://endpoint.test/rest/v1"}`, payload: map[string]any{"auth_provider": "external", "jwks_url": "https://auth.test/jwks", "provider_name": "Custom", "jwt_audience": "aud", "add_default_grants": false, "skip_auth_schema": false, "settings": payloadSettings}},
		dataAPITestCall{method: "GET", status: 200, body: dataAPIGetBody},
	)
	state := dataAPIState(t, map[string]any{"id": tftypes.UnknownValue, "url": tftypes.UnknownValue, "auth_provider": "external", "jwks_url": "https://auth.test/jwks", "provider_name": "Custom", "jwt_audience": "aud", "add_default_grants": false, "skip_auth_schema": false, "settings": settings})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	require.Equal(t, "pr-test/br-test/neondb", dataAPIString(t, resp.State, "id"))
	require.Equal(t, "https://endpoint.test/rest/v1", dataAPIString(t, resp.State, "url"))
	require.Equal(t, "external", dataAPIString(t, resp.State, "auth_provider"))
	var maxRows types.Int64
	require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings").AtName("db_max_rows"), &maxRows).HasError())
	require.Equal(t, int64(0), maxRows.ValueInt64())
}

func TestDataAPICreateOmission(t *testing.T) {
	r := dataAPIConfiguredResource(t,
		dataAPITestCall{method: "POST", status: 201, body: `{"url":"https://endpoint.test/rest/v1"}`, payload: map[string]any{}},
		dataAPITestCall{method: "GET", status: 200, body: dataAPIGetBody},
	)
	state := dataAPIState(t, nil)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var settings types.Object
	require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings"), &settings).HasError())
	require.True(t, settings.IsNull(), "unconfigured server defaults must stay unmanaged")
}

func TestDataAPIReadManagedSettings(t *testing.T) {
	r := dataAPIConfiguredResource(t, dataAPITestCall{method: "GET", status: 200, body: dataAPIGetBody})
	state := dataAPIState(t, map[string]any{"settings": map[string]any{"db_max_rows": float64(42), "db_aggregates_enabled": true}})
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var rows types.Int64
	require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings").AtName("db_max_rows"), &rows).HasError())
	require.Equal(t, int64(0), rows.ValueInt64())
	var schemas types.List
	require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings").AtName("db_schemas"), &schemas).HasError())
	require.True(t, schemas.IsNull())
}

func TestDataAPINullableSettings(t *testing.T) {
	for _, body := range []string{`{"url":"https://endpoint.test/rest/v1","status":"active","settings":null}`, `{"url":"https://endpoint.test/rest/v1","status":"active","settings":{}}`} {
		t.Run(body, func(t *testing.T) {
			r := dataAPIConfiguredResource(t, dataAPITestCall{method: "GET", status: 200, body: body})
			state := dataAPIState(t, map[string]any{"settings": map[string]any{"db_max_rows": float64(42)}})
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Positive(t, resp.Diagnostics.WarningsCount())
			var rows types.Int64
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings").AtName("db_max_rows"), &rows).HasError())
			require.Equal(t, int64(42), rows.ValueInt64())
		})
	}
}

func TestDataAPIUpdateSettings(t *testing.T) {
	r := dataAPIConfiguredResource(t,
		dataAPITestCall{method: "PATCH", status: 201, body: `{}`, payload: map[string]any{"settings": map[string]any{"db_max_rows": float64(0), "db_aggregates_enabled": false}}},
		dataAPITestCall{method: "GET", status: 200, body: dataAPIGetBody},
	)
	prior := dataAPIState(t, map[string]any{"settings": map[string]any{"db_max_rows": float64(10), "db_aggregates_enabled": true}})
	plan := dataAPIState(t, map[string]any{"settings": map[string]any{"db_max_rows": float64(0), "db_aggregates_enabled": false}})
	resp := resource.UpdateResponse{State: prior}
	r.Update(context.Background(), resource.UpdateRequest{Plan: tfsdk.Plan(plan), State: prior}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	require.Equal(t, "pr-test/br-test/neondb", dataAPIString(t, resp.State, "id"))
}

func TestDataAPIAbsence(t *testing.T) {
	for _, method := range []string{"GET", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			r := dataAPIConfiguredResource(t, dataAPITestCall{method: method, status: 404, body: `{"code":"ANY_REASON","message":"not found","details":"discarded by SDK"}`})
			state := dataAPIState(t, nil)
			if method == "GET" {
				resp := resource.ReadResponse{State: state}
				r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
				require.True(t, resp.State.Raw.IsNull())
			} else {
				resp := resource.DeleteResponse{State: state}
				r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
				require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			}
		})
	}
}

func TestDataAPIDelete(t *testing.T) {
	r := dataAPIConfiguredResource(t, dataAPITestCall{method: "DELETE", status: 200, body: `{}`})
	state := dataAPIState(t, nil)
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
}

func TestDataAPIErrorAndPartialState(t *testing.T) {
	for _, tt := range []struct {
		name    string
		calls   []dataAPITestCall
		created bool
		want    string
	}{
		{name: "server create rejection", calls: []dataAPITestCall{{method: "POST", status: 400, body: `{"code":"BAD_INPUT","message":"server-specific rejection"}`}}, want: "server-specific rejection"},
		{name: "transport failure", calls: []dataAPITestCall{{method: "POST", err: errors.New("fixture transport failure")}}, want: "fixture transport failure"},
		{name: "follow-up read failure", calls: []dataAPITestCall{{method: "POST", status: 201, body: `{"url":"https://endpoint.test/rest/v1"}`}, {method: "GET", status: 403, body: `{"code":"DENIED","message":"server read denied"}`}}, created: true, want: "server read denied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := dataAPIConfiguredResource(t, tt.calls...)
			state := dataAPIState(t, nil)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema, Raw: tftypes.NewValue(state.Raw.Type(), nil)}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			require.Contains(t, resp.Diagnostics.Errors()[0].Detail(), tt.want)
			if tt.created {
				require.Equal(t, "pr-test/br-test/neondb", dataAPIString(t, resp.State, "id"))
			} else {
				require.True(t, resp.State.Raw.IsNull())
			}
		})
	}
}

func TestDataAPISDKMappingLimitations(t *testing.T) {
	for _, tt := range []struct {
		name      string
		overrides map[string]any
		want      string
	}{
		{name: "empty schemas", overrides: map[string]any{"settings": map[string]any{"db_schemas": []string{}}}, want: "empty db_schemas"},
		{name: "unknown auth provider", overrides: map[string]any{"auth_provider": "unrecognized"}, want: "unknown value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := dataAPIConfiguredResource(t)
			state := dataAPIState(t, tt.overrides)
			resp := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &resp)
			require.True(t, resp.Diagnostics.HasError())
			require.Contains(t, resp.Diagnostics.Errors()[0].Detail(), tt.want)
		})
	}
}

func TestDataAPIImport(t *testing.T) {
	for _, id := range []string{"pr-test/br-test/neondb", "pr-test/br-test/db/with/slash", "missing", "pr-test//neondb", "/br-test/neondb", "pr-test/br-test/"} {
		t.Run(id, func(t *testing.T) {
			r := dataAPITestResource(t)
			s := dataAPISchema(t)
			resp := resource.ImportStateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}}
			r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
			if strings.Count(id, "/") < 2 || strings.Contains(id, "//") || strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
				require.True(t, resp.Diagnostics.HasError())
				return
			}
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, id, dataAPIString(t, resp.State, "id"))
			require.Equal(t, strings.SplitN(id, "/", 3)[2], dataAPIString(t, resp.State, "database_name"))
			var settings types.Object
			require.False(t, resp.State.GetAttribute(context.Background(), path.Root("settings"), &settings).HasError())
			require.True(t, settings.IsNull())
		})
	}
}

func TestDataAPISettingsRemovalReplacement(t *testing.T) {
	for _, tt := range []struct {
		name       string
		prior      any
		configured any
		replace    bool
	}{
		{name: "remove object", prior: map[string]any{"db_max_rows": float64(10)}, replace: true},
		{name: "remove member", prior: map[string]any{"db_max_rows": float64(10), "db_anon_role": "guest"}, configured: map[string]any{"db_anon_role": "guest"}, replace: true},
		{name: "change value", prior: map[string]any{"db_max_rows": float64(10)}, configured: map[string]any{"db_max_rows": float64(20)}},
		{name: "unknown value", prior: map[string]any{"db_max_rows": float64(10)}, configured: map[string]any{"db_max_rows": tftypes.UnknownValue}},
		{name: "add object", configured: map[string]any{"db_max_rows": float64(10)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := dataAPITestResource(t)
			state := dataAPIState(t, map[string]any{"settings": tt.prior})
			planned := dataAPIState(t, map[string]any{"settings": tt.configured})
			req := resource.ModifyPlanRequest{State: state, Plan: tfsdk.Plan(planned), Config: tfsdk.Config(planned)}
			resp := resource.ModifyPlanResponse{Plan: req.Plan}
			r.(resource.ResourceWithModifyPlan).ModifyPlan(context.Background(), req, &resp)
			require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Equal(t, tt.replace, len(resp.RequiresReplace) > 0)
		})
	}
}
