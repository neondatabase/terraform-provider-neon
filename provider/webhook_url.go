package provider

import (
	"context"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type webhookURLValidator struct{}

func (webhookURLValidator) Description(context.Context) string {
	return "Use an HTTPS URL with a domain hostname."
}
func (v webhookURLValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func numericIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) > 4 {
		return false
	}
	for i, p := range parts {
		bits := 8
		if i == len(parts)-1 {
			bits = 8 * (5 - len(parts))
		}
		base := 10
		if strings.HasPrefix(p, "0x") {
			base = 16
			p = p[2:]
		} else if strings.HasPrefix(p, "0") {
			base = 8
		}
		if _, err := strconv.ParseUint(p, base, bits); err != nil {
			return false
		}
	}
	return true
}

func (v webhookURLValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsUnknown() || req.ConfigValue.IsNull() {
		return
	}
	raw := req.ConfigValue.ValueString()
	scheme, _, ok := strings.Cut(raw, "://")
	if ok && len(scheme) == len("https") && strings.EqualFold(scheme, "https") {
		u, err := url.Parse(raw)
		if err != nil {
			return
		} // Defer parser-specific syntax to Neon.
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		_, ipErr := netip.ParseAddr(host)
		if host != "" && host != "localhost" && ipErr != nil && !numericIPv4(host) {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid webhook URL", v.Description(ctx))
}
