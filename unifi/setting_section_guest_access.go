package unifi

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// guestAccessSection is the settingSection implementation for the
// "guest_access" settings section: the guest-portal operational core (auth
// mode, portal enable/network behavior, session expiry, subnet/DNS
// restriction, RADIUS-backed auth, OAuth SSO connection settings, and the
// full payment-gateway credential surface). Of the 97 controller fields on
// go-unifi's settings.GuestAccess, 56 are modeled here (18 of them secrets,
// added in a follow-up commit) and 41 are deliberately preserved by
// read-modify-write, never decoded into state — see
// docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md for the
// full field table and rationale (Key Decision 1). The preserved surface is
// almost entirely the portal template/styling surface (colors, fonts, logo,
// background, welcome/success/ToS copy, language list), normally authored
// once through the controller's own guest-portal editor UI rather than
// hand-typed into HCL — matching how mgmt's alert_enabled/boot_sound/etc.
// and radius's configure_whole_network/tunneled_reply are preserved today.
type guestAccessSection struct{}

func init() {
	registerSection(guestAccessSection{})
}

func (guestAccessSection) key() string      { return "guest_access" }
func (guestAccessSection) attrName() string { return "guest_access" }

// settingGuestAccessModel is the Terraform model for the "guest_access"
// section (settingResourceModel.GuestAccess).
type settingGuestAccessModel struct {
	Auth                      types.String `tfsdk:"auth"`
	AuthUrl                   types.String `tfsdk:"auth_url"`
	PortalEnabled             types.Bool   `tfsdk:"portal_enabled"`
	PortalUseHostname         types.Bool   `tfsdk:"portal_use_hostname"`
	PortalHostname            types.String `tfsdk:"portal_hostname"`
	CustomIP                  types.String `tfsdk:"custom_ip"`
	EcEnabled                 types.Bool   `tfsdk:"ec_enabled"`
	Expire                    types.String `tfsdk:"expire"`
	ExpireNumber              types.Int64  `tfsdk:"expire_number"`
	ExpireUnit                types.Int64  `tfsdk:"expire_unit"`
	RedirectEnabled           types.Bool   `tfsdk:"redirect_enabled"`
	RedirectUrl               types.String `tfsdk:"redirect_url"`
	RedirectToHttps           types.Bool   `tfsdk:"redirect_to_https"`
	RedirectHttps             types.Bool   `tfsdk:"redirect_https"`
	AllowedSubnet             types.String `tfsdk:"allowed_subnet"`
	RestrictedSubnet          types.String `tfsdk:"restricted_subnet"`
	RestrictedDNSEnabled      types.Bool   `tfsdk:"restricted_dns_enabled"`
	RestrictedDNSServers      types.List   `tfsdk:"restricted_dns_servers"`
	PasswordEnabled           types.Bool   `tfsdk:"password_enabled"`
	VoucherEnabled            types.Bool   `tfsdk:"voucher_enabled"`
	RADIUSEnabled             types.Bool   `tfsdk:"radius_enabled"`
	RADIUSProfileID           types.String `tfsdk:"radiusprofile_id"`
	RADIUSAuthType            types.String `tfsdk:"radius_auth_type"`
	RADIUSDisconnectEnabled   types.Bool   `tfsdk:"radius_disconnect_enabled"`
	RADIUSDisconnectPort      types.Int64  `tfsdk:"radius_disconnect_port"`
	FacebookEnabled           types.Bool   `tfsdk:"facebook_enabled"`
	FacebookAppID             types.String `tfsdk:"facebook_app_id"`
	GoogleEnabled             types.Bool   `tfsdk:"google_enabled"`
	GoogleClientID            types.String `tfsdk:"google_client_id"`
	WechatEnabled             types.Bool   `tfsdk:"wechat_enabled"`
	WechatAppID               types.String `tfsdk:"wechat_app_id"`
	PaymentEnabled            types.Bool   `tfsdk:"payment_enabled"`
	Gateway                   types.String `tfsdk:"gateway"`
	PaypalUseSandbox          types.Bool   `tfsdk:"paypal_use_sandbox"`
	AuthorizeUseSandbox       types.Bool   `tfsdk:"authorize_use_sandbox"`
	QuickpayTestmode          types.Bool   `tfsdk:"quickpay_testmode"`
	MerchantwarriorUseSandbox types.Bool   `tfsdk:"merchantwarrior_use_sandbox"`
	IPpayUseSandbox           types.Bool   `tfsdk:"ippay_use_sandbox"`
}

