package provider

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/hashicorp/go-version"
)

// skipBelowTerraform skips t when the Terraform CLI that runs acceptance tests
// is older than minVersion, e.g. for ephemeral resources, which need 1.10.
func skipBelowTerraform(t *testing.T, minVersion string) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		return
	}
	bin := os.Getenv("TF_ACC_TERRAFORM_PATH")
	if bin == "" {
		bin = "terraform"
	}
	out, err := exec.Command(bin, "version", "-json").Output()
	if err != nil {
		t.Fatalf("read the Terraform CLI version with %s version -json: %v", bin, err)
	}
	var v struct {
		TerraformVersion string `json:"terraform_version"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("parse %s version -json output: %v", bin, err)
	}
	if version.Must(version.NewVersion(v.TerraformVersion)).LessThan(version.Must(version.NewVersion(minVersion))) {
		t.Skipf("requires Terraform %s or later, found %s", minVersion, v.TerraformVersion)
	}
}

// usePersonalAPIKey points t at NEON_PERSONAL_API_KEY when it is set. Neon refuses organization API keys for
// managing API keys, members, and project permissions; every other test runs with an organization key.
func usePersonalAPIKey(t *testing.T) {
	t.Helper()
	if key := os.Getenv("NEON_PERSONAL_API_KEY"); key != "" {
		t.Setenv("NEON_API_KEY", key)
	}
}
