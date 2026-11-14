//go:build acceptance

package provider

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

func TestAccNeonDataAPI(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}
	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set to an organization with Data API access")
	}
	if os.Getenv("NEON_API_KEY") == "" {
		t.Skip("NEON_API_KEY must be set")
	}
	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	require.NoError(t, err)

	t.Run("create and read", func(t *testing.T) {
		projectName := dataAPIAccProject(t, client, orgID)
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: dataAPIAccCheckDestroy(client), Steps: []resource.TestStep{
			{Config: newDataAPIConfig(projectName, orgID, "neondb", ""), Check: dataAPIAccCheck(client, nil)},
		}})
	})

	t.Run("update and forced replacement", func(t *testing.T) {
		projectName := dataAPIAccProject(t, client, orgID)
		var identity dataAPIAccIdentity
		check := dataAPIAccCheck(client, &identity)
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: dataAPIAccCheckDestroy(client), Steps: []resource.TestStep{
			{Config: newDataAPIConfig(projectName, orgID, "neondb", `settings = { db_max_rows = 10, db_aggregates_enabled = true }`), Check: check},
			{Config: newDataAPIConfig(projectName, orgID, "neondb", `settings = { db_max_rows = 20, db_aggregates_enabled = false }`), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("neon_data_api.this", "settings.db_max_rows", "20"), resource.TestCheckResourceAttr("neon_data_api.this", "settings.db_aggregates_enabled", "false"), check)},
			{Config: newDataAPIConfig(projectName, orgID, "neondb", `skip_auth_schema = true`), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("neon_data_api.this", "skip_auth_schema", "true"), check)},
			{Config: newDataAPIConfig(projectName, orgID, "replacementdb", `skip_auth_schema = true`), Check: resource.ComposeTestCheckFunc(resource.TestCheckResourceAttr("neon_data_api.this", "database_name", "replacementdb"), check)},
		}})
	})

	t.Run("import", func(t *testing.T) {
		projectName := dataAPIAccProject(t, client, orgID)
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: dataAPIAccCheckDestroy(client), Steps: []resource.TestStep{
			{Config: newDataAPIConfig(projectName, orgID, "neondb", `skip_auth_schema = true
 settings = { db_max_rows = 10 }`), Check: dataAPIAccCheck(client, nil)},
			{ResourceName: "neon_data_api.this", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"skip_auth_schema", "settings"}, ImportStateCheck: func(states []*terraform.InstanceState) error {
				if len(states) != 1 {
					return fmt.Errorf("expected one imported endpoint, got %d", len(states))
				}
				attributes := states[0].Attributes
				if attributes["skip_auth_schema"] != "" || attributes["settings.db_max_rows"] != "" {
					return fmt.Errorf("import must leave creation-only options and settings unmanaged")
				}
				return dataAPIAccVerifyAttributes(client, attributes)
			}},
		}})
	})

	t.Run("out-of-band deletion and recreation", func(t *testing.T) {
		projectName := dataAPIAccProject(t, client, orgID)
		var identity dataAPIAccIdentity
		config := newDataAPIConfig(projectName, orgID, "neondb", "")
		remove := func() {
			_, err := client.DeleteProjectBranchDataAPI(identity.projectID, identity.branchID, identity.databaseName)
			require.NoError(t, err)
		}
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: dataAPIAccCheckDestroy(client), Steps: []resource.TestStep{
			{Config: config, Check: dataAPIAccCheck(client, &identity)},
			{PreConfig: remove, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(state *terraform.State) error {
				if _, ok := state.RootModule().Resources["neon_data_api.this"]; ok {
					return fmt.Errorf("externally deleted endpoint remains in state")
				}
				return dataAPIAccAbsent(client, identity)
			}},
			{Config: config, Check: dataAPIAccCheck(client, &identity)},
			{PreConfig: remove, Config: config, Destroy: true},
		}})
	})

	t.Run("invalid authentication and failed-apply cleanup", func(t *testing.T) {
		projectName := dataAPIAccProject(t, client, orgID)
		resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), CheckDestroy: dataAPIAccCheckDestroy(client), Steps: []resource.TestStep{
			{Config: newDataAPIConfig(projectName, orgID, "neondb", `auth_provider = "external"`), ExpectError: regexp.MustCompile(`\[HTTP Code: (400|422)\]`)},
			{Config: newDataAPIConfig(projectName, orgID, "neondb", ""), Check: dataAPIAccCheck(client, nil)},
		}})
	})
}

type dataAPIAccIdentity struct {
	projectID    string
	branchID     string
	databaseName string
}

