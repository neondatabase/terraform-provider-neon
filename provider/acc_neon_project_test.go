package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tfResource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestRecreateProjectIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "projectRecreation-"

	var preConfig = func(projectName string) string {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}
		_, err = client.DeleteProject(ref.ID)
		if err != nil {
			panic(err)
		}
		return ref.ID
	}

	t.Run("shall indicate non empty plan if the project was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = %q}`, projectName)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this",
								"name", projectName,
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

	t.Run("shall destroy even if the project was deleted outside of terraform,", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = %q}`, projectName)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this",
								"name", projectName,
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName)
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_project.this"]
							assert.False(t, ok, "resource neon_project.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate project upon update if it was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var refProjectID string
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s-foo"}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this",
								"name", fmt.Sprintf("%s-foo", projectName),
							),
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s-bar"}`, projectName),
						PreConfig: func() {
							refProjectID = preConfig(fmt.Sprintf("%s-foo", projectName))
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this",
								"name", fmt.Sprintf("%s-bar", projectName),
							),
							func(_ *terraform.State) error {
								got, err := readProjectInfo(client, fmt.Sprintf("%s-bar", projectName))
								if err != nil {
									return err
								}
								assert.NotEqualf(t, refProjectID, got.ID,
									"project ID should be different after recreation")
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import project if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = %q}`, projectName)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
					},
					{
						Config: config,
						PreConfig: func() {
							preConfig(projectName)
						},
						ImportState:  true,
						ResourceName: "neon_project.this",
						ExpectError:  regexp.MustCompile("Cannot import non-existent remote object"),
					},
				},
			})
	})
}

func TestProject(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "pr-"

	var verify = func(state *terraform.State) error {
		r, ok := state.RootModule().Resources["neon_project.this"]
		assert.Truef(t, ok, "no project resource found")
		resp, err := client.GetProject(r.Primary.ID)
		if err != nil {
			return err
		}
		assert.Equal(t, resp.Project.RegionID, r.Primary.Attributes["region_id"])
		assert.Equal(t, *resp.Project.OrgID, r.Primary.Attributes["org_id"])
		assert.Equal(t, resp.Project.Name, r.Primary.Attributes["name"])
		assert.Equal(t, fmt.Sprintf("%d", resp.Project.PgVersion), r.Primary.Attributes["pg_version"])
		assert.EqualValues(t, resp.Project.Provisioner, r.Primary.Attributes["compute_provisioner"])
		assert.Equal(t, fmt.Sprintf("%v", resp.Project.StorePasswords), r.Primary.Attributes["store_password"])

		settings := resp.Project.Settings
		if settings != nil {
			if settings.EnableLogicalReplication != nil {
				assert.Equal(t, fmt.Sprintf("%v", *settings.EnableLogicalReplication),
					r.Primary.Attributes["enable_logical_replication"])
			}

			if settings.BlockPublicConnections != nil {
				assert.Equal(t, fmt.Sprintf("%v", *settings.BlockPublicConnections),
					r.Primary.Attributes["block_public_connections"])
			}

			if settings.BlockVpcConnections != nil {
				assert.Equal(t, fmt.Sprintf("%v", *settings.BlockVpcConnections), r.Primary.Attributes["block_vpc_connections"])
			}

			if settings.Hipaa != nil {
				assert.Equal(t, fmt.Sprintf("%v", *settings.Hipaa), r.Primary.Attributes["hipaa"])
			}

			if settings.PreloadLibraries != nil {
				if len(settings.PreloadLibraries.EnabledLibraries) > 0 {
					assert.Equal(t, fmt.Sprintf("%d", len(settings.PreloadLibraries.EnabledLibraries)),
						r.Primary.Attributes["enabled_libraries.#"])
				} else {
					assert.Empty(t, r.Primary.Attributes["enabled_libraries.#"])
				}

				if settings.PreloadLibraries.UseDefaults != nil {
					assert.Equal(t, fmt.Sprintf("%v", *settings.PreloadLibraries.UseDefaults),
						r.Primary.Attributes["enabled_libraries_use_defaults"])
				}
			} else {
				assert.Equal(t, "", r.Primary.Attributes["enabled_libraries"])
				assert.Equal(t, "", r.Primary.Attributes["enabled_libraries_use_defaults"])
			}

			if settings.AllowedIps != nil {
				if len(settings.AllowedIps.Ips) > 0 {
					assert.Equal(t, fmt.Sprintf("%d", len(settings.AllowedIps.Ips)),
						r.Primary.Attributes["allowed_ips.#"])
				} else {
					assert.Empty(t, r.Primary.Attributes["allowed_ips.#"])
				}

				if settings.AllowedIps.ProtectedBranchesOnly != nil {
					assert.Equal(t, fmt.Sprintf("%v", *settings.AllowedIps.ProtectedBranchesOnly),
						r.Primary.Attributes["allowed_ips_protected_branches_only"])
				}
			} else {
				assert.Equal(t, "", r.Primary.Attributes["allowed_ips"])
				assert.Equal(t, "", r.Primary.Attributes["allowed_ips_protected_branches_only"])
			}

			if settings.MaintenanceWindow != nil {
				assert.Equal(t, settings.MaintenanceWindow.StartTime, r.Primary.Attributes["maintenance_window.start_time"])
				assert.Equal(t, settings.MaintenanceWindow.EndTime, r.Primary.Attributes["maintenance_window.end_time"])
				assert.Equal(t, fmt.Sprintf("%v", len(settings.MaintenanceWindow.Weekdays)),
					r.Primary.Attributes["maintenance_window.weekdays.#"])
				for i, weekday := range settings.MaintenanceWindow.Weekdays {
					assert.Equal(t, fmt.Sprintf("%v", weekday),
						r.Primary.Attributes[fmt.Sprintf("maintenance_window.weekdays.%d", i)])
				}
			}

			if settings.Quota != nil {
				if settings.Quota.WrittenDataBytes != nil {
					assert.Equal(t, fmt.Sprintf("%d", *settings.Quota.WrittenDataBytes),
						r.Primary.Attributes["quota.written_data_bytes"])
				}
				if settings.Quota.LogicalSizeBytes != nil {
					assert.Equal(t, fmt.Sprintf("%d", *settings.Quota.LogicalSizeBytes),
						r.Primary.Attributes["quota.logical_size_bytes"])
				}
				if settings.Quota.DataTransferBytes != nil {
					assert.Equal(t, fmt.Sprintf("%d", *settings.Quota.DataTransferBytes),
						r.Primary.Attributes["quota.data_transfer_bytes"])
				}
				if settings.Quota.ComputeTimeSeconds != nil {
					assert.Equal(t, fmt.Sprintf("%d", *settings.Quota.ComputeTimeSeconds),
						r.Primary.Attributes["quota.compute_time_seconds"])
				}
				if settings.Quota.ActiveTimeSeconds != nil {
					assert.Equal(t, fmt.Sprintf("%d", *settings.Quota.ActiveTimeSeconds),
						r.Primary.Attributes["quota.active_time_seconds"])
				}
			}
		}

		defaultEndpointSettings := resp.Project.DefaultEndpointSettings
		var asserAutoscalingUnits = func(want *neon.ComputeUnit, got string) {
			if want == nil {
				return
			}
			var ref string
			switch {
			case *want >= 1:
				ref = fmt.Sprintf("%d", int64(*want))
			// to avoid rounding error
			case *want > 0.3:
				ref = "0.5"
			// to avoid rounding error
			case *want > 0:
				ref = "0.25"
			default:
				ref = fmt.Sprintf("%.2f", *want)
			}
			assert.Equal(t, ref, got)
		}
		if defaultEndpointSettings != nil {
			asserAutoscalingUnits(defaultEndpointSettings.AutoscalingLimitMinCu, r.Primary.Attributes["autoscaling_limit_min_cu"])
			asserAutoscalingUnits(defaultEndpointSettings.AutoscalingLimitMaxCu, r.Primary.Attributes["autoscaling_limit_max_cu"])
			assert.Equal(t, fmt.Sprintf("%d", *defaultEndpointSettings.SuspendTimeoutSeconds),
				r.Primary.Attributes["suspend_timeout_seconds"])
		}

		return nil
	}

	t.Parallel()

	t.Run("shall create a project w/ default configs only", func(t *testing.T) {
		endpointSchemaResp := tfResource.SchemaResponse{}
		endpointResource := &neonEndpointResource{}
		endpointResource.Schema(context.TODO(), tfResource.SchemaRequest{}, &endpointSchemaResp)
		endpointSchemaAttributes := endpointSchemaResp.Schema.Attributes

		branchSchemaResp := tfResource.SchemaResponse{}
		branchResource := &neonBranchResource{}
		branchResource.Schema(context.TODO(), tfResource.SchemaRequest{}, &branchSchemaResp)
		branchSchemaAttributes := branchSchemaResp.Schema.Attributes

		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: `resource "neon_project" "this" {}`,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttrSet("neon_project.this", "id"),
							resource.TestCheckResourceAttrSet("neon_project.this", "org_id"),
							resource.TestCheckResourceAttrSet("neon_project.this", "name"),
							resource.TestCheckResourceAttrSet("neon_project.this", "region_id"),
							resource.TestCheckResourceAttrSet("neon_project.this", "pg_version"),
							resource.TestCheckResourceAttrSet("neon_project.this", "history_retention_seconds"),
							resource.TestCheckResourceAttrSet("neon_project.this", "database_name"),
							resource.TestCheckResourceAttrSet("neon_project.this", "database_user"),
							resource.TestCheckResourceAttrSet("neon_project.this", "autoscaling_limit_min_cu"),
							resource.TestCheckResourceAttrSet("neon_project.this", "autoscaling_limit_max_cu"),
							resource.TestCheckResourceAttr("neon_project.this", "hipaa", "false"),

							resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication",
								"false"),
							resource.TestCheckResourceAttr("neon_project.this", "block_public_connections",
								"false"),
							resource.TestCheckResourceAttr("neon_project.this", "block_vpc_connections",
								"false"),
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips_protected_branches_only",
								"false"),
							resource.TestCheckResourceAttr("neon_project.this", "store_password",
								"true"),
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#", "0"),

							resource.TestCheckResourceAttr("neon_project.this", "enabled_libraries.#", "0"),

							resource.TestCheckNoResourceAttr("neon_project.this", "enabled_libraries_use_defaults"),
							resource.TestCheckResourceAttr("neon_project.this", "suspend_timeout_seconds", "0"),

							resource.TestCheckResourceAttr("neon_project.this", "compute_provisioner", "k8s-neonvm"),

							// block attrs.
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.%", "3"),
							resource.TestCheckResourceAttrSet("neon_project.this", "maintenance_window.start_time"),
							resource.TestCheckResourceAttrSet("neon_project.this", "maintenance_window.end_time"),
							resource.TestCheckResourceAttrWith("neon_project.this",
								"maintenance_window.weekdays.#", func(s string) error {
									v, err := strconv.Atoi(s)
									if err != nil {
										return err
									}
									if v == 0 {
										return fmt.Errorf("expected non empty maintenance_window.weekdays")
									}
									return nil
								}),

							resource.TestCheckResourceAttr("neon_project.this", "quota.%", "5"),

							resource.TestCheckResourceAttr("neon_project.this", "branch.%",
								fmt.Sprintf("%d", len(branchSchemaAttributes))),
							resource.TestCheckResourceAttrPair("neon_project.this", "branch.project_id",
								"neon_project.this", "id"),
							resource.TestCheckResourceAttr("neon_project.this", "branch.protected", "false"),
							// TODO: remove after off. release: database_host is present for BW compatibility reasons only
							resource.TestCheckResourceAttrSet("neon_project.this", "default_branch_id"),
							// TODO: remove after off. release: database_host is present for BW compatibility reasons only
							resource.TestCheckResourceAttrPair("neon_project.this", "branch.id",
								"neon_project.this", "default_branch_id"),

							resource.TestCheckResourceAttr("neon_project.this", "primary_compute.%",
								fmt.Sprintf("%d", len(endpointSchemaAttributes))),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.project_id",
								"neon_project.this", "id"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.region_id",
								"neon_project.this", "region_id"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.branch_id",
								"neon_project.this", "branch.id"),
							resource.TestCheckResourceAttr("neon_project.this", "primary_compute.type", "read_write"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.autoscaling_limit_min_cu",
								"neon_project.this", "autoscaling_limit_min_cu"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.autoscaling_limit_max_cu",
								"neon_project.this", "autoscaling_limit_max_cu"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.suspend_timeout_seconds",
								"neon_project.this", "suspend_timeout_seconds"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.compute_provisioner",
								"neon_project.this", "compute_provisioner"),
							resource.TestCheckResourceAttr("neon_project.this", "primary_compute.disabled", "false"),
							resource.TestCheckResourceAttr("neon_project.this", "primary_compute.passwordless_access", "true"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.enabled_libraries",
								"neon_project.this", "enabled_libraries"),
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.enabled_libraries_use_defaults",
								"neon_project.this", "enabled_libraries_use_defaults"),

							// TODO: remove after off. release: database_host is present for BW compatibility reasons only
							resource.TestCheckResourceAttrSet("neon_project.this", "default_endpoint_id"),
							// TODO: remove after off. release: database_host is present for BW compatibility reasons only
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.id",
								"neon_project.this", "default_endpoint_id"),

							// TODO: remove after off. release: it's present for BW compatibility reasons only
							resource.TestCheckResourceAttrSet("neon_project.this", "database_host"),
							// TODO: remove after off. release: database_host is present for BW compatibility reasons only
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.host",
								"neon_project.this", "database_host"),
							// TODO: remove after off. release: it's present for BW compatibility reasons only
							resource.TestCheckResourceAttrSet("neon_project.this", "database_host_pooler"),
							// TODO: remove after off. release: database_host_pooler is present for BW compatibility reasons only
							resource.TestCheckResourceAttrPair("neon_project.this", "primary_compute.host_pooler",
								"neon_project.this", "database_host_pooler"),

							// verify removal of the attributes that are present in v0.18.0
							resource.TestCheckNoResourceAttr("neon_project.this", "connection_uri"),
							// verify removal of the attributes that are present in v0.18.0
							resource.TestCheckNoResourceAttr("neon_project.this", "connection_uri_pooler"),
							// verify removal of the attributes that are present in v0.18.0
							resource.TestCheckNoResourceAttr("neon_project.this", "database_password"),
							verify,
						),
					},
				},
			})
	})

	t.Run("shall create a project w/ custom name", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name = %q
						}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "name", projectName),
							verify,
						),
					},
				},
			})
	})

	t.Run("shall create a project w/ custom org_id", func(t *testing.T) {
		orgID := os.Getenv("ORG_ID")
		if orgID == "" {
			t.Skip("the test required ORG_ID env. variable set to the Neon organization id")
		}
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							org_id = %q
						}`, projectName, orgID),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "org_id", orgID),
							verify,
						),
					},
				},
			})
	})

	t.Run("shall create a project w/ custom enable_logical_replication and update it", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var projectID string
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
						}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "false"),
							verify,
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							enable_logical_replication = false
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "false"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID != projectID {
									return fmt.Errorf("project was replaced instead of update in-place")
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							enable_logical_replication = true
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "true"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID != projectID {
									return fmt.Errorf("project was replaced instead of update in-place")
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							enable_logical_replication = false
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "false"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID == projectID {
									return fmt.Errorf("project was not replaced as expected")
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "enable_logical_replication", "false"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID != projectID {
									return fmt.Errorf("project was replaced instead of update in-place")
								}
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall create a project w/ custom hipaa and update it", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var projectID string
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
						}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "hipaa", "false"),
							verify,
							hipaaPrerequisiteCheck(t, client),
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							hipaa = false
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "hipaa", "false"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID != projectID {
									return fmt.Errorf("project was replaced instead of update in-place")
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							hipaa = true
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "hipaa", "true"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID != projectID {
									return fmt.Errorf("project was replaced instead of update in-place")
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
							hipaa = false
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "hipaa", "false"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID == projectID {
									return fmt.Errorf("project was not replaced as expected")
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name   = %q
						}`, projectName),
						PreConfig: func() {
							p, err := readProjectInfo(client, projectName)
							if err != nil {
								panic(err)
							}
							projectID = p.ID
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "hipaa", "false"),
							verify,
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								if r.Primary.ID != projectID {
									return fmt.Errorf("project was replaced instead of update in-place")
								}
								return nil
							},
						),
					},
				},
			})
	})

	var config = func(projectName string, customCfg string) string {
		return fmt.Sprintf(`resource "neon_project" "this" {
		name = %q
		%s
	}`, projectName, customCfg)
	}

	t.Run("shall create a project w/ custom maintenance window", func(t *testing.T) {
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config(newProjectName(projectNamePrefix),
							`maintenance_window = {
	start_time = "00:00"
	end_time   = "01:00"
	weekdays   = [1,2]
}`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.start_time", "00:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.end_time", "01:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.#", "2"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.0", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.1", "2"),
							verify,
						),
					},
				},
			})
	})

	t.Run("shall update the name", func(t *testing.T) {
		before, after := newProjectName(projectNamePrefix), newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name = %q
						}`, before),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "name", before),
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
							name = %q
						}`, after),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "name", after),
						),
					},
				},
			})
	})

	t.Run("shall update maintenance window", func(t *testing.T) {
		// see: https://github.com/neondatabase/terraform-provider-neon/issues/60
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config(projectName, ``),
						Check:  verify,
					},
					{
						Config: config(projectName, `maintenance_window = {
	start_time = "00:00"
	end_time   = "01:00"
	weekdays   = [1,2]
}`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.start_time", "00:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.end_time", "01:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.#", "2"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.0", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.1", "2"),
							verify,
						),
					},
					{
						Config: config(projectName, `maintenance_window = {
	start_time = "00:00"
	end_time   = "01:00"
	weekdays   = [1]
}`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.start_time", "00:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.end_time", "01:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.#", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.0", "1"),
							verify,
						),
					},
					{
						Config: config(projectName, `maintenance_window = {
	start_time = "00:00"
	end_time   = "02:00"
	weekdays   = [1]
}`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.start_time", "00:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.end_time", "02:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.#", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.0", "1"),
							verify,
						),
					},
					{
						Config: config(projectName, `maintenance_window = {
	start_time = "01:00"
	end_time   = "02:00"
	weekdays   = [1]
}`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.start_time", "01:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.end_time", "02:00"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.#", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.0", "1"),
							verify,
						),
					},
					{
						Config: config(projectName, `maintenance_window = {
	start_time = "01:30"
	end_time   = "02:30"
	weekdays   = [2]
}`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.start_time", "01:30"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.end_time", "02:30"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.#", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "maintenance_window.weekdays.0", "2"),
							verify,
						),
					},
				},
			})
	})

	t.Run("shall create a new project with different compute configs and the default compute's configs",
		func(t *testing.T) {
			projectName := newProjectName(projectNamePrefix)
			resource.Test(
				t, resource.TestCase{
					ProtoV6ProviderFactories: newProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
		name = %q
		autoscaling_limit_min_cu = 0.25
		autoscaling_limit_max_cu = 1
		suspend_timeout_seconds  = 300
		
		primary_compute = {
			autoscaling_limit_min_cu = 0.5
			autoscaling_limit_max_cu = 2
			suspend_timeout_seconds  = -1
		}
}
`, projectName),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"name", projectName,
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_min_cu", "0.25",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_max_cu", "1",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"suspend_timeout_seconds", "300",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_min_cu", "0.5",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_max_cu", "2",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.suspend_timeout_seconds", "-1",
								),
								verify,
							),
						},
					},
				})
		})

	t.Run("shall update project's compute configs w/o affecting default compute's configs",
		func(t *testing.T) {
			projectName := newProjectName(projectNamePrefix)
			resource.Test(
				t, resource.TestCase{
					ProtoV6ProviderFactories: newProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
		name = %q
		autoscaling_limit_min_cu = 0.25
		autoscaling_limit_max_cu = 1
		suspend_timeout_seconds  = 300
		
		primary_compute = {
			autoscaling_limit_min_cu = 0.5
			autoscaling_limit_max_cu = 2
			suspend_timeout_seconds  = -1
		}
}
`, projectName),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"name", projectName,
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_min_cu", "0.25",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_max_cu", "1",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"suspend_timeout_seconds", "300",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_min_cu", "0.5",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_max_cu", "2",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.suspend_timeout_seconds", "-1",
								),
							),
						},
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
		name = %q
		autoscaling_limit_min_cu = 0.5
		autoscaling_limit_max_cu = 2
		suspend_timeout_seconds  = 600
		
		primary_compute = {
			autoscaling_limit_min_cu = 0.5
			autoscaling_limit_max_cu = 2
			suspend_timeout_seconds  = -1
		}
}
`, projectName),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"name", projectName,
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_min_cu", "0.5",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_max_cu", "2",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"suspend_timeout_seconds", "600",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_min_cu", "0.5",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_max_cu", "2",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.suspend_timeout_seconds", "-1",
								),
							),
						},
					},
				})
		})

	t.Run("shall update default compute's configs w/o affecting project's compute configs",
		func(t *testing.T) {
			projectName := newProjectName(projectNamePrefix)
			resource.Test(
				t, resource.TestCase{
					ProtoV6ProviderFactories: newProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
		name = %q
		autoscaling_limit_min_cu = 0.25
		autoscaling_limit_max_cu = 1
		suspend_timeout_seconds  = 300
		
		primary_compute = {
			autoscaling_limit_min_cu = 0.5
			autoscaling_limit_max_cu = 2
			suspend_timeout_seconds  = -1
		}
}
`, projectName),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"name", projectName,
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_min_cu", "0.25",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_max_cu", "1",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"suspend_timeout_seconds", "300",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_min_cu", "0.5",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_max_cu", "2",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.suspend_timeout_seconds", "-1",
								),
							),
						},
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
		name = %q
		autoscaling_limit_min_cu = 0.25
		autoscaling_limit_max_cu = 1
		suspend_timeout_seconds  = 300
		
		primary_compute = {
			autoscaling_limit_min_cu = 1
			autoscaling_limit_max_cu = 4
			suspend_timeout_seconds  = 1200
		}
}
`, projectName),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"name", projectName,
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_min_cu", "0.25",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"autoscaling_limit_max_cu", "1",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"suspend_timeout_seconds", "300",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_min_cu", "1",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.autoscaling_limit_max_cu", "4",
								),
								resource.TestCheckResourceAttr(
									"neon_project.this",
									"primary_compute.suspend_timeout_seconds", "1200",
								),
							),
						},
					},
				})
		})

	t.Run("shall enable/disable default branch protection", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
	name   = %q
	branch = {protected = true}
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this", "branch.protected", "true",
							),
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								resp, err := client.ListProjectBranches(r.Primary.ID,
									nil, nil, nil, nil, nil, nil)
								if err != nil {
									return err
								}
								for _, branch := range resp.Branches {
									if branch.Default {
										assert.Truef(t, branch.Protected, "default branch shall be protected")
									}
								}
								return nil
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
	name   = %q
	branch = {protected = false}
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this", "branch.protected", "false",
							),
							func(state *terraform.State) error {
								r, ok := state.RootModule().Resources["neon_project.this"]
								if !ok {
									return fmt.Errorf("no project resource found")
								}
								resp, err := client.ListProjectBranches(r.Primary.ID,
									nil, nil, nil, nil, nil, nil)
								if err != nil {
									return err
								}
								for _, branch := range resp.Branches {
									if branch.Default {
										assert.False(t, branch.Protected, "default branch shall not be protected")
									}
								}
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall maintain default branch protection", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = %q}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_project.this", "branch.protected", "false",
							),
						),
					},
					{
						Config:   fmt.Sprintf(`resource "neon_project" "this" {name = %q}`, projectName),
						PlanOnly: true,
					},
				},
			})
	})

	t.Run("issue83", func(t *testing.T) {
		// It's expected that the default database and role names will not be overwritten
		// if custom database and role would be created using the default branch.
		// See details: https://github.com/kislerdm/terraform-provider-neon/issues/83
		projectName := newProjectName(projectNamePrefix)
		resourceDefinition := fmt.Sprintf(`resource "neon_project" "this" {
  name                = %q
  region_id           = "aws-us-west-2"
  pg_version          = "16"
  compute_provisioner = "k8s-neonvm"

  enable_logical_replication = false

  history_retention_seconds = 604800 # 7 days

  database_name = "do_not_use_neon_default"
  database_user = "main"

  branch = {
    name = "main"
  }

  autoscaling_limit_min_cu = 0.25
  autoscaling_limit_max_cu = 0.25
  suspend_timeout_seconds  = 300
}

resource "neon_role" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id

  name = "app"
}

resource "neon_database" "this" {
  name = "app"

  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  owner_name = neon_role.this.name
}`, projectName)

		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						ResourceName: "initial provisioning",
						Config:       resourceDefinition,
					},
					{
						ResourceName: "second plan",
						Config:       resourceDefinition,
						PlanOnly:     true,
					},
				},
			},
		)
	})
}

