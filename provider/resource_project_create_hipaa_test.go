package provider

import (
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

// Create must send hipaa when it is set, whether true or false.
// An earlier version sent it only when false, so hipaa = true never reached the
// API.
func TestProjectCreateSendsHIPAA(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		hipaaLine string
		// want is the expected settings.hipaa in the create request; nil means
		// the key must be absent.
		want any
	}{
		{name: "hipaa true", hipaaLine: "hipaa = true", want: true},
		{name: "hipaa false", hipaaLine: "hipaa = false", want: false},
		{name: "hipaa unset", hipaaLine: "", want: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &projectCreateFixture{hipaa: tt.want}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`resource "neon_project" "this" {
  name = "hipaa"
  %s
}`, tt.hipaaLine),
					Check: func(_ *terraform.State) error {
						settings := api.createSettings(t)
						got, ok := settings["hipaa"]
						if tt.want == nil {
							require.False(t, ok, "hipaa sent although unset: %v", got)
							return nil
						}
						require.True(t, ok, "hipaa missing from the create request")
						require.Equal(t, tt.want, got)
						return nil
					},
				}},
			})
		})
	}
}

// projectCreateFixture returns one project response for every call and
// records the create request body. Its values match the schema defaults, so
// the plan after apply is empty.
type projectCreateFixture struct {
	// hipaa is the settings.hipaa value the API reports; nil leaves it out.
	hipaa any

	mu         sync.Mutex
	createBody []byte
}

func (f *projectCreateFixture) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Method == http.MethodPost && req.URL.Path == "/api/v2/projects" {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		f.createBody = body
	}

	settings := map[string]any{}
	if f.hipaa != nil {
		settings["hipaa"] = f.hipaa
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
  "project": {
    "id": "p-1", "name": "hipaa", "region_id": "aws-us-east-2", "pg_version": 17,
    "org_id": "org-1", "provisioner": "k8s-neonvm", "store_passwords": true,
    "history_retention_seconds": 86400,
    "default_endpoint_settings": {"suspend_timeout_seconds": 0},
    "settings": %s
  },
  "branch": {"id": "br-1", "project_id": "p-1", "name": "main", "default": true, "protected": false},
  "branches": [{"id": "br-1", "project_id": "p-1", "name": "main", "default": true, "protected": false}],
  "endpoints": [{"id": "ep-1", "project_id": "p-1", "branch_id": "br-1", "type": "read_write",
    "region_id": "aws-us-east-2", "host": "ep-1.example", "provisioner": "k8s-neonvm",
    "autoscaling_limit_min_cu": 0.25, "autoscaling_limit_max_cu": 2,
    "suspend_timeout_seconds": 0, "settings": {}, "created_at": "2026-01-01T00:00:00Z"}],
  "databases": [{"id": 1, "branch_id": "br-1", "name": "neondb", "owner_name": "neondb_owner"}],
  "roles": [{"branch_id": "br-1", "name": "neondb_owner"}],
  "operations": []
}`, settingsJSON))),
	}, nil
}

func (f *projectCreateFixture) createSettings(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.createBody, "no create request was sent")
	var req struct {
		Project struct {
			Settings map[string]any `json:"settings"`
		} `json:"project"`
	}
	require.NoError(t, json.Unmarshal(f.createBody, &req))
	return req.Project.Settings
}
