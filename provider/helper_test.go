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
