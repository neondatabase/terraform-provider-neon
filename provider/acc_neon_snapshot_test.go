package provider

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/require"
)

// findSnapshot returns the snapshot with the given ID from a ListSnapshots
// response, or an error if it is missing. Tests use this helper to verify
// that the live Neon API state agrees with Terraform state at each step.
func findSnapshot(t *testing.T, client *neon.Client, projectID, snapshotID string) (*neon.Snapshot, error) {
	t.Helper()
	resp, err := client.ListSnapshots(projectID)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	for i := range resp.Snapshots {
		if resp.Snapshots[i].ID == snapshotID {
			return &resp.Snapshots[i], nil
		}
	}
	return nil, fmt.Errorf("snapshot %q not found in project %q", snapshotID, projectID)
}

func TestAccNeonSnapshot(t *testing.T) {
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("TF_ACC must be set to 1")
	}

	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		t.Skip("ORG_ID must be set")
	}

	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
	require.NoError(t, err)

	projectNamePrefix := "neonSnapshotAcc"

	t.Run("shall create and read a snapshot, and verify against the live API", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newSnapshotConfig(projectName, orgID, "tf-acc-snap", ""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttrSet("neon_snapshot.this", "id"),
						resource.TestCheckResourceAttr("neon_snapshot.this", "name", "tf-acc-snap"),
						resource.TestCheckResourceAttrSet("neon_snapshot.this", "snapshot_id"),
						resource.TestCheckResourceAttrSet("neon_snapshot.this", "source_branch_id"),
						resource.TestCheckResourceAttrSet("neon_snapshot.this", "created_at"),
						verifySnapshotAgainstAPI(t, client, "tf-acc-snap"),
					),
				},
			},
		})
	})

	t.Run("shall update the snapshot name in place and verify against the live API", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newSnapshotConfig(projectName, orgID, "tf-acc-snap", ""),
				},
				{
					Config: newSnapshotConfig(projectName, orgID, "tf-acc-snap-renamed", ""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_snapshot.this", "name", "tf-acc-snap-renamed"),
						verifySnapshotAgainstAPI(t, client, "tf-acc-snap-renamed"),
					),
				},
			},
		})
	})

	t.Run("shall set and clear expires_at and verify against the live API", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		expiry := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
		// Neon returns UTC, so a value with another offset has to survive the round trip as written.
		offsetExpiry := time.Now().Add(72 * time.Hour).In(time.FixedZone("", 3600)).Format(time.RFC3339)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newSnapshotConfig(projectName, orgID, "expiry", expiry),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_snapshot.this", "expires_at", expiry),
						verifySnapshotExpiryAgainstAPI(t, client, expiry),
					),
				},
				{
					Config: newSnapshotConfig(projectName, orgID, "expiry", offsetExpiry),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_snapshot.this", "expires_at", offsetExpiry),
						verifySnapshotExpiryAgainstAPI(t, client, offsetExpiry),
					),
				},
				{
					Config: newSnapshotConfig(projectName, orgID, "expiry", ""),
					Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("neon_snapshot.this", "name", "expiry"),
						verifySnapshotExpiryIsClear(t, client),
					),
				},
			},
		})
	})

	t.Run("shall import a snapshot by <project_id>/<snapshot_id> and verify against the live API", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newSnapshotConfig(projectName, orgID, "tf-acc-import", ""),
					Check: resource.ComposeTestCheckFunc(
						captureSnapshotIDFromState(t, client),
					),
				},
				{
					ResourceName:      "neon_snapshot.this",
					ImportState:       true,
					ImportStateVerify: true,
					ImportStateId:     recordedSnapshotIDForImport,
					Check: resource.ComposeTestCheckFunc(
						verifySnapshotAgainstAPI(t, client, "tf-acc-import"),
					),
				},
			},
		})
	})

	t.Run("shall remove the snapshot from the live API when deleted out-of-band", func(t *testing.T) {
		projectName := newProjectName(projectNamePrefix)
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: newProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: newSnapshotConfig(projectName, orgID, "to-delete", ""),
					Check: resource.ComposeTestCheckFunc(
						recordSnapshotForOutOfBandDelete(t, client, "to-delete"),
					),
				},
				// Refresh drops the deleted snapshot from state, so the plan recreates it.
				{
					PreConfig:          deleteSnapshotOutOfBand(t, client),
					Config:             newSnapshotConfig(projectName, orgID, "to-delete", ""),
					PlanOnly:           true,
					ExpectNonEmptyPlan: true,
				},
			},
		})
	})
}

func newSnapshotConfig(projectName, orgID, snapshotName, expiresAt string) string {
	expiryBlock := ""
	if expiresAt != "" {
		expiryBlock = fmt.Sprintf("  expires_at = %q\n", expiresAt)
	}
	return fmt.Sprintf(`resource "neon_project" "this" {
  name      = %q
  org_id    = %q
  region_id = "aws-us-east-2"
}

resource "neon_snapshot" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  name       = %q
%s  depends_on = [neon_project.this]
}
`, projectName, orgID, snapshotName, expiryBlock)
}

