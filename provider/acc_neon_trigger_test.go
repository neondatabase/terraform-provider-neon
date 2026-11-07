package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

	t.Run("schedule without cron", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: strings.Replace(triggerScheduleConfig("test-project", "test-org", "sched", "myfn", "x"),
						`  cron           = "x"
`, "", 1),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile("When type is `schedule`, `cron` is required"),
				},
			},
		})
	})
}

func TestAccNeonTrigger(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	projectNamePrefix := "neonTrigger"

	t.Run("shall provision a schedule trigger", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		name := "sched"
		resource.Test(t, resource.TestCase{
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
		resource.Test(t, resource.TestCase{
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

	t.Run("shall update mutable attributes in place, import, and replace on type change", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		name := "upd"
		var triggerID string
		resource.Test(t, resource.TestCase{
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
						if triggerID != "" && rs.Primary.Attributes["trigger_id"] != triggerID {
							return fmt.Errorf("cron change replaced the trigger")
						}
						triggerID = rs.Primary.Attributes["trigger_id"]
						return nil
					},
				},
				{
					// cron is unknown at plan time until terraform_data.cron exists.
					Config: strings.Replace(triggerScheduleConfig(projectName, orgID, name, "myfn", "cron"),
						`cron           = "cron"`, `cron           = terraform_data.cron.output`, 1) + `
resource "terraform_data" "cron" {
  input = "*/15 * * * *"
}
`,
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_trigger.this", "cron", "*/15 * * * *"),
						func(state *terraform.State) error {
							if state.RootModule().Resources["neon_trigger.this"].Primary.Attributes["trigger_id"] != triggerID {
								return fmt.Errorf("cron change replaced the trigger")
							}
							return nil
						},
					),
				},
				{
					ResourceName:      "neon_trigger.this",
					ImportState:       true,
					ImportStateVerify: true,
					ImportStateIdFunc: func(state *terraform.State) (string, error) {
						a := state.RootModule().Resources["neon_trigger.this"].Primary.Attributes
						return a["project_id"] + "/" + a["branch_id"] + "/" + a["trigger_id"], nil
					},
				},
				{
					Config: triggerStorageConfig(projectName, orgID, name, "myfn", "uploads", "logs/"),
					Check: func(state *terraform.State) error {
						rs := state.RootModule().Resources["neon_trigger.this"]
						if rs.Primary.Attributes["type"] != "storage_object_created" {
							return fmt.Errorf("type is %q", rs.Primary.Attributes["type"])
						}
						if rs.Primary.Attributes["trigger_id"] == triggerID {
							return fmt.Errorf("type change did not replace the trigger")
						}
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

// triggerTestZip is the function archive the triggers invoke.
func triggerTestZip() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "testdata/function/function.zip")
}

func triggerScheduleConfig(projectName, orgID, triggerName, fnSlug, cron string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_function" "this" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  slug          = %q
  runtime       = "nodejs24"
  zip_file_path = %q
}

resource "neon_trigger" "this" {
  project_id     = neon_project.this.id
  branch_id      = neon_project.this.branch.id
  name           = %q
  type           = "schedule"
  function_slug  = neon_function.this.slug
  cron           = %q
}
`, projectName, orgID, fnSlug, triggerTestZip(), triggerName, cron)
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

resource "neon_function" "this" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  slug          = %q
  runtime       = "nodejs24"
  zip_file_path = %q
}

resource "neon_trigger" "this" {
  project_id     = neon_project.this.id
  branch_id      = neon_project.this.branch.id
  name           = %q
  type           = "storage_object_created"
  function_slug  = neon_function.this.slug
  bucket_name    = neon_bucket.this.name
  prefix         = %q
}
`, projectName, orgID, bucket, fnSlug, triggerTestZip(), triggerName, prefix)
}
