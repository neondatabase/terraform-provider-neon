package provider

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

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

// lockRetryingHTTPClient retries requests that Neon answers with 423 Locked while an operation from an earlier test
// step is still running. Tests make their out-of-band changes through it.
type lockRetryingHTTPClient struct{}

func (lockRetryingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusLocked || attempt == 120 {
			return resp, err
		}
		_ = resp.Body.Close()
		time.Sleep(time.Second)
		if req.GetBody != nil {
			if req.Body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
	}
}
