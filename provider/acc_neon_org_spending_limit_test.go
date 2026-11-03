//go:build acceptance

package provider

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestAccNeonOrgSpendingLimit(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	require.NoError(t, err)

	t.Run("create and read verifies the live API", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newOrgSpendingLimitConfig(orgID, 10000),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(
							"neon_org_spending_limit.this",
							"org_id",
							orgID,
						),
						verifyOrgSpendingLimitAgainstAPI(t, client),
					),
				},
			},
		})
	})

	t.Run("update verifies the live API", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: newOrgSpendingLimitConfig(orgID, 11000)},
				{
					Config: newOrgSpendingLimitConfig(orgID, 12000),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr(
							"neon_org_spending_limit.this",
							"spending_limit_cents",
							"12000",
						),
						verifyOrgSpendingLimitAgainstAPI(t, client),
					),
				},
			},
		})
	})

	t.Run("import verifies the live API", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: newOrgSpendingLimitConfig(orgID, 13000)},
				{
					ResourceName:      "neon_org_spending_limit.this",
					ImportState:       true,
					ImportStateVerify: true,
					ImportStateId:     orgID,
					Check: resource.ComposeTestCheckFunc(
						verifyOrgSpendingLimitAgainstAPI(t, client),
					),
				},
			},
		})
	})

	t.Run("out-of-band deletion removes the resource from state", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newOrgSpendingLimitConfig(orgID, 14000),
					Check: resource.ComposeTestCheckFunc(
						recordOrgSpendingLimitID(t),
					),
				},
				{
					PreConfig: func() {
						_, deleteErr := client.DeleteOrganizationSpendingLimit(orgID)
						require.NoError(t, deleteErr)
					},
					Config: newOrgSpendingLimitConfig(orgID, 15000),
					Check: resource.ComposeTestCheckFunc(
						assertOrgSpendingLimitRecreated(t, client, orgID, 15000),
					),
				},
			},
		})
	})

	t.Run("rejects non-positive values", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config:      newOrgSpendingLimitConfig(orgID, 0),
					ExpectError: regexp.MustCompile("spending_limit_cents must be greater than zero"),
				},
				{
					Config:      newOrgSpendingLimitConfig(orgID, -1),
					ExpectError: regexp.MustCompile("spending_limit_cents must be greater than zero"),
				},
			},
		})
	})
}

func newOrgSpendingLimitConfig(orgID string, cents int64) string {
	return fmt.Sprintf(`resource "neon_org_spending_limit" "this" {
  org_id               = %q
  spending_limit_cents = %d
}
`, orgID, cents)
}

var recordedOrgSpendingLimitID string

func recordOrgSpendingLimitID(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rawResource, ok := state.RootModule().Resources["neon_org_spending_limit.this"]
		if !ok {
			return fmt.Errorf("neon_org_spending_limit.this not found in state")
		}
		recordedOrgSpendingLimitID = rawResource.Primary.ID
		if recordedOrgSpendingLimitID == "" {
			return fmt.Errorf("resource ID is empty")
		}
		return nil
	}
}

func assertOrgSpendingLimitRecreated(
	t *testing.T,
	client *neon.Client,
	orgID string,
	expectedCents int64,
) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rawResource, ok := state.RootModule().Resources["neon_org_spending_limit.this"]
		if !ok {
			return fmt.Errorf("neon_org_spending_limit.this not found in state")
		}
		if rawResource.Primary.ID == recordedOrgSpendingLimitID {
			return fmt.Errorf("resource ID %q was not recreated after out-of-band deletion", recordedOrgSpendingLimitID)
		}

		response, err := client.GetOrganizationSpendingLimit(orgID)
		if err != nil {
			return fmt.Errorf("get recreated organization spending limit: %w", err)
		}
		if response.SpendingLimitCents != expectedCents {
			return fmt.Errorf("API spending limit is %d cents, want %d", response.SpendingLimitCents, expectedCents)
		}
		return nil
	}
}

func verifyOrgSpendingLimitAgainstAPI(
	t *testing.T,
	client *neon.Client,
) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rawResource, ok := state.RootModule().Resources["neon_org_spending_limit.this"]
		if !ok {
			return fmt.Errorf("neon_org_spending_limit.this not found in state")
		}

		orgID := rawResource.Primary.Attributes["org_id"]
		stateCents, err := strconv.ParseInt(
			rawResource.Primary.Attributes["spending_limit_cents"],
			10,
			64,
		)
		if err != nil {
			return fmt.Errorf("parse state spending_limit_cents: %w", err)
		}

		response, err := client.GetOrganizationSpendingLimit(orgID)
		if err != nil {
			return fmt.Errorf("get organization spending limit: %w", err)
		}
		if response.SpendingLimitCents != stateCents {
			return fmt.Errorf(
				"API spending limit is %d cents, Terraform state is %d cents",
				response.SpendingLimitCents,
				stateCents,
			)
		}
		return nil
	}
}
