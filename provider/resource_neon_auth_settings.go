package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	neon "github.com/kislerdm/neon-sdk-go"
)

type neonAuthEmailAndPasswordModel struct {
	Enabled                       types.Bool   `tfsdk:"enabled"`
	EmailVerificationMethod       types.String `tfsdk:"email_verification_method"`
	RequireEmailVerification      types.Bool   `tfsdk:"require_email_verification"`
	AutoSignInAfterVerification   types.Bool   `tfsdk:"auto_sign_in_after_verification"`
	SendVerificationEmailOnSignUp types.Bool   `tfsdk:"send_verification_email_on_sign_up"`
	SendVerificationEmailOnSignIn types.Bool   `tfsdk:"send_verification_email_on_sign_in"`
	DisableSignUp                 types.Bool   `tfsdk:"disable_sign_up"`
	RevokeSessionsOnPasswordReset types.Bool   `tfsdk:"revoke_sessions_on_password_reset"`
}

type neonAuthMagicLinkModel struct {
	Enabled       types.Bool  `tfsdk:"enabled"`
	ExpiresIn     types.Int64 `tfsdk:"expires_in"`
	DisableSignUp types.Bool  `tfsdk:"disable_sign_up"`
}

type neonAuthPhoneNumberModel struct {
	Enabled      types.Bool  `tfsdk:"enabled"`
	OTPExpiresIn types.Int64 `tfsdk:"otp_expires_in"`
}

type neonAuthOrganizationModel struct {
	Enabled             types.Bool   `tfsdk:"enabled"`
	OrganizationLimit   types.Int64  `tfsdk:"organization_limit"`
	MembershipLimit     types.Int64  `tfsdk:"membership_limit"`
	CreatorRole         types.String `tfsdk:"creator_role"`
	SendInvitationEmail types.Bool   `tfsdk:"send_invitation_email"`
}

var (
	neonAuthEmailAndPasswordTypes = map[string]attr.Type{
		"enabled":                            types.BoolType,
		"email_verification_method":          types.StringType,
		"require_email_verification":         types.BoolType,
		"auto_sign_in_after_verification":    types.BoolType,
		"send_verification_email_on_sign_up": types.BoolType,
		"send_verification_email_on_sign_in": types.BoolType,
		"disable_sign_up":                    types.BoolType,
		"revoke_sessions_on_password_reset":  types.BoolType,
	}
	neonAuthMagicLinkTypes = map[string]attr.Type{
		"enabled":         types.BoolType,
		"expires_in":      types.Int64Type,
		"disable_sign_up": types.BoolType,
	}
	neonAuthPhoneNumberTypes = map[string]attr.Type{
		"enabled":        types.BoolType,
		"otp_expires_in": types.Int64Type,
	}
	neonAuthOrganizationTypes = map[string]attr.Type{
		"enabled":               types.BoolType,
		"organization_limit":    types.Int64Type,
		"membership_limit":      types.Int64Type,
		"creator_role":          types.StringType,
		"send_invitation_email": types.BoolType,
	}
)

func neonAuthSettingsAttributes() map[string]schema.Attribute {
	setting := func(description string, attributes map[string]schema.Attribute) schema.SingleNestedAttribute {
		return schema.SingleNestedAttribute{
			Optional:      true,
			Computed:      true,
			Description:   description + " Settings left out keep their current value on Neon.",
			PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
			Attributes:    attributes,
		}
	}
	flag := func(description string) schema.BoolAttribute {
		return schema.BoolAttribute{
			Optional:      true,
			Computed:      true,
			Description:   description,
			PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
		}
	}
	number := func(description string, validators ...validator.Int64) schema.Int64Attribute {
		return schema.Int64Attribute{
			Optional:      true,
			Computed:      true,
			Description:   description,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			Validators:    validators,
		}
	}
	choice := func(description string, values ...string) schema.StringAttribute {
		return schema.StringAttribute{
			Optional:      true,
			Computed:      true,
			Description:   description,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			Validators:    []validator.String{stringvalidator.OneOf(values...)},
		}
	}

	return map[string]schema.Attribute{
		"email_and_password": setting("Email and password sign-in.", map[string]schema.Attribute{
			"enabled": flag("Whether users can sign in with email and password. Neon enables it by default."),
			"email_verification_method": choice("How email addresses are verified: `otp` sends a one-time "+
				"password, `link` sends a verification link. `link` needs a custom email provider.", "otp", "link"),
			"require_email_verification":         flag("Whether users must verify their email address before they can sign in."),
			"auto_sign_in_after_verification":    flag("Whether users are signed in automatically after verifying their email address."),
			"send_verification_email_on_sign_up": flag("Whether a verification email is sent when a user signs up."),
			"send_verification_email_on_sign_in": flag("Whether a verification email is sent when a user with an unverified email address signs in."),
			"disable_sign_up":                    flag("Whether new users can't sign up with email and password."),
			"revoke_sessions_on_password_reset":  flag("Whether a password reset revokes all of the user's sessions."),
		}),
		"magic_link": setting("Passwordless sign-in with a link sent by email.", map[string]schema.Attribute{
			"enabled":         flag("Whether users can sign in with a magic link."),
			"expires_in":      number("Minutes until a magic link expires, from 5 to 1440.", int64validator.Between(5, 1440)),
			"disable_sign_up": flag("Whether new users can't sign up with a magic link."),
		}),
		"phone_number": setting("Sign-in with a one-time password sent to a phone number.", map[string]schema.Attribute{
			"enabled":        flag("Whether users can sign in with a phone number."),
			"otp_expires_in": number("Seconds until a one-time password expires, from 60 to 600.", int64validator.Between(60, 600)),
		}),
		"organization": setting("Organizations of users within Neon Auth, separate from Neon organizations.", map[string]schema.Attribute{
			"enabled":            flag("Whether users can create and join organizations. Neon enables it by default."),
			"organization_limit": number("Maximum number of organizations a user can create or join.", int64validator.AtLeast(1)),
			"membership_limit":   number("Maximum number of members per organization.", int64validator.AtLeast(1)),
			"creator_role": choice("Role of the user who creates an organization: `owner` has full control, `admin` "+
				"manages members and settings.", "owner", "admin"),
			"send_invitation_email": flag("Whether invited users receive an email with a link to accept the invitation."),
		}),
	}
}

