package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestRecreateDatabaseIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "database"

	var preConfig = func(projectName, dbName string) int64 {
		ref, err := readProjectInfo(client, projectName)
		if err != nil {
			panic(err)
		}
		br, err := client.ListProjectBranches(ref.ID,
			nil, nil, nil, nil, nil, nil)
		if err != nil {
			panic(err)
		}
		var dbID int64
		for _, branch := range br.Branches {
			if branch.Default {
				resp, err := client.GetProjectBranchDatabase(ref.ID, branch.ID, dbName)
				if err != nil {
					panic(err)
				}
				dbID = resp.Database.ID
				op, err := client.DeleteProjectBranchDatabase(ref.ID, branch.ID, dbName)
				if err != nil {
					panic(err)
				}
				if err := waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations); err != nil {
					panic(err)
				}
			}
		}
		return dbID
	}

	t.Run(`shall create the project with a custom role and database with owner default project role, 
then update the database name, then update the database owner_name`, func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var config = func(dbName string, customRole bool) string {
			ownerName := "neon_project.this.database_user"
			if customRole {
				ownerName = "neon_role.this.name"
			}
			return fmt.Sprintf(`resource "neon_project" "this" {name = %q}

resource "neon_role" "this" {
	name       = "r-custom"
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
}

resource "neon_database" "this" {
	name       = %q
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	owner_name = %s
}
`, projectName, dbName, ownerName)
		}

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config("db-foo", false),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_database.this", "name", "db-foo"),
						func(state *terraform.State) error {
							resources := state.RootModule().Resources
							assert.Equal(t,
								resources["neon_project.this"].Primary.Attributes["database_user"],
								resources["neon_database.this"].Primary.Attributes["owner_name"],
							)
							return nil
						},
					),
				},
				{
					Config: config("db-bar", false),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_database.this", "name", "db-bar"),
						func(state *terraform.State) error {
							resources := state.RootModule().Resources
							assert.Equal(t,
								resources["neon_project.this"].Primary.Attributes["database_user"],
								resources["neon_database.this"].Primary.Attributes["owner_name"],
							)
							return nil
						},
					),
				},
				{
					Config: config("db-bar", true),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_database.this", "name", "db-bar"),
						func(state *terraform.State) error {
							resources := state.RootModule().Resources
							assert.Equal(t,
								resources["neon_role.this"].Primary.Attributes["name"],
								resources["neon_database.this"].Primary.Attributes["owner_name"],
							)
							return nil
						},
					),
				},
			},
		})
	})

	t.Run("shall yield non empty refresh plan if the database was deleted outside of terraform",
		func(t *testing.T) {
			projectName := newProjectName(projectNamePrefix)
			config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_database" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	owner_name = neon_project.this.database_user
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
									"neon_database.this",
									"name", "test",
								),
								func(_ *terraform.State) error {
									ref, err := readProjectInfo(client, projectName)
									if err != nil {
										return err
									}
									br, err := client.ListProjectBranches(ref.ID,
										nil, nil, nil, nil, nil, nil)
									if err != nil {
										return err
									}

									var branchID string
									for _, branch := range br.Branches {
										if branch.Default {
											branchID = branch.ID
										}
									}

									resp, err := client.ListProjectBranchDatabases(ref.ID, branchID)
									if err != nil {
										return err
									}
									assert.Len(t, resp.Databases, 2)

									return nil
								},
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

	t.Run("shall destroy even if the database was deleted outside of terraform,", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_database" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	owner_name = neon_project.this.database_user
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
						Config: config,
						PreConfig: func() {
							preConfig(projectName, "test")
						},
						Destroy: true,
					},
				},
			})
	})

	t.Run("shall recreate database upon update if it was deleted outside of terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var refDatabaseID int64
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_database" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	owner_name = neon_project.this.database_user
	name       = "foo"
}`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_database.this",
								"name", "foo",
							),
						),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_database" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	owner_name = neon_project.this.database_user
	name       = "bar"
}`, projectName),
						PreConfig: func() {
							refDatabaseID = preConfig(projectName, "foo")
						},
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_database.this",
								"name", "bar",
							),
							func(_ *terraform.State) error {
								pr, err := readProjectInfo(client, projectName)
								if err != nil {
									return err
								}
								br, err := client.ListProjectBranches(pr.ID,
									nil, nil, nil, nil, nil, nil)
								if err != nil {
									return err
								}
								for _, branch := range br.Branches {
									if branch.Default {
										resp, err := client.ListProjectBranchDatabases(pr.ID, branch.ID)
										if err != nil {
											return err
										}
										assert.Len(t, resp.Databases, 2)
										for _, db := range resp.Databases {
											if db.Name == "bar" {
												assert.NotEqualf(t, refDatabaseID, db.ID,
													"database ID should be different after recreation")
											}
										}
									}
								}
								return nil
							},
						),
					},
				},
			})
	})

	t.Run("shall fail to import database if it was deleted", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {name = "%s"}
resource "neon_database" "this" {
	project_id = neon_project.this.id
	branch_id  = neon_project.this.branch.id
	owner_name = neon_project.this.database_user
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
						ResourceName: "neon_database.this",
						ImportStateIdFunc: func(s *terraform.State) (string, error) {
							ref, err := readProjectInfo(client, projectName)
							if err != nil {
								return "", err
							}

							br, err := client.ListProjectBranches(ref.ID,
								nil, nil, nil, nil, nil, nil)
							if err != nil {
								return "", err
							}

							var branchID string
							for _, branch := range br.Branches {
								if branch.Default {
									branchID = branch.ID
								}
							}

							resp, err := client.ListProjectBranchDatabases(ref.ID, branchID)
							if err != nil {
								return "", err
							}

							for _, db := range resp.Databases {
								if db.Name == "test" {
									op, err := client.DeleteProjectBranchDatabase(ref.ID, branchID, db.Name)
									if err != nil {
										return "", err
									}
									if err := waitUnfinishedOperations(context.TODO(), client, op.OperationsResponse.Operations); err != nil {
										return "", err
									}
								}
							}
							return fmt.Sprintf("%s/%s/%s", ref.ID, branchID, "test"), nil
						},
						ExpectError: regexp.MustCompile("404"),
					},
					{
						// recreate the database to avoid errors on post-test destroy error
						Config: config,
					},
				},
			})
	})
}
