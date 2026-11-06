package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

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

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
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

	t.Run("shall set the environment variables and deploy changes in place", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := func(env string) string {
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
  name          = "hello"
  zip_file_path = %q
%s
}
`, projectName, orgID, zipPath, env)
		}
		// Neon reports a deployment as active before every request reaches it, so the
		// invocation is polled until the expected environment answers.
		invokeUntil := func(state *terraform.State, want func(env map[string]string) error) error {
			fn, ok := state.RootModule().Resources["neon_function.this"]
			if !ok {
				return fmt.Errorf("neon_function.this not found in state")
			}
			start := time.Now()
			var lastErr error
			for time.Since(start) < 5*time.Minute {
				resp, err := http.Get(fn.Primary.Attributes["invocation_url"])
				if err == nil {
					var env map[string]string
					if resp.StatusCode == http.StatusOK {
						err = json.NewDecoder(resp.Body).Decode(&env)
					} else {
						err = fmt.Errorf("invoking the function returned HTTP %d", resp.StatusCode)
					}
					_ = resp.Body.Close()
					if err == nil {
						err = want(env)
					}
					if err == nil {
						t.Logf("new deployment answered after %s", time.Since(start).Round(time.Second))
						return nil
					}
				}
				lastErr = err
				time.Sleep(3 * time.Second)
			}
			return lastErr
		}
		var createdAt string
		var deploymentID int
		sameFunctionNewDeployment := func(state *terraform.State) error {
			fn := state.RootModule().Resources["neon_function.this"].Primary.Attributes
			if createdAt != "" && fn["created_at"] != createdAt {
				return fmt.Errorf("function was replaced: created_at %s became %s", createdAt, fn["created_at"])
			}
			var id int
			if _, err := fmt.Sscan(fn["current_deployment_id"], &id); err != nil {
				return err
			}
			if deploymentID != 0 && id <= deploymentID {
				return fmt.Errorf("current_deployment_id %d did not advance past %d", id, deploymentID)
			}
			createdAt, deploymentID = fn["created_at"], id
			return nil
		}

		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config(`  environment_variables = {
    FOO = 1
    BAR = "baz"
  }`),
						Check: resource.ComposeTestCheckFunc(
							sameFunctionNewDeployment,
							func(state *terraform.State) error {
								return invokeUntil(state, func(env map[string]string) error {
									if env["FOO"] != "1" || env["BAR"] != "baz" {
										return fmt.Errorf("function returned FOO=%q, BAR=%q; want 1 and baz", env["FOO"], env["BAR"])
									}
									return nil
								})
							},
						),
					},
					{
						Config: config(`  environment_variables = {
    FOO = 2
  }`),
						Check: resource.ComposeTestCheckFunc(
							sameFunctionNewDeployment,
							resource.TestCheckResourceAttr("neon_function.this", "environment_variable_names.#", "1"),
							func(state *terraform.State) error {
								return invokeUntil(state, func(env map[string]string) error {
									if _, hasBar := env["BAR"]; hasBar || env["FOO"] != "2" {
										return fmt.Errorf("function returned FOO=%q, BAR set=%t; want FOO=\"2\" and no BAR", env["FOO"], hasBar)
									}
									return nil
								})
							},
						),
					},
					{
						// The value is unknown until terraform_data.foo exists.
						Config: config(`  environment_variables = {
    FOO = terraform_data.foo.output
  }`) + `
resource "terraform_data" "foo" {
  input = "3"
}
`,
						Check: resource.ComposeTestCheckFunc(
							sameFunctionNewDeployment,
							func(state *terraform.State) error {
								return invokeUntil(state, func(env map[string]string) error {
									if env["FOO"] != "3" {
										return fmt.Errorf("function returned FOO=%q; want 3", env["FOO"])
									}
									return nil
								})
							},
						),
					},
					{
						Config: config(""),
						Check: resource.ComposeTestCheckFunc(
							sameFunctionNewDeployment,
							resource.TestCheckNoResourceAttr("neon_function.this", "environment_variable_names.#"),
							func(state *terraform.State) error {
								return invokeUntil(state, func(env map[string]string) error {
									if _, ok := env["FOO"]; ok {
										return fmt.Errorf("FOO is still set in the function")
									}
									return nil
								})
							},
						),
					},
					{
						ResourceName:            "neon_function.this",
						ImportState:             true,
						ImportStateVerify:       true,
						ImportStateVerifyIgnore: []string{"zip_file_path"},
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

	t.Run("shall leave an existing function alone when a create with its slug fails before uploading", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		base := fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_function" "this" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  zip_file_path = %q
}
`, projectName, orgID, zipPath)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: base},
				{
					Config: base + `
resource "neon_function" "other" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  zip_file_path = "/nonexistent/function.zip"
}
`,
					ExpectError: regexp.MustCompile("could not open the ZIP archive"),
				},
				{
					// Had the failed create adopted the function, dropping it from the configuration would delete it.
					Config: base,
					Check: func(state *terraform.State) error {
						if _, ok := state.RootModule().Resources["neon_function.other"]; ok {
							return errors.New("neon_function.other is in state after its create failed")
						}
						project := state.RootModule().Resources["neon_project.this"].Primary
						_, err := client.GetProjectBranchFunction(project.ID, project.Attributes["branch.id"], "hello")
						return err
					},
				},
			},
		})
	})

	t.Run("shall record a successful deploy when the rename after it fails", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := func(name, env string) string {
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

  environment_variables = {
%s
  }
}
`, projectName, orgID, name, zipPath, env)
		}
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: config("hello", `    FOO = "1"`)},
				{
					// Neon rejects the empty name after the deployment that adds BAR is active.
					Config:      config("", "    FOO = \"1\"\n    BAR = \"baz\""),
					ExpectError: regexp.MustCompile("Neon rejected the request"),
				},
				{
					// Removing BAR only reaches Neon if state recorded the deployment that added it. The names come
					// from Neon's current deployment, which traffic can lag behind.
					Config: config("hello", `    FOO = "1"`),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_function.this", "environment_variable_names.#", "1"),
						resource.TestCheckResourceAttr("neon_function.this", "environment_variable_names.0", "FOO"),
					),
				},
			},
		})
	})

	t.Run("shall reject an empty environment variable", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: fmt.Sprintf(`resource "neon_function" "this" {
  project_id    = "project-id"
  branch_id     = "br-id"
  slug          = "hello"
  runtime       = "nodejs24"
  zip_file_path = %q
  environment_variables = {
    FOO = ""
  }
}`, zipPath),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("Empty environment variable"),
			}},
		})
	})
}
