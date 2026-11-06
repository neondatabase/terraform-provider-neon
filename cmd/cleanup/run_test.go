package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestCleanupCLIRejectsInvalidInputBeforeClient(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{}, {"--execute"}, {"--project-id="}, {"--project-id", " p-one"},
		{"--project-id", "p-one/p-two"}, {"--project-id", "*"},
		{"--project-id", "p-one,p-two"}, {"--project-id", "p-one?other=all"},
		{"--project-id", "https://example.test"}, {"--unknown"},
		{"--project-id", "p-one", "--unprotect"}, {"--project-id", "p-one", "extra"},
		{"--project-id", "p-one", "--timeout", "0s"},
		{"--project-id", "p-one", "--timeout", "-1s"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			called := false
			err := run(context.Background(), args, io.Discard, func(context.Context) (*neon.Client, error) {
				called = true
				return nil, errors.New("client must not be constructed")
			})
			require.Error(t, err)
			require.False(t, called, "invalid targeting must fail before credentials/client/API use")
		})
	}
}

func TestCleanupCLIHelpNeverConstructsClient(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	called := false
	err := run(context.Background(), []string{"--help"}, &output, func(context.Context) (*neon.Client, error) {
		called = true
		return nil, errors.New("help must be offline")
	})
	require.NoError(t, err)
	require.False(t, called)
	require.Contains(t, output.String(), "project-id")
	require.Contains(t, output.String(), "execute")
}

func TestCleanupCLIRequiresExplicitExecute(t *testing.T) {
	t.Parallel()
	for _, execute := range []bool{false, true} {
		t.Run(fmt.Sprint(execute), func(t *testing.T) {
			t.Parallel()
			api := &cliHTTP{}
			var output bytes.Buffer
			args := []string{"--project-id", "p-one", "--project-id", "p-two", "--project-id", "p-one"}
			if execute {
				args = append(args, "--execute")
			}
			require.NoError(t, run(context.Background(), args, &output, cliFactory(api)))
			want := []string{
				"GET /api/v2/projects/p-one", "GET /api/v2/projects/p-one/branches",
				"GET /api/v2/projects/p-two", "GET /api/v2/projects/p-two/branches",
			}
			if execute {
				want = append(want, "DELETE /api/v2/projects/p-one", "DELETE /api/v2/projects/p-two")
			}
			require.Equal(t, want, api.requests(), "preflight every deduplicated target before any delete")
			require.Contains(t, output.String(), "p-one")
			require.Contains(t, output.String(), "project p-two")
			if !execute {
				require.Contains(t, output.String(), "preview")
			}
		})
	}
}

func TestCleanupCLIProtectedTarget(t *testing.T) {
	t.Parallel()
	for _, execute := range []bool{false, true} {
		t.Run(fmt.Sprint(execute), func(t *testing.T) {
			t.Parallel()
			api := &cliHTTP{protected: map[string]bool{"p-two": true}}
			var output bytes.Buffer
			args := []string{"--project-id", "p-one", "--project-id", "p-two"}
			if execute {
				args = append(args, "--execute")
			}
			err := run(context.Background(), args, &output, cliFactory(api))
			if execute {
				require.ErrorContains(t, err, "protected")
			} else {
				require.NoError(t, err)
			}
			require.Contains(t, output.String(), "protected=true")
			require.Equal(t, []string{
				"GET /api/v2/projects/p-one", "GET /api/v2/projects/p-one/branches",
				"GET /api/v2/projects/p-two", "GET /api/v2/projects/p-two/branches",
			}, api.requests(), "protected target blocks the entire execution; never PATCH or DELETE")
		})
	}
}

func TestCleanupCLIPreflightFailurePreventsAllDeletes(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{
		"project 403", "project 404", "project 500", "branch list error",
		"project identity mismatch", "branch project mismatch", "branch missing ID", "missing branches", "null branches",
		"repeated cursor", "later page error", "protected later page", "protected earlier page",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			api := &cliHTTP{scenario: scenario}
			err := run(context.Background(), []string{"--execute", "--project-id", "p-one", "--project-id", "p-two"}, io.Discard, cliFactory(api))
			require.Error(t, err)
			require.Contains(t, err.Error(), "p-two", "failure names the explicit target")
			require.NotEmpty(t, api.requests())
			for _, request := range api.requests() {
				require.True(t, strings.HasPrefix(request, "GET "), "failed preflight must remain read-only: %s", request)
			}
		})
	}
}

func TestCleanupCLIBranchPagination(t *testing.T) {
	t.Parallel()
	api := &cliHTTP{scenario: "two pages"}
	require.NoError(t, run(context.Background(), []string{"--execute", "--project-id", "p-two"}, io.Discard, cliFactory(api)))
	require.Equal(t, []string{
		"GET /api/v2/projects/p-two", "GET /api/v2/projects/p-two/branches",
		"GET /api/v2/projects/p-two/branches?cursor=next", "DELETE /api/v2/projects/p-two",
	}, api.requests())
}

func TestCleanupCLIDeleteFailureStopsRemainingTargets(t *testing.T) {
	t.Parallel()
	for _, status := range []int{403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			api := &cliHTTP{deleteStatus: status}
			err := run(context.Background(), []string{"--execute", "--project-id", "p-one", "--project-id", "p-two"}, io.Discard, cliFactory(api))
			require.ErrorContains(t, err, "p-one")
			require.Equal(t, []string{
				"GET /api/v2/projects/p-one", "GET /api/v2/projects/p-one/branches",
				"GET /api/v2/projects/p-two", "GET /api/v2/projects/p-two/branches",
				"DELETE /api/v2/projects/p-one",
			}, api.requests(), "do not delete a later project after an earlier delete fails")
		})
	}
}

