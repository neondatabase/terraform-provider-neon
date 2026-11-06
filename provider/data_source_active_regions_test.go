package provider

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

type HTTPClientMock struct {
	Err        error
	Req        *http.Request
	StatusCode int
	Body       []byte
}

func (h *HTTPClientMock) Do(r *http.Request) (*http.Response, error) {
	h.Req = r
	if h.Err != nil {
		return nil, h.Err
	}
	return &http.Response{
		StatusCode: h.StatusCode,
		Body:       io.NopCloser(bytes.NewReader(h.Body)),
	}, nil
}

func TestActiveRegionsDataSource(t *testing.T) {
	t.Parallel()

	t.Run("shall contain 3 active regions and a fixed id", func(t *testing.T) {
		sdkMock, err := neon.NewClient(neon.Config{
			Key: "foo",
			HTTPClient: &HTTPClientMock{
				StatusCode: http.StatusOK,
				Body: []byte(`{
  "regions": [
    {
      "region_id": "aws-us-east-2",
      "name": "AWS US East 2 (Ohio)",
      "default": false,
      "geo_lat": "39.96",
      "geo_long": "-83"
    },
    {
      "region_id": "aws-us-east-1",
      "name": "AWS US East 1 (N. Virginia)",
      "default": true,
      "geo_lat": "38.13",
      "geo_long": "-78.45"
    },
    {
      "region_id": "aws-us-west-2",
      "name": "AWS US West 2 (Oregon)",
      "default": false,
      "geo_lat": "46.15",
      "geo_long": "-123.88"
    }
  ]
}`),
			},
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config: `data "neon_active_regions" "this" {}`,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "id", "activeRegions"),
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "regions.#", "3"),
					),
				},
			},
		})
	})

	t.Run("shall contain 3 active regions, org_id and a fixed id", func(t *testing.T) {
		sdkMock, err := neon.NewClient(neon.Config{
			Key: "foo",
			HTTPClient: &HTTPClientMock{
				StatusCode: http.StatusOK,
				Body: []byte(`{
  "regions": [
    {
      "region_id": "aws-us-east-2",
      "name": "AWS US East 2 (Ohio)",
      "default": false,
      "geo_lat": "39.96",
      "geo_long": "-83"
    },
    {
      "region_id": "aws-us-east-1",
      "name": "AWS US East 1 (N. Virginia)",
      "default": true,
      "geo_lat": "38.13",
      "geo_long": "-78.45"
    },
    {
      "region_id": "aws-us-west-2",
      "name": "AWS US West 2 (Oregon)",
      "default": false,
      "geo_lat": "46.15",
      "geo_long": "-123.88"
    }
  ]
}`),
			},
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config: `data "neon_active_regions" "this" {org_id = "foo"}`,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "id", "activeRegions"),
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "org_id", "foo"),
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "regions.#", "3"),
					),
				},
			},
		})
	})

	t.Run("shall contain all defined region's attributes", func(t *testing.T) {
		sdkMock, err := neon.NewClient(neon.Config{
			Key: "foo",
			HTTPClient: &HTTPClientMock{
				StatusCode: http.StatusOK,
				Body: []byte(`{
  "regions": [
    {
      "region_id": "aws-us-east-2",
      "name": "AWS US East 2 (Ohio)",
      "default": false,
      "geo_lat": "39.96",
      "geo_long": "-83"
    }
  ]
}`),
			},
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config: `data "neon_active_regions" "this" {}`,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "regions.0.region_id",
							"aws-us-east-2"),
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "regions.0.name",
							"AWS US East 2 (Ohio)"),
						resource.TestCheckResourceAttr("data.neon_active_regions.this", "regions.0.default",
							"false"),
					),
				},
			},
		})
	})

	t.Run("shall yield an error", func(t *testing.T) {
		sdkMock, err := neon.NewClient(neon.Config{
			Key: "foo",
			HTTPClient: &HTTPClientMock{
				Err: errors.New("error"),
			},
		})
		assert.NoErrorf(t, err, "could not create Neon SDK mock")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newUnitTestProviderFactories(&providerAdapter{
				sdk: sdkMock,
			}),
			Steps: []resource.TestStep{
				{
					Config:      `data "neon_active_regions" "this" {}`,
					ExpectError: regexp.MustCompile("error"),
				},
			},
		})
	})
}
