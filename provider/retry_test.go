package provider

import (
	"context"
	"net/http"
	"testing"

	neon "github.com/kislerdm/neon-sdk-go"
)

func TestRetryCreateDoesNotRepeatServerErrors(t *testing.T) {
	for _, code := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		calls := 0
		diags := retryCreate(context.Background(), func(context.Context) error {
			calls++
			return neon.Error{HTTPCode: code}
		})
		if calls != 1 {
			t.Errorf("HTTP %d: create sent %d times, want 1", code, calls)
		}
		if !diags.HasError() || diags[len(diags)-1].Summary() != "Neon may have created the resource" {
			t.Errorf("HTTP %d: diagnostics do not warn about a possibly created resource: %v", code, diags)
		}
	}
}

func TestRetryCreateRepeatsRefusedRequests(t *testing.T) {
	calls := 0
	diags := retryCreate(context.Background(), func(context.Context) error {
		calls++
		if calls == 1 {
			return neon.Error{HTTPCode: http.StatusLocked}
		}
		return nil
	})
	if calls != 2 || diags.HasError() {
		t.Errorf("a locked project must be retried: calls=%d diags=%v", calls, diags)
	}
}
