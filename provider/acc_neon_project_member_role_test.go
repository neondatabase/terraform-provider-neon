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

func TestProjectMember(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	projectName := newProjectName("projectMember")
	projectCreateResp, err := client.CreateProject(neon.ProjectCreateRequest{Project: neon.ProjectCreateRequestProject{
		Name: &projectName,
	}})
	assert.NoError(t, err, "failed to create project")
	projectID := projectCreateResp.ProjectResponse.Project.ID
	t.Cleanup(func() {
		_, _ = client.DeleteProject(projectID)
	})

	var findMemberByEmail = func(email string) neon.ProjectMember {
		var cursor *string
		for {
			resp, err := client.ListProjectMembers(projectID, cursor, nil)
			assert.NoErrorf(t, err, "failed to list project members")

			for _, el := range resp.ProjectMembers {
				if el.Email != nil && email == *el.Email {
					return el
				}
			}
			cursor = resp.Pagination.Next
			if cursor == nil {
				break
			}
		}
		return neon.ProjectMember{}
	}

	var newConfig = func(memberID string, role neon.ProjectRole) string {
		return fmt.Sprintf(`resource "neon_project_member_role" "this" {
  project_id = %q
  member_id  = %q
  role       = %q
}`, projectID, memberID, role.String())
	}

	t.Run("shall set project role for the org. non-admin", func(t *testing.T) {
		// FIXME(?): make configurable via envvars
		testMemberEmail := "admin+neontest@dkisler.com"

		member := findMemberByEmail(testMemberEmail)

		if member.MemberID == "" || member.OrgRole == neon.ProjectMemberOrgRoleAdmin {
			t.Skip("no test member w/o admin permissions found in the org.")
		}

		memberID := member.MemberID

		var verifyRole = func(role neon.ProjectRole) resource.TestCheckFunc {
			return resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("neon_project_member_role.this", "role", role.String()),
				func(_ *terraform.State) error {
					m, err := findMember(t.Context(), client, projectID, memberID)
					if err != nil {
						return err
					}
					assert.NotNil(t, m.ProjectRole)
					assert.Equal(t, role, *m.ProjectRole)
					return nil
				},
			)
		}

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				// set member's role to admin
				{
					Config: newConfig(memberID, neon.ProjectRoleAdmin),
					Check:  verifyRole(neon.ProjectRoleAdmin),
				},
				// update in place to viewer
				{
					Config: newConfig(memberID, neon.ProjectRoleViewer),
					Check:  verifyRole(neon.ProjectRoleViewer),
				},
				// import
				{
					Config:            newConfig(memberID, neon.ProjectRoleViewer),
					Check:             verifyRole(neon.ProjectRoleViewer),
					ResourceName:      "neon_project_member_role.this",
					ImportState:       true,
					ImportStateId:     fmt.Sprintf("%s/%s", projectID, memberID),
					ImportStateVerify: true,
				},
				// remove project role
				{
					Config:  newConfig(memberID, neon.ProjectRoleViewer),
					Destroy: true,
				},
				// remove removed project role
				{
					PreConfig: func() {
						r, err := client.RemoveProjectMemberRole(projectID, memberID, nil)
						if err != nil {
							panic(err)
						}
						assert.Nil(t, r.ProjectRole)
					},
					Config:  newConfig(memberID, neon.ProjectRoleViewer),
					Destroy: true,
				},
			},
		})
	})

	t.Run("shall fail plan for the the org. admin member", func(t *testing.T) {
		// FIXME(?): make configurable via envvars
		testMemberEmail := "admin@dkisler.com"

		member := findMemberByEmail(testMemberEmail)

		if member.MemberID == "" || member.OrgRole != neon.ProjectMemberOrgRoleAdmin {
			t.Skip("no test member w/ admin permissions found in the org.")
		}

		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:      newConfig(member.MemberID, neon.ProjectRoleViewer),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile("cannot set project-level role"),
				},
				{
					Config:             newConfig(member.MemberID, neon.ProjectRoleAdmin),
					PlanOnly:           true,
					ExpectNonEmptyPlan: true,
				},
			},
		})
	})
}