// guestAccessAttrTypes is the object attribute-type map for
// settingGuestAccessModel.
var guestAccessAttrTypes = map[string]attr.Type{
	"auth":                        types.StringType,
	"auth_url":                    types.StringType,
	"portal_enabled":              types.BoolType,
	"portal_use_hostname":         types.BoolType,
	"portal_hostname":             types.StringType,
	"custom_ip":                   types.StringType,
	"ec_enabled":                  types.BoolType,
	"expire":                      types.StringType,
	"expire_number":               types.Int64Type,
	"expire_unit":                 types.Int64Type,
	"redirect_enabled":            types.BoolType,
	"redirect_url":                types.StringType,
	"redirect_to_https":           types.BoolType,
	"redirect_https":              types.BoolType,
	"allowed_subnet":              types.StringType,
	"restricted_subnet":           types.StringType,
	"restricted_dns_enabled":      types.BoolType,
	"restricted_dns_servers":      types.ListType{ElemType: types.StringType},
	"password_enabled":            types.BoolType,
	"voucher_enabled":             types.BoolType,
	"radius_enabled":              types.BoolType,
	"radiusprofile_id":            types.StringType,
	"radius_auth_type":            types.StringType,
	"radius_disconnect_enabled":   types.BoolType,
	"radius_disconnect_port":      types.Int64Type,
	"facebook_enabled":            types.BoolType,
	"facebook_app_id":             types.StringType,
	"google_enabled":              types.BoolType,
	"google_client_id":            types.StringType,
	"wechat_enabled":              types.BoolType,
	"wechat_app_id":               types.StringType,
	"payment_enabled":             types.BoolType,
	"gateway":                     types.StringType,
	"paypal_use_sandbox":          types.BoolType,
	"authorize_use_sandbox":       types.BoolType,
	"quickpay_testmode":           types.BoolType,
	"merchantwarrior_use_sandbox": types.BoolType,
	"ippay_use_sandbox":           types.BoolType,
}

// guestAccessDottedQuadOrEmpty matches an IPv4 dotted-quad or the empty
// string, mirroring go-unifi's own regex comment for custom_ip and
// restricted_dns_servers' elements.
var guestAccessDottedQuadOrEmpty = regexp.MustCompile(
	`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$|^$`,
)

// guestAccessHostnameOrEmpty matches go-unifi's portal_hostname regex.
var guestAccessHostnameOrEmpty = regexp.MustCompile(`^[a-zA-Z0-9.-]+$|^$`)

