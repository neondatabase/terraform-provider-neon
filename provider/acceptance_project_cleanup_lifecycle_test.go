package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// Exercise the real Terraform apply/import/refresh/destroy runner with the
// same wrapper used by migrated suites. Only HTTP calls are replaced.
func TestAcceptanceCleanupTerraformLifecycle(t *testing.T) {
	t.Parallel()
	api := &cleanupLifecycleHTTP{}
	client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
	require.NoError(t, err)
	reporter := &cleanupReporter{}
	projects := newAcceptanceProjects(reporter, client)
	name := projects.NewName("pr-")
	config := fmt.Sprintf("resource \"neon_project\" \"this\" { name = %q }", name)
	checks := 0
	resource.UnitTest(t, projects.prepare(resource.TestCase{
		ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
		Steps: []resource.TestStep{
			{Config: config, Check: func(state *terraform.State) error {
				checks++
				return resource.TestCheckResourceAttr("neon_project.this", "name", name)(state)
			}},
			{ResourceName: "neon_project.this", ImportState: true, ImportStateVerify: true},
			{Config: config},
		},
	}))
	require.Equal(t, 1, checks, "existing checks must run once, unchanged")
	require.NoError(t, projects.cleanup(context.Background()))
	api.mu.Lock()
	defer api.mu.Unlock()
	require.Equal(t, []string{"p-1", "p-1"}, api.deletes, "Terraform destroy, followed by harmless saved-ID 404 cleanup")
	require.Zero(t, api.unscopedLists)
	require.Zero(t, api.recoveryLists, "a final successful step captures ownership again after import")
	require.Empty(t, reporter.failures)
}

type cleanupLifecycleHTTP struct {
	project projectLibrariesAPI
	mu      sync.Mutex
	name    string
	deleted bool

	deletes       []string
	recoveryLists int
	unscopedLists int
}

func (f *cleanupLifecycleHTTP) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Method == http.MethodPost && req.URL.Path == "/api/v2/projects" {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Project struct {
				Name string `json:"name"`
			} `json:"project"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		f.name = payload.Project.Name
		req.Body = io.NopCloser(strings.NewReader(string(body)))
	}
	if req.Method == http.MethodGet && req.URL.Path == "/api/v2/projects" {
		if req.URL.Query().Get("search") == "" {
			f.unscopedLists++
			return nil, fmt.Errorf("unscoped recovery list refused")
		}
		f.recoveryLists++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"projects":[{"id":"p-other","name":"pr-production"}]}`))}, nil
	}
	if req.Method == http.MethodDelete && req.URL.Path == "/api/v2/projects/p-1" {
		f.deletes = append(f.deletes, "p-1")
		if f.deleted {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"message":"already deleted"}`))}, nil
		}
		f.deleted = true
	}
	response, err := f.project.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if projectJSON, ok := payload["project"]; ok {
		var project map[string]any
		if err := json.Unmarshal(projectJSON, &project); err != nil {
			return nil, err
		}
		project["name"] = f.name
		payload["project"], err = json.Marshal(project)
		if err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: response.StatusCode, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}
