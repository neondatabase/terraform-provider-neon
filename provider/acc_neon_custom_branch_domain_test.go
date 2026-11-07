package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestCustomBranchDomain(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	d, err := os.Getwd()
	assert.NoError(t, err)
	functionZipPath := filepath.Join(d, "testdata/function/function.zip")

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}

	projectName := newProjectName("customBranchDomain")
	t.Cleanup(func() {
		pr, err := readProjectInfo(client, projectName)
		if err == nil {
			_, _ = client.DeleteProject(pr.ID)
		}
	})

	t.Run("shall manage custom domain pointing to a function", func(t *testing.T) {
		slug := "foo"
		var newConfig = func(domain string) string {
			return fmt.Sprintf(`resource "neon_project" "this" {
	name      = %q
	region_id = "aws-us-east-2"
}

resource "neon_function" "this" {
	project_id    = neon_project.this.id
	branch_id     = neon_project.this.branch.id
	slug          = %q
	name          = %q
	runtime       = "nodejs24"
	zip_file_path = %q
}

resource "neon_custom_branch_domain" "this" {
	project_id  = neon_project.this.id
	branch_id   = neon_project.this.branch.id
	entity_type = "function"
	entity_id   = neon_function.this.slug
	domain      = %q
}
`, projectName, slug, slug, functionZipPath, domain)
		}

		var verify = func(domainRef string) resource.TestCheckFunc {
			return resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("neon_custom_branch_domain.this",
					"domain", domainRef),
				resource.TestCheckResourceAttr("neon_custom_branch_domain.this",
					"entity_type", "function"),
				func(state *terraform.State) error {
					pr, ok := state.RootModule().Resources["neon_project.this"]
					assert.Truef(t, ok, "neon_project.this not found")
					projectID := pr.Primary.Attributes["id"]
					branchID := pr.Primary.Attributes["branch.id"]
					resource.TestCheckResourceAttr("neon_custom_branch_domain.this",
						"project_id", projectID)
					resource.TestCheckResourceAttr("neon_custom_branch_domain.this",
						"branch_id", branchID)

					fn, ok := state.RootModule().Resources["neon_function.this"]
					assert.Truef(t, ok, "neon_function.this not found")
					wantSlug := fn.Primary.Attributes["slug"]
					resource.TestCheckResourceAttr("neon_custom_branch_domain.this",
						"entity_id", wantSlug)

					domain, err := fundCustomBranchDomain(t.Context(), client, projectID, branchID, domainRef)
					if err != nil {
						return err
					}
					resource.TestCheckResourceAttr("neon_custom_branch_domain.this",
						"cname_target", domain.CnameTarget)
					return nil
				})
		}

		var projectID, branchID string

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				// create
				{
					Config: newConfig("foo.bar.com"),
					Check:  verify("foo.bar.com"),
				},
				// update
				{
					Config: newConfig("baz.bar.com"),
					Check:  verify("baz.bar.com"),
				},
				// import
				{
					Config: fmt.Sprintf(`resource "neon_custom_branch_domain" "this" {
				project_id  = %q
				branch_id   = %q
				entity_type = "function"
				entity_id   = %q
				domain      = %q
}`, projectID, branchID, slug, "baz.bar.com"),
					ImportState: true,
					ImportStateIdFunc: func(_ *terraform.State) (string, error) {
						pr, err := readProjectInfo(client, projectName)
						if err != nil {
							return "", err
						}
						projectID = pr.ID
						branchResp, err := client.ListProjectBranches(projectID, nil, nil, nil, nil,
							nil, nil)
						if err != nil {
							return "", err
						}
						branchID = branchResp.Branches[0].ID
						return fmt.Sprintf("%s/%s/%s", projectID, branchID, "baz.bar.com"), nil
					},
					ResourceName:      "neon_custom_branch_domain.this",
					Check:             verify("baz.bar.com"),
					ImportStateVerify: true,
				},
				// remove removed resource
				{
					PreConfig: func() {
						pr, err := readProjectInfo(client, projectName)
						assert.NoError(t, err)
						projectID = pr.ID

						branchResp, err := client.ListProjectBranches(projectID, nil, nil, nil, nil,
							nil, nil)
						assert.NoError(t, err)
						branchID = branchResp.Branches[0].ID

						assert.NoError(t, client.DeleteProjectBranchCustomDomain(projectID, branchID,
							"baz.bar.com"))
					},
					Config:  newConfig("baz.bar.com"),
					Destroy: true,
				},
				//	remove present resource
				{
					Config: newConfig("baz.bar.com"),
				},
				{
					Config:  newConfig("baz.bar.com"),
					Destroy: true,
				},
			},
		})
	})
}
