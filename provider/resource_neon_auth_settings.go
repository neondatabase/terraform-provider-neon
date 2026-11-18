package provider

import (
	"context"
	"fmt"
	"math"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
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

type neonAuthEmailProviderModel struct {
	Type        types.String `tfsdk:"type"`
	Host        types.String `tfsdk:"host"`
	Port        types.Int64  `tfsdk:"port"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	SenderEmail types.String `tfsdk:"sender_email"`
	SenderName  types.String `tfsdk:"sender_name"`
}

type neonAuthWebhookModel struct {
	Enabled        types.Bool   `tfsdk:"enabled"`
	URL            types.String `tfsdk:"url"`
	EnabledEvents  types.Set    `tfsdk:"enabled_events"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
}

var neonAuthWebhookEvents = []string{
	"user.before_create", "user.created", "send.otp", "send.magic_link",
	"organization.invitation.created", "organization.invitation.accepted", "phone_number.verified",
}

var (
	neonAuthEmailProviderTypes = map[string]attr.Type{
		"type":         types.StringType,
		"host":         types.StringType,
		"port":         types.Int64Type,
		"username":     types.StringType,
		"password":     types.StringType,
		"sender_email": types.StringType,
		"sender_name":  types.StringType,
	}
	neonAuthWebhookTypes = map[string]attr.Type{
		"enabled":         types.BoolType,
		"url":             types.StringType,
		"enabled_events":  types.SetType{ElemType: types.StringType},
		"timeout_seconds": types.Int64Type,
	}
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

	text := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Optional:      true,
			Computed:      true,
			Description:   description,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}

	return map[string]schema.Attribute{
		"email_provider": setting("Email server that sends verification, sign-in, and invitation emails.", map[string]schema.Attribute{
			"type": choice("`shared` sends from Neon's email server, the default. `standard` sends through your SMTP "+
				"server and needs `host`, `port`, `username`, `password`, `sender_email`, and `sender_name`.",
				"shared", "standard"),
			"host":     text("SMTP server hostname."),
			"port":     number("SMTP server port, such as 587."),
			"username": text("SMTP username."),
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "SMTP password. Neon doesn't return it, so Terraform can't detect a password changed " +
					"outside Terraform.",
			},
			"sender_email": text("Address emails are sent from."),
			"sender_name":  text("Sender name shown on emails."),
		}),
		"webhook": setting("Webhook that Neon Auth calls on user, email, organization, and phone number events.", map[string]schema.Attribute{
			"enabled": flag("Whether Neon Auth calls the webhook."),
			"url":     text("HTTPS URL that receives the event payloads."),
			"enabled_events": schema.SetAttribute{
				Optional:      true,
				Computed:      true,
				ElementType:   types.StringType,
				Description:   "Events that call the webhook: `user.before_create`, `user.created`, `send.otp`, `send.magic_link`, `organization.invitation.created`, `organization.invitation.accepted`, and `phone_number.verified`.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
				Validators:    []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(neonAuthWebhookEvents...))},
			},
			"timeout_seconds": number("Seconds Neon Auth waits for the webhook to respond, from 1 to 10.", int64validator.Between(1, 10)),
		}),
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
			"enabled": flag("Whether users can create and join organizations. Neon enables it by default."),
			"organization_limit": number("Maximum number of organizations a user can create or join.",
				int64validator.Between(1, math.MaxInt32)),
			"membership_limit": number("Maximum number of members per organization.", int64validator.Between(1, math.MaxInt32)),
			"creator_role": choice("Role of the user who creates an organization: `owner` has full control, `admin` "+
				"manages members and settings.", "owner", "admin"),
			"send_invitation_email": flag("Whether invited users receive an email with a link to accept the invitation."),
		}),
	}
}

