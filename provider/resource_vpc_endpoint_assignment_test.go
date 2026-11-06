package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

type httpClientPipe struct {
	cnt  int
	Pipe []*HTTPClientMock
}

func (h *httpClientPipe) Do(req *http.Request) (*http.Response, error) {
	if len(h.Pipe)-1 < h.cnt {
		panic("pipe is empty")
	}
	client := h.Pipe[h.cnt]
	h.cnt++
	return client.Do(req)
}

func TestVpcEndpointAssignment(t *testing.T) {
	t.Parallel()

	t.Run("shall create and update in-place the resource", func(t *testing.T) {
		const (
			orgID         = "orgID"
			regionID      = "regionID"
			vpcEndpointID = "endpointID"
			labelInit     = "label0"
			labelUpdate   = "label1"
		)

		var config = func(label string) string {
			return fmt.Sprintf(`resource "neon_vpc_endpoint_assignment" "this" {
org_id          = %q
region_id       = %q
vpc_endpoint_id = %q
label           = %q
}`, orgID, regionID, vpcEndpointID, label)
		}

		httpClient := &httpClientPipe{
			Pipe: []*HTTPClientMock{
				// mocks creation request's handling
				{
					StatusCode: http.StatusOK,
				},
				// mocks refresh request's handling
				{
					StatusCode: http.StatusOK,
					Body: []byte(fmt.Sprintf(`{
"label": %q,
"vpc_endpoint_id": %q,
"state":"new",
"num_restricted_projects": 0
}`, labelInit, vpcEndpointID)),
				},
				// mocks pre-update refresh request's handling
				{
					StatusCode: http.StatusOK,
					Body: []byte(fmt.Sprintf(`{
"label": %q,
"vpc_endpoint_id": %q,
"state":"new",
"num_restricted_projects": 0
}`, labelInit, vpcEndpointID)),
				},
				// mocks update request's handling
				{
					StatusCode: http.StatusOK,
				},
				// mocks post-update refresh request's handling
				{
					StatusCode: http.StatusOK,
					Body: []byte(fmt.Sprintf(`{
"label": %q,
"vpc_endpoint_id": %q,
"state":"new",
"num_restricted_projects": 0
}`, labelUpdate, vpcEndpointID)),
				},
				// mocks delete request's handling
				{
					StatusCode: http.StatusOK,
				},
			},
		}
		sdkMock, err := neon.NewClient(neon.Config{
			Key:        "foo",
			HTTPClient: httpClient,
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				// shall provision the resource
				{
					Config: config(labelInit),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "id",
							"orgID/regionID/endpointID"),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "org_id",
							orgID),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "region_id",
							regionID),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "vpc_endpoint_id",
							vpcEndpointID),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "label",
							labelInit),
						func(_ *terraform.State) error {
							assert.Equal(t, http.MethodPost, httpClient.Pipe[0].Req.Method)
							assert.Equal(t,
								fmt.Sprintf("/api/v2/organizations/%s/vpc/region/%s/vpc_endpoints/%s", orgID,
									regionID, vpcEndpointID),
								httpClient.Pipe[0].Req.URL.Path)
							return nil
						},
					),
				},
				// shall update the label in-place
				{
					Config: config(labelUpdate),
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "id",
							"orgID/regionID/endpointID"),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "org_id",
							orgID),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "region_id",
							regionID),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "vpc_endpoint_id",
							vpcEndpointID),
						resource.TestCheckResourceAttr("neon_vpc_endpoint_assignment.this", "label",
							labelUpdate),
						func(_ *terraform.State) error {
							assert.Equal(t, http.MethodPost, httpClient.Pipe[3].Req.Method)
							assert.Equal(t,
								fmt.Sprintf("/api/v2/organizations/%s/vpc/region/%s/vpc_endpoints/%s", orgID,
									regionID, vpcEndpointID),
								httpClient.Pipe[3].Req.URL.Path)
							return nil
						},
					),
				},
			},
		})
	})

	t.Run("shall import existing resource", func(t *testing.T) {
		const (
			orgID         = "orgID"
			regionID      = "regionID"
			vpcEndpointID = "endpointID"
			label         = "label0"
		)

		httpClient := &httpClientPipe{
			Pipe: []*HTTPClientMock{
				// mocks read request's handling
				{
					StatusCode: http.StatusOK,
					Body: []byte(fmt.Sprintf(`{
"label": %q,
"vpc_endpoint_id": %q,
"state":"accepted",
"num_restricted_projects": 0
}`, label, vpcEndpointID)),
				},
				// mocks refresh request's handling
				{
					StatusCode: http.StatusOK,
					Body: []byte(fmt.Sprintf(`{
"label": %q,
"vpc_endpoint_id": %q,
"state":"new",
"num_restricted_projects": 0
}`, label, vpcEndpointID)),
				},
				// mocks delete request's handling
				{
					StatusCode: http.StatusOK,
				},
			},
		}
		sdkMock, err := neon.NewClient(neon.Config{
			Key:        "foo",
			HTTPClient: httpClient,
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`resource "neon_vpc_endpoint_assignment" "this" {
org_id          = %q
region_id       = %q
vpc_endpoint_id = %q
label           = %q
}`, orgID, regionID, vpcEndpointID, label),
					ImportState:   true,
					ResourceName:  "neon_vpc_endpoint_assignment.this",
					ImportStateId: fmt.Sprintf("%s/%s/%s", orgID, regionID, vpcEndpointID),
					ImportStateCheck: func(states []*terraform.InstanceState) error {
						assert.Equal(t, http.MethodGet, httpClient.Pipe[0].Req.Method)
						s := states[0]
						assert.Equal(t, label, s.Attributes["label"])
						assert.Equal(t, orgID, s.Attributes["org_id"])
						assert.Equal(t, regionID, s.Attributes["region_id"])
						assert.Equal(t, vpcEndpointID, s.Attributes["vpc_endpoint_id"])
						assert.Equal(t, fmt.Sprintf("%s/%s/%s", orgID, regionID, vpcEndpointID),
							s.Attributes["id"])
						return nil
					},
				},
			},
		})
	})

	t.Run("shall fail to import the resource given its ID is invalid", func(t *testing.T) {
		sdkMock, err := neon.NewClient(neon.Config{
			Key: "foo",
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config: `resource "neon_vpc_endpoint_assignment" "this" {
org_id          = "foo"
region_id       = "foo"
vpc_endpoint_id = "foo"
label           = "foo"
}`,
					ImportState:   true,
					ResourceName:  "neon_vpc_endpoint_assignment.this",
					ImportStateId: "id",
					ExpectError:   regexp.MustCompile("Invalid Terraform State VPC Endpoint Assignment ID"),
				},
				{
					Config: `resource "neon_vpc_endpoint_assignment" "this" {
org_id          = "foo"
region_id       = "foo"
vpc_endpoint_id = "foo"
label           = "foo"
}`,
					ImportState:   true,
					ResourceName:  "neon_vpc_endpoint_assignment.this",
					ImportStateId: "0/1",
					ExpectError:   regexp.MustCompile("Invalid Terraform State VPC Endpoint Assignment ID"),
				},
			},
		})
	})

	t.Run("shall fail to import non-existent resource given its invalid ID", func(t *testing.T) {
		httpClient := &httpClientPipe{
			Pipe: []*HTTPClientMock{
				// mocks read request with throttling handling
				{
					StatusCode: http.StatusTooManyRequests,
				},

				// mocks read request
				{
					StatusCode: http.StatusNotFound,
					Body:       []byte(`{"message": "not found"}`),
				},
			},
		}

		sdkMock, err := neon.NewClient(neon.Config{
			Key:        "foo",
			HTTPClient: httpClient,
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config: `resource "neon_vpc_endpoint_assignment" "this" {
org_id          = "foo"
region_id       = "foo"
vpc_endpoint_id = "foo"
label           = "foo"
}`,
					ImportState:   true,
					ResourceName:  "neon_vpc_endpoint_assignment.this",
					ImportStateId: "foo/foo/foo",
					ExpectError:   regexp.MustCompile("404"),
				},
			},
		})
	})
}