func TestCleanupCLICancellationAndClientErrors(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"cancelled", "client error", "cancel during preflight"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("client setup failed")
			api := &cliHTTP{}
			factory := cliFactory(api)
			switch scenario {
			case "cancelled":
				cancel()
			case "client error":
				factory = func(context.Context) (*neon.Client, error) { return nil, cause }
			case "cancel during preflight":
				api.cancel = cancel
			}
			err := run(ctx, []string{"--execute", "--project-id", "p-one"}, io.Discard, factory)
			if scenario == "client error" {
				require.ErrorIs(t, err, cause)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			for _, request := range api.requests() {
				require.True(t, strings.HasPrefix(request, "GET "))
			}
		})
	}
}

func TestCleanupCLITotalTimeoutReachesHTTPRequest(t *testing.T) {
	t.Parallel()
	api := &cliHTTP{scenario: "timeout"}
	err := run(context.Background(), []string{"--execute", "--project-id", "p-one", "--timeout", "20ms"}, io.Discard, cliFactory(api))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	for _, request := range api.requests() {
		require.True(t, strings.HasPrefix(request, "GET "))
	}
}

func TestCleanupCLIOutputFailurePreventsDeletion(t *testing.T) {
	t.Parallel()
	api := &cliHTTP{}
	cause := errors.New("output unavailable")
	err := run(context.Background(), []string{"--execute", "--project-id", "p-one"}, cliBrokenWriter{cause}, cliFactory(api))
	require.ErrorIs(t, err, cause)
	for _, request := range api.requests() {
		require.True(t, strings.HasPrefix(request, "GET "))
	}
}

func cliFactory(api *cliHTTP) clientFactory {
	return func(ctx context.Context) (*neon.Client, error) {
		return neon.NewClient(neon.Config{Key: "fixture", HTTPClient: &cliBoundHTTP{ctx: ctx, base: api}})
	}
}

type cliBrokenWriter struct{ err error }

func (w cliBrokenWriter) Write([]byte) (int, error) { return 0, w.err }

type cliHTTP struct {
	mu           sync.Mutex
	log          []string
	protected    map[string]bool
	scenario     string
	deleteStatus int
	cancel       context.CancelFunc
}

func (f *cliHTTP) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

// The SDK stays real. This transport rejects project enumeration, arbitrary
// endpoints and all writes other than explicit project deletes.
func (f *cliHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if _, ok := req.Context().Deadline(); !ok {
		return nil, errors.New("cleanup request has no deadline")
	}
	f.log = append(f.log, req.Method+" "+req.URL.RequestURI())
	if f.scenario == "timeout" {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	const prefix = "/api/v2/projects/"
	if !strings.HasPrefix(req.URL.Path, prefix) {
		return nil, fmt.Errorf("unscoped/disallowed request: %s %s", req.Method, req.URL.Path)
	}
	parts := strings.Split(strings.TrimPrefix(req.URL.Path, prefix), "/")
	id := parts[0]
	if id != "p-one" && id != "p-two" {
		return nil, fmt.Errorf("unexpected explicit project %q", id)
	}
	status := 200
	var payload any
	switch {
	case req.Method == http.MethodGet && len(parts) == 1:
		actual := id
		if id == "p-two" {
			switch f.scenario {
			case "project 403":
				status = 403
			case "project 404":
				status = 404
			case "project 500":
				status = 500
			case "project identity mismatch":
				actual = "p-other"
			}
		}
		payload = map[string]any{"project": map[string]any{"id": actual, "name": "project " + id}}
		if f.cancel != nil {
			f.cancel()
		}
	case req.Method == http.MethodGet && len(parts) == 2 && parts[1] == "branches":
		branchProject, branchID, protected := id, "br-"+id, f.protected[id]
		cursor := ""
		if id == "p-two" {
			switch f.scenario {
			case "branch list error":
				status = 500
			case "branch project mismatch":
				branchProject = "p-other"
			case "branch missing ID":
				branchID = ""
			case "repeated cursor":
				cursor = "repeat"
			case "later page error", "protected later page", "protected earlier page", "two pages":
				if req.URL.Query().Get("cursor") == "" {
					cursor = "next"
					protected = f.scenario == "protected earlier page"
				} else if f.scenario == "later page error" {
					status = 500
				} else if f.scenario == "protected later page" {
					protected = true
				}
			}
		}
		body := map[string]any{"branches": []any{map[string]any{"id": branchID, "project_id": branchProject, "protected": protected}}}
		if cursor != "" {
			body["pagination"] = map[string]any{"next": cursor}
		}
		if id == "p-two" && f.scenario == "missing branches" {
			delete(body, "branches")
		}
		if id == "p-two" && f.scenario == "null branches" {
			body["branches"] = nil
		}
		payload = body
	case req.Method == http.MethodDelete && len(parts) == 1:
		if id == "p-one" && f.deleteStatus != 0 {
			status = f.deleteStatus
		}
		payload = map[string]any{"project": map[string]any{"id": id}}
	default:
		return nil, fmt.Errorf("unprotect or unexpected mutation refused: %s %s", req.Method, req.URL.Path)
	}
	if status >= 400 {
		payload = map[string]string{"message": "fixture failure"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}