// schemaAttribute returns the "guest_access" SingleNestedAttribute: Optional
// + Computed, no UseStateForUnknown (matches radius's parent-level shape,
// not mgmt's — guest_access has no nested list requiring churn
// suppression). See spec's "Schema shape".
func (guestAccessSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Guest portal, authentication, RADIUS, SSO, and payment-gateway settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"auth": schema.StringAttribute{
				MarkdownDescription: "Guest portal authentication mode.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("none", "hotspot", "facebook_wifi", "custom"),
				},
			},
			"auth_url": schema.StringAttribute{
				MarkdownDescription: "Custom authentication endpoint, used when `auth = \"custom\"`.",
				Optional:            true,
				Computed:            true,
			},
			"portal_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the guest portal.",
				Optional:            true,
				Computed:            true,
			},
			"portal_use_hostname": schema.BoolAttribute{
				MarkdownDescription: "Use `portal_hostname` instead of the controller's own address for the portal.",
				Optional:            true,
				Computed:            true,
			},
			"portal_hostname": schema.StringAttribute{
				MarkdownDescription: "Guest portal hostname.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(guestAccessHostnameOrEmpty, "must be a valid hostname or empty"),
				},
			},
			"custom_ip": schema.StringAttribute{
				MarkdownDescription: "Alternate portal address, pinned to a specific IPv4 address instead of `portal_hostname`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(guestAccessDottedQuadOrEmpty, "must be a dotted-quad IPv4 address or empty"),
				},
			},
			"ec_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Elliptic-Curve TLS/crypto mode for the guest portal.",
				Optional:            true,
				Computed:            true,
			},
			"expire": schema.StringAttribute{
				MarkdownDescription: "Guest session expiry, in `expire_unit` units, or `\"custom\"`.",
				Optional:            true,
				Computed:            true,
			},
			"expire_number": schema.Int64Attribute{
				MarkdownDescription: "Guest session expiry duration.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 1000000),
				},
			},
			"expire_unit": schema.Int64Attribute{
				MarkdownDescription: "Guest session expiry unit multiplier in minutes: `1` (minutes), `60` (hours), or `1440` (days).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(1, 60, 1440),
				},
			},
			"redirect_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable redirecting guests to a URL after successful authentication.",
				Optional:            true,
				Computed:            true,
			},
			"redirect_url": schema.StringAttribute{
				MarkdownDescription: "URL to redirect guests to after successful authentication.",
				Optional:            true,
				Computed:            true,
			},
			"redirect_to_https": schema.BoolAttribute{
				MarkdownDescription: "Redirect the guest portal to HTTPS.",
				Optional:            true,
				Computed:            true,
			},
			"redirect_https": schema.BoolAttribute{
				MarkdownDescription: "Additional HTTPS-redirect toggle, distinct from `redirect_to_https` — both are independent controller fields.",
				Optional:            true,
				Computed:            true,
			},
			"allowed_subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet (CIDR) allowed to bypass the guest portal.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet (CIDR) restricted from guest access.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_dns_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable restricting guests to specific DNS servers.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_dns_servers": schema.ListAttribute{
				MarkdownDescription: "DNS servers guests are restricted to when `restricted_dns_enabled` is set.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(
						stringvalidator.RegexMatches(guestAccessDottedQuadOrEmpty, "must be a dotted-quad IPv4 address or empty"),
					),
				},
			},
			"password_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable shared/hotspot password mode.",
				Optional:            true,
				Computed:            true,
			},
			"voucher_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable voucher-based guest access.",
				Optional:            true,
				Computed:            true,
			},
			"radius_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable RADIUS-backed guest authentication.",
				Optional:            true,
				Computed:            true,
			},
			"radiusprofile_id": schema.StringAttribute{
				MarkdownDescription: "RADIUS profile ID used for guest authentication.",
				Optional:            true,
				Computed:            true,
			},
			"radius_auth_type": schema.StringAttribute{
				MarkdownDescription: "RADIUS authentication type.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("chap", "mschapv2"),
				},
			},
			"radius_disconnect_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable RADIUS Disconnect-Message (CoA) support.",
				Optional:            true,
				Computed:            true,
			},
			"radius_disconnect_port": schema.Int64Attribute{
				MarkdownDescription: "RADIUS Disconnect-Message listening port.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"facebook_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Facebook Wi-Fi / Facebook login SSO.",
				Optional:            true,
				Computed:            true,
			},
			"facebook_app_id": schema.StringAttribute{
				MarkdownDescription: "Facebook app ID for guest SSO.",
				Optional:            true,
				Computed:            true,
			},
			"google_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Google SSO.",
				Optional:            true,
				Computed:            true,
			},
			"google_client_id": schema.StringAttribute{
				MarkdownDescription: "Google OAuth client ID for guest SSO.",
				Optional:            true,
				Computed:            true,
			},
			"wechat_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable WeChat SSO.",
				Optional:            true,
				Computed:            true,
			},
			"wechat_app_id": schema.StringAttribute{
				MarkdownDescription: "WeChat app ID for guest SSO.",
				Optional:            true,
				Computed:            true,
			},
			"payment_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable paid guest access via a payment gateway.",
				Optional:            true,
				Computed:            true,
			},
			"gateway": schema.StringAttribute{
				MarkdownDescription: "Payment gateway used for paid guest access.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("paypal", "stripe", "authorize", "quickpay", "merchantwarrior", "ippay"),
				},
			},
			"paypal_use_sandbox": schema.BoolAttribute{
				MarkdownDescription: "Use the PayPal sandbox environment.",
				Optional:            true,
				Computed:            true,
			},
			"authorize_use_sandbox": schema.BoolAttribute{
				MarkdownDescription: "Use the Authorize.Net sandbox environment.",
				Optional:            true,
				Computed:            true,
			},
			"quickpay_testmode": schema.BoolAttribute{
				MarkdownDescription: "Use Quickpay's test mode.",
				Optional:            true,
				Computed:            true,
			},
			"merchantwarrior_use_sandbox": schema.BoolAttribute{
				MarkdownDescription: "Use the MerchantWarrior sandbox environment.",
				Optional:            true,
				Computed:            true,
			},
			"ippay_use_sandbox": schema.BoolAttribute{
				MarkdownDescription: "Use the ippay sandbox environment.",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

// decode populates model.GuestAccess from snap's "guest_access" section
// data. Only the 38 non-secret leaves are decoded at this point; the 41
// unmodeled (preserved) fields are never read into the model — they survive
// solely via overlay's read-modify-write base.
func (s guestAccessSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingGuestAccessModel
	if !prior.GuestAccess.IsNull() && !prior.GuestAccess.IsUnknown() {
		diags.Append(prior.GuestAccess.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	auth, d := decodeString(data, "auth", priorModel.Auth)
	diags.Append(d...)
	authURL, d := decodeString(data, "auth_url", priorModel.AuthUrl)
	diags.Append(d...)
	portalEnabled, d := decodeBool(data, "portal_enabled", priorModel.PortalEnabled)
	diags.Append(d...)
	portalUseHostname, d := decodeBool(data, "portal_use_hostname", priorModel.PortalUseHostname)
	diags.Append(d...)
	portalHostname, d := decodeString(data, "portal_hostname", priorModel.PortalHostname)
	diags.Append(d...)
	customIP, d := decodeString(data, "custom_ip", priorModel.CustomIP)
	diags.Append(d...)
	ecEnabled, d := decodeBool(data, "ec_enabled", priorModel.EcEnabled)
	diags.Append(d...)
	expire, d := decodeString(data, "expire", priorModel.Expire)
	diags.Append(d...)
	expireNumber, d := decodeInt64(data, "expire_number", priorModel.ExpireNumber)
	diags.Append(d...)
	expireUnit, d := decodeInt64(data, "expire_unit", priorModel.ExpireUnit)
	diags.Append(d...)
	redirectEnabled, d := decodeBool(data, "redirect_enabled", priorModel.RedirectEnabled)
	diags.Append(d...)
	redirectURL, d := decodeString(data, "redirect_url", priorModel.RedirectUrl)
	diags.Append(d...)
	redirectToHTTPS, d := decodeBool(data, "redirect_to_https", priorModel.RedirectToHttps)
	diags.Append(d...)
	redirectHTTPS, d := decodeBool(data, "redirect_https", priorModel.RedirectHttps)
	diags.Append(d...)
	allowedSubnet, d := decodeString(data, "allowed_subnet_", priorModel.AllowedSubnet)
	diags.Append(d...)
	restrictedSubnet, d := decodeString(data, "restricted_subnet_", priorModel.RestrictedSubnet)
	diags.Append(d...)
	restrictedDNSEnabled, d := decodeBool(data, "restricted_dns_enabled", priorModel.RestrictedDNSEnabled)
	diags.Append(d...)
	restrictedDNSServers, d := decodeStringList(ctx, data, "restricted_dns_servers", priorModel.RestrictedDNSServers)
	diags.Append(d...)
	passwordEnabled, d := decodeBool(data, "password_enabled", priorModel.PasswordEnabled)
	diags.Append(d...)
	voucherEnabled, d := decodeBool(data, "voucher_enabled", priorModel.VoucherEnabled)
	diags.Append(d...)
	radiusEnabled, d := decodeBool(data, "radius_enabled", priorModel.RADIUSEnabled)
	diags.Append(d...)
	radiusProfileID, d := decodeString(data, "radiusprofile_id", priorModel.RADIUSProfileID)
	diags.Append(d...)
	radiusAuthType, d := decodeString(data, "radius_auth_type", priorModel.RADIUSAuthType)
	diags.Append(d...)
	radiusDisconnectEnabled, d := decodeBool(data, "radius_disconnect_enabled", priorModel.RADIUSDisconnectEnabled)
	diags.Append(d...)
	radiusDisconnectPort, d := decodeInt64(data, "radius_disconnect_port", priorModel.RADIUSDisconnectPort)
	diags.Append(d...)
	facebookEnabled, d := decodeBool(data, "facebook_enabled", priorModel.FacebookEnabled)
	diags.Append(d...)
	facebookAppID, d := decodeString(data, "facebook_app_id", priorModel.FacebookAppID)
	diags.Append(d...)
	googleEnabled, d := decodeBool(data, "google_enabled", priorModel.GoogleEnabled)
	diags.Append(d...)
	googleClientID, d := decodeString(data, "google_client_id", priorModel.GoogleClientID)
	diags.Append(d...)
	wechatEnabled, d := decodeBool(data, "wechat_enabled", priorModel.WechatEnabled)
	diags.Append(d...)
	wechatAppID, d := decodeString(data, "wechat_app_id", priorModel.WechatAppID)
	diags.Append(d...)
	paymentEnabled, d := decodeBool(data, "payment_enabled", priorModel.PaymentEnabled)
	diags.Append(d...)
	gateway, d := decodeString(data, "gateway", priorModel.Gateway)
	diags.Append(d...)
	paypalUseSandbox, d := decodeBool(data, "paypal_use_sandbox", priorModel.PaypalUseSandbox)
	diags.Append(d...)
	authorizeUseSandbox, d := decodeBool(data, "authorize_use_sandbox", priorModel.AuthorizeUseSandbox)
	diags.Append(d...)
	quickpayTestmode, d := decodeBool(data, "quickpay_testmode", priorModel.QuickpayTestmode)
	diags.Append(d...)
	merchantwarriorUseSandbox, d := decodeBool(data, "merchantwarrior_use_sandbox", priorModel.MerchantwarriorUseSandbox)
	diags.Append(d...)
	ippayUseSandbox, d := decodeBool(data, "ippay_use_sandbox", priorModel.IPpayUseSandbox)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingGuestAccessModel{
		Auth:                      auth,
		AuthUrl:                   authURL,
		PortalEnabled:             portalEnabled,
		PortalUseHostname:         portalUseHostname,
		PortalHostname:            portalHostname,
		CustomIP:                  customIP,
		EcEnabled:                 ecEnabled,
		Expire:                    expire,
		ExpireNumber:              expireNumber,
		ExpireUnit:                expireUnit,
		RedirectEnabled:           redirectEnabled,
		RedirectUrl:               redirectURL,
		RedirectToHttps:           redirectToHTTPS,
		RedirectHttps:             redirectHTTPS,
		AllowedSubnet:             allowedSubnet,
		RestrictedSubnet:          restrictedSubnet,
		RestrictedDNSEnabled:      restrictedDNSEnabled,
		RestrictedDNSServers:      restrictedDNSServers,
		PasswordEnabled:           passwordEnabled,
		VoucherEnabled:            voucherEnabled,
		RADIUSEnabled:             radiusEnabled,
		RADIUSProfileID:           radiusProfileID,
		RADIUSAuthType:            radiusAuthType,
		RADIUSDisconnectEnabled:   radiusDisconnectEnabled,
		RADIUSDisconnectPort:      radiusDisconnectPort,
		FacebookEnabled:           facebookEnabled,
		FacebookAppID:             facebookAppID,
		GoogleEnabled:             googleEnabled,
		GoogleClientID:            googleClientID,
		WechatEnabled:             wechatEnabled,
		WechatAppID:               wechatAppID,
		PaymentEnabled:            paymentEnabled,
		Gateway:                   gateway,
		PaypalUseSandbox:          paypalUseSandbox,
		AuthorizeUseSandbox:       authorizeUseSandbox,
		QuickpayTestmode:          quickpayTestmode,
		MerchantwarriorUseSandbox: merchantwarriorUseSandbox,
		IPpayUseSandbox:           ippayUseSandbox,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, guestAccessAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.GuestAccess = obj
	return diags
}

// overlay computes the "guest_access" PUT body from model.GuestAccess,
// starting from a deep copy of the snapshot's current section data so all 41
// unmodeled (preserved) fields survive the merge untouched (RMW). Returns
// configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (s guestAccessSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.GuestAccess.IsNull() || model.GuestAccess.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingGuestAccessModel
	diags.Append(model.GuestAccess.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "auth", m.Auth)
	overlayString(base, "auth_url", m.AuthUrl)
	overlayBool(base, "portal_enabled", m.PortalEnabled)
	overlayBool(base, "portal_use_hostname", m.PortalUseHostname)
	overlayString(base, "portal_hostname", m.PortalHostname)
	overlayString(base, "custom_ip", m.CustomIP)
	overlayBool(base, "ec_enabled", m.EcEnabled)
	overlayString(base, "expire", m.Expire)
	overlayInt64(base, "expire_number", m.ExpireNumber)
	overlayInt64(base, "expire_unit", m.ExpireUnit)
	overlayBool(base, "redirect_enabled", m.RedirectEnabled)
	overlayString(base, "redirect_url", m.RedirectUrl)
	overlayBool(base, "redirect_to_https", m.RedirectToHttps)
	overlayBool(base, "redirect_https", m.RedirectHttps)
	overlayString(base, "allowed_subnet_", m.AllowedSubnet)
	overlayString(base, "restricted_subnet_", m.RestrictedSubnet)
	overlayBool(base, "restricted_dns_enabled", m.RestrictedDNSEnabled)
	diags.Append(overlayStringList(ctx, base, "restricted_dns_servers", m.RestrictedDNSServers)...)
	overlayBool(base, "password_enabled", m.PasswordEnabled)
	overlayBool(base, "voucher_enabled", m.VoucherEnabled)
	overlayBool(base, "radius_enabled", m.RADIUSEnabled)
	overlayString(base, "radiusprofile_id", m.RADIUSProfileID)
	overlayString(base, "radius_auth_type", m.RADIUSAuthType)
	overlayBool(base, "radius_disconnect_enabled", m.RADIUSDisconnectEnabled)
	overlayInt64(base, "radius_disconnect_port", m.RADIUSDisconnectPort)
	overlayBool(base, "facebook_enabled", m.FacebookEnabled)
	overlayString(base, "facebook_app_id", m.FacebookAppID)
	overlayBool(base, "google_enabled", m.GoogleEnabled)
	overlayString(base, "google_client_id", m.GoogleClientID)
	overlayBool(base, "wechat_enabled", m.WechatEnabled)
	overlayString(base, "wechat_app_id", m.WechatAppID)
	overlayBool(base, "payment_enabled", m.PaymentEnabled)
	overlayString(base, "gateway", m.Gateway)
	overlayBool(base, "paypal_use_sandbox", m.PaypalUseSandbox)
	overlayBool(base, "authorize_use_sandbox", m.AuthorizeUseSandbox)
	overlayBool(base, "quickpay_testmode", m.QuickpayTestmode)
	overlayBool(base, "merchantwarrior_use_sandbox", m.MerchantwarriorUseSandbox)
	overlayBool(base, "ippay_use_sandbox", m.IPpayUseSandbox)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's guest_access value onto dst. This
// section holds no secret leaves yet (added in a follow-up commit, which
// replaces this with a carryGuestAccessSecrets call), so for now it is a
// straight copy with no per-leaf plan/prior choice needed.
func (guestAccessSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.GuestAccess = plan.GuestAccess
	return nil
}

// isConfigured reports whether m.GuestAccess is set (non-null, non-unknown),
// gating whether Create/Update push this section to the controller at all.
func (guestAccessSection) isConfigured(m settingResourceModel) bool {
	return !m.GuestAccess.IsNull() && !m.GuestAccess.IsUnknown()
}