func TestProjectAllowedIPs(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	if os.Getenv("SCALE_PLAN") != "1" {
		t.Skip("test requires Scale plan; set env. variable SCALE_PLAN to 1 to run the test")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "prAllowedIps"

	wantAllowedIPs := []string{"192.168.1.0", "192.168.2.0/24"}
	ips := `["` + strings.Join(wantAllowedIPs, `", "`) + `"]`

	var verify = func(projectName string, wantAllowedIPs []string) (neon.Project, error) {
		project, err := readProjectInfo(client, projectName)
		if err != nil {
			return neon.Project{}, err
		}
		var exceedingAllowedIPs []string
		missingIPs := map[string]struct{}{}
		for _, ip := range wantAllowedIPs {
			missingIPs[ip] = struct{}{}
		}
		for _, ip := range project.Settings.AllowedIps.Ips {
			if _, ok := missingIPs[ip]; ok {
				delete(missingIPs, ip)
				continue
			}
			exceedingAllowedIPs = append(exceedingAllowedIPs, ip)
		}
		if len(exceedingAllowedIPs) > 0 || len(missingIPs) > 0 {
			return neon.Project{}, fmt.Errorf("unexpected allowed ips. want=%v, got=%v",
				wantAllowedIPs, project.Settings.AllowedIps.Ips)
		}
		return project, nil
	}

	t.Run("shall provision a project with a custom list of allowed IPs and update it", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				  = %q
			allowed_ips               = %s
		}`, projectName, ips),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#",
								fmt.Sprintf("%d", len(wantAllowedIPs))),
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.0",
								wantAllowedIPs[0]),
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.1",
								wantAllowedIPs[1]),
							resource.TestCheckResourceAttr("neon_project.this",
								"allowed_ips_protected_branches_only", "false"),
							func(_ *terraform.State) error {
								_, err := verify(projectName, wantAllowedIPs)
								return err
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				  = %q
			allowed_ips               = [%q]
		}`, projectName, wantAllowedIPs[0]),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.0",
								wantAllowedIPs[0]),
							resource.TestCheckResourceAttr("neon_project.this",
								"allowed_ips_protected_branches_only", "false"),
							func(_ *terraform.State) error {
								_, err := verify(projectName, wantAllowedIPs[:1])
								return err
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				  = %q
			allowed_ips               = []
		}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#", "0"),
							resource.TestCheckResourceAttr("neon_project.this",
								"allowed_ips_protected_branches_only", "false"),
							func(_ *terraform.State) error {
								_, err := verify(projectName, nil)
								return err
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				  = %q
		}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#", "0"),
							resource.TestCheckResourceAttr("neon_project.this",
								"allowed_ips_protected_branches_only", "false"),
							func(_ *terraform.State) error {
								_, err := verify(projectName, nil)
								return err
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				  = %q
			allowed_ips               = [%q]
		}`, projectName, wantAllowedIPs[0]),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#", "1"),
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.0",
								wantAllowedIPs[0]),
							resource.TestCheckResourceAttr("neon_project.this",
								"allowed_ips_protected_branches_only", "false"),
							func(_ *terraform.State) error {
								_, err := verify(projectName, wantAllowedIPs[:1])
								return err
							},
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				  = %q
		}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#", "0"),
							resource.TestCheckResourceAttr("neon_project.this",
								"allowed_ips_protected_branches_only", "false"),
							func(_ *terraform.State) error {
								_, err := verify(projectName, nil)
								return err
							},
						),
					},
				},
			},
		)
	})

	t.Run("shall provision a project with a custom list of allowed IPs w/ variable cfg for protected branch only",
		func(t *testing.T) {
			projectName := newProjectName(projectNamePrefix)
			resource.Test(
				t, resource.TestCase{
					ProtoV6ProviderFactories: newProviderFactories(),
					Steps: []resource.TestStep{
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				            = %q
			allowed_ips                         = %s
			allowed_ips_protected_branches_only = true
		}`, projectName, ips),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#",
									fmt.Sprintf("%d", len(wantAllowedIPs))),
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.0",
									wantAllowedIPs[0]),
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.1",
									wantAllowedIPs[1]),
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips_protected_branches_only", "true"),
								func(_ *terraform.State) error {
									project, err := verify(projectName, wantAllowedIPs)
									if err != nil {
										return err
									}
									if project.Settings.AllowedIps.ProtectedBranchesOnly == nil ||
										!*project.Settings.AllowedIps.ProtectedBranchesOnly {
										return errors.New("allowed_ips_protected_branches_only is expected to be set to 'true'")
									}
									return nil
								},
							),
						},
						{
							Config: fmt.Sprintf(`resource "neon_project" "this" {
			name      				            = %q
			allowed_ips                         = %s
		}`, projectName, ips),
							Check: resource.ComposeTestCheckFunc(
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.#",
									fmt.Sprintf("%d", len(wantAllowedIPs))),
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.0",
									wantAllowedIPs[0]),
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips.1",
									wantAllowedIPs[1]),
								resource.TestCheckResourceAttr("neon_project.this", "allowed_ips_protected_branches_only", "false"),
								func(_ *terraform.State) error {
									project, err := verify(projectName, wantAllowedIPs)
									if err != nil {
										return err
									}
									if project.Settings.AllowedIps.ProtectedBranchesOnly == nil ||
										*project.Settings.AllowedIps.ProtectedBranchesOnly {
										return errors.New("allowed_ips_protected_branches_only is expected to be set to 'false'")
									}
									return nil
								},
							),
						},
					},
				},
			)
		})
}