// writeSettings sends each settings group whose plan differs from state. Unknown members are left out of the
// request, so Neon keeps their current values.
func (r *neonAuthResource) writeSettings(ctx context.Context, plan, state neonAuthResourceModel) (diags diag.Diagnostics) {
	projectID, branchID := plan.ProjectID.ValueString(), plan.BranchID.ValueString()
	changed := func(planned, prior types.Object) bool {
		return !planned.IsNull() && !planned.IsUnknown() && !planned.Equal(prior)
	}

	if changed(plan.EmailAndPassword, state.EmailAndPassword) {
		var m neonAuthEmailAndPasswordModel
		if diags.Append(plan.EmailAndPassword.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
			return diags
		}
		cfg := neon.NeonAuthEmailAndPasswordConfigUpdate{
			Enabled:                       knownBool(m.Enabled),
			RequireEmailVerification:      knownBool(m.RequireEmailVerification),
			AutoSignInAfterVerification:   knownBool(m.AutoSignInAfterVerification),
			SendVerificationEmailOnSignUp: knownBool(m.SendVerificationEmailOnSignUp),
			SendVerificationEmailOnSignIn: knownBool(m.SendVerificationEmailOnSignIn),
			DisableSignUp:                 knownBool(m.DisableSignUp),
			RevokeSessionsOnPasswordReset: knownBool(m.RevokeSessionsOnPasswordReset),
		}
		if known(m.EmailVerificationMethod) {
			method, err := neon.NewNeonAuthEmailVerificationMethod(m.EmailVerificationMethod.ValueString())
			if err != nil {
				diags.AddAttributeError(path.Root("email_and_password").AtName("email_verification_method"),
					"Invalid email verification method", err.Error())
				return diags
			}
			cfg.EmailVerificationMethod = &method
		}
		if diags.Append(r.writeNeonAuth(ctx, projectID, branchID, func() error {
			_, err := r.client.UpdateNeonAuthEmailAndPasswordConfig(projectID, branchID, cfg)
			return err
		})...); diags.HasError() {
			return diags
		}
	}

	if changed(plan.MagicLink, state.MagicLink) {
		var m neonAuthMagicLinkModel
		if diags.Append(plan.MagicLink.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
			return diags
		}
		cfg := neon.NeonAuthMagicLinkConfigUpdate{Enabled: knownBool(m.Enabled), DisableSignUp: knownBool(m.DisableSignUp)}
		if known(m.ExpiresIn) {
			v := int32(m.ExpiresIn.ValueInt64())
			cfg.ExpiresIn = &v
		}
		if diags.Append(r.writeNeonAuth(ctx, projectID, branchID, func() error {
			_, err := r.client.UpdateNeonAuthMagicLinkPlugin(projectID, branchID, cfg)
			return err
		})...); diags.HasError() {
			return diags
		}
	}

	if changed(plan.PhoneNumber, state.PhoneNumber) {
		var m neonAuthPhoneNumberModel
		if diags.Append(plan.PhoneNumber.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
			return diags
		}
		cfg := neon.NeonAuthPhoneNumberConfigUpdate{Enabled: knownBool(m.Enabled)}
		if known(m.OTPExpiresIn) {
			v := uint16(m.OTPExpiresIn.ValueInt64())
			cfg.OtpExpiresIn = &v
		}
		if diags.Append(r.writeNeonAuth(ctx, projectID, branchID, func() error {
			_, err := r.client.UpdateNeonAuthPhoneNumberPlugin(projectID, branchID, cfg)
			return err
		})...); diags.HasError() {
			return diags
		}
	}

	if changed(plan.Organization, state.Organization) {
		var m neonAuthOrganizationModel
		if diags.Append(plan.Organization.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
			return diags
		}
		cfg := neon.NeonAuthOrganizationConfigUpdate{
			Enabled:             knownBool(m.Enabled),
			SendInvitationEmail: knownBool(m.SendInvitationEmail),
		}
		if known(m.OrganizationLimit) {
			v := int32(m.OrganizationLimit.ValueInt64())
			cfg.OrganizationLimit = &v
		}
		if known(m.MembershipLimit) {
			v := int32(m.MembershipLimit.ValueInt64())
			cfg.MembershipLimit = &v
		}
		if known(m.CreatorRole) {
			role, err := neon.NewNeonAuthOrganizationConfigUpdateCreatorRole(m.CreatorRole.ValueString())
			if err != nil {
				diags.AddAttributeError(path.Root("organization").AtName("creator_role"), "Invalid creator role", err.Error())
				return diags
			}
			cfg.CreatorRole = &role
		}
		diags.Append(r.writeNeonAuth(ctx, projectID, branchID, func() error {
			_, err := r.client.UpdateNeonAuthOrganizationPlugin(projectID, branchID, cfg)
			return err
		})...)
	}
	return diags
}

