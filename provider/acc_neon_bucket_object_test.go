package provider

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestBucketObject(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}

	projectNamePrefix := "bucketObject"

	t.Run("shall fail plan: content, content_base64, source, is_directory are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id     = "foo"
  branch_id      = "br"
  bucket         = "foo"
  key            = "foo"
  content        = "foo"
  content_base64 = "foo"
  source         = "foo"
  is_directory   = true
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: content, content_base64, source are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id     = "foo"
  branch_id      = "br"
  bucket         = "foo"
  key            = "foo"
  content        = "foo"
  content_base64 = "foo"
  source         = "foo"
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: content, content_base64 are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id     = "foo"
  branch_id      = "br"
  bucket         = "foo"
  key            = "foo"
  content        = "foo"
  content_base64 = "foo"
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: content, source are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id = "foo"
  branch_id  = "br"
  bucket     = "foo"
  key        = "foo"
  content    = "foo"
  source     = "foo"
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: content_base64, source are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id     = "foo"
  branch_id      = "br"
  bucket         = "foo"
  key            = "foo"
  content_base64 = "foo"
  source         = "foo"
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: content and is_directory are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id   = "foo"
  branch_id    = "br"
  bucket       = "foo"
  key          = "foo"
  content      = "foo"
  is_directory = true
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: content_base64 and is_directory are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id   = "foo"
  branch_id    = "br"
  bucket       = "foo"
  key          = "foo"
  content_base64 = "foo"
  is_directory = true
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: source and is_directory are set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id   = "foo"
  branch_id    = "br"
  bucket       = "foo"
  key          = "foo"
  source       = "foo"
  is_directory = true
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("shall fail plan: neither of content, content_base64, source, or is_directory is set", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: `resource "neon_bucket_object" "this" {
  project_id   = "foo"
  branch_id    = "br"
  bucket       = "foo"
  key          = "foo"
}`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile("conflicting configuration"),
			}},
		})
	})

	t.Run("creates objects from content, base64, and source", func(t *testing.T) {
		content := "hello from content"
		contentBase64 := base64.StdEncoding.EncodeToString([]byte("hello from base64"))
		sourcePath := filepath.Join(t.TempDir(), "object.txt")
		if err := os.WriteFile(sourcePath, []byte("hello from source"), 0o600); err != nil {
			t.Fatal(err)
		}

		configs := []struct {
			name              string
			input             string
			wantContentLength int
		}{
			{"content", fmt.Sprintf("content = %q", content), len(content)},
			{"content_base64", fmt.Sprintf("content_base64 = %q", contentBase64), len("hello from base64")},
			{"source", fmt.Sprintf("source = %q", sourcePath), len("hello from source")},
		}
		for _, tc := range configs {
			t.Run(tc.name, func(t *testing.T) {
				projectName := newProjectName(projectNamePrefix)
				resource.Test(t, resource.TestCase{
					ProtoV6ProviderFactories: newProviderFactories(),
					Steps: []resource.TestStep{{
						Config: bucketObjectConfig(projectName, "object", tc.input+`
content_type = "text/plain"`),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("neon_bucket_object.this", "key", "object"),
							resource.TestCheckResourceAttr("neon_bucket_object.this", "content_length", fmt.Sprint(tc.wantContentLength)),
							resource.TestCheckResourceAttr("neon_bucket_object.this", "content_type", "text/plain"),
							resource.TestCheckResourceAttrWith("neon_bucket_object.this", "etag", nonEmptyAttribute("etag")),
							resource.TestCheckResourceAttrWith("neon_bucket_object.this", "trigger", nonEmptyAttribute("trigger")),
						),
					}},
				})
			})
		}
	})

	t.Run("creates a folder", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: bucketObjectConfig(newProjectName(projectNamePrefix), "folder", "is_directory = true"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_bucket_object.this", "key", "folder"),
					resource.TestCheckResourceAttr("neon_bucket_object.this", "content_length", "0"),
					resource.TestCheckResourceAttrWith("neon_bucket_object.this", "etag", nonEmptyAttribute("etag")),
					resource.TestCheckNoResourceAttr("neon_bucket_object.this", "trigger"),
					resource.TestCheckNoResourceAttr("neon_bucket_object.this", "content_type"),
				),
			}},
		})
	})

	for _, key := range []string{"folder", "folder/"} {
		t.Run("imports a folder created as "+strings.ReplaceAll(key, "/", "-slash"), func(t *testing.T) {
			config := bucketObjectConfig(newProjectName(projectNamePrefix), key, "is_directory = true")
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{Config: config},
					{
						// The imported state must match what the create produced, or the next plan replaces the folder.
						Config: config, ResourceName: "neon_bucket_object.this",
						ImportState: true, ImportStateVerify: true,
					},
				},
			})
		})
	}

	t.Run("deletes an object deleted outside terraform", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		config := bucketObjectConfig(projectName, "delete-me", `content = "delete me"`)
		var projectID, branchID string
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config,
					Check: func(s *terraform.State) error {
						o, ok := s.RootModule().Resources["neon_bucket_object.this"]
						assert.True(t, ok, "resource neon_bucket_object.this must be present")
						projectID = o.Primary.Attributes["project_id"]
						branchID = o.Primary.Attributes["branch_id"]
						return nil
					},
				},
				{
					PreConfig: func() {
						if err := client.DeleteProjectBranchBucketObject(projectID, branchID, "objects",
							"delete-me"); err != nil {
							panic(err)
						}
					},
					Destroy: true,
					Config:  config,
					Check: func(s *terraform.State) error {
						_, ok := s.RootModule().Resources["neon_bucket_object.this"]
						assert.False(t, ok, "resource neon_bucket_object.this must be destroyed")
						return nil
					},
				},
			},
		})
	})

	t.Run("renames an object", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: bucketObjectConfig(projectName, "before.txt", `content = "rename me"`)},
				{Config: bucketObjectConfig(projectName, "after.txt", `content = "rename me"`), Check: resource.TestCheckResourceAttr("neon_bucket_object.this", "key", "after.txt")},
			},
		})
	})

	t.Run("updates object content", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		var oldETag string
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: bucketObjectConfig(projectName, "mutable.txt", `content = "first"`), Check: resource.TestCheckResourceAttrWith("neon_bucket_object.this", "etag", func(value string) error { oldETag = value; return nil })},
				{Config: bucketObjectConfig(projectName, "mutable.txt", `content = "second"`), Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_bucket_object.this", "content_length", "6"),
					resource.TestCheckResourceAttrWith("neon_bucket_object.this", "etag", func(value string) error {
						if value == oldETag {
							return fmt.Errorf("etag did not change")
						}
						return nil
					}),
				)},
			},
		})
	})

	for _, key := range []string{"import me.txt", "reports/100% done?.txt"} {
		t.Run("imports an object named "+key, func(t *testing.T) {
			projectName := newProjectName(projectNamePrefix)
			projectID, branchID, err := createBucketObjectFixture(t, client, projectName, key,
				[]byte("imported"), "text/plain")
			if err != nil {
				t.Fatal(err)
			}
			config := fmt.Sprintf(`resource "neon_bucket_object" "this" {
  project_id   = %q
  branch_id    = %q
  bucket       = "objects"
  key          = %q
  content      = "imported"
  content_type = "text/plain"
}
`, projectID, branchID, key)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      config,
						ImportState: true, ImportStatePersist: true, ResourceName: "neon_bucket_object.this",
						ImportStateId: fmt.Sprintf("%s/%s/objects/%s", projectID, branchID, key),
						ImportStateCheck: func(states []*terraform.InstanceState) error {
							if len(states) != 1 {
								return fmt.Errorf("imported %d resources, want 1", len(states))
							}
							for attr, want := range map[string]string{
								"key": key, "content_length": "8", "is_directory": "false",
							} {
								if got := states[0].Attributes[attr]; got != want {
									return fmt.Errorf("%s = %q, want %q", attr, got, want)
								}
							}
							return nil
						},
					},
					{
						// Neon cannot return an object's content or content type, so the first plan after an
						// import uploads the configured content in place.
						Config:             config,
						PlanOnly:           true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
		})
	}

	t.Run("creates an empty string object from content", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{{
				Config: bucketObjectConfig(newProjectName(projectNamePrefix), "empty obj", "content = \"\""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("neon_bucket_object.this", "key", "empty obj"),
					resource.TestCheckResourceAttr("neon_bucket_object.this", "content_length", "0"),
					resource.TestCheckResourceAttrWith("neon_bucket_object.this", "etag", nonEmptyAttribute("etag")),
					resource.TestCheckResourceAttrWith("neon_bucket_object.this", "trigger", nonEmptyAttribute("trigger")),
				),
			}},
		})
	})

	t.Run("updates an object provided by source when trigger is updated", func(t *testing.T) {
		sourcePath := filepath.Join(t.TempDir(), "object.txt")
		if err := os.WriteFile(sourcePath, []byte("foo"), 0o600); err != nil {
			t.Fatal(err)
		}
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: bucketObjectConfig(projectName, "obj", fmt.Sprintf(`source = %q`, sourcePath)),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_bucket_object.this", "key", "obj"),
						resource.TestCheckResourceAttr("neon_bucket_object.this", "content_length", "3"),
						resource.TestCheckResourceAttrWith("neon_bucket_object.this", "etag", nonEmptyAttribute("etag")),
						resource.TestCheckResourceAttrWith("neon_bucket_object.this", "trigger", nonEmptyAttribute("trigger")),
					),
				},
				{
					Config:   bucketObjectConfig(projectName, "obj", fmt.Sprintf(`source = %q`, sourcePath)),
					PlanOnly: true,
				},
				{
					Config: bucketObjectConfig(projectName, "obj", fmt.Sprintf(`source = %q
trigger = "foo"`, sourcePath)),
					PlanOnly:           true,
					ExpectNonEmptyPlan: true,
				},
				{
					PreConfig: func() {
						if err := os.WriteFile(sourcePath, []byte("foobar"), 0o600); err != nil {
							t.Fatal(err)
						}
					},
					Config: bucketObjectConfig(projectName, "obj", fmt.Sprintf(`source = %q
trigger = "foo"`, sourcePath)),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_bucket_object.this", "content_length", "6"),
					),
				},
			},
		})
	})
}

