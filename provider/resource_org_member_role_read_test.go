package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

type orgMemberRoleReadHTTP func(*http.Request) (*http.Response, error)

func (f orgMemberRoleReadHTTP) Do(req *http.Request) (*http.Response, error) { return f(req) }

func orgMemberRoleReadState(t *testing.T) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var schemaResp resource.SchemaResponse
	(&neonOrgMemberRoleResource{}).Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	require.False(t, schemaResp.Diagnostics.HasError(), "%v", schemaResp.Diagnostics)
	attrs := map[string]tftypes.Value{
		"id":        tftypes.NewValue(tftypes.String, "prior-member-role-id"),
		"org_id":    tftypes.NewValue(tftypes.String, "org-test"),
		"member_id": tftypes.NewValue(tftypes.String, "member-test"),
		"role":      tftypes.NewValue(tftypes.String, "viewer"),
	}
	return tfsdk.State{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), attrs),
	}
}

func TestOrgMemberRoleReadPreservesPriorStateOnLookupErrors(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		transport   error
		cancelAfter bool
		wantDetail  string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"code":"AUTHENTICATION_FAILED","message":"fixture unauthorized"}`, wantDetail: "fixture unauthorized"},
		{name: "forbidden", status: http.StatusForbidden, body: `{"code":"DENIED","message":"fixture forbidden"}`, wantDetail: "fixture forbidden"},
		{name: "not found without definitive absence contract", status: http.StatusNotFound, body: `{"code":"NOT_FOUND","message":"fixture member lookup not found"}`, wantDetail: "fixture member lookup not found"},
		{name: "transport error", transport: errors.New("fixture transport failure"), wantDetail: "fixture transport failure"},
		{name: "server error with canceled retry", status: http.StatusInternalServerError, body: `{"code":"SERVER_ERROR","message":"fixture server error"}`, cancelAfter: true, wantDetail: "context canceled"},
		{name: "unavailable with canceled retry", status: http.StatusServiceUnavailable, body: `{"code":"UNAVAILABLE","message":"fixture unavailable"}`, cancelAfter: true, wantDetail: "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client, err := neon.NewClient(neon.Config{Key: "fixture-key", HTTPClient: orgMemberRoleReadHTTP(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "/api/v2/organizations/org-test/members/member-test", req.URL.Path)
				if tc.transport != nil {
					return nil, tc.transport
				}
				if tc.cancelAfter {
					cancel()
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			require.NoError(t, err)
			prior := orgMemberRoleReadState(t)
			resp := resource.ReadResponse{State: prior}
			(&neonOrgMemberRoleResource{client: client}).Read(ctx, resource.ReadRequest{State: prior}, &resp)

			require.True(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
			require.Contains(t, resp.Diagnostics.Errors()[0].Detail(), tc.wantDetail)
			require.True(t, prior.Raw.Equal(resp.State.Raw), "Read error changed prior state")
			require.Equal(t, 1, calls, "lookup errors and cancellation fixtures must make one GET")
		})
	}
}

func TestOrgMemberRoleReadRefreshesRoleAndIdentity(t *testing.T) {
	calls := 0
	client, err := neon.NewClient(neon.Config{Key: "fixture-key", HTTPClient: orgMemberRoleReadHTTP(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/api/v2/organizations/org-test/members/member-test", req.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"member-test","org_id":"org-test","user_id":"user-test","role":"admin","joined_at":"2025-01-01T00:00:00Z"}`))}, nil
	})})
	require.NoError(t, err)
	prior := orgMemberRoleReadState(t)
	resp := resource.ReadResponse{State: prior}
	(&neonOrgMemberRoleResource{client: client}).Read(context.Background(), resource.ReadRequest{State: prior}, &resp)

	require.False(t, resp.Diagnostics.HasError(), "%v", resp.Diagnostics)
	var got neonOrgMemberRoleResourceModel
	require.False(t, resp.State.Get(context.Background(), &got).HasError())
	require.Equal(t, "admin", got.Role.ValueString())
	require.Equal(t, "org-test/member-test", got.ID.ValueString())
	require.Equal(t, 1, calls)
}

func TestOrgMemberRoleReadWithoutClientPreservesPriorState(t *testing.T) {
	prior := orgMemberRoleReadState(t)
	resp := resource.ReadResponse{State: prior}
	(&neonOrgMemberRoleResource{}).Read(context.Background(), resource.ReadRequest{State: prior}, &resp)

	require.True(t, resp.Diagnostics.HasError())
	require.True(t, prior.Raw.Equal(resp.State.Raw), "unconfigured client changed prior state")
}

func TestOrgMemberRoleReadWithInvalidPriorStatePreservesRawState(t *testing.T) {
	prior := orgMemberRoleReadState(t)
	prior.Raw = tftypes.NewValue(tftypes.String, "invalid prior state")
	resp := resource.ReadResponse{State: prior}
	(&neonOrgMemberRoleResource{client: &neon.Client{}}).Read(context.Background(), resource.ReadRequest{State: prior}, &resp)

	require.True(t, resp.Diagnostics.HasError())
	require.True(t, prior.Raw.Equal(resp.State.Raw), "failed state decode changed prior raw state")
}

func TestOrgMemberRoleDeleteStillRemovesOnlyTerraformState(t *testing.T) {
	prior := orgMemberRoleReadState(t)
	resp := resource.DeleteResponse{State: prior}
	(&neonOrgMemberRoleResource{}).Delete(context.Background(), resource.DeleteRequest{State: prior}, &resp)

	require.False(t, resp.Diagnostics.HasError())
	require.Len(t, resp.Diagnostics.Warnings(), 1)
	require.True(t, resp.State.Raw.IsNull(), "explicit Delete must remove resource from Terraform state")
}
