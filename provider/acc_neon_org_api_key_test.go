package provider

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestRecreateOrgAPIKeyIfNotFound(t *testing.T) {
	// see: https://github.com/kislerdm/terraform-provider-neon/issues/209

	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	usePersonalAPIKey(t)

	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	var preConfig = func(name string) {
		ref, err := client.ListOrgApiKeys(orgID)
		if err != nil {
			panic(err)
		}

		for _, key := range ref {
			if key.Name == name {
				_, err = client.RevokeOrgApiKey(orgID, key.ID)
				if err != nil {
					panic(err)
				}
			}
		}

	}

	t.Run("shall indicate non empty plan if the API key was deleted outside of terraform", func(t *testing.T) {
		keyName := "test" + uuid.NewString()
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "neon_org_api_key" "this" {
	name   = "%s"
	org_id = "%s"
}`, keyName, orgID),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_org_api_key.this",
								"name", keyName,
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(keyName)
						},
						RefreshState:       true,
						ExpectNonEmptyPlan: true,
					},
				},
			})
	})

	t.Run("shall destroy even if the API key was deleted outside of terraform,", func(t *testing.T) {
		keyName := "test" + uuid.NewString()
		config := fmt.Sprintf(`resource "neon_org_api_key" "this" {
	name   = "%s"
	org_id = "%s"
}`, keyName, orgID)
		resource.Test(
			t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"neon_org_api_key.this",
								"name", keyName,
							),
						),
					},
					{
						PreConfig: func() {
							preConfig(keyName)
						},
						Config:  config,
						Destroy: true,
						Check: func(s *terraform.State) error {
							_, ok := s.RootModule().Resources["neon_org_api_key.this"]
							assert.False(t, ok, "resource neon_org_api_key.this should be destroyed")
							return nil
						},
					},
				},
			})
	})
}

func TestOrgAPIKey(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	usePersonalAPIKey(t)

	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		t.Fatal(err)
	}

	wantKeyName := "test" + uuid.NewString()

	t.Cleanup(func() {
		keys, _ := client.ListOrgApiKeys(orgID)
		for _, key := range keys {
			if wantKeyName == key.Name {
				_, _ = client.RevokeOrgApiKey(orgID, key.ID)
			}
		}
	})

	resourceDefinition := fmt.Sprintf(`resource "neon_org_api_key" "this" { 
org_id = "%s" 
name   = "%s"
}`, orgID, wantKeyName)
	const resourceName = "neon_org_api_key.this"

	resource.Test(
		t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: resourceDefinition,
					Check: resource.ComposeTestCheckFunc(
						// verify the recorded key name
						resource.TestCheckResourceAttr(resourceName, "name", wantKeyName),
						// verify that the key with the given name was created
						func(_ *terraform.State) error {
							keys, e := client.ListOrgApiKeys(orgID)
							if e == nil {
								if !slices.ContainsFunc(keys, func(key neon.OrgApiKeysListResponseItem) bool {
									return wantKeyName == key.Name
								}) {
									e = fmt.Errorf("key %s not found", wantKeyName)
								}
							}
							return e
						},
						// verify that the valid key was recorded
						resource.TestCheckResourceAttrWith(resourceName, "key", func(value string) error {
							_, err := neon.NewClient(neon.Config{Key: value})
							return err
						}),
					),
				},
				{
					Config:  resourceDefinition,
					Destroy: true,
					Check: resource.ComposeTestCheckFunc(
						// verify that the key with the given name was indeed revoked
						func(_ *terraform.State) error {
							keys, e := client.ListOrgApiKeys(orgID)
							if e == nil {
								if slices.ContainsFunc(keys, func(key neon.OrgApiKeysListResponseItem) bool {
									return wantKeyName == key.Name
								}) {
									e = fmt.Errorf("key %s is expected to be not found", wantKeyName)
								}
							}

							return e
						},
					),
				},
			},
		})
}
