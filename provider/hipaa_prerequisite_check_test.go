package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	neon "github.com/kislerdm/neon-sdk-go"
)

type hipaaPrerequisiteReporter interface {
	Errorf(string, ...any)
	Skipf(string, ...any)
}

func hipaaPrerequisiteCheck(t hipaaPrerequisiteReporter, client *neon.Client) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		r, ok := state.RootModule().Resources["neon_project.this"]
		if !ok || r.Primary == nil || r.Primary.ID == "" {
			return fmt.Errorf("HIPAA prerequisite requires neon_project.this with a project ID")
		}
		flag := false
		p, err := client.UpdateProject(r.Primary.ID, neon.ProjectUpdateRequest{
			Project: neon.ProjectUpdateRequestProject{
				Settings: &neon.ProjectSettingsData{Hipaa: &flag},
			},
		})
		if err != nil {
			if hipaaEntitlementDenied(err) {
				orgID := r.Primary.Attributes["org_id"]
				if orgID == "" {
					orgID = "unknown"
				}
				t.Skipf("org %s HIPAA compliance is not supported", orgID)
				return nil
			}
			return fmt.Errorf("check HIPAA prerequisite for project %s: %w", r.Primary.ID, err)
		}
		waitUnfinishedOperations(context.Background(), client, p.OperationsResponse.Operations)
		return nil
	}
}

// A permission failure or a transient HIPAA error must still fail the test.
func hipaaEntitlementDenied(err error) bool {
	var apiErr neon.Error
	var apiErrPointer *neon.Error
	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &apiErrPointer) && apiErrPointer != nil:
		apiErr = *apiErrPointer
	default:
		return false
	}
	if apiErr.HTTPCode != http.StatusBadRequest && apiErr.HTTPCode != http.StatusForbidden {
		return false
	}
	for _, detail := range apiErr.Details {
		if strings.Contains(strings.ToLower(detail.Message), "organization cannot contain hipaa") {
			return true
		}
	}
	return false
}
