package provider

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// legacyStateUpgrader upgrades state written by the last Terraform Plugin SDK v2 release of the provider
// (kislerdm/neon v0.18.0) to the current schema. The rewrite edits the raw JSON attributes in place; attributes
// the current schema no longer defines are dropped.
func legacyStateUpgrader(
	schemaFn func(context.Context, resource.SchemaRequest, *resource.SchemaResponse),
	rewrite func(attrs map[string]any),
) resource.StateUpgrader {
	return resource.StateUpgrader{
		StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
			if req.RawState == nil || len(req.RawState.JSON) == 0 {
				resp.Diagnostics.AddError("Unable to Upgrade Resource State", "The prior state is not in JSON format.")
				return
			}

			dec := json.NewDecoder(bytes.NewReader(req.RawState.JSON))
			dec.UseNumber()
			var attrs map[string]any
			if err := dec.Decode(&attrs); err != nil {
				resp.Diagnostics.AddError("Unable to Upgrade Resource State", err.Error())
				return
			}

			if rewrite != nil {
				rewrite(attrs)
			}

			raw, err := json.Marshal(attrs)
			if err != nil {
				resp.Diagnostics.AddError("Unable to Upgrade Resource State", err.Error())
				return
			}

			var schemaResp resource.SchemaResponse
			schemaFn(ctx, resource.SchemaRequest{}, &schemaResp)
			typ := schemaResp.Schema.Type().TerraformType(ctx)

			val, err := (tfprotov6.RawState{JSON: raw}).UnmarshalWithOpts(typ, tfprotov6.UnmarshalOpts{
				ValueFromJSONOpts: tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true},
			})
			if err != nil {
				resp.Diagnostics.AddError("Unable to Upgrade Resource State", err.Error())
				return
			}

			dv, err := tfprotov6.NewDynamicValue(typ, val)
			if err != nil {
				resp.Diagnostics.AddError("Unable to Upgrade Resource State", err.Error())
				return
			}
			resp.DynamicValue = &dv
		},
	}
}

// unwrapLegacyBlocks converts SDK v2 single-element list blocks into nested objects.
func unwrapLegacyBlocks(attrs map[string]any, keys ...string) {
	for _, k := range keys {
		l, ok := attrs[k].([]any)
		if !ok {
			continue
		}
		if len(l) == 0 {
			attrs[k] = nil
			continue
		}
		attrs[k] = l[0]
	}
}

// legacyYesNoToBool converts SDK v2 "yes"/"no" string flags into booleans; an empty string means unset.
func legacyYesNoToBool(attrs map[string]any, keys ...string) {
	for _, k := range keys {
		s, ok := attrs[k].(string)
		if !ok {
			continue
		}
		switch s {
		case "yes":
			attrs[k] = true
		case "no":
			attrs[k] = false
		case "":
			attrs[k] = nil
		}
	}
}

// legacyEmptyStringToNull converts the empty strings SDK v2 stored for unset optional attributes into null.
func legacyEmptyStringToNull(attrs map[string]any, keys ...string) {
	for _, k := range keys {
		if s, ok := attrs[k].(string); ok && s == "" {
			attrs[k] = nil
		}
	}
}