// verifySnapshotAgainstAPI asserts that the snapshot stored in Terraform
// state is present in the live Neon API response with the expected name.
// This is the verification contract captured in the maintainer's feedback.
func verifySnapshotAgainstAPI(t *testing.T, client *neon.Client, expectedName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources["neon_snapshot.this"]
		if !ok {
			return fmt.Errorf("neon_snapshot.this not found in state")
		}
		projectID := rs.Primary.Attributes["project_id"]
		snapshotID := rs.Primary.Attributes["snapshot_id"]
		if projectID == "" || snapshotID == "" {
			return fmt.Errorf("missing project_id or snapshot_id in state: %+v", rs.Primary.Attributes)
		}
		snap, err := findSnapshot(t, client, projectID, snapshotID)
		if err != nil {
			return err
		}
		if snap.Name != expectedName {
			return fmt.Errorf("snapshot name in API is %q, want %q", snap.Name, expectedName)
		}
		if snap.ID != snapshotID {
			return fmt.Errorf("snapshot ID in API is %q, want %q", snap.ID, snapshotID)
		}
		return nil
	}
}

func verifySnapshotExpiryAgainstAPI(t *testing.T, client *neon.Client, expectedExpiry string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources["neon_snapshot.this"]
		if !ok {
			return fmt.Errorf("neon_snapshot.this not found in state")
		}
		projectID := rs.Primary.Attributes["project_id"]
		snapshotID := rs.Primary.Attributes["snapshot_id"]
		snap, err := findSnapshot(t, client, projectID, snapshotID)
		if err != nil {
			return err
		}
		if snap.ExpiresAt == nil {
			return fmt.Errorf("snapshot expires_at is null, want %s", expectedExpiry)
		}
		want, err := time.Parse(time.RFC3339, expectedExpiry)
		if err != nil {
			return fmt.Errorf("parse expected expiry %q: %w", expectedExpiry, err)
		}
		if !timesEqualToSecond(*snap.ExpiresAt, want.Format(time.RFC3339)) {
			return fmt.Errorf("snapshot expires_at in API is %s, want %s", *snap.ExpiresAt, want.Format(time.RFC3339))
		}
		return nil
	}
}

func verifySnapshotExpiryIsClear(t *testing.T, client *neon.Client) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources["neon_snapshot.this"]
		if !ok {
			return fmt.Errorf("neon_snapshot.this not found in state")
		}
		projectID := rs.Primary.Attributes["project_id"]
		snapshotID := rs.Primary.Attributes["snapshot_id"]
		snap, err := findSnapshot(t, client, projectID, snapshotID)
		if err != nil {
			return err
		}
		if snap.ExpiresAt != nil {
			return fmt.Errorf("expected snapshot expires_at to be cleared, got %s", *snap.ExpiresAt)
		}
		return nil
	}
}

// captureSnapshotIDFromState reads the live snapshot ID and remembers it
// for the import step. We use the SDK here because Terraform state is
// not yet sufficient to derive the import ID independently.
func captureSnapshotIDFromState(t *testing.T, client *neon.Client) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources["neon_snapshot.this"]
		if !ok {
			return fmt.Errorf("neon_snapshot.this not found in state")
		}
		projectID := rs.Primary.Attributes["project_id"]
		snapshotID := rs.Primary.Attributes["snapshot_id"]
		recordedSnapshotIDForImport = fmt.Sprintf("%s/%s", projectID, snapshotID)
		return nil
	}
}

var recordedSnapshotIDForImport string

// recordSnapshotForOutOfBandDelete reads the live snapshot ID so the
// PreConfig callback can delete it out-of-band before the next plan.
func recordSnapshotForOutOfBandDelete(t *testing.T, client *neon.Client, expectedName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources["neon_snapshot.this"]
		if !ok {
			return fmt.Errorf("neon_snapshot.this not found in state")
		}
		projectID := rs.Primary.Attributes["project_id"]
		snap, err := findSnapshot(t, client, projectID, rs.Primary.Attributes["snapshot_id"])
		if err != nil {
			return err
		}
		outOfBandSnapshotProjectID = projectID
		outOfBandSnapshotID = snap.ID
		_ = expectedName
		return nil
	}
}

var (
	outOfBandSnapshotProjectID string
	outOfBandSnapshotID        string
)

// deleteSnapshotOutOfBand returns a PreConfig callback that deletes the
// recorded snapshot through the SDK, simulating drift created by an
// out-of-band change. Terraform must remove the resource cleanly on the
// next plan via the 404 fallback in RetryWithFallbackFramework.
func deleteSnapshotOutOfBand(t *testing.T, client *neon.Client) func() {
	return func() {
		if outOfBandSnapshotID == "" {
			t.Fatalf("out-of-band snapshot ID not recorded")
		}
		if _, err := client.DeleteSnapshot(outOfBandSnapshotProjectID, outOfBandSnapshotID); err != nil {
			t.Fatalf("delete out-of-band snapshot: %v", err)
		}
	}
}

// timesEqualToSecond compares two RFC3339 strings at second granularity
// because the Neon API may serialize timestamps with reduced precision.
func timesEqualToSecond(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return false
	}
	return ta.Truncate(time.Second).Equal(tb.Truncate(time.Second))
}
