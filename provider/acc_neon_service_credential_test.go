package provider

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestServiceCredential(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "serviceCredential"

	t.Run("shall provision a service credential", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: serviceCredentialConfig(projectName, "credential"),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_service_credential.this", "name", "credential"),
						resource.TestCheckResourceAttr("neon_service_credential.this", "scopes.#", "1"),
						resource.TestCheckResourceAttr("neon_service_credential.this", "scopes.0", "functions:invoke"),
						resource.TestCheckResourceAttr("neon_service_credential.this", "is_expired", "false"),
						resource.TestCheckResourceAttrWith("neon_service_credential.this", "token_id",
							func(value string) error {
								if value == "" {
									return fmt.Errorf("expected token_id to be set")
								}
								return nil
							}),
						resource.TestCheckResourceAttrWith("neon_service_credential.this", "api_token",
							func(value string) error {
								if value == "" {
									return fmt.Errorf("expected api_token to be set")
								}
								return nil
							}),
						resource.TestCheckResourceAttrWith("neon_service_credential.this", "s3_secret_access_key",
							func(value string) error {
								if value == "" {
									return fmt.Errorf("expected s3_secret_access_key to be set")
								}
								return nil
							}),
					),
				},
			},
		})
	})

	t.Run("shall import a service credential", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		projectID, branchID := createServiceCredentialProject(t, client, projectName)
		name := "imported"
		created, err := client.CreateCredential(projectID, branchID, neon.CreateCredentialRequest{
			Name:          &name,
			PrincipalType: neon.CreateCredentialRequestPrincipalTypeUser,
			Scopes:        []neon.CredentialScope{neon.CredentialScopeFunctionsInvoke},
		})
		assert.NoError(t, err)

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`resource "neon_service_credential" "this" {
  project_id = %q
  branch_id  = %q
  name       = "imported"
  scopes     = ["functions:invoke"]
}
`, projectID, branchID),
					ImportState:   true,
					ResourceName:  "neon_service_credential.this",
					ImportStateId: fmt.Sprintf("%s/%s/%s", projectID, branchID, created.TokenID),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_service_credential.this", "name", "imported"),
						resource.TestCheckResourceAttr("neon_service_credential.this", "is_expired", "false"),
					),
				},
			},
		})
	})

	t.Run("shall fail to import a service credential given invalid id", func(t *testing.T) {
		config := `resource "neon_service_credential" "this" {
  project_id = "0"
  branch_id  = "br-1"
  scopes     = ["functions:invoke"]
}`
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:        config,
					ImportState:   true,
					ResourceName:  "neon_service_credential.this",
					ImportStateId: "invalid-id",
					ExpectError: regexp.MustCompile(
						"Expected an import ID in the form <project_id>/<branch_id>/<token_id>",
					),
				},
				{
					Config:        config,
					ImportState:   true,
					ResourceName:  "neon_service_credential.this",
					ImportStateId: "0/br-1/asf/asd",
					ExpectError: regexp.MustCompile(
						"Expected an import ID in the form <project_id>/<branch_id>/<token_id>",
					),
				},
				{
					Config:        config,
					ImportState:   true,
					ResourceName:  "neon_service_credential.this",
					ImportStateId: "0/br-1",
					ExpectError: regexp.MustCompile(
						"Expected an import ID in the form <project_id>/<branch_id>/<token_id>",
					),
				},
			},
		})
	})

	t.Run("shall fail to import a non-existent service credential", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		projectID, branchID := createServiceCredentialProject(t, client, projectName)

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: fmt.Sprintf(`resource "neon_service_credential" "this" {
  project_id = %q
  branch_id  = %q
  scopes     = ["functions:invoke"]
}
`, projectID, branchID),
					ImportState:   true,
					ResourceName:  "neon_service_credential.this",
					ImportStateId: fmt.Sprintf("%s/%s/nak_live_nonexistent", projectID, branchID),
					ExpectError: regexp.MustCompile(
						"Service Credential Not Found",
					),
				},
			},
		})
	})

	t.Run("shall delete the revoked service credential", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		name := "deletable"
		config := serviceCredentialConfig(projectName, name)

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config,
				},
				{
					Config: config,
					PreConfig: func() {
						pr, err := readProjectInfo(client, projectName)
						if err != nil {
							panic(err)
						}
						br, err := client.ListProjectBranches(pr.ID,
							nil, nil, nil, nil, nil, nil)
						if err != nil {
							panic(err)
						}
						branchID := br.Branches[0].ID
						cr, err := client.ListCredentials(pr.ID, branchID)
						if err != nil {
							panic(err)
						}
						for _, credential := range cr.Credentials {
							if credential.Name != nil && name == *credential.Name {
								err = client.RevokeCredential(pr.ID, branchID, credential.TokenID)
								if err != nil {
									panic(err)
								}
							}
						}
					},
					Destroy: true,
					Check: func(state *terraform.State) error {
						_, ok := state.RootModule().Resources["neon_service_credential.this"]
						assert.False(t, ok, "resource neon_service_credential.this should be destroyed")
						return nil
					},
				},
			},
		})
	})

	t.Run("shall plan service credential replacement", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: serviceCredentialConfig(projectName, "foo"),
					},
					{
						Config:             serviceCredentialConfig(projectName, "bar"),
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
				},
			},
		)
	})
}

func serviceCredentialConfig(projectName, credentialName string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  region_id = "aws-us-east-2"
}

resource "neon_service_credential" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = %q
  scopes     = ["functions:invoke"]
}
`, projectName, credentialName)
}

func createServiceCredentialProject(t *testing.T, client *neon.Client, projectName string) (string, string) {
	t.Helper()

	created, err := client.CreateProject(neon.ProjectCreateRequest{
		Project: neon.ProjectCreateRequestProject{
			Name:     &projectName,
			OrgID:    pointer(os.Getenv("ORG_ID")),
			RegionID: pointer("aws-us-east-2"),
		},
	})
	if err != nil {
		t.Fatalf("could not provision the project: %v", err)
	}

	projectID := created.Project.ID
	if projectID == "" {
		t.Fatalf("could not provision the project: response has no project ID")
	}
	t.Cleanup(func() {
		if _, err := client.DeleteProject(projectID); err != nil {
			var value neon.Error
			var pointer *neon.Error
			if errors.As(err, &value) && value.HTTPCode == http.StatusNotFound {
				return
			}
			if errors.As(err, &pointer) && pointer != nil && pointer.HTTPCode == http.StatusNotFound {
				return
			}
			t.Errorf("delete test project %s: %v", projectID, err)
		}
	})
	sleepDuringRunningOperations(t, client, projectID)
	branches, err := client.ListProjectBranches(projectID, nil, nil, nil, nil, nil, nil)
	assert.NoError(t, err)
	if err != nil || len(branches.Branches) == 0 {
		t.Fatalf("could not find project branch: %v", err)
	}

	return projectID, branches.Branches[0].ID
}
