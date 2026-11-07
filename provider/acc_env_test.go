package provider

import (
	"net/http"
	"os"
	"testing"
	"time"
)

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