func (m *neonAuthResourceModel) refreshSettings(plugins neon.NeonAuthPluginConfigs) (diags diag.Diagnostics) {
	set := func(target *types.Object, attrTypes map[string]attr.Type, values map[string]attr.Value) {
		value, d := basetypes.NewObjectValue(attrTypes, values)
		diags.Append(d...)
		*target = value
	}

	m.EmailAndPassword = types.ObjectNull(neonAuthEmailAndPasswordTypes)
	if v := plugins.EmailAndPassword; v != nil {
		set(&m.EmailAndPassword, neonAuthEmailAndPasswordTypes, map[string]attr.Value{
			"enabled":                            types.BoolValue(v.Enabled),
			"email_verification_method":          types.StringValue(v.EmailVerificationMethod.String()),
			"require_email_verification":         types.BoolValue(v.RequireEmailVerification),
			"auto_sign_in_after_verification":    types.BoolValue(v.AutoSignInAfterVerification),
			"send_verification_email_on_sign_up": types.BoolValue(v.SendVerificationEmailOnSignUp),
			"send_verification_email_on_sign_in": types.BoolValue(v.SendVerificationEmailOnSignIn),
			"disable_sign_up":                    types.BoolValue(v.DisableSignUp),
			"revoke_sessions_on_password_reset":  types.BoolValue(v.RevokeSessionsOnPasswordReset),
		})
	}

	m.MagicLink = types.ObjectNull(neonAuthMagicLinkTypes)
	if v := plugins.MagicLink; v != nil {
		set(&m.MagicLink, neonAuthMagicLinkTypes, map[string]attr.Value{
			"enabled":         types.BoolValue(v.Enabled),
			"expires_in":      types.Int64Value(int64(v.ExpiresIn)),
			"disable_sign_up": types.BoolValue(v.DisableSignUp),
		})
	}

	m.PhoneNumber = types.ObjectNull(neonAuthPhoneNumberTypes)
	if v := plugins.PhoneNumber; v != nil {
		otp := types.Int64Null()
		if v.OtpExpiresIn != nil {
			otp = types.Int64Value(int64(*v.OtpExpiresIn))
		}
		set(&m.PhoneNumber, neonAuthPhoneNumberTypes, map[string]attr.Value{
			"enabled":        types.BoolValue(v.Enabled),
			"otp_expires_in": otp,
		})
	}

	m.Organization = types.ObjectNull(neonAuthOrganizationTypes)
	if v := plugins.Organization; v != nil {
		set(&m.Organization, neonAuthOrganizationTypes, map[string]attr.Value{
			"enabled":               types.BoolValue(v.Enabled),
			"organization_limit":    types.Int64Value(int64(v.OrganizationLimit)),
			"membership_limit":      types.Int64Value(int64(v.MembershipLimit)),
			"creator_role":          types.StringValue(v.CreatorRole.String()),
			"send_invitation_email": types.BoolValue(v.SendInvitationEmail),
		})
	}
	return diags
}

type knownValue interface {
	IsNull() bool
	IsUnknown() bool
}

func known(v knownValue) bool {
	return !v.IsNull() && !v.IsUnknown()
}

func knownBool(v types.Bool) *bool {
	if !known(v) {
		return nil
	}
	return v.ValueBoolPointer()
}
