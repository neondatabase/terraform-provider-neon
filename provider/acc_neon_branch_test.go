package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestBranch(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "branch-"

	var preConfig = func(projectName string, branchName string) {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}

		resp, err := client.ListProjectBranches(ref.ID,
			nil, nil, nil, nil, nil, nil)
		if err != nil {
			panic(err)
		}
		for _, branch := range resp.Branches {
			if branch.Name == branchName {
				op, err := client.DeleteProjectBranch(ref.ID, branch.ID)
				if err != nil {
					panic(err)
				}
				if err := waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations); err != nil {
					panic(err)
				}
			}
		}
	}

	t.Run(`shall create the project with a custom branch w/o specification of the protection status, 
set it to be protected, then unprotected explicitly, and remove protection spec afterwards`, func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		const branchName = "foo"
		var config = func(protected *bool) string {
			var cfg string
			if protected != nil {
				cfg = fmt.Sprintf("protected = %v", *protected)
			}
			return fmt.Sprintf(`resource "neon_project" "this" {name = %q}

resource "neon_branch" "this" {
	name       = %q
	project_id = neon_project.this.id
	%s
}`, projectName, branchName, cfg)
		}

		var verify = func(protected bool) func(state *terraform.State) error {
			return func(state *terraform.State) error {
				projectID := state.RootModule().Resources["neon_branch.this"].Primary.Attributes["project_id"]
				branchID := state.RootModule().Resources["neon_branch.this"].Primary.Attributes["id"]
				respBranch, err := client.GetProjectBranch(projectID, branchID)
				if err != nil {
					return err
				}
				assert.Equal(t, protected, respBranch.BranchResponse.Branch.Protected)
				return nil
			}
		}

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config(nil),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_branch.this", "protected", "false"),
						verify(false),
					),
				},
				{
					Config: config(pointer(true)),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_branch.this", "protected", "true"),
						verify(true),
					),
				},
				{
					Config: config(pointer(false)),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_branch.this", "protected", "false"),
						verify(false),
					),
				},
				{
					Config: config(nil),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_branch.this", "protected", "false"),
						verify(false),
					),
				},
			},
		})
	})

	t.Run("shall indicate non empty plan if the branch was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
	name       = "test"
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "test",
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the branch was deleted outside of terraform,", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
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
								"neon_branch.this",
								"name", "test",
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_branch.this"]
							assert.False(t, ok, "resource neon_branch.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall indicate non empty plan if the branch was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
	name       = "test"
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "test",
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the branch was deleted outside of terraform,", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
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
								"neon_branch.this",
								"name", "test",
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_branch.this"]
							assert.False(t, ok, "resource neon_branch.this should be destroyed")
							return nil
						},
					},
				},
			})
	})

	t.Run("shall recreate branch upon update if it was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
	name       = "foo"
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "foo",
							),
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
	name       = "bar"
}`, projectName),
						PreConfig: func() {
							preConfig(projectName, "foo")
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_branch.this",
								"name", "bar",
							),
							func(_ *terraform.State) error {
								ref, err := readProjectInfo(client, projectName)
								if err != nil {
									return err
								}

								resp, err := client.ListProjectBranches(ref.ID,
									nil, nil, nil, nil, nil, nil)
								if err != nil {
									return err
								}
								assert.Len(t, resp.Branches, 2,
									"2 branches are expected after recreation")
								var found bool
								var oldFound bool
								for _, branch := range resp.Branches {
									if branch.Name == "bar" {
										found = true
									}
									if branch.Name == "foo" {
										oldFound = true
									}
								}
								assert.Truef(t, found, "branch 'bar' is expected to be found after recreation")
								assert.Falsef(t, oldFound, "branch 'foo' is not expected to be found")
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import branch if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_branch" "this" {
	project_id = neon_project.this.id 
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
						ResourceName: "neon_branch.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							ref, err := readProjectInfo(client, projectName)
							if err != nil {
								return "", err
							}

							resp, err := client.ListProjectBranches(ref.ID,
								nil, nil, nil, nil, nil, nil)
							if err != nil {
								return "", err
							}
							var branchID string
							for _, branch := range resp.Branches {
								if branch.Name == "test" {
									branchID = branch.ID
									op, err := client.DeleteProjectBranch(ref.ID, branch.ID)
									if err != nil {
										return "", err
									}
									if err := waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations); err != nil {
										return "", err
									}
								}
							}
							return fmt.Sprintf("%s/%s", ref.ID, branchID), nil
						},
						ExpectError: regexp.MustCompile("404"),
					},
					// to avoid dangling resources on post-test destroy
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" { name = "%s" }`, projectName),
					},
				},
			})
	})

	t.Run("shall replace a child branch when its parent branch is replaced", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := func(parentOfA string) string {
			return fmt.Sprintf(`resource "neon_project" "this" { name = "%s" }

resource "neon_branch" "base" {
  project_id = neon_project.this.id
  name       = "base"
}

resource "neon_branch" "a" {
  project_id = neon_project.this.id
  parent_id  = %s
  name       = "a"
}

resource "neon_branch" "b" {
  project_id = neon_project.this.id
  parent_id  = neon_branch.a.id
  name       = "b"
}
`, projectName, parentOfA)
		}
		var firstChildID string
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config("neon_project.this.branch.id"),
					Check: func(s *terraform.State) error {
						firstChildID = s.RootModule().Resources["neon_branch.b"].Primary.ID
						return nil
					},
				},
				{
					// The new parent of a forces a's replacement, so b's parent_id is unknown at plan time.
					Config: config("neon_branch.base.id"),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttrPair("neon_branch.b", "parent_id", "neon_branch.a", "id"),
						func(s *terraform.State) error {
							if s.RootModule().Resources["neon_branch.b"].Primary.ID == firstChildID {
								return errors.New("neon_branch.b was not replaced")
							}
							return nil
						},
					),
				},
			},
		})
	})

	t.Run("shall reject parent_lsn and parent_timestamp together", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: `resource "neon_branch" "this" {
  project_id       = "project-id"
  parent_lsn       = "0/1"
  parent_timestamp = 1
}`,
					PlanOnly:    true,
					ExpectError: regexp.MustCompile("Invalid Attribute Combination"),
				},
			},
		})
	})
}
