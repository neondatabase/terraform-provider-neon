package provider

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

func TestAccNeonAuth(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}
	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "neonAuth"

	project := func(name, rest string) string {
		return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}
%s`, name, orgID, rest)
	}
	ids := func(state *terraform.State) (string, string) {
		p := state.RootModule().Resources["neon_project.this"].Primary
		return p.ID, p.Attributes["branch.id"]
	}
	// Neon cannot enable Neon Auth again on a branch that keeps the neon_auth schema.
	disableOutOfBand := func(projectName string) func() {
		return func() {
			ref, err := readProjectInfo(client, projectName)
			if err != nil {
				t.Fatal(err)
			}
			branches, err := client.ListProjectBranches(ref.ID, nil, nil, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			deleteData := true
			for _, b := range branches.Branches {
				if b.Default {
					if err := client.DisableNeonAuth(ref.ID, b.ID, &neon.DisableNeonAuthCfg{DeleteData: &deleteData}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}

	t.Run("enables Neon Auth, updates allow_localhost in place, imports, and disables it on destroy", func(t *testing.T) {
		projectName := newProjectName(prefix)
		auth := func(localhost bool) string {
			return project(projectName, fmt.Sprintf(`
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  allow_localhost        = %t
  delete_data_on_destroy = true
}
`, localhost))
		}
		var createdAt string
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: auth(false),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_auth.this", "auth_provider", "better_auth"),
						resource.TestCheckResourceAttr("neon_auth.this", "allow_localhost", "false"),
						resource.TestCheckResourceAttr("neon_auth.this", "database_name", "neondb"),
						resource.TestMatchResourceAttr("neon_auth.this", "base_url", regexp.MustCompile(`^https://.+/auth$`)),
						resource.TestMatchResourceAttr("neon_auth.this", "jwks_url", regexp.MustCompile(`/\.well-known/jwks\.json$`)),
						func(state *terraform.State) error {
							createdAt = state.RootModule().Resources["neon_auth.this"].Primary.Attributes["created_at"]
							projectID, branchID := ids(state)
							got, err := client.GetNeonAuthAllowLocalhost(projectID, branchID)
							if err != nil {
								return err
							}
							if got.AllowLocalhost {
								return errors.New("allow_localhost is true in Neon, want false")
							}
							return nil
						},
					),
				},
				{
					Config: auth(true),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("neon_auth.this", plancheck.ResourceActionUpdate),
						},
					},
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_auth.this", "allow_localhost", "true"),
						func(state *terraform.State) error {
							if got := state.RootModule().Resources["neon_auth.this"].Primary.Attributes["created_at"]; got != createdAt {
								return fmt.Errorf("created_at changed from %s to %s", createdAt, got)
							}
							return nil
						},
					),
				},
				{
					Config:            auth(true),
					ResourceName:      "neon_auth.this",
					ImportState:       true,
					ImportStateVerify: true,
					// delete_data_on_destroy only exists in the configuration.
					ImportStateVerifyIgnore: []string{"delete_data_on_destroy"},
				},
				{
					// Moving Neon Auth to another database would disable it on a branch where it cannot come back.
					Config: project(projectName, `
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  database_name          = "other"
  allow_localhost        = true
  delete_data_on_destroy = true
}
`),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile("Cannot move Neon Auth to another database"),
				},
				{
					Config: project(projectName, ""),
					Check: func(state *terraform.State) error {
						projectID, branchID := ids(state)
						if _, err := client.GetNeonAuth(projectID, branchID); !isNotFoundError(err) {
							return fmt.Errorf("Neon Auth still enabled after destroy (err = %v)", err)
						}
						return nil
					},
				},
			},
		})
	})

	t.Run("manages trusted domains and OAuth providers", func(t *testing.T) {
		projectName := newProjectName(prefix)
		config := func(githubClientID string) string {
			return project(projectName, fmt.Sprintf(`
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  delete_data_on_destroy = true
}

resource "neon_auth_trusted_domain" "app" {
  project_id = neon_auth.this.project_id
  branch_id  = neon_auth.this.branch_id
  domain     = "https://app.example.com"
}

resource "neon_auth_trusted_domain" "admin" {
  project_id = neon_auth.this.project_id
  branch_id  = neon_auth.this.branch_id
  domain     = "https://admin.example.com"
}

resource "neon_auth_oauth_provider" "google" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "google"
  client_id     = "tf-acc.apps.googleusercontent.com"
  client_secret = "tf-acc-google-secret"
}

resource "neon_auth_oauth_provider" "github" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "github"
  client_id     = %q
  client_secret = "tf-acc-github-secret"
}
`, githubClientID))
		}
		verify := func(wantGithubClientID string) resource.TestCheckFunc {
			return func(state *terraform.State) error {
				projectID, branchID := ids(state)
				domains, err := client.ListBranchNeonAuthTrustedDomains(projectID, branchID)
				if err != nil {
					return err
				}
				var got []string
				for _, d := range domains.Domains {
					got = append(got, d.Domain)
				}
				for _, want := range []string{"https://app.example.com", "https://admin.example.com"} {
					if !slices.Contains(got, want) {
						return fmt.Errorf("trusted domains %v lack %s", got, want)
					}
				}
				providers, err := client.ListBranchNeonAuthOauthProviders(projectID, branchID)
				if err != nil {
					return err
				}
				byID := map[string]neon.NeonAuthOauthProvider{}
				for _, p := range providers.Providers {
					byID[p.ID.String()] = p
				}
				if g := byID["google"]; g.Type.String() != "standard" || g.ClientID == nil || *g.ClientID != "tf-acc.apps.googleusercontent.com" {
					return fmt.Errorf("google is %+v, want the configured standard credentials", g)
				}
				if gh := byID["github"]; gh.ClientID == nil || *gh.ClientID != wantGithubClientID {
					return fmt.Errorf("github client_id is %v, want %s", gh.ClientID, wantGithubClientID)
				}
				return nil
			}
		}
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					// Neon creates google with shared development keys; the resource takes it over.
					Config: config("tf-acc-github-1"),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_auth_oauth_provider.google", "type", "standard"),
						verify("tf-acc-github-1"),
					),
				},
				{
					Config: config("tf-acc-github-2"),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("neon_auth_oauth_provider.github", plancheck.ResourceActionUpdate),
						},
					},
					Check: verify("tf-acc-github-2"),
				},
				{
					Config:                  config("tf-acc-github-2"),
					ResourceName:            "neon_auth_oauth_provider.github",
					ImportState:             true,
					ImportStateVerify:       true,
					ImportStateVerifyIgnore: []string{"client_secret"},
				},
				{
					Config:            config("tf-acc-github-2"),
					ResourceName:      "neon_auth_trusted_domain.app",
					ImportState:       true,
					ImportStateVerify: true,
				},
				{
					// Disabling Neon Auth drops its domains and providers; refresh has to notice all three.
					PreConfig:          disableOutOfBand(projectName),
					Config:             config("tf-acc-github-2"),
					PlanOnly:           true,
					ExpectNonEmptyPlan: true,
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PostApplyPostRefresh: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("neon_auth.this", plancheck.ResourceActionCreate),
							plancheck.ExpectResourceAction("neon_auth_trusted_domain.app", plancheck.ResourceActionCreate),
							plancheck.ExpectResourceAction("neon_auth_oauth_provider.github", plancheck.ResourceActionCreate),
						},
					},
				},
				{
					// Neon cannot enable Neon Auth again on this branch, so the configuration drops it.
					Config: project(projectName, ""),
				},
			},
		})
	})

	t.Run("keeps every trusted domain and OAuth provider that one apply creates", func(t *testing.T) {
		projectName := newProjectName(prefix)
		const domains = 8
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: project(projectName, fmt.Sprintf(`
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  delete_data_on_destroy = true
}

resource "neon_auth_trusted_domain" "app" {
  count      = %d
  project_id = neon_auth.this.project_id
  branch_id  = neon_auth.this.branch_id
  domain     = "https://app${count.index}.example.com"
}

resource "neon_auth_oauth_provider" "github" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "github"
  client_id     = "tf-acc-github"
  client_secret = "tf-acc-github-secret"
}

resource "neon_auth_oauth_provider" "vercel" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "vercel"
  client_id     = "tf-acc-vercel"
  client_secret = "tf-acc-vercel-secret"
}
`, domains)),
					Check: func(state *terraform.State) error {
						projectID, branchID := ids(state)
						listed, err := client.ListBranchNeonAuthTrustedDomains(projectID, branchID)
						if err != nil {
							return err
						}
						var got []string
						for _, d := range listed.Domains {
							got = append(got, d.Domain)
						}
						for i := range domains {
							if want := fmt.Sprintf("https://app%d.example.com", i); !slices.Contains(got, want) {
								return fmt.Errorf("trusted domains %v lack %s", got, want)
							}
						}
						providers, err := client.ListBranchNeonAuthOauthProviders(projectID, branchID)
						if err != nil {
							return err
						}
						var providerIDs []string
						for _, p := range providers.Providers {
							providerIDs = append(providerIDs, p.ID.String())
						}
						for _, want := range []string{"github", "vercel"} {
							if !slices.Contains(providerIDs, want) {
								return fmt.Errorf("OAuth providers %v lack %s", providerIDs, want)
							}
						}
						return nil
					},
				},
			},
		})
	})

	t.Run("adopts an imported OAuth provider and then plans no changes", func(t *testing.T) {
		projectName := newProjectName(prefix)
		base := project(projectName, `
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  delete_data_on_destroy = true
}
`)
		provider := `
resource "neon_auth_oauth_provider" "github" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "github"
  client_id     = "tf-acc-github"
  client_secret = "tf-acc-github-secret"
}
`
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: base,
					Check: func(state *terraform.State) error {
						projectID, branchID := ids(state)
						clientID, secret := "tf-acc-github", "tf-acc-github-secret"
						_, err := client.AddBranchNeonAuthOauthProvider(projectID, branchID, neon.NeonAuthAddOAuthProviderRequest{
							ID: neon.NeonAuthOauthProviderIdGithub, ClientID: &clientID, ClientSecret: &secret,
						})
						return err
					},
				},
				{
					// A provider that already has its own credentials is refused rather than silently adopted.
					Config:      base + provider,
					ExpectError: regexp.MustCompile("OAuth provider already configured"),
				},
				{
					Config:       base + provider,
					ResourceName: "neon_auth_oauth_provider.github",
					ImportState:  true,
					ImportStateIdFunc: func(state *terraform.State) (string, error) {
						projectID, branchID := ids(state)
						return projectID + "/" + branchID + "/github", nil
					},
					ImportStatePersist: true,
				},
				{
					// The import has no secret, so the first apply sets it in place.
					Config: base + provider,
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("neon_auth_oauth_provider.github", plancheck.ResourceActionUpdate),
						},
					},
				},
			},
		})
	})

	t.Run("imports the shared google provider and sets its credentials in place", func(t *testing.T) {
		projectName := newProjectName(prefix)
		base := project(projectName, `
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  delete_data_on_destroy = true
}
`)
		google := `
resource "neon_auth_oauth_provider" "google" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "google"
  client_id     = "tf-acc.apps.googleusercontent.com"
  client_secret = "tf-acc-google-secret"
}
`
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{Config: base},
				{
					Config:       base + google,
					ResourceName: "neon_auth_oauth_provider.google",
					ImportState:  true,
					ImportStateIdFunc: func(state *terraform.State) (string, error) {
						projectID, branchID := ids(state)
						return projectID + "/" + branchID + "/google", nil
					},
					ImportStatePersist: true,
					ImportStateCheck: func(states []*terraform.InstanceState) error {
						for _, st := range states {
							if strings.HasSuffix(st.ID, "/google") {
								if got := st.Attributes["type"]; got != "shared" {
									return fmt.Errorf("imported type = %q, want shared", got)
								}
								return nil
							}
						}
						return errors.New("the google provider was not imported")
					},
				},
				{
					Config: base + google,
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("neon_auth_oauth_provider.google", plancheck.ResourceActionUpdate),
						},
					},
					Check: resource.TestCheckResourceAttr("neon_auth_oauth_provider.google", "type", "standard"),
				},
			},
		})
	})

	t.Run("replaces a microsoft provider when its tenant is removed", func(t *testing.T) {
		projectName := newProjectName(prefix)
		config := func(tenant string) string {
			return project(projectName, fmt.Sprintf(`
resource "neon_auth" "this" {
  project_id             = neon_project.this.id
  branch_id              = neon_project.this.branch.id
  delete_data_on_destroy = true
}

resource "neon_auth_oauth_provider" "microsoft" {
  project_id    = neon_auth.this.project_id
  branch_id     = neon_auth.this.branch_id
  provider_id   = "microsoft"
  client_id     = "tf-acc-microsoft"
  client_secret = "tf-acc-microsoft-secret"
  %s
}
`, tenant))
		}
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: config(`microsoft_tenant_id = "contoso.onmicrosoft.com"`),
					Check: resource.TestCheckResourceAttr("neon_auth_oauth_provider.microsoft", "microsoft_tenant_id",
						"contoso.onmicrosoft.com"),
				},
				{
					Config: config(""),
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("neon_auth_oauth_provider.microsoft", plancheck.ResourceActionReplace),
						},
					},
					Check: resource.TestCheckNoResourceAttr("neon_auth_oauth_provider.microsoft", "microsoft_tenant_id"),
				},
			},
		})
	})
}

func TestNeonAuthOAuthProviderValidation(t *testing.T) {
	for _, tc := range []struct {
		name, attrs, want string
	}{
		{"apple without key and team", `provider_id = "apple"`, "Set apple_key_id"},
		{"tenant outside microsoft", `provider_id = "github"
  microsoft_tenant_id = "contoso"`, "only applies to microsoft"},
		{"apple ids outside apple", `provider_id = "google"
  apple_key_id = "K"
  apple_team_id = "T"`, "only applies to apple"},
		{"unknown provider", `provider_id = "gitlab"`, "value must be one of"},
		{"empty tenant", `provider_id = "microsoft"
  microsoft_tenant_id = ""`, "string length must be at least 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: newProviderFactories(),
				Steps: []resource.TestStep{{
					Config: fmt.Sprintf(`resource "neon_auth_oauth_provider" "this" {
  project_id    = "project-id"
  branch_id     = "br-id"
  client_id     = "id"
  client_secret = "secret"
  %s
}`, tc.attrs),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
}
