package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithValidateConfig = (*neonProjectResource)(nil)

func (r neonProjectResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse) {
	configuredProjectProtection(ctx, req.Config, &resp.Diagnostics)
}

// Both attributes configure the same API setting. Read the raw configuration,
// not copied prior state, so an omitted nested field cannot mask a legacy edit.
func configuredProjectProtection(ctx context.Context, config tfsdk.Config, diagnostics *diag.Diagnostics) types.Bool {
	var legacy types.Bool
	var branch types.Object
	diagnostics.Append(config.GetAttribute(ctx, path.Root("default_branch_protected"), &legacy)...)
	diagnostics.Append(config.GetAttribute(ctx, path.Root("branch"), &branch)...)
	if diagnostics.HasError() {
		return types.BoolUnknown()
	}

	nested := types.BoolNull()
	switch {
	case branch.IsUnknown():
		nested = types.BoolUnknown()
	case !branch.IsNull():
		diagnostics.Append(config.GetAttribute(ctx, path.Root("branch").AtName("protected"), &nested)...)
	}
	if diagnostics.HasError() {
		return types.BoolUnknown()
	}
	if legacy.IsUnknown() || nested.IsUnknown() {
		return types.BoolUnknown()
	}
	if !legacy.IsNull() && !nested.IsNull() && !legacy.Equal(nested) {
		diagnostics.AddAttributeError(path.Root("branch").AtName("protected"),
			"Conflicting branch protection",
			"default_branch_protected and branch.protected configure the same branch and must agree. "+
				"Remove the deprecated default_branch_protected attribute or set both values identically.")
		return types.BoolUnknown()
	}
	if !nested.IsNull() {
		return nested
	}
	if !legacy.IsNull() {
		return legacy
	}
	return types.BoolValue(false)
}

func planProjectProtection(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	protection := configuredProjectProtection(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var legacy types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("default_branch_protected"), &legacy)...)
	// An explicit value, even an unknown one, must remain as configured.
	if legacy.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("default_branch_protected"), protection)...)
	}

	var plannedBranch, configuredBranch types.Object
	resp.Diagnostics.Append(resp.Plan.GetAttribute(ctx, path.Root("branch"), &plannedBranch)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("branch"), &configuredBranch)...)
	if resp.Diagnostics.HasError() || plannedBranch.IsUnknown() || plannedBranch.IsNull() || configuredBranch.IsUnknown() {
		return
	}
	if !configuredBranch.IsNull() {
		var nested types.Bool
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("branch").AtName("protected"), &nested)...)
		if resp.Diagnostics.HasError() || !nested.IsNull() {
			return
		}
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("branch").AtName("protected"), protection)...)
}