// writeSettings sends each settings group whose plan differs from state. A request carries only the members set in
// the configuration, so Neon keeps every other value, including one changed after the plan was made.
func (r *neonAuthResource) writeSettings(ctx context.Context, config tfsdk.Config, plan, state neonAuthResourceModel) (diags diag.Diagnostics) {
	projectID, branchID := plan.ProjectID.ValueString(), plan.BranchID.ValueString()
	changed := func(planned, prior types.Object) bool {
		return !planned.IsNull() && !planned.IsUnknown() && !planned.Equal(prior)
	}
	var configured neonAuthResourceModel
	if diags.Append(config.Get(ctx, &configured)...); diags.HasError() {
		return diags
	}

	// Link verification needs a custom email provider, so a custom provider is saved before the
	// email and password settings, and the shared provider after them.
	providerChanged := changed(plan.EmailProvider, state.EmailProvider)
	customProvider := providerChanged && plan.EmailProvider.Attributes()["type"].Equal(types.StringValue("standard"))
	if customProvider && known(configured.EmailProvider) {
		if diags.Append(r.writeEmailProvider(ctx, plan, configured.EmailProvider)...); diags.HasError() {
			return diags
		}
	}
	if changed(plan.EmailAndPassword, state.EmailAndPassword) && known(configured.EmailAndPassword) {
		if diags.Append(r.writeEmailAndPassword(ctx, plan, configured.EmailAndPassword)...); diags.HasError() {
			return diags
		}
	}
	if providerChanged && !customProvider && known(configured.EmailProvider) {
		if diags.Append(r.writeEmailProvider(ctx, plan, configured.EmailProvider)...); diags.HasError() {
			return diags
		}
	}

	if changed(plan.MagicLink, state.MagicLink) && known(configured.MagicLink) {
		var m neonAuthMagicLinkModel
		if diags.Append(configured.MagicLink.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
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

	if changed(plan.PhoneNumber, state.PhoneNumber) && known(configured.PhoneNumber) {
		var m neonAuthPhoneNumberModel
		if diags.Append(configured.PhoneNumber.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
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

	if changed(plan.Organization, state.Organization) && known(configured.Organization) {
		var m neonAuthOrganizationModel
		if diags.Append(configured.Organization.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
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
		if diags.Append(r.writeNeonAuth(ctx, projectID, branchID, func() error {
			_, err := r.client.UpdateNeonAuthOrganizationPlugin(projectID, branchID, cfg)
			return err
		})...); diags.HasError() {
			return diags
		}
	}

	if changed(plan.Webhook, state.Webhook) && known(configured.Webhook) {
		diags.Append(r.writeWebhook(ctx, plan, configured.Webhook)...)
	}
	return diags
}

func (r *neonAuthResource) writeEmailAndPassword(ctx context.Context, plan neonAuthResourceModel,
	configured types.Object) (diags diag.Diagnostics) {
	projectID, branchID := plan.ProjectID.ValueString(), plan.BranchID.ValueString()
	var m neonAuthEmailAndPasswordModel
	if diags.Append(configured.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
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
	return r.writeNeonAuth(ctx, projectID, branchID, func() error {
		_, err := r.client.UpdateNeonAuthEmailAndPasswordConfig(projectID, branchID, cfg)
		return err
	})
}

func (r *neonAuthResource) writeEmailProvider(ctx context.Context, plan neonAuthResourceModel,
	configured types.Object) (diags diag.Diagnostics) {
	projectID, branchID := plan.ProjectID.ValueString(), plan.BranchID.ValueString()
	var m neonAuthEmailProviderModel
	if diags.Append(configured.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
		return diags
	}
	cfg := neon.NeonAuthEmailServerConfig{Type: m.Type.ValueString()}
	if cfg.Type == "standard" {
		cfg.Host = knownString(m.Host)
		cfg.Username = knownString(m.Username)
		cfg.Password = knownString(m.Password)
		cfg.StandardEmailServer.SenderEmail = knownString(m.SenderEmail)
		cfg.StandardEmailServer.SenderName = knownString(m.SenderName)
		if known(m.Port) {
			port := int(m.Port.ValueInt64())
			cfg.Port = &port
		}
	}
	return r.writeNeonAuth(ctx, projectID, branchID, func() error {
		_, err := r.client.UpdateNeonAuthEmailProvider(projectID, branchID, cfg)
		return err
	})
}

// writeWebhook reads the webhook configuration and sends it back with the configured members changed, because Neon
// replaces the whole configuration on every write.
func (r *neonAuthResource) writeWebhook(ctx context.Context, plan neonAuthResourceModel,
	configured types.Object) (diags diag.Diagnostics) {
	projectID, branchID := plan.ProjectID.ValueString(), plan.BranchID.ValueString()
	var m neonAuthWebhookModel
	if diags.Append(configured.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
		return diags
	}
	var events []neon.NeonAuthWebhookConfigEnabledEventsItem
	if known(m.EnabledEvents) {
		var names []string
		if diags.Append(m.EnabledEvents.ElementsAs(ctx, &names, false)...); diags.HasError() {
			return diags
		}
		events = []neon.NeonAuthWebhookConfigEnabledEventsItem{}
		for _, name := range names {
			event, err := neon.NewNeonAuthWebhookConfigEnabledEventsItem(name)
			if err != nil {
				diags.AddAttributeError(path.Root("webhook").AtName("enabled_events"), "Invalid webhook event", err.Error())
				return diags
			}
			events = append(events, event)
		}
	}
	return r.writeNeonAuth(ctx, projectID, branchID, func() error {
		cfg, err := r.client.GetNeonAuthWebhookConfig(projectID, branchID)
		if err != nil {
			return err
		}
		if known(m.Enabled) {
			cfg.Enabled = m.Enabled.ValueBool()
		}
		if known(m.URL) {
			cfg.WebhookURL = m.URL.ValueStringPointer()
		}
		if known(m.TimeoutSeconds) {
			timeout := uint8(m.TimeoutSeconds.ValueInt64())
			cfg.TimeoutSeconds = &timeout
		}
		if events != nil {
			cfg.EnabledEvents = events
		}
		_, err = r.client.UpdateNeonAuthWebhookConfig(projectID, branchID, cfg)
		return err
	})
}

func (m *neonAuthResourceModel) refreshSettings(plugins neon.NeonAuthPluginConfigs,
	emailProvider neon.NeonAuthEmailServerConfigResponse, webhook neon.NeonAuthWebhookConfig) (diags diag.Diagnostics) {
	set := func(target *types.Object, attrTypes map[string]attr.Type, values map[string]attr.Value) {
		value, d := basetypes.NewObjectValue(attrTypes, values)
		diags.Append(d...)
		*target = value
	}

	password := types.StringNull()
	if known(m.EmailProvider) {
		if prior, ok := m.EmailProvider.Attributes()["password"].(types.String); ok {
			password = prior
		}
	}
	emailValues := map[string]attr.Value{
		"type":         types.StringValue(emailProvider.Type),
		"host":         types.StringNull(),
		"port":         types.Int64Null(),
		"username":     types.StringNull(),
		"password":     types.StringNull(),
		"sender_email": types.StringPointerValue(emailProvider.SharedEmailServer.SenderEmail),
		"sender_name":  types.StringPointerValue(emailProvider.SharedEmailServer.SenderName),
	}
	if emailProvider.Type == "standard" {
		emailValues["host"] = types.StringValue(emailProvider.Host)
		emailValues["port"] = types.Int64Value(int64(emailProvider.Port))
		emailValues["username"] = types.StringValue(emailProvider.Username)
		emailValues["password"] = password
		emailValues["sender_email"] = types.StringValue(emailProvider.StandardEmailServerResponse.SenderEmail)
		emailValues["sender_name"] = types.StringValue(emailProvider.StandardEmailServerResponse.SenderName)
	}
	set(&m.EmailProvider, neonAuthEmailProviderTypes, emailValues)

	events := make([]attr.Value, 0, len(webhook.EnabledEvents))
	for _, event := range webhook.EnabledEvents {
		events = append(events, types.StringValue(event.String()))
	}
	eventSet, d := types.SetValue(types.StringType, events)
	diags.Append(d...)
	timeout := types.Int64Null()
	if webhook.TimeoutSeconds != nil {
		timeout = types.Int64Value(int64(*webhook.TimeoutSeconds))
	}
	set(&m.Webhook, neonAuthWebhookTypes, map[string]attr.Value{
		"enabled":         types.BoolValue(webhook.Enabled),
		"url":             types.StringPointerValue(webhook.WebhookURL),
		"enabled_events":  eventSet,
		"timeout_seconds": timeout,
	})

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

// planSettingsChanges marks the members that a changed settings group leaves out of the configuration as unknown:
// Neon may have changed them since the plan's refresh, and apply reads them back.
func planSettingsChanges(ctx context.Context, config tfsdk.Config, state neonAuthResourceModel,
	plan *neonAuthResourceModel) (diags diag.Diagnostics) {
	var configured neonAuthResourceModel
	if diags.Append(config.Get(ctx, &configured)...); diags.HasError() {
		return diags
	}
	for _, group := range []struct {
		planned           *types.Object
		prior, configured types.Object
	}{
		{&plan.EmailAndPassword, state.EmailAndPassword, configured.EmailAndPassword},
		{&plan.MagicLink, state.MagicLink, configured.MagicLink},
		{&plan.PhoneNumber, state.PhoneNumber, configured.PhoneNumber},
		{&plan.Organization, state.Organization, configured.Organization},
		{&plan.EmailProvider, state.EmailProvider, configured.EmailProvider},
		{&plan.Webhook, state.Webhook, configured.Webhook},
	} {
		if !known(*group.planned) || group.planned.Equal(group.prior) {
			continue
		}
		configuredMembers := map[string]attr.Value{}
		if known(group.configured) {
			configuredMembers = group.configured.Attributes()
		}
		members := group.planned.Attributes()
		for name, value := range members {
			// Neon never returns the SMTP password, so it stays whatever the configuration says.
			if member, ok := configuredMembers[name]; (ok && !member.IsNull()) || name == "password" {
				continue
			}
			switch value.(type) {
			case types.Bool:
				members[name] = types.BoolUnknown()
			case types.Int64:
				members[name] = types.Int64Unknown()
			case types.String:
				members[name] = types.StringUnknown()
			case types.Set:
				members[name] = types.SetUnknown(types.StringType)
			default:
				diags.AddError("Unsupported Neon Auth setting type", fmt.Sprintf("%s has the unsupported type %T.", name, value))
				return diags
			}
		}
		object, d := types.ObjectValue(group.planned.AttributeTypes(ctx), members)
		if diags.Append(d...); diags.HasError() {
			return diags
		}
		*group.planned = object
	}
	return diags
}

// keepUnchangedSettings restores the settings groups that an update didn't write to their planned values, so a
// change made on Neon after the plan shows up at the next refresh instead of failing the apply.
func keepUnchangedSettings(plan neonAuthResourceModel, state *neonAuthResourceModel) {
	for _, group := range []struct{ planned, refreshed *types.Object }{
		{&plan.EmailAndPassword, &state.EmailAndPassword},
		{&plan.MagicLink, &state.MagicLink},
		{&plan.PhoneNumber, &state.PhoneNumber},
		{&plan.Organization, &state.Organization},
		{&plan.EmailProvider, &state.EmailProvider},
		{&plan.Webhook, &state.Webhook},
	} {
		if known(*group.planned) && !hasUnknownMember(*group.planned) {
			*group.refreshed = *group.planned
		}
	}
}

func hasUnknownMember(object types.Object) bool {
	for _, value := range object.Attributes() {
		if value.IsUnknown() {
			return true
		}
	}
	return false
}

// validateNewIntegrationSettings fails during plan for settings Neon rejects right after Neon Auth is enabled,
// where a failed write can only be reported as a warning.
func validateNewIntegrationSettings(ctx context.Context, plan tfsdk.Plan) (diags diag.Diagnostics) {
	var method, providerType types.String
	diags.Append(plan.GetAttribute(ctx, path.Root("email_and_password").AtName("email_verification_method"), &method)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("email_provider").AtName("type"), &providerType)...)
	if diags.HasError() || !method.Equal(types.StringValue("link")) || providerType.Equal(types.StringValue("standard")) {
		return diags
	}
	diags.AddAttributeError(path.Root("email_and_password").AtName("email_verification_method"),
		"Verification links need a custom email provider",
		"Neon Auth starts with Neon's shared email provider, which sends one-time passwords only. Set "+
			"email_provider to a standard SMTP server in the same configuration, or use `otp`.")
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

func knownString(v types.String) *string {
	if !known(v) {
		return nil
	}
	return v.ValueStringPointer()
}

// validateEmailProvider fails during plan where Neon would reject the email provider or silently drop part of it.
func validateEmailProvider(ctx context.Context, object types.Object) (diags diag.Diagnostics) {
	if !known(object) {
		return diags
	}
	var m neonAuthEmailProviderModel
	if diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...); diags.HasError() {
		return diags
	}
	root := path.Root("email_provider")
	if m.Type.IsNull() {
		diags.AddAttributeError(root.AtName("type"), "Missing email provider type",
			"Set type to `shared` for Neon's email server or `standard` for your SMTP server.")
		return diags
	}
	if m.Type.IsUnknown() {
		return diags
	}
	fields := []struct {
		name  string
		value knownValue
	}{
		{"host", m.Host}, {"port", m.Port}, {"username", m.Username}, {"password", m.Password},
		{"sender_email", m.SenderEmail}, {"sender_name", m.SenderName},
	}
	for _, field := range fields {
		switch {
		case m.Type.ValueString() == "shared" && !field.value.IsNull():
			diags.AddAttributeError(root.AtName(field.name), "Attribute only applies to a standard email provider",
				fmt.Sprintf("Remove %s, or set type to `standard`. Neon's shared email server sends from its own address.", field.name))
		case m.Type.ValueString() == "standard" && field.value.IsNull():
			diags.AddAttributeError(root.AtName(field.name), "Missing SMTP setting",
				fmt.Sprintf("Set %s; a standard email provider needs host, port, username, password, sender_email, and sender_name.", field.name))
		}
	}
	return diags
}
