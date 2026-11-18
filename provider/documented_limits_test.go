package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestNeonAuthLinkVerificationPlansWithAnUnknownEmailProviderType(t *testing.T) {
	t.Parallel()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config: `provider "neon" {
  api_key = "unused"
}

resource "terraform_data" "smtp" { input = "standard" }

resource "neon_auth" "this" {
  project_id = "cool-moon-42"
  branch_id  = "br-cool-moon-42"
  email_provider = {
    type         = terraform_data.smtp.output
    host         = "smtp.example.com"
    port         = 587
    username     = "apikey"
    password     = "secret"
    sender_email = "auth@example.com"
    sender_name  = "Example"
  }
  email_and_password = { email_verification_method = "link" }
}`,
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}

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
			wantError: `value must be between 1 and 2147483647`,
		},
		"organization limit over the int32 range": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  organization = { organization_limit = 4294967297 }
}`, ids),
			wantError: `value must be between 1 and 2147483647`,
		},
		"unknown organization creator role": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  organization = { creator_role = "member" }
}`, ids),
			wantError: `value must be one of`,
		},
		"verification links on a new Neon Auth integration": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  email_and_password = { email_verification_method = "link" }
}`, ids),
			wantError: `Verification links need a custom email provider`,
		},
		"webhook timeout over 10 seconds": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  webhook = { timeout_seconds = 11 }
}`, ids),
			wantError: `value must be between 1 and 10`,
		},
		"unknown webhook event": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  webhook = { enabled_events = ["user.deleted"] }
}`, ids),
			wantError: `value must be one of`,
		},
		"email provider without type": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  email_provider = { host = "smtp.example.com" }
}`, ids),
			wantError: `Missing email provider type`,
		},
		"shared email provider with a sender name": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  email_provider = {
    type        = "shared"
    sender_name = "Example"
  }
}`, ids),
			wantError: `Attribute only applies to a standard email provider`,
		},
		"standard email provider without a password": {
			config: fmt.Sprintf(`resource "neon_auth" "this" {
  %s
  email_provider = {
    type         = "standard"
    host         = "smtp.example.com"
    port         = 587
    username     = "apikey"
    sender_email = "auth@example.com"
    sender_name  = "Example"
  }
}`, ids),
			wantError: `Set password; a standard email provider needs`,
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
