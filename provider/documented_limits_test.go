package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestDocumentedLimitsAreValidatedDuringPlan(t *testing.T) {
	t.Parallel()

	ids := `project_id = "cool-moon-42"
  branch_id  = "br-cool-moon-42"`
	for name, tt := range map[string]struct {
		config    string
		wantError string
	}{
		"api key name over 64 characters": {
			config:    fmt.Sprintf(`resource "neon_api_key" "this" { name = %q }`, strings.Repeat("a", 65)),
			wantError: `character count must be at most 64`,
		},
		"org api key name over 64 characters": {
			config: fmt.Sprintf(`resource "neon_org_api_key" "this" {
  org_id = "org-cool-moon-42"
  name   = %q
}`, strings.Repeat("a", 65)),
			wantError: `character count must be at most 64`,
		},
		"empty branch name": {
			config: `resource "neon_branch" "this" {
  project_id = "cool-moon-42"
  name       = ""
}`,
			wantError: `character count must be between 1 and 256`,
		},
		"branch name over 256 characters": {
			config: fmt.Sprintf(`resource "neon_branch" "this" {
  project_id = "cool-moon-42"
  name       = %q
}`, strings.Repeat("a", 257)),
			wantError: `character count must be between 1 and 256`,
		},
		"endpoint name over 64 characters": {
			config: fmt.Sprintf(`resource "neon_endpoint" "this" {
  %s
  name = %q
}`, ids, strings.Repeat("a", 65)),
			wantError: `character count must be between 1 and 64`,
		},
		"project primary compute name over 64 characters": {
			config: fmt.Sprintf(`resource "neon_project" "this" {
  primary_compute = {
    name = %q
  }
}`, strings.Repeat("a", 65)),
			wantError: `character count must be between 1 and 64`,
		},
		"no service credential scopes": {
			config: fmt.Sprintf(`resource "neon_service_credential" "this" {
  %s
  scopes = []
}`, ids),
			wantError: `list must contain at least 1 elements and at most 16`,
		},
		"unknown data api auth provider": {
			config: fmt.Sprintf(`resource "neon_data_api" "this" {
  %s
  database_name = "neondb"
  auth_provider = "unsupported"
}`, ids),
			wantError: `value must be one of`,
		},
		"magic link expiring in under 5 minutes": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  magic_link = { expires_in = 4 }
}`, ids),
			wantError: `value must be between 5 and 1440`,
		},
		"phone number one-time password expiring after 600 seconds": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  phone_number = { otp_expires_in = 601 }
}`, ids),
			wantError: `value must be between 60 and 600`,
		},
		"organization without members": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  organization = { membership_limit = 0 }
}`, ids),
			wantError: `value must be at least 1`,
		},
		"unknown organization creator role": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  organization = { creator_role = "member" }
}`, ids),
			wantError: `value must be one of`,
		},
		"unknown email verification method": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  email_and_password = { email_verification_method = "sms" }
}`, ids),
			wantError: `value must be one of`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{{
					Config:      "provider \"neon\" {\n  api_key = \"unused\"\n}\n\n" + tt.config,
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(strings.ReplaceAll(tt.wantError, " ", `\s+`)),
				}},
			})
		})
	}
}

// Neon counts characters, so a 64-character name made of multi-byte characters is valid.
func TestAPIKeyNameLimitCountsCharacters(t *testing.T) {
	t.Parallel()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf("provider \"neon\" {\n  api_key = \"unused\"\n}\n\nresource \"neon_api_key\" \"this\" { name = %q }",
				strings.Repeat("é", 64)),
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}

// Granted credentials can carry scopes that can't be requested, such as telemetry:write, and an imported
// credential's configuration has to list them.
func TestServiceCredentialAcceptsGrantedScopes(t *testing.T) {
	t.Parallel()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config: `provider "neon" {
  api_key = "unused"
}

resource "neon_service_credential" "this" {
  project_id = "cool-moon-42"
  branch_id  = "br-cool-moon-42"
  scopes     = ["telemetry:write"]
}`,
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}
