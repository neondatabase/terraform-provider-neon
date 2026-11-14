package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	neon "github.com/kislerdm/neon-sdk-go"
)

func TestPersonalAPIKeyRequiredIsNotNotFound(t *testing.T) {
	personalKeyRequired := neon.Error{HTTPCode: http.StatusNotFound, Details: []neon.ErrorDetails{{
		Message: "This endpoint requires a personal API key. Organization API keys are not supported.",
	}}}
	notFound := neon.Error{HTTPCode: http.StatusNotFound, Details: []neon.ErrorDetails{{
		Code: "NOT_FOUND", Message: "project not found",
	}}}

	if isNotFoundError(personalKeyRequired) {
		t.Error("a refused organization API key must not read as a deleted resource")
	}
	if !isNotFoundError(notFound) {
		t.Error("a plain 404 must read as a deleted resource")
	}

	fallbackRan := false
	err := projectReadiness.Do(context.Background(), func(context.Context) error { return personalKeyRequired },
		map[int]func(context.Context) error{http.StatusNotFound: func(context.Context) error {
			fallbackRan = true
			return nil
		}})
	if fallbackRan || err == nil {
		t.Errorf("the 404 fallback must not run for a refused organization API key, got err=%v", err)
	}

	diags := newDiagnostics(personalKeyRequired)
	if got := diags[0].Summary(); got != "Neon API key not allowed" {
		t.Errorf("summary = %q", got)
	}
	if !strings.Contains(diags[0].Detail(), "personal API key") {
		t.Errorf("detail lacks the fix: %q", diags[0].Detail())
	}
}

func TestDiagnosticsNameTheMissingDependency(t *testing.T) {
	noEndpoint := neon.Error{HTTPCode: http.StatusNotFound, Details: []neon.ErrorDetails{{
		Message: "no read-write endpoint for branch",
	}}}
	if got := newDiagnostics(noEndpoint)[0]; got.Summary() != "Branch has no read-write compute" ||
		!strings.Contains(got.Detail(), "depends_on = [neon_endpoint.<name>]") {
		t.Errorf("got %q: %q", got.Summary(), got.Detail())
	}

	deleteWithoutEndpoint := neon.Error{HTTPCode: http.StatusPreconditionFailed, Details: []neon.ErrorDetails{{
		Message: "could not apply config without read-write endpoint",
	}}}
	if got := newDiagnostics(deleteWithoutEndpoint)[0].Summary(); got != "Branch has no read-write compute" {
		t.Errorf("summary = %q", got)
	}

	roleNotFound := neon.Error{HTTPCode: http.StatusNotFound, Details: []neon.ErrorDetails{{
		Message: `role not found for branch; role_name:"app", branch_id:"br-example-123"`,
	}}}
	if got := newEphemeralRoleDiagnostics(roleNotFound)[0]; got.Summary() != "Neon role not found" ||
		!strings.Contains(got.Detail(), "terraform apply -target=<address>") {
		t.Errorf("got %q: %q", got.Summary(), got.Detail())
	}

	projectNotFound := neon.Error{HTTPCode: http.StatusNotFound, Details: []neon.ErrorDetails{{Message: "project not found"}}}
	if got := newEphemeralRoleDiagnostics(projectNotFound)[0].Summary(); got != "Neon resource not found" {
		t.Errorf("summary = %q", got)
	}
}

func TestWithoutRequestURLDropsPresignedQuery(t *testing.T) {
	const signature = "X-Amz-Signature=secret"
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:1/bucket/key?"+signature, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = http.DefaultClient.Do(req)
	if err == nil || !strings.Contains(err.Error(), signature) {
		t.Fatalf("expected a transport error carrying the URL, got %v", err)
	}
	if got := withoutRequestURL(err).Error(); strings.Contains(got, signature) {
		t.Errorf("error still carries the presigned URL: %s", got)
	}
}