func newDataAPIConfig(projectName, orgID, databaseName, options string) string {
	return fmt.Sprintf(`resource "neon_project" "this" {
 name = %q
 org_id = %q
 region_id = "aws-us-east-2"
}
resource "neon_database" "replacement" {
 project_id = neon_project.this.id
 branch_id = neon_project.this.branch.id
 owner_name = neon_project.this.database_user
 name = "replacementdb"
}
resource "neon_data_api" "this" {
 project_id = neon_project.this.id
 branch_id = neon_project.this.branch.id
 database_name = %q
 depends_on = [neon_database.replacement]
 %s
}
`, projectName, orgID, databaseName, options)
}

func dataAPIAccProject(t *testing.T, client *neon.Client, orgID string) string {
	t.Helper()
	name := newProjectName("neonDataAPIAcc")
	// Failed applies can leave a project before any endpoint identity reaches state.
	t.Cleanup(func() {
		response, err := client.ListProjects(nil, nil, &name, &orgID, nil, nil)
		if err != nil {
			t.Errorf("list cleanup project %q: %v", name, err)
			return
		}
		for _, project := range response.Projects {
			if project.Name != name {
				continue
			}
			if _, err := client.DeleteProject(project.ID); err != nil {
				var apiError neon.Error
				if !errors.As(err, &apiError) || apiError.HTTPCode != http.StatusNotFound {
					t.Errorf("delete cleanup project %q: %v", project.ID, err)
				}
			}
		}
	})
	return name
}

func dataAPIAccCheck(client *neon.Client, identity *dataAPIAccIdentity) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		endpoint, ok := state.RootModule().Resources["neon_data_api.this"]
		if !ok {
			return fmt.Errorf("neon_data_api.this missing from state")
		}
		attributes := endpoint.Primary.Attributes
		if identity != nil {
			*identity = dataAPIAccIdentity{projectID: attributes["project_id"], branchID: attributes["branch_id"], databaseName: attributes["database_name"]}
		}
		return dataAPIAccVerifyAttributes(client, attributes)
	}
}

func dataAPIAccVerifyAttributes(client *neon.Client, attributes map[string]string) error {
	identity := dataAPIAccIdentity{projectID: attributes["project_id"], branchID: attributes["branch_id"], databaseName: attributes["database_name"]}
	wantID := fmt.Sprintf("%s/%s/%s", identity.projectID, identity.branchID, identity.databaseName)
	if attributes["id"] != wantID {
		return fmt.Errorf("Data API identity %q does not match %q", attributes["id"], wantID)
	}
	remote, err := client.GetProjectBranchDataAPI(identity.projectID, identity.branchID, identity.databaseName)
	if err != nil {
		return fmt.Errorf("independent Data API read: %w", err)
	}
	if remote.URL == "" || remote.URL != attributes["url"] {
		return fmt.Errorf("Data API URL differs between Neon and Terraform")
	}
	if expected, ok := attributes["settings.db_max_rows"]; ok && expected != "" {
		if remote.Settings == nil || remote.Settings.DbMaxRows == nil {
			return fmt.Errorf("Data API backend must return configured db_max_rows for this test")
		}
		if strconv.Itoa(*remote.Settings.DbMaxRows) != expected {
			return fmt.Errorf("db_max_rows differs between Neon and Terraform")
		}
	}
	if expected, ok := attributes["settings.db_aggregates_enabled"]; ok && expected != "" {
		if remote.Settings == nil || remote.Settings.DbAggregatesEnabled == nil {
			return fmt.Errorf("Data API backend must return configured db_aggregates_enabled for this test")
		}
		if strconv.FormatBool(*remote.Settings.DbAggregatesEnabled) != expected {
			return fmt.Errorf("db_aggregates_enabled differs between Neon and Terraform")
		}
	}
	return nil
}

func dataAPIAccAbsent(client *neon.Client, identity dataAPIAccIdentity) error {
	_, err := client.GetProjectBranchDataAPI(identity.projectID, identity.branchID, identity.databaseName)
	var apiError neon.Error
	if errors.As(err, &apiError) && apiError.HTTPCode == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("confirm Data API absence: %w", err)
	}
	return fmt.Errorf("Data API endpoint still exists after deletion")
}

func dataAPIAccCheckDestroy(client *neon.Client) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		for _, endpoint := range state.RootModule().Resources {
			if endpoint.Type != "neon_data_api" {
				continue
			}
			attributes := endpoint.Primary.Attributes
			if err := dataAPIAccAbsent(client, dataAPIAccIdentity{projectID: attributes["project_id"], branchID: attributes["branch_id"], databaseName: attributes["database_name"]}); err != nil {
				return err
			}
		}
		return nil
	}
}
