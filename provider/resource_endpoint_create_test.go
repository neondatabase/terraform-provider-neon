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

// The schema defaults type and compute_provisioner. Create must send the
// defaults when the configuration leaves them unset, and must not send
// unset computed attributes, such as region_id and name, as empty strings.
func TestEndpointCreateSendsSchemaDefaults(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		typeLine string
		wantType string
	}{
		{name: "type unset uses the default", typeLine: "", wantType: "read_write"},
		{name: "type set is sent as configured", typeLine: `type = "read_only"`, wantType: "read_only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &endpointAPIFixture{endpointType: tt.wantType}
			client, err := neon.NewClient(neon.Config{Key: "fixture", HTTPClient: api})
			require.NoError(t, err)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{sdk: client}),
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`resource "neon_endpoint" "this" {
  project_id = "p-1"
  branch_id  = "br-1"
  %s
}`, tt.typeLine),
					Check: func(_ *terraform.State) error {
						endpoint := api.createRequest(t)
						require.Equal(t, tt.wantType, endpoint["type"], "type")
						require.Equal(t, "k8s-neonvm", endpoint["provisioner"], "provisioner")
						for _, unset := range []string{"region_id", "name"} {
							if v, ok := endpoint[unset]; ok {
								require.NotEqual(t, "", v, "%s sent as an empty string", unset)
							}
						}
						return nil
					},
				}},
			})
			require.Empty(t, api.unexpectedCalls(), "unexpected API calls")
		})
	}
}

// endpointAPIFixture answers the endpoint API calls made during create, read
// and delete, and records the create request body.
type endpointAPIFixture struct {
	endpointType string

	mu         sync.Mutex
	createBody []byte
	unexpected []string
}

func (f *endpointAPIFixture) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	const base = "/api/v2/projects/p-1/endpoints"
	switch {
	case req.Method == http.MethodPost && req.URL.Path == base:
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		f.createBody = body
	case (req.Method == http.MethodGet || req.Method == http.MethodDelete) && req.URL.Path == base+"/ep-1":
	default:
		f.unexpected = append(f.unexpected, req.Method+" "+req.URL.Path)
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"unexpected call"}`)),
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
  "endpoint": {
    "id": "ep-1", "project_id": "p-1", "branch_id": "br-1",
    "region_id": "aws-us-east-2", "type": %q, "host": "ep-1.example",
    "autoscaling_limit_min_cu": 0.25, "autoscaling_limit_max_cu": 2,
    "suspend_timeout_seconds": 0, "provisioner": "k8s-neonvm",
    "disabled": false, "passwordless_access": true, "settings": {}
  },
  "operations": []
}`, f.endpointType))),
	}, nil
}

func (f *endpointAPIFixture) createRequest(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.createBody, "no create request was sent")
	var req struct {
		Endpoint map[string]any `json:"endpoint"`
	}
	require.NoError(t, json.Unmarshal(f.createBody, &req))
	return req.Endpoint
}

func (f *endpointAPIFixture) unexpectedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unexpected
}
