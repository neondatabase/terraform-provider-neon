package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestTriggerConfigPlan(t *testing.T) {
	t.Setenv("NEON_API_KEY", "test-api-key")

	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "schedule",
			config: triggerScheduleConfig("test-project", "test-org", "sched", "myfn", "*/5 * * * *"),
		},
		{
			name:   "storage_object_created",
			config: triggerStorageConfig("test-project", "test-org", "stor", "myfn", "mybucket", "logs/"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:             tt.config,
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
		})
	}
}

func TestAccNeonTrigger(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "neonTrigger"

	projects := newAcceptanceProjects(t, client)
	newProjectName := projects.NewName

	t.Run("shall provision a schedule trigger", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		name := "sched"
		projects.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: triggerScheduleConfig(projectName, orgID, name, "myfn", "*/5 * * * *"),
					Check: func(state *terraform.State) error {
						rs, ok := state.RootModule().Resources["neon_trigger.this"]
						if !ok {
							return fmt.Errorf("neon_trigger.this not found in state")
						}
						assert.Equal(t, "schedule", rs.Primary.Attributes["type"])
						assert.Equal(t, "*/5 * * * *", rs.Primary.Attributes["cron"])
						assert.NotEmpty(t, rs.Primary.Attributes["trigger_id"])
						assert.NotEmpty(t, rs.Primary.Attributes["id"])
						return nil
					},
				},
			},
		})
	})

	t.Run("shall provision a storage_object_created trigger", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		projects.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: triggerStorageConfig(projectName, orgID, "stor", "myfn", "mybucket", "logs/"),
					Check: func(state *terraform.State) error {
						rs, ok := state.RootModule().Resources["neon_trigger.this"]
						if !ok {
							return fmt.Errorf("neon_trigger.this not found in state")
						}
						assert.Equal(t, "storage_object_created", rs.Primary.Attributes["type"])
						assert.Equal(t, "mybucket", rs.Primary.Attributes["bucket_name"])
						assert.Equal(t, "logs/", rs.Primary.Attributes["prefix"])
						assert.NotEmpty(t, rs.Primary.Attributes["trigger_id"])
						return nil
					},
				},
			},
		})
	})

	t.Run("shall update mutable attributes without forced replacement", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		name := "upd"
		projects.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: triggerScheduleConfig(projectName, orgID, name, "myfn", "*/5 * * * *"),
				},
				{
					Config: triggerScheduleConfig(projectName, orgID, name, "myfn", "*/10 * * * *"),
					Check: func(state *terraform.State) error {
						rs, ok := state.RootModule().Resources["neon_trigger.this"]
						if !ok {
							return fmt.Errorf("neon_trigger.this not found in state")
						}
						assert.Equal(t, "*/10 * * * *", rs.Primary.Attributes["cron"])
						return nil
					},
				},
			},
		})
	})

	t.Run("shall reject malformed import IDs with a clear diagnostic", func(t *testing.T) {
		config := `resource "neon_trigger" "this" {
  project_id     = "0"
  branch_id      = "br-1"
  type           = "schedule"
  name           = "imp"
  function_slug  = "myfn"
  cron           = "*/5 * * * *"
}`
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:        config,
					ImportState:   true,
					ResourceName:  "neon_trigger.this",
					ImportStateId: "0/br-1/",
					ExpectError: regexp.MustCompile(
						"Invalid Neon Trigger Import ID",
					),
				},
			},
		})
	})
}

func triggerScheduleConfig(projectName, orgID, triggerName, fnSlug, cron string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_trigger" "this" {
  project_id     = neon_project.this.id
  branch_id      = neon_project.this.branch.id
  name           = %q
  type           = "schedule"
  function_slug  = %q
  cron           = %q
}
`, projectName, orgID, triggerName, fnSlug, cron)
}

func triggerStorageConfig(projectName, orgID, triggerName, fnSlug, bucket, prefix string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = %q
}

resource "neon_trigger" "this" {
  project_id     = neon_project.this.id
  branch_id      = neon_project.this.branch.id
  name           = %q
  type           = "storage_object_created"
  function_slug  = %q
  bucket_name    = neon_bucket.this.name
  prefix         = %q
  depends_on = [neon_bucket.this]
}
`, projectName, orgID, bucket, triggerName, fnSlug, prefix)
}