func bucketObjectConfig(projectName, key, input string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  region_id = "aws-us-east-2"
}

resource "neon_bucket" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = "objects"
}

resource "neon_bucket_object" "this" {
  project_id  = neon_project.this.id
  branch_id   = neon_project.this.branch.id
  bucket      = neon_bucket.this.name
  key         = %q
  %s
}
`, projectName, key, input)
}

func nonEmptyAttribute(name string) func(string) error {
	return func(value string) error {
		if value == "" {
			return fmt.Errorf("expected %s to be set", name)
		}
		return nil
	}
}

func createBucketObjectFixture(t *testing.T, client *neon.Client, projectName, key string, content []byte, contentType string) (string, string, error) {
	t.Helper()
	created, err := client.CreateProject(neon.ProjectCreateRequest{Project: neon.ProjectCreateRequestProject{
		Name:     &projectName,
		OrgID:    pointer(os.Getenv("ORG_ID")),
		RegionID: pointer("aws-us-east-2"),
	}})
	if err != nil {
		return "", "", err
	}
	projectID := created.Project.ID
	if projectID == "" {
		return "", "", fmt.Errorf("create test project: response has no project ID")
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
	if err != nil {
		return "", "", err
	}
	branchID := branches.Branches[0].ID
	if _, err = client.CreateProjectBranchBucket(projectID, branchID, neon.BucketCreateRequest{Name: "objects"}); err != nil {
		return "", "", err
	}
	presigned, err := client.PresignProjectBranchBucketObject(projectID, branchID, "objects", url.PathEscape(key), neon.PresignRequest{Operation: neon.PresignRequestOperationUpload, ContentType: &contentType})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequest(presigned.Method, presigned.URL, bytes.NewReader(content))
	if err != nil {
		return "", "", withoutRequestURL(err)
	}
	for key, value := range presigned.Headers {
		req.Header.Set(key, fmt.Sprint(value))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", withoutRequestURL(err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return "", "", fmt.Errorf("upload returned %s", res.Status)
	}
	return projectID, branchID, nil
}
