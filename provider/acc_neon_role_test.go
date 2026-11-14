package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestRecreateRoleIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "roleRecreation"

	var preConfig = func(projectID string, roleName string) {
		respBranches, err := client.ListProjectBranches(projectID,
			nil, nil, nil, nil, nil, nil)
		if err != nil {
			panic(err)
		}

		var branchID string
		for _, branch := range respBranches.Branches {
			if branch.Default {
				branchID = branch.ID
			}
		}

		op, err := client.DeleteProjectBranchRole(projectID, branchID, roleName)
		if err != nil {
			panic(err)
		}
		if err := waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations); err != nil {
			panic(err)
		}
	}

	t.Run("shall indicate non empty plan if the role was deleted outside of terraform", func(t *testing.T) {
		var stateProjectID string

		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	name       = "test"
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
								"name", "test",
							),
							captureStateAttr("neon_project.this", "id", &stateProjectID),
						),
					},
					{
						PreConfig: func() {
							preConfig(stateProjectID, "test")
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the role was deleted outside of terraform,", func(t *testing.T) {
		var stateProjectID string

		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	name       = "test"
}`, projectName)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
								"name", "test",
							),
							captureStateAttr("neon_project.this", "id", &stateProjectID),
						),
					},
					{
						PreConfig: func() {
							preConfig(stateProjectID, "test")
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_role.this"]
							assert.False(t, ok, "resource neon_role.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate role upon update if it was deleted outside of terraform", func(t *testing.T) {
		var stateProjectID string

		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_role" "this" {
	project_id = neon_project.this.id 
	branch_id  = neon_project.this.branch.id
	name       = "foo"
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
								"name", "foo",
							),
							captureStateAttr("neon_project.this", "id", &stateProjectID),
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_role" "this" {
	project_id = neon_project.this.id 
	branch_id  = neon_project.this.branch.id
	name       = "bar"
}`, projectName),
						PreConfig: func() {
							preConfig(stateProjectID, "foo")
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_role.this",
								"name", "bar",
							),
							func(state *terraform.State) error {
								idFromState, err := stateAttr(state, "neon_project.this", "id")
								if err != nil {
									return err
								}

								resp, err := client.ListProjectBranches(idFromState,
									nil, nil, nil, nil, nil, nil)
								if err != nil {
									return err
								}

								var branchID string
								for _, branch := range resp.Branches {
									if branch.Default {
										branchID = branch.ID
									}
								}

								respRoles, err := client.ListProjectBranchRoles(idFromState, branchID)
								if err != nil {
									return err
								}

								var found bool
								var oldFound bool
								for _, role := range respRoles.Roles {
									if role.Name == "bar" {
										found = true
									}
									if role.Name == "foo" {
										oldFound = true
									}
								}
								assert.Truef(t, found, "role 'bar' is expected to be found after recreation")
								assert.Falsef(t, oldFound, "role 'foo' is not expected to be found")
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import role if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_role" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	name       = "test"
}`, projectName)
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
						ResourceName: "neon_role.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							idFromState, err := stateAttr(s, "neon_project.this", "id")
							if err != nil {
								return "", err
							}

							resp, err := client.ListProjectBranches(idFromState,
								nil, nil, nil, nil, nil, nil)
							if err != nil {
								return "", err
							}
							var branchID string
							for _, branch := range resp.Branches {
								if branch.Default {
									branchID = branch.ID
								}
							}

							op, err := client.DeleteProjectBranchRole(idFromState, branchID, "test")
							if err != nil {
								return "", err
							}
							if err := waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations); err != nil {
								return "", err
							}

							return fmt.Sprintf("%s/%s/test", idFromState, branchID), nil
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

// test covers the use case in the issue https://github.com/kislerdm/terraform-provider-neon/issues/108
func TestRoleShallImportCreatedOutsideOfTerraform(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "roleImport"

	projectName := newProjectName(projectNamePrefix)
	respCreateProject, err := client.CreateProject(neon.ProjectCreateRequest{
		Project: neon.ProjectCreateRequestProject{
			Name:  &projectName,
			OrgID: pointer(os.Getenv("ORG_ID")),
		},
	})
	if err != nil {
		t.Fatalf("could not provision the project: %v", err)
	}

	projectID := respCreateProject.Project.ID
	if projectID == "" {
		t.Fatal("could not provision the project: response has no project ID")
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
	assert.NoError(t, waitUnfinishedOperations(context.TODO(), client, respCreateProject.OperationsResponse.Operations))
	branchID := respCreateProject.Branch.ID
	const roleName = "foo"
	respCreateRole, err := client.CreateProjectBranchRole(projectID, branchID, neon.RoleCreateRequest{
		Role: neon.RoleCreateRequestRole{Name: roleName},
	})
	assert.NoError(t, err)
	assert.NoError(t, waitUnfinishedOperations(context.TODO(), client, respCreateRole.OperationsResponse.Operations))

	resourceDefinition := fmt.Sprintf(`resource "neon_role" "this" {
  name       = %q
  project_id = %q
  branch_id  = %q
}`, roleName, projectID, branchID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				ResourceName:  "neon_role.this",
				Config:        resourceDefinition,
				ImportState:   true,
				ImportStateId: fmt.Sprintf("%s/%s/%s", projectID, branchID, roleName),
			},
			{
				RefreshState: true,
				PlanOnly:     true,
			},
		},
	})
}
