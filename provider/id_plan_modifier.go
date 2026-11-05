package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
)

// idPlanModifiers keeps a resource ID known during in-place updates.
// Without it, any update plans the ID as unknown, and every resource that
// references the ID in a RequiresReplace attribute is replaced.
// Only use it when no attribute in the ID can change in place.
func idPlanModifiers() []planmodifier.String {
	return []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
}

// keepPriorString keeps computed attributes that other resources reference,
// such as neon_project.default_branch_id, known during in-place updates.
// It only applies when the configuration leaves the attribute unset.
var keepPriorString = []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
