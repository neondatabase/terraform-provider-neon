package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestOrgMember(t *testing.T) {
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

	var findMemberByEmail = func(email string) neon.Member {
		var cursor *string
		for {
			resp, err := client.GetOrganizationMembers(orgID, nil, cursor, nil, nil)
			assert.NoErrorf(t, err, "failed to list project members")

			for _, el := range resp.Members {
				if el.User.Email == email {
					return el.Member
				}
			}

			cursor = resp.CursorPaginationResponse.Pagination.Next
			if cursor == nil {
				break
			}
		}
		return neon.Member{}
	}

	var newConfig = func(memberID string, role neon.MemberRole) string {
		return fmt.Sprintf(`resource "neon_org_member_role" "this" {
  org_id     = %q
  member_id  = %q
  role       = %q
}`, orgID, memberID, role.String())
	}

	// FIXME(?): make configurable via envvars
	testMemberEmail := "admin+neontest@dkisler.com"

	member := findMemberByEmail(testMemberEmail)
	if member.ID == "" {
		t.Skip("no test member found in the org.")
	}
	memberID := member.ID

	var verifyRole = func(role neon.MemberRole) resource.TestCheckFunc {
		return resource.ComposeTestCheckFunc(
			resource.TestCheckResourceAttr("neon_org_member_role.this", "role", role.String()),
			func(_ *terraform.State) error {
				m, err := client.GetOrganizationMember(orgID, memberID)
				assert.NoError(t, err)
				assert.Equal(t, role, m.Role)
				return nil
			},
		)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			// set member's role to admin
			{
				Config: newConfig(memberID, neon.MemberRoleAdmin),
				Check:  verifyRole(neon.MemberRoleAdmin),
			},
			// update in place to collaborator
			{
				Config: newConfig(memberID, neon.MemberRoleCollaborator),
				Check:  verifyRole(neon.MemberRoleCollaborator),
			},
			// import
			{
				Config:            newConfig(memberID, neon.MemberRoleCollaborator),
				Check:             verifyRole(neon.MemberRoleCollaborator),
				ResourceName:      "neon_org_member_role.this",
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/%s", orgID, memberID),
				ImportStateVerify: true,
			},
			// remove the role from the state
			{
				Config:  newConfig(memberID, neon.MemberRoleCollaborator),
				Destroy: true,
			},
			{
				RefreshState: true,
				Check: func(state *terraform.State) error {
					_, ok := state.RootModule().Resources["neon_org_member_role.this"]
					assert.False(t, ok)
					return nil
				},
				// it's expected that the plan will suggest to create deleted resource
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
