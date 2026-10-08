//go:build !acceptance
// +build !acceptance

package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func Test_confirmDeleted(t *testing.T) {
	refused := neon.Error{HTTPCode: http.StatusUnprocessableEntity}
	notFound := neon.Error{HTTPCode: http.StatusNotFound}

	tests := map[string]struct {
		err  error
		get  error
		want error
	}{
		"nil passes through":                  {err: nil, get: nil, want: nil},
		"non-422 passes through":              {err: notFound, get: nil, want: notFound},
		"untyped error passes through":        {err: errors.New("boom"), get: notFound, want: errors.New("boom")},
		"422 kept when resource still exists": {err: refused, get: nil, want: refused},
		"422 kept when the check fails":       {err: refused, get: neon.Error{HTTPCode: http.StatusInternalServerError}, want: refused},
		"422 becomes 404 when resource gone":  {err: refused, get: notFound, want: notFound},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := confirmDeleted(tt.err, func() error { return tt.get })
			assert.Equal(t, tt.want, got)
		})
	}
}

type stubProjectDelete struct {
	sdkClientStub
	deleteErr error
	getErr    error
}

func (s *stubProjectDelete) DeleteProject(string) (neon.ProjectResponse, error) {
	return neon.ProjectResponse{}, s.deleteErr
}

func (s *stubProjectDelete) GetProject(string) (neon.ProjectResponse, error) {
	return neon.ProjectResponse{}, s.getErr
}

func Test_resourceProjectDeleteRetry(t *testing.T) {
	refused := neon.Error{HTTPCode: http.StatusUnprocessableEntity}
	refused.Message = "project has protected branch"

	t.Run("shall fail and keep the project in state when Neon refuses the delete", func(t *testing.T) {
		d := resourceProject().TestResourceData()
		d.SetId("foo")

		diags := resourceProjectDeleteRetry(context.TODO(), d, &stubProjectDelete{deleteErr: refused})

		if !assert.True(t, diags.HasError()) {
			return
		}
		assert.Contains(t, diags[0].Summary, "project has protected branch")
		assert.Equal(t, "foo", d.Id())
	})

	t.Run("shall remove the project from state when the 422 is for a project already gone", func(t *testing.T) {
		d := resourceProject().TestResourceData()
		d.SetId("foo")

		diags := resourceProjectDeleteRetry(context.TODO(), d, &stubProjectDelete{
			deleteErr: refused,
			getErr:    neon.Error{HTTPCode: http.StatusNotFound},
		})

		assert.False(t, diags.HasError())
		assert.Empty(t, d.Id())
	})
}

// fakeNeonAPI refuses every DELETE with a 422 and answers every GET with the given response.
type fakeNeonAPI struct {
	getStatus int
	getBody   string
}

func (f fakeNeonAPI) Do(req *http.Request) (*http.Response, error) {
	status, body := http.StatusUnprocessableEntity, `{"message":"delete refused"}`
	if req.Method == http.MethodGet {
		status, body = f.getStatus, f.getBody
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func Test_deleteRetry_refusedDelete(t *testing.T) {
	notFound := fakeNeonAPI{getStatus: http.StatusNotFound, getBody: `{"message":"not found"}`}

	tests := map[string]struct {
		resource *schema.Resource
		attrs    map[string]string
		deleteFn schema.DeleteContextFunc
		exists   fakeNeonAPI
		gone     fakeNeonAPI
	}{
		"neon_branch": {
			resource: resourceBranch(),
			attrs:    map[string]string{"project_id": "p"},
			deleteFn: resourceBranchDeleteRetry,
			exists:   fakeNeonAPI{getStatus: http.StatusOK, getBody: `{}`},
			gone:     notFound,
		},
		"neon_endpoint": {
			resource: resourceEndpoint(),
			attrs:    map[string]string{"project_id": "p"},
			deleteFn: resourceEndpointDeleteRetry,
			exists:   fakeNeonAPI{getStatus: http.StatusOK, getBody: `{}`},
			gone:     notFound,
		},
		"neon_database": {
			resource: resourceDatabase(),
			attrs:    map[string]string{"project_id": "p", "branch_id": "b", "name": "db"},
			deleteFn: resourceDatabaseDeleteRetry,
			exists:   fakeNeonAPI{getStatus: http.StatusOK, getBody: `{}`},
			gone:     notFound,
		},
		"neon_role": {
			resource: resourceRole(),
			attrs:    map[string]string{"project_id": "p", "branch_id": "b", "name": "r"},
			deleteFn: resourceRoleDeleteRetry,
			exists:   fakeNeonAPI{getStatus: http.StatusOK, getBody: `{}`},
			gone:     notFound,
		},
		"neon_vpc_endpoint_assignment": {
			resource: resourceVPCEndpointAssignment(),
			attrs:    map[string]string{"org_id": "o", "region_id": "aws-eu-west-2", "vpc_endpoint_id": "vpce-1"},
			deleteFn: resourceVPCEndpointAssignmentDeleteRetry,
			exists:   fakeNeonAPI{getStatus: http.StatusOK, getBody: `{}`},
			gone:     notFound,
		},
		"neon_vpc_endpoint_restriction": {
			resource: resourceVPCEndpointRestriction(),
			attrs:    map[string]string{"project_id": "p", "vpc_endpoint_id": "vpce-1"},
			deleteFn: resourceVPCEndpointRestrictionDeleteRetry,
			exists:   fakeNeonAPI{getStatus: http.StatusOK, getBody: `{"endpoints":[{"vpc_endpoint_id":"vpce-1"}]}`},
			gone:     fakeNeonAPI{getStatus: http.StatusOK, getBody: `{"endpoints":[{"vpc_endpoint_id":"vpce-2"}]}`},
		},
	}

	newData := func(t *testing.T, r *schema.Resource, attrs map[string]string) *schema.ResourceData {
		d := r.TestResourceData()
		d.SetId("foo")
		for k, v := range attrs {
			if err := d.Set(k, v); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}

	for name, tt := range tests {
		t.Run(name+" shall fail and stay in state when the resource still exists", func(t *testing.T) {
			client, _ := neon.NewClient(neon.Config{Key: "foo", HTTPClient: tt.exists})
			d := newData(t, tt.resource, tt.attrs)

			diags := tt.deleteFn(context.TODO(), d, client)

			if !assert.True(t, diags.HasError()) {
				return
			}
			assert.Contains(t, diags[0].Summary, "delete refused")
			assert.Equal(t, "foo", d.Id())
		})

		t.Run(name+" shall leave state when the resource is gone", func(t *testing.T) {
			client, _ := neon.NewClient(neon.Config{Key: "foo", HTTPClient: tt.gone})
			d := newData(t, tt.resource, tt.attrs)

			diags := tt.deleteFn(context.TODO(), d, client)

			assert.False(t, diags.HasError())
			assert.Empty(t, d.Id())
		})
	}
}
