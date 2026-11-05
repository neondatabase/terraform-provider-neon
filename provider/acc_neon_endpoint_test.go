package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestEndpoint(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "endpoint"

	var verify = func(state *terraform.State) error {
		r, ok := state.RootModule().Resources["neon_endpoint.this"]
		assert.Truef(t, ok, "no endpoint resource found")
		resp, err := client.GetProjectEndpoint(r.Primary.Attributes["project_id"], r.Primary.ID)
		if err != nil {
			return err
		}
		assert.Equal(t, resp.Endpoint.RegionID, r.Primary.Attributes["region_id"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.PasswordlessAccess), r.Primary.Attributes["passwordless_access"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.Disabled), r.Primary.Attributes["disabled"])
		assert.Equal(t, string(resp.Endpoint.Provisioner), r.Primary.Attributes["compute_provisioner"])
		assert.Equal(t, resp.Endpoint.Host, r.Primary.Attributes["host"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.AutoscalingLimitMinCu),
			r.Primary.Attributes["autoscaling_limit_min_cu"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.AutoscalingLimitMaxCu),
			r.Primary.Attributes["autoscaling_limit_max_cu"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Endpoint.SuspendTimeoutSeconds),
			r.Primary.Attributes["suspend_timeout_seconds"])
		assert.Equal(t, resp.Endpoint.Type.String(), r.Primary.Attributes["type"])
		if resp.Endpoint.Name != nil {
			assert.Equal(t, *resp.Endpoint.Name, r.Primary.Attributes["name"])
		}
		if resp.Endpoint.Settings.PgSettings != nil && len(*resp.Endpoint.Settings.PgSettings) > 0 {
			var actual neon.PgSettingsData
			actualRaw, ok := r.Primary.Attributes["pg_settings"]
			assert.Truef(t, ok, "no pg_settings attribute found")
			assert.NoErrorf(t, json.Unmarshal([]byte(actualRaw), &actual), "could not deserialize pg_settings")
			assert.EqualValuesf(t, *resp.Endpoint.Settings.PgSettings, actual,
				"unexpected pg_settings")
		}
		if resp.Endpoint.Settings.PreloadLibraries != nil {
			cnt, ok := r.Primary.Attributes["enabled_libraries.#"]
			if !ok {
				if len(resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries) > 0 {
					return fmt.Errorf("unexpected size of enabled_libraries")
				}
				cnt = "0"
			}
			if !assert.EqualValuesf(t,
				fmt.Sprintf("%d", len(resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries)), cnt,
				"unexpected size of enabled_libraries") {
				return fmt.Errorf("unexpected size of enabled_libraries")
			}

			if len(resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries) > 0 {
				for i, lib := range resp.Endpoint.Settings.PreloadLibraries.EnabledLibraries {
					if !assert.Equalf(t, lib, r.Primary.Attributes[fmt.Sprintf("enabled_libraries.%d", i)],
						"unexpected value for enabled_libraries[%d]", i) {
						return fmt.Errorf("unexpected value for enabled_libraries[%d]", i)
					}
				}
			}

			actualRaw, ok := r.Primary.Attributes["enabled_libraries_use_defaults"]
			if resp.Endpoint.Settings.PreloadLibraries.UseDefaults == nil {
				if !assert.Truef(t, !ok || actualRaw == "", "unexpected enabled_libraries_use_defaults value") {
					return fmt.Errorf("unexpected enabled_libraries_use_defaults value")
				}
			} else {
				if !assert.Truef(t, ok, "no enabled_libraries_use_defaults attribute found") {
					return fmt.Errorf("no enabled_libraries_use_defaults attribute found")
				}
				actual, err := strconv.ParseBool(actualRaw)
				if !assert.NoErrorf(t, err, "could not parse enabled_libraries_use_defaults") {
					return fmt.Errorf("could not parse enabled_libraries_use_defaults")
				}
				if !assert.EqualValuesf(t, *resp.Endpoint.Settings.PreloadLibraries.UseDefaults, actual,
					"unexpected enabled_libraries_use_defaults") {
					return fmt.Errorf("unexpected enabled_libraries_use_defaults")
				}
			}

		}
		return nil
	}

	t.Run("shall create read-only endpoint for default branch with inherited autoscaling config "+
		"and update its attr in-place", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
					resource "neon_endpoint" "this" {
						project_id = neon_project.this.id
						branch_id  = neon_project.this.branch.id
						region_id  = neon_project.this.region_id
						type       = "read_only"
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "type", "read_only"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host_pooler"),
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_endpoint.this"]
								assert.Truef(t, ok, "no endpoint resource found")

								host, ok := r.Primary.Attributes["host"]
								assert.Truef(t, ok, `"host" attribute not found`)

								hostPooler, ok := r.Primary.Attributes["host_pooler"]
								assert.Truef(t, ok, `"host_pooler" attribute not found`)

								assert.Equal(t, host, strings.ReplaceAll(hostPooler, "-pooler", ""))
								return nil
							},
							verify,
						),
					},
					// rename
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.branch.id
											region_id  = neon_project.this.region_id
											type       = "read_only"
											name       = %q
										}
										`, projectName, "foo"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "name", "foo"),
							verify,
						),
					},
					// update autoscaling
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.branch.id
											region_id  = neon_project.this.region_id
											type       = "read_only"
					
											autoscaling_limit_min_cu = 0.25
											autoscaling_limit_max_cu = 0.5
											suspend_timeout_seconds  = 120
										}
										`, projectName),
						Check: resource.ComposeTestCheckFunc(
							// the name remains as set in the prev. step despite the config removal from the manifest
							resource.TestCheckResourceAttr("neon_endpoint.this", "name", "foo"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
							verify,
						),
					},
					// 	update passwordless access
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.branch.id
											region_id  = neon_project.this.region_id
											type       = "read_only"
											disabled   = true
										}
										`, projectName),
						Check: resource.ComposeTestCheckFunc(
							// the autoscaling configs remain as set in the prev. step despite the config removal from the manifest
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "disabled", "true"),
							verify,
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
										resource "neon_endpoint" "this" {
											project_id = neon_project.this.id
											branch_id  = neon_project.this.branch.id
											region_id  = neon_project.this.region_id
											type       = "read_only"
										}
										`, projectName),
						Check: resource.ComposeTestCheckFunc(
							// the disabled config remains as set in the prev. step despite the config removal from the manifest
							resource.TestCheckResourceAttr("neon_endpoint.this", "disabled", "true"),
							verify,
						),
					},
					// update enabled_libraries
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					
					resource "neon_endpoint" "this" {
						project_id        = neon_project.this.id
						branch_id         = neon_project.this.branch.id
						region_id         = neon_project.this.region_id
						type              = "read_only"
						enabled_libraries = ["uuid-ossp"]
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#",
								"1"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0",
								"uuid-ossp"),
							resource.TestCheckResourceAttr("neon_endpoint.this",
								"enabled_libraries_use_defaults", "false"),
							verify,
						),
					},
					// update enabled_libraries_use_defaults
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}

					resource "neon_endpoint" "this" {
						project_id                     = neon_project.this.id
						branch_id                      = neon_project.this.branch.id
						region_id                      = neon_project.this.region_id
						type                           = "read_only"
						enabled_libraries              = ["uuid-ossp"]
						enabled_libraries_use_defaults = true   
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#",
								"1"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0",
								"uuid-ossp"),
							resource.TestCheckResourceAttr("neon_endpoint.this",
								"enabled_libraries_use_defaults", "true"),
							verify,
						),
					},
					// rm enabled_libraries and enabled_libraries_use_defaults from the manifest
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}

					resource "neon_endpoint" "this" {
						project_id                     = neon_project.this.id
						branch_id                      = neon_project.this.branch.id
						region_id                      = neon_project.this.region_id
						type                           = "read_only"
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#",
								"1"),
							resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0",
								"uuid-ossp"),
							resource.TestCheckResourceAttr("neon_endpoint.this",
								"enabled_libraries_use_defaults", "true"),
							verify,
						),
					},
				},
			},
		)
	})

	t.Run("shall create a read-write endpoint", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}
					resource "neon_branch" "this" {project_id = neon_project.this.id}
					resource "neon_endpoint" "this" {
						project_id = neon_project.this.id
						branch_id  = neon_branch.this.id
						region_id  = neon_project.this.region_id
						type       = "read_write"
					}
					`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_endpoint.this", "type", "read_write"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host"),
							resource.TestCheckResourceAttrSet("neon_endpoint.this", "host_pooler"),
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_endpoint.this"]
								assert.Truef(t, ok, "no endpoint resource found")

								host, ok := r.Primary.Attributes["host"]
								assert.Truef(t, ok, `"host" attribute not found`)

								hostPooler, ok := r.Primary.Attributes["host_pooler"]
								assert.Truef(t, ok, `"host_pooler" attribute not found`)

								assert.Equal(t, host, strings.ReplaceAll(hostPooler, "-pooler", ""))
								return nil
							},
							verify,
						),
					},
				},
			},
		)
	})

	var endpointConfig = func(projectName, customConfig string) string {
		return fmt.Sprintf(`resource "neon_project" "this" {
		region_id = "aws-us-east-2"
		name = %q
}
	resource "neon_endpoint" "this" {
		project_id = neon_project.this.id
		branch_id  = neon_project.this.branch.id
		region_id  = neon_project.this.region_id
		type       = "read_only"
		%s
	}
	`, projectName, customConfig)
	}

	t.Run("shall create w/ custom autoscaling limits", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `
		autoscaling_limit_min_cu = 0.25
		autoscaling_limit_max_cu = 0.5`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom suspend timeout", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `suspend_timeout_seconds = 120`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom name", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `name = "custom-endpoint"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "name", "custom-endpoint"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom compute_provisioner", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `compute_provisioner = "k8s-neonvm"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "compute_provisioner", "k8s-neonvm"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom disabled", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `disabled = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "disabled", "true"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom enabled_libraries", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `enabled_libraries = ["uuid-ossp"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#", "1"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0", "uuid-ossp"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom enabled_libraries_use_defaults", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `enabled_libraries_use_defaults = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries_use_defaults", "true"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom passwordless access", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `passwordless_access = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "passwordless_access", "true"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom autoscaling and suspend timeout", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `
		autoscaling_limit_min_cu = 0.25
		autoscaling_limit_max_cu = 0.5
		suspend_timeout_seconds  = 120`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom name and compute provisioner", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `
		name                = "custom-endpoint"
		compute_provisioner = "k8s-neonvm"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "name", "custom-endpoint"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "compute_provisioner", "k8s-neonvm"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ custom libraries and defaults", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `
		enabled_libraries              = ["uuid-ossp"]
		enabled_libraries_use_defaults = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#", "1"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0", "uuid-ossp"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries_use_defaults", "true"),
					verify,
				),
			}},
		})
	})

	t.Run("shall create w/ all custom configs", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: endpointConfig(projectName, `
		name                         = "custom-endpoint"
		autoscaling_limit_min_cu    = 0.25
		autoscaling_limit_max_cu    = 0.5
		suspend_timeout_seconds     = 120
		compute_provisioner          = "k8s-neonvm"
		disabled                     = true
		enabled_libraries            = ["uuid-ossp"]
		enabled_libraries_use_defaults = true
		passwordless_access          = true`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_endpoint.this", "name", "custom-endpoint"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_min_cu", "0.25"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "autoscaling_limit_max_cu", "0.5"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "suspend_timeout_seconds", "120"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "compute_provisioner", "k8s-neonvm"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "disabled", "true"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.#", "1"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries.0", "uuid-ossp"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "enabled_libraries_use_defaults", "true"),
					resource.TestCheckResourceAttr("neon_endpoint.this", "passwordless_access", "true"),
					verify,
				),
			}},
		})
	})
}

func TestRecreateEndpointIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "endpointRecreation"

	var preConfig = func(projectName string) {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}

		resp, err := client.ListProjectEndpoints(ref.ID)
		if err != nil {
			panic(err)
		}
		for _, endpoint := range resp.Endpoints {
			if endpoint.Type == endpointTypeReadOnly {
				op, err := client.DeleteProjectEndpoint(ref.ID, endpoint.ID)
				if err != nil {
					panic(err)
				}
				waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
			}
		}
	}

	t.Run("shall indicate non empty plan if the endpoint was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`
							resource "neon_project" "this" { name = "%s" }
							resource "neon_endpoint" "this" {
								project_id = neon_project.this.id
								branch_id  = neon_project.this.branch.id
								type       = "%s"
							}
						`, projectName, endpointTypeReadOnly),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_endpoint.this",
								"type", endpointTypeReadOnly.String(),
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName)
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the endpoint was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" { name = "%s" }
resource "neon_endpoint" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	type       = "%s"
}`, projectName, endpointTypeReadOnly)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
					},
					{
						PreConfig: func() {
							preConfig(projectName)
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_endpoint.this"]
							assert.False(t, ok, "resource neon_endpoint.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate endpoint upon update if it was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`
							resource "neon_project" "this" { name = "%s" }
							resource "neon_endpoint" "this" {
								project_id = neon_project.this.id
								branch_id  = neon_project.this.branch.id
								type       = "%s"
								disabled   = false
							}
						`, projectName, endpointTypeReadOnly),
						Check: resource.TestCheckResourceAttr(
							"neon_endpoint.this",
							"disabled", "false",
						),
					},
					{
						Config: fmt.Sprintf(`
							resource "neon_project" "this" { name = "%s" }
							resource "neon_endpoint" "this" {
								project_id = neon_project.this.id
								branch_id  = neon_project.this.branch.id
								type       = "%s"
								disabled   = true
							}
						`, projectName, endpointTypeReadOnly),
						PreConfig: func() {
							preConfig(projectName)
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_endpoint.this",
								"disabled", "true",
							),
							func(_ *terraform.State) error {
								ref, err := readProjectInfo(client, projectName)
								if err != nil {
									return err
								}

								resp, err := client.ListProjectEndpoints(ref.ID)
								if err != nil {
									return err
								}
								assert.Len(t, resp.Endpoints, 2,
									"2 endpoints are expected after recreation")
								for _, endpoint := range resp.Endpoints {
									if endpoint.Type == endpointTypeReadOnly {
										assert.True(t, endpoint.Disabled,
											"endpoint should be disabled after recreation")
									}
								}
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import endpoint if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`
			resource "neon_project" "this" { name = "%s" }
			resource "neon_endpoint" "this" {
				project_id = neon_project.this.id
				branch_id  = neon_project.this.branch.id
				type       = "%s"
			}
		`, projectName, endpointTypeReadOnly)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
					},
					{
						Config:       config,
						ImportState:  true,
						ResourceName: "neon_endpoint.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							ref, err := readProjectInfo(client, projectName)
							if err != nil {
								return "", err
							}

							resp, err := client.ListProjectEndpoints(ref.ID)
							if err != nil {
								return "", err
							}
							var endpointID string
							for _, endpoint := range resp.Endpoints {
								if endpoint.Type == endpointTypeReadOnly {
									endpointID = endpoint.ID
									op, err := client.DeleteProjectEndpoint(ref.ID, endpointID)
									if err != nil {
										return "", err
									}
									waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations)
								}
							}

							return fmt.Sprintf("%s/%s", ref.ID, endpointID), nil
						},
						ExpectError: regexp.MustCompile("Cannot import non-existent remote object"),
					},
					// to avoid dangling resources on post-test destroy
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" { name = "%s" }`, projectName),
					},
				},
			})
	})
}
