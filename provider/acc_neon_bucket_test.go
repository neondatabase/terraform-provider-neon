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

func TestBucket(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "bucket"

	projects := newAcceptanceProjects(t, client)
	newProjectName := projects.NewName

	t.Run("shall create a bucket with default access level", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		projects.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
	name      = "%s"
	region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = "foo"
}
`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_bucket.this", "name", "foo"),
							resource.TestCheckResourceAttr("neon_bucket.this", "access_level",
								"private"),
							resource.TestCheckResourceAttr("neon_bucket.this", "region",
								"us-east-2"),
							resource.TestCheckResourceAttrWith("neon_bucket.this", "s3_endpoint",
								func(value string) error {
									if value == "" {
										return fmt.Errorf("expected S3 endpoint to be set")
									}
									return nil
								}),
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

								b, err := client.ListProjectBranchBuckets(pr.ID, br.Branches[0].ID)
								if err != nil {
									return err
								}
								assert.Len(t, b.Buckets, 1)
								assert.Equal(t, b.Buckets[0].Name, "foo")
								assert.Equal(t, neon.BucketAccessLevelPrivate, b.Buckets[0].AccessLevel)
								return nil
							},
						),
					},
				},
			},
		)
	})

	t.Run("shall create a bucket with the public access level", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		projects.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
	name      = "%s"
	region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id   = neon_project.this.id
  branch_id    = neon_project.this.branch.id
  name         = "foo"
  access_level = "public_read"
}
`, projectName),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_bucket.this", "name", "foo"),
							resource.TestCheckResourceAttr("neon_bucket.this", "access_level",
								"public_read"),
							resource.TestCheckResourceAttr("neon_bucket.this", "region",
								"us-east-2"),
							resource.TestCheckResourceAttrWith("neon_bucket.this", "s3_endpoint",
								func(value string) error {
									if value == "" {
										return fmt.Errorf("expected S3 endpoint to be set")
									}
									return nil
								}),
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

								b, err := client.ListProjectBranchBuckets(pr.ID, br.Branches[0].ID)
								if err != nil {
									return err
								}
								assert.Len(t, b.Buckets, 1)
								assert.Equal(t, b.Buckets[0].Name, "foo")
								assert.Equal(t, neon.BucketAccessLevelPublicRead, b.Buckets[0].AccessLevel)
								return nil
							},
						),
					},
				},
			},
		)
	})

	t.Run("shall import a bucket", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		prCreateResp, err := client.CreateProject(neon.ProjectCreateRequest{Project: neon.ProjectCreateRequestProject{
			Name:     &projectName,
			RegionID: pointer("aws-us-east-2"),
		}})
		assert.NoErrorf(t, err, "could not provision the project")
		if err := projects.RecordProject(prCreateResp.Project); err != nil {
			t.Fatal(err)
		}

		projectID := prCreateResp.Project.ID
		sleepDuringRunningOperations(t, client, projectID)

		br, err := client.ListProjectBranches(projectID,
			nil, nil, nil, nil, nil, nil)
		assert.NoErrorf(t, err, "could not list branches")
		branchID := br.Branches[0].ID

		_, err = client.CreateProjectBranchBucket(projectID, branchID, neon.BucketCreateRequest{
			Name:        "foo",
			AccessLevel: &neon.BucketCreateRequestAccessLevelPublicRead,
		})
		assert.NoErrorf(t, err, "could not create bucket")

		projects.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_bucket" "this" {
  project_id   = "%s"
  branch_id    = "%s"
  name         = "foo"
  access_level = "public_read"
}
`, projectID, branchID),
						ImportState:   true,
						ResourceName:  "neon_bucket.this",
						ImportStateId: fmt.Sprintf("%s/%s/%s", projectID, branchID, "foo"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_bucket.this", "access_level",
								"public_read"),
							resource.TestCheckResourceAttr("neon_bucket.this", "region",
								"us-east-2"),
							resource.TestCheckResourceAttrWith("neon_bucket.this", "s3_endpoint",
								func(value string) error {
									if value == "" {
										return fmt.Errorf("expected S3 endpoint to be set")
									}
									return nil
								}),
						),
					},
				},
			},
		)
	})

	t.Run("shall fail to import a bucket given invalid id", func(t *testing.T) {
		resource.UnitTest(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: `resource "neon_bucket" "this" {
  project_id   = "0"
  branch_id    = "br-1"
  name         = "foo"
  access_level = "public_read"
}
`,
						ImportState:   true,
						ResourceName:  "neon_bucket.this",
						ImportStateId: "foo",
						ExpectError: regexp.MustCompile(
							"Expected an import ID in the form <project_id>/<branch_id>/<bucket_name>",
						),
					},
					{
						Config: `resource "neon_bucket" "this" {
  project_id   = "0"
  branch_id    = "br-1"
  name         = "foo"
  access_level = "public_read"
}
`,
						ImportState:   true,
						ResourceName:  "neon_bucket.this",
						ImportStateId: "0/br-1/foo/asd",
						ExpectError: regexp.MustCompile(
							"Expected an import ID in the form <project_id>/<branch_id>/<bucket_name>",
						),
					},
					{
						Config: `resource "neon_bucket" "this" {
  project_id   = "0"
  branch_id    = "br-1"
  name         = "foo"
  access_level = "public_read"
}
`,
						ImportState:   true,
						ResourceName:  "neon_bucket.this",
						ImportStateId: "br-1/foo",
						ExpectError: regexp.MustCompile(
							"Expected an import ID in the form <project_id>/<branch_id>/<bucket_name>",
						),
					},
				},
			},
		)
	})

	t.Run("shall fail to import non-existent bucket", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		prCreateResp, err := client.CreateProject(neon.ProjectCreateRequest{Project: neon.ProjectCreateRequestProject{
			Name:     &projectName,
			RegionID: pointer("aws-us-east-2"),
		}})
		assert.NoErrorf(t, err, "could not provision the project")
		if err := projects.RecordProject(prCreateResp.Project); err != nil {
			t.Fatal(err)
		}

		projectID := prCreateResp.Project.ID
		sleepDuringRunningOperations(t, client, projectID)

		br, err := client.ListProjectBranches(projectID,
			nil, nil, nil, nil, nil, nil)
		assert.NoErrorf(t, err, "could not list branches")
		branchID := br.Branches[0].ID

		_, err = client.CreateProjectBranchBucket(projectID, branchID, neon.BucketCreateRequest{
			Name:        "foo",
			AccessLevel: &neon.BucketCreateRequestAccessLevelPublicRead,
		})
		assert.NoErrorf(t, err, "could not create bucket")

		projects.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_bucket" "this" {
  project_id   = "%s"
  branch_id    = "%s"
  name         = "bar"
}
`, projectID, branchID),
						ImportState:   true,
						ResourceName:  "neon_bucket.this",
						ImportStateId: fmt.Sprintf("%s/%s/%s", projectID, branchID, "bar"),
						ExpectError:   regexp.MustCompile("Bucket Not Found"),
					},
				},
			},
		)
	})

	t.Run("shall fail to import non-existent bucket, no buckets exist in the project", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		prCreateResp, err := client.CreateProject(neon.ProjectCreateRequest{Project: neon.ProjectCreateRequestProject{
			Name:     &projectName,
			RegionID: pointer("aws-us-east-2"),
		}})
		assert.NoErrorf(t, err, "could not provision the project")
		if err := projects.RecordProject(prCreateResp.Project); err != nil {
			t.Fatal(err)
		}

		projectID := prCreateResp.Project.ID
		sleepDuringRunningOperations(t, client, projectID)

		br, err := client.ListProjectBranches(projectID,
			nil, nil, nil, nil, nil, nil)
		assert.NoErrorf(t, err, "could not list branches")
		branchID := br.Branches[0].ID

		projects.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_bucket" "this" {
  project_id   = "%s"
  branch_id    = "%s"
  name         = "foo"
}
`, projectID, branchID),
						ImportState:   true,
						ResourceName:  "neon_bucket.this",
						ImportStateId: fmt.Sprintf("%s/%s/%s", projectID, branchID, "foo"),
						ExpectError:   regexp.MustCompile("Bucket Not Found"),
					},
				},
			},
		)
	})

	t.Run("shall destroy deleted bucket", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := fmt.Sprintf(`resource "neon_project" "this" {
	name      = "%s"
	region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = "foo"
}
`, projectName)
		projects.Test(
			t, resource.TestCase{
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

							b, err := client.ListProjectBranchBuckets(pr.ID, br.Branches[0].ID)
							if err != nil {
								panic(err)
							}
							assert.Len(t, b.Buckets, 1)
							assert.Equal(t, b.Buckets[0].Name, "foo")
							assert.Equal(t, neon.BucketAccessLevelPrivate, b.Buckets[0].AccessLevel)

							err = client.DeleteProjectBranchBucket(pr.ID, br.Branches[0].ID, b.Buckets[0].Name)
							if err != nil {
								panic(err)
							}
						},
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_bucket.this"]
							assert.False(t, ok, "resource neon_bucket.this should be destroyed")
							return nil
						},
					},
				},
			},
		)
	})

	t.Run("shall plan bucket replacement", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		projects.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
	name      = "%s"
	region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = "foo"
}
`, projectName),
					},
					{
						Config: fmt.Sprintf(`resource "neon_project" "this" {
	name      = "%s"
	region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id   = neon_project.this.id
  branch_id    = neon_project.this.branch.id
  name         = "foo"
  access_level = "public_read"
}
`, projectName),
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
				},
			},
		)
	})
}
