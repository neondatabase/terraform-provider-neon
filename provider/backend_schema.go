package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var functionSlugPattern = regexp.MustCompile(`^[a-z0-9]{1,20}$`)

type backendStringValidator struct{ kind string }

func (v backendStringValidator) Description(context.Context) string { return "validates " + v.kind }
func (v backendStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v backendStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	valid := s != "" && strings.TrimSpace(s) == s
	switch v.kind {
	case "identifier":
		valid = valid && !strings.Contains(s, "/")
	case "slug":
		valid = functionSlugPattern.MatchString(s)
	case "runtime":
		valid = s == "nodejs24"
	case "entity_type":
		valid = s == "function"
	case "domain":
		valid = valid && len(s) <= 253 && strings.Contains(s, ".") && s == strings.ToLower(s)
		for _, label := range strings.Split(s, ".") {
			valid = valid && len(label) <= 63 && slugPattern.MatchString(label)
		}
	case "deployment_timeout":
		d, err := time.ParseDuration(s)
		valid = err == nil && d >= time.Second && d <= time.Hour
	}
	if !valid {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+v.kind, fmt.Sprintf("Invalid %s value %q.", v.kind, s))
	}
}

func backendIdentityAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{Required: true, Description: description,
		Validators:    []validator.String{backendStringValidator{"identifier"}},
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
}

func configureBackend(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *neonClient {
	if req.ProviderData == nil {
		return nil
	}
	client, ok := req.ProviderData.(*neonClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *neonClient.")
		return nil
	}
	return client
}
