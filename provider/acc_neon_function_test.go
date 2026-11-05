package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestFunction(t *testing.T) {
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

	projectNamePrefix := "function"

	wd, err := os.Getwd()
	assert.NoError(t, err)
	zipPath := filepath.Join(wd, "testdata/function/function.zip")

	t.Run("shall create a function and update its name in place", func(t *testing.T) {
		var newFunctionConfig = func(projectName string, functionName string) string {
			return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_function" "this" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  name          = %q
  zip_file_path = %q
}
`, projectName, orgID, functionName, zipPath)
		}

		projectName := newProjectName(projectNamePrefix)

		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: newFunctionConfig(projectName, "hello"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_function.this", "slug", "hello"),
							resource.TestCheckResourceAttr("neon_function.this", "runtime", "nodejs24"),
							resource.TestCheckResourceAttr("neon_function.this", "name", "hello"),
							resource.TestCheckResourceAttrSet("neon_function.this", "id"),
							resource.TestCheckResourceAttrSet("neon_function.this", "invocation_url"),
							func(state *terraform.State) error {
								fn, ok := state.RootModule().Resources["neon_function.this"]
								if !ok {
									return fmt.Errorf("neon_function.this not found in state")
								}
								rsp, err := client.GetProjectBranchFunction(
									fn.Primary.Attributes["project_id"],
									fn.Primary.Attributes["branch_id"],
									"hello",
								)
								if err != nil {
									return err
								}
								assert.Equal(t, "hello", rsp.Function.Slug)
								assert.Equal(t, "hello", rsp.Function.Name)
								return nil
							},
						),
					},
					{
						Config: newFunctionConfig(projectName, "renamed"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_function.this", "name", "renamed"),
							func(state *terraform.State) error {
								fn, ok := state.RootModule().Resources["neon_function.this"]
								if !ok {
									return fmt.Errorf("neon_function.this not found in state")
								}
								rsp, err := client.GetProjectBranchFunction(
									fn.Primary.Attributes["project_id"],
									fn.Primary.Attributes["branch_id"],
									"hello",
								)
								if err != nil {
									return err
								}
								assert.Equal(t, "hello", rsp.Function.Slug)
								assert.Equal(t, "renamed", rsp.Function.Name)
								return nil
							},
						),
					},
				},
			},
		)
	})

	t.Run("shall set the environment variables for the runtime", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)

		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_function" "this" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  name          = "hello"
  zip_file_path = %q

  environment_variables = {
    FOO = 1
    BAR = "baz"
  }
}
`, projectName, orgID, zipPath),
						Check: func(state *terraform.State) error {
							fn, ok := state.RootModule().Resources["neon_function.this"]
							assert.True(t, ok)
							invocationURL := fn.Primary.Attributes["invocation_url"]
							assert.NotEmpty(t, invocationURL)

							resp, err := http.Get(invocationURL)
							if err != nil {
								return err
							}
							assert.Equal(t, http.StatusOK, resp.StatusCode)

							var setEnvVars map[string]string
							assert.NoError(t, json.NewDecoder(resp.Body).Decode(&setEnvVars))
							assert.NoError(t, resp.Body.Close())

							assert.Equal(t, "1", setEnvVars["FOO"])
							assert.Equal(t, "baz", setEnvVars["BAR"])

							return nil
						},
					},
				},
			},
		)
	})

	t.Run("shall create a function with the name different from slug", func(t *testing.T) {
		var newFunctionConfig = func(projectName string, slug, functionName string) string {
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
  name          = %q
  zip_file_path = %q
}
`, projectName, orgID, slug, functionName, zipPath)
		}

		projectName := newProjectName(projectNamePrefix)

		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: newFunctionConfig(projectName, "foo", "bar"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_function.this", "slug", "foo"),
							resource.TestCheckResourceAttr("neon_function.this", "runtime", "nodejs24"),
							resource.TestCheckResourceAttr("neon_function.this", "name", "bar"),
							resource.TestCheckResourceAttrSet("neon_function.this", "id"),
							resource.TestCheckResourceAttrSet("neon_function.this", "invocation_url"),
							func(state *terraform.State) error {
								fn, ok := state.RootModule().Resources["neon_function.this"]
								if !ok {
									return fmt.Errorf("neon_function.this not found in state")
								}
								rsp, err := client.GetProjectBranchFunction(
									fn.Primary.Attributes["project_id"],
									fn.Primary.Attributes["branch_id"],
									"foo",
								)
								if err != nil {
									return err
								}
								assert.Equal(t, "foo", rsp.Function.Slug)
								assert.Equal(t, "bar", rsp.Function.Name)
								return nil
							},
						),
					},
				},
			},
		)
	})
}
