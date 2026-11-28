package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func webhookURLConfig(expression string) string {
	return fmt.Sprintf(`provider "neon" { api_key = "unused" }
resource "terraform_data" "url" { input = "https://example.com" }
resource "neon_auth" "this" {
 project_id = "cool-moon-42"
 branch_id = "br-cool-moon-42"
 webhook = { url = %s }
}`, expression)
}

func TestWebhookGuideContract(t *testing.T) {
	b, err := os.ReadFile("testdata/webhook-url.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			URL    string
			Reject bool
		}
	}
	if err = json.Unmarshal(b, &fixture); err != nil || len(fixture.Cases) == 0 {
		t.Fatalf("invalid fixture: %v", err)
	}
	for _, c := range fixture.Cases {
		var resp validator.StringResponse
		attr := path.Root("webhook").AtName("url")
		webhookURLValidator{}.ValidateString(context.Background(), validator.StringRequest{Path: attr, ConfigValue: types.StringValue(c.URL)}, &resp)
		if resp.Diagnostics.HasError() != c.Reject {
			t.Errorf("unexpected validation for %q", c.URL)
			continue
		}
		if c.Reject {
			d, ok := resp.Diagnostics[0].(diag.DiagnosticWithPath)
			if !ok || !d.Path().Equal(attr) || d.Detail() != "Use an HTTPS URL with a domain hostname." {
				t.Errorf("unexpected diagnostic: %v", resp.Diagnostics)
			}
		}
	}
}

func TestNeonAuthWebhookURL(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		invalid          bool
	}{
		{"non-ASCII scheme", `"httpſ://hooks.example.com/events"`, true},
		{"http", `"http://hooks.example.com/events"`, true},
		{"missing host", `"https:///events"`, true},
		{"IPv4", `"https://192.0.2.1/events"`, true},
		{"IPv6", `"https://[2001:db8::1]/events"`, true},
		{"empty label", `"https://hooks..example.com/events"`, false},
		{"leading hyphen", `"https://-hooks.example.com/events"`, false},
		{"underscore", `"https://bad_host.example.com/events"`, false},
		{"decimal IP", `"https://2130706433/events"`, true},
		{"octal IP", `"https://0177.0.0.1/events"`, true},
		{"hex IP", `"https://0x7f000001/events"`, true},
		{"short IP", `"https://127.1/events"`, true},
		{"interior hyphens", `"https://ab--preview.example/events"`, false},
		{"mixed direction", `"https://aא.example/events"`, false},
		{"malformed punycode", `"https://xn--.example/events"`, false},
		{"unicode hostname", `"https://bücher.example/events"`, false},
		{"root dot", `"https://hooks.example.com./events"`, false},
		{"encoded domain", `"https://%65xample.com/events"`, false},
		{"domain with hex suffix", `"https://hooks.0xbeef/events"`, false},
		{"localhost", `"https://localhost/events"`, true},
		{"https", `"HTTPS://hooks.example.com:8443/events"`, false},
		{"null", `null`, false},
		{"unknown", `terraform_data.url.output`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			step := resource.TestStep{Config: webhookURLConfig(tc.expression), PlanOnly: true, ExpectNonEmptyPlan: !tc.invalid}
			if tc.invalid {
				step.ExpectError = regexp.MustCompile(`Invalid webhook URL`)
			}
			resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: newProviderFactories(), Steps: []resource.TestStep{step}})
		})
	}
}
