package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestHIPAAPrerequisiteCallbackErrors(t *testing.T) {
	t.Parallel()
	denied := neon.Error{HTTPCode: http.StatusForbidden, Details: []neon.ErrorDetails{{Message: "organization cannot contain HIPAA"}}}
	transportFailure := errors.New("connection refused")
	for _, tt := range []struct {
		name       string
		status     int
		body       string
		err        error
		wantSkip   bool
		wantError  bool
		missingOrg bool
		operations int
	}{
		{name: "SDK value 400", status: 400, body: `{"message":"organization cannot contain HIPAA"}`, wantSkip: true},
		{name: "SDK value 403", status: 403, body: `{"message":"organization cannot contain HIPAA"}`, wantSkip: true},
		{name: "uppercase denial", status: 403, body: `{"message":"ORGANIZATION CANNOT CONTAIN HIPAA"}`, wantSkip: true},
		{name: "denial in later detail", status: 403, body: `{"message":"request rejected","reasons":[{"message":"generic validation failure"},{"message":"organization cannot contain HIPAA"}]}`, wantSkip: true},
		{name: "wrapped SDK value", err: fmt.Errorf("proxy: %w", denied), wantSkip: true},
		{name: "wrapped SDK pointer", err: fmt.Errorf("proxy: %w", &denied), wantSkip: true},
		{name: "empty details", err: neon.Error{HTTPCode: 403}, wantError: true},
		{name: "authentication denied", status: 401, body: `{"message":"organization cannot contain HIPAA"}`, wantError: true},
		{name: "HIPAA permission error", status: 403, body: `{"message":"not authorized to update hipaa"}`, wantError: true},
		{name: "backend failure", status: 500, body: `{"message":"organization cannot contain HIPAA"}`, wantError: true},
		{name: "unrecognized HIPAA error", status: 403, body: `{"message":"hipaa unavailable"}`, wantError: true},
		{name: "other API error", status: 400, body: `{"message":"invalid project setting"}`, wantError: true},
		{name: "transport error", err: transportFailure, wantError: true},
		{name: "missing saved org", status: 403, body: `{"message":"organization cannot contain HIPAA"}`, missingOrg: true, wantSkip: true},
		{name: "success without operations", status: 200, body: `{"project":{"id":"p-1"},"operations":[]}`},
		{name: "success waits for operation", status: 200, body: `{"project":{"id":"p-1"},"operations":[{"id":"op-1","project_id":"p-1","status":"running"}]}`, operations: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &hipaaPrerequisiteHTTP{status: tt.status, body: tt.body, err: tt.err}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			reporter := &hipaaPrerequisiteSpy{}
			state := hipaaPrerequisiteState()
			if tt.missingOrg {
				delete(state.RootModule().Resources["neon_project.this"].Primary.Attributes, "org_id")
			}
			var got error
			require.NotPanics(t, func() { got = hipaaPrerequisiteCheck(reporter, client)(state) })
			if tt.wantError {
				require.Error(t, got)
				if tt.err == transportFailure {
					require.ErrorIs(t, got, transportFailure, "transport cause must remain available")
				}
			} else {
				require.NoError(t, got)
			}
			require.Empty(t, reporter.failures, "errors must be returned, not hidden in assertion side effects")
			if tt.wantSkip {
				require.Len(t, reporter.skips, 1)
				orgID := "org-fixture"
				if tt.missingOrg {
					orgID = "unknown"
				}
				require.Contains(t, reporter.skips[0], orgID, "skip uses saved org metadata, not failed response")
			} else {
				require.Empty(t, reporter.skips, "unrelated errors must not skip the acceptance case")
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			require.Equal(t, 1, api.patches)
			require.Equal(t, tt.operations, api.operationGets)
			var request struct {
				Project struct {
					Settings struct {
						HIPAA *bool `json:"hipaa"`
					} `json:"settings"`
				} `json:"project"`
			}
			require.NoError(t, json.Unmarshal(api.request, &request))
			require.NotNil(t, request.Project.Settings.HIPAA)
			require.False(t, *request.Project.Settings.HIPAA, "prerequisite must not enable HIPAA")
		})
	}
}

func TestHIPAAPrerequisiteMissingProjectMetadata(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing resource", "missing primary", "empty ID"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state := hipaaPrerequisiteState()
			switch name {
			case "missing resource":
				delete(state.RootModule().Resources, "neon_project.this")
			case "missing primary":
				state.RootModule().Resources["neon_project.this"].Primary = nil
			case "empty ID":
				state.RootModule().Resources["neon_project.this"].Primary.ID = ""
			}
			api := &hipaaPrerequisiteHTTP{}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)
			reporter := &hipaaPrerequisiteSpy{}
			var got error
			require.NotPanics(t, func() { got = hipaaPrerequisiteCheck(reporter, client)(state) })
			require.Error(t, got)
			require.Empty(t, reporter.skips)
			require.Zero(t, api.patches)
			require.Zero(t, api.operationGets)
		})
	}
}

func hipaaPrerequisiteState() *terraform.State {
	return &terraform.State{Modules: []*terraform.ModuleState{{
		Path: []string{"root"},
		Resources: map[string]*terraform.ResourceState{
			"neon_project.this": {Primary: &terraform.InstanceState{
				ID: "p-1", Attributes: map[string]string{"org_id": "org-fixture"},
			}},
		},
	}}}
}

// A reporter spy records Skipf without ending the unit test. The real
// acceptance callback receives *testing.T and retains normal skip behavior.
type hipaaPrerequisiteSpy struct {
	failures []string
	skips    []string
}

func (s *hipaaPrerequisiteSpy) Errorf(format string, args ...any) {
	s.failures = append(s.failures, fmt.Sprintf(format, args...))
}

func (s *hipaaPrerequisiteSpy) Skipf(format string, args ...any) {
	s.skips = append(s.skips, fmt.Sprintf(format, args...))
}

// Only the SDK's HTTP boundary is replaced. Unexpected paths cannot reach
// any real API, and pending operations complete after one recorded poll.
type hipaaPrerequisiteHTTP struct {
	status int
	body   string
	err    error

	mu            sync.Mutex
	patches       int
	operationGets int
	request       []byte
}

func (f *hipaaPrerequisiteHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Method == http.MethodGet && req.URL.Path == "/api/v2/projects/p-1/operations/op-1" {
		f.operationGets++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"operation":{"id":"op-1","project_id":"p-1","status":"finished"}}`)),
		}, nil
	}
	if req.Method != http.MethodPatch || req.URL.Path != "/api/v2/projects/p-1" {
		return nil, fmt.Errorf("unexpected SDK request: %s %s", req.Method, req.URL.Path)
	}
	f.patches++
	var err error
	f.request, err = io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}
