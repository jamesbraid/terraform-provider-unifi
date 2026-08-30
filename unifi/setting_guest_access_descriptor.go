package unifi

// The guest_access section descriptor: an unconditional-mirror hydration
// whose only specials are the #303 write-side OmitZero guards on
// expire_number, expire_unit and radius_disconnect_port -- the reason
// lcm's brightness/idle_timeout and syslog's port/netconsole_port carry the
// same pair (see setting_lcm_descriptor.go, setting_syslog_descriptor.go) --
// plus, since Task 3, the plan-conditioned nulling every one of the
// section's 18 x_-prefixed fields needs, the same shape radiusAfterReceive
// and snmpAfterReceive apply to their own secrets. Replaces nothing
// hand-written: guest_access never had a legacy writeGuestAccessSection /
// readGuestAccessSection, so this is new rather than a migration. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.
//
// This is a five-task rollout (.superpowers/sdd/plan-r2b-guest-access).
// Task 2 modelled the 21 core scalars -- portal access and mode, post-auth
// redirect, session and voucher expiry, the RADIUS guest-auth group without
// secrets, password_enabled without its secret, voucher_enabled,
// payment_enabled, gateway and ec_enabled. settings.GuestAccess carries 92
// fields total (unifi/setting_guest_access_fieldsplit.go). Task 3 added the
// 18 x_-prefixed secrets. Task 4's brief named 22 more, but two --
// allowed_subnet_ and restricted_subnet_ -- were withdrawn after a live
// apply against the pinned controller (10.6.101) rejected both with
// api.err.InvalidKey; every other field in the brief wrote cleanly. See this
// file's own note below and setting_guest_access_descriptor_test.go's
// TestGuestAccessNetworkScopingSocialLoginPaymentAndStragglersRoundTrip for
// where that live check happened. So Task 4 actually added 20: the
// facebook_*, google_* and wechat_* non-secret companions of Task 3's
// social-login secrets (10); authorize_use_sandbox, ippay_use_sandbox,
// merchantwarrior_use_sandbox, paypal_use_sandbox and quickpay_testmode (the
// payment-gateway sandbox/test switches beside Task 3's credentials);
// restricted_dns_enabled and restricted_dns_servers (network scoping, minus
// the two withdrawn subnet fields); and three stragglers -- auth_url,
// custom_ip (both under portal access and mode, missed by Task 2's own
// brief) and voucher_customized. That totals 59 of the 92; the remaining 33
// -- every portal_customized_* field (31, Task 5's) plus the two withdrawn
// subnet fields -- are still named in provider-codegen/policy/setting.json's
// top-level "omitted" list as "GuestAccess.<field>" rather than as omitted
// members of this grouping, so Task 5's own diff is exactly "move its fields
// from omitted to managed" rather than a rewrite of this file's member list.
// See that policy file's "guest_access" grouping.
//
// Task 4's own notes: allowed_subnet_ and restricted_subnet_'s withdrawal is
// the first case in this policy corpus where the SDK's own generated struct
// -- itself derived from a captured controller schema -- named a field the
// running controller does not actually accept a write for. Both carry a
// trailing underscore on the wire, unlike every other field this task
// modelled, which is itself a signal (shared with several genuinely
// deprecated UniFi settings) that these predate the controller generation
// pinned here; go-unifi keeps them because its capture lock's schema source
// still lists them, not because this controller honours them. Neither
// field's purpose was ever confirmed against controller documentation
// either, for what that is worth now that both are unmodelled again.
// auth_url and custom_ip are not a hedge any more, but the measured fact is
// asymmetric, not a pair requirement: a live probe against the pinned
// controller found custom_ip alone (auth=custom, auth_url unset) writes
// cleanly, while auth_url alone (auth=custom, custom_ip unset) is rejected
// with api.err.CustomAuthMissingExternalServer -- so custom_ip is what auth
// = custom actually requires; auth_url carries no requirement of its own,
// it is simply meaningless (and silently discarded, not rejected) outside
// auth = custom. Each field's own shipped description now states its own
// half of this, not a shared "required together" claim the controller
// never enforced. wechat_shop_id's and voucher_customized's purpose is
// still not confirmed against controller documentation; each hedges in its
// own shipped description exactly the way ec_enabled's already does, not
// just in this comment.
// restricted_dns_servers is this policy corpus's
// first per-element-validated string collection: policy/setting.json
// composes listvalidator.ValueStringsAre with controllerregex.Matches, the
// same composition site_to_site_vpn's remote_subnets and firewall_policy's
// connection_states already use with their own per-element validators, over
// the exact IP-address pattern SettingGuestAccess's constraint table gives
// restricted_dns_servers -- the same pattern custom_ip carries as a scalar.
// It ships as a list, not a set: no shipped section had made this call yet
// for a plain string collection, and doh's server_names is the nearest
// precedent, list over set, list plan modifier included. None of Task 4's
// 20 fields are nullable integers, so the #303 OmitZero guard below
// (expire_number, expire_unit, radius_disconnect_port) has nothing new to
// extend to.
//
// Three fields carry a fact worth flagging rather than assuming. The plan's
// "Known risks" names a claim for ec_enabled inherited from an abandoned
// prior design -- that it is the guest portal's TLS crypto-mode flag -- but
// that is not what this file ships: the SDK's own field carries no
// comment, and "express checkout" (below) is this task's own inference
// from the field name and its position beside payment_enabled/gateway, not
// a controller-documented fact either. Since the description states it as
// settled where a practitioner would read and act on it, the description
// itself hedges too, not just this comment. radiusprofile_id is a
// cross-resource reference into unifi_radius_profile, unrelated to this
// resource's own "radius" section despite the shared name; the provider
// does not check the ID exists, matching every other cross-resource
// reference in this codebase. A third, smaller one: redirect_https and
// redirect_to_https are two distinct SDK fields with adjacent names and no
// controller documentation distinguishing them: this file assumes the
// former governs the post-auth redirect target and the latter forces the
// portal page itself to HTTPS, purely from the field names, and echoes
// that same assumption in each one's description.
//
// auth, expire, expire_unit, gateway, portal_hostname and radius_auth_type
// carry a validator the compiler derives from the SDK's own constraint
// table (SettingGuestAccess in go-unifi's settings/validation.generated.go)
// -- see internal/providercompiler/derive_validators.go. None of the six is
// hand-written in policy/setting.json; the compiler's own redundancy gate
// refuses generation if a hand validator duplicates a derived one.
// expire_number and radius_disconnect_port carry a constraint-table entry
// too (a pattern for the former, Min/Max bounds for the latter) but get no
// derived validator: the deriver only emits int64validator.OneOf from an
// enumerated value set, never int64validator.Between from bounds, and never
// translates a pattern onto an Int64Attribute at all (regex derivation is
// string-only). That gap is a compiler limitation, not something this task
// works around -- both fields are still write-safe via OmitZero below,
// which is a wire-level guard independent of any schema-level validator.
//
// Task 3 adds the section's 18 x_-prefixed fields (guestAccessSecret in
// setting_guest_access_fieldsplit.go): a portal password, four social-login
// secrets, and thirteen payment-gateway credentials/identifiers across six
// gateways. Task 0's live-controller probe
// (.superpowers/sdd/plan-r2b-guest-access/task-0-report.md) found every one
// of the 18 echoed back verbatim on read -- no mask, no hash, no truncation,
// no absence -- and that the six identifier-shaped fields among them (e.g.
// x_paypal_username) behave identically to the twelve genuine credentials.
// So there is no per-field split: all 18 are Optional+Computed+Sensitive
// StringFields, and guestAccessAfterReceive (below) nulls each one only when
// the section was never configured for it, the same rule radiusAfterReceive
// and snmpAfterReceive apply to secret/community/password. Unlike those two,
// none of the 18 carries any entry at all in SettingGuestAccess's own
// constraint table (verified against go-unifi's
// settings/validation.generated.go: the table's 25 entries for this section
// are all non-secret fields), so each one's Elide is KeepZero rather than
// NullZero -- resourcekit.ElideProblems' zeroIsRejected only wants NullZero
// when a validator would reject "", and these 18 have no validator to reject
// anything. That is also why an explicit empty string is a legal write for
// every one of them: nothing here enforces a minimum length. The 13 non-secret
// fields that share a gateway or social-login provider with these 18 (the
// *_enabled/*_id companions, the *_use_sandbox/testmode bools, auth_url and
// custom_ip) are deliberately left in policy/setting.json's omitted list --
// this task's scope is exactly guestAccessSecret's 18 fields, not the wider
// per-provider groups the original plan sketch (before Task 0's ruling
// collapsed 3a/3b/3c into one dispatch) sized around.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingGuestAccessModel is guest_access's own section model, decoded out
// of settingResourceModel.GuestAccess. Named and ordered to match the
// generated schema's own attribute order (alphabetical by terraform_name),
// the same convention every other section descriptor in this package
// follows.
type settingGuestAccessModel struct {
	Auth                         types.String `tfsdk:"auth"`
	AuthUrl                      types.String `tfsdk:"auth_url"`
	AuthorizeLoginid             types.String `tfsdk:"authorize_loginid"`
	AuthorizeTransactionkey      types.String `tfsdk:"authorize_transactionkey"`
	AuthorizeUseSandbox          types.Bool   `tfsdk:"authorize_use_sandbox"`
	CustomIP                     types.String `tfsdk:"custom_ip"`
	EcEnabled                    types.Bool   `tfsdk:"ec_enabled"`
	Expire                       types.String `tfsdk:"expire"`
	ExpireNumber                 types.Int64  `tfsdk:"expire_number"`
	ExpireUnit                   types.Int64  `tfsdk:"expire_unit"`
	FacebookAppID                types.String `tfsdk:"facebook_app_id"`
	FacebookAppSecret            types.String `tfsdk:"facebook_app_secret"`
	FacebookEnabled              types.Bool   `tfsdk:"facebook_enabled"`
	FacebookScopeEmail           types.Bool   `tfsdk:"facebook_scope_email"`
	Gateway                      types.String `tfsdk:"gateway"`
	GoogleClientID               types.String `tfsdk:"google_client_id"`
	GoogleClientSecret           types.String `tfsdk:"google_client_secret"`
	GoogleDomain                 types.String `tfsdk:"google_domain"`
	GoogleEnabled                types.Bool   `tfsdk:"google_enabled"`
	GoogleScopeEmail             types.Bool   `tfsdk:"google_scope_email"`
	IPpayTerminalid              types.String `tfsdk:"ippay_terminalid"`
	IPpayUseSandbox              types.Bool   `tfsdk:"ippay_use_sandbox"`
	MerchantwarriorApikey        types.String `tfsdk:"merchantwarrior_apikey"`
	MerchantwarriorApipassphrase types.String `tfsdk:"merchantwarrior_apipassphrase"`
	MerchantwarriorMerchantuuid  types.String `tfsdk:"merchantwarrior_merchantuuid"`
	MerchantwarriorUseSandbox    types.Bool   `tfsdk:"merchantwarrior_use_sandbox"`
	Password                     types.String `tfsdk:"password"`
	PasswordEnabled              types.Bool   `tfsdk:"password_enabled"`
	PaymentEnabled               types.Bool   `tfsdk:"payment_enabled"`
	PaypalPassword               types.String `tfsdk:"paypal_password"`
	PaypalSignature              types.String `tfsdk:"paypal_signature"`
	PaypalUseSandbox             types.Bool   `tfsdk:"paypal_use_sandbox"`
	PaypalUsername               types.String `tfsdk:"paypal_username"`
	PortalEnabled                types.Bool   `tfsdk:"portal_enabled"`
	PortalHostname               types.String `tfsdk:"portal_hostname"`
	PortalUseHostname            types.Bool   `tfsdk:"portal_use_hostname"`
	QuickpayAgreementid          types.String `tfsdk:"quickpay_agreementid"`
	QuickpayApikey               types.String `tfsdk:"quickpay_apikey"`
	QuickpayMerchantid           types.String `tfsdk:"quickpay_merchantid"`
	QuickpayTestmode             types.Bool   `tfsdk:"quickpay_testmode"`
	RADIUSAuthType               types.String `tfsdk:"radius_auth_type"`
	RADIUSDisconnectEnabled      types.Bool   `tfsdk:"radius_disconnect_enabled"`
	RADIUSDisconnectPort         types.Int64  `tfsdk:"radius_disconnect_port"`
	RADIUSEnabled                types.Bool   `tfsdk:"radius_enabled"`
	RADIUSProfileID              types.String `tfsdk:"radiusprofile_id"`
	RedirectEnabled              types.Bool   `tfsdk:"redirect_enabled"`
	RedirectHttps                types.Bool   `tfsdk:"redirect_https"`
	RedirectToHttps              types.Bool   `tfsdk:"redirect_to_https"`
	RedirectUrl                  types.String `tfsdk:"redirect_url"`
	RestrictedDNSEnabled         types.Bool   `tfsdk:"restricted_dns_enabled"`
	RestrictedDNSServers         types.List   `tfsdk:"restricted_dns_servers"`
	StripeApiKey                 types.String `tfsdk:"stripe_api_key"`
	VoucherCustomized            types.Bool   `tfsdk:"voucher_customized"`
	VoucherEnabled               types.Bool   `tfsdk:"voucher_enabled"`
	WechatAppID                  types.String `tfsdk:"wechat_app_id"`
	WechatAppSecret              types.String `tfsdk:"wechat_app_secret"`
	WechatEnabled                types.Bool   `tfsdk:"wechat_enabled"`
	WechatSecretKey              types.String `tfsdk:"wechat_secret_key"`
	WechatShopID                 types.String `tfsdk:"wechat_shop_id"`
}

// guestAccessAttrTypes types guest_access's own object in state; it must
// match the generated schema exactly.
var guestAccessAttrTypes = map[string]attr.Type{
	"auth":                          types.StringType,
	"auth_url":                      types.StringType,
	"authorize_loginid":             types.StringType,
	"authorize_transactionkey":      types.StringType,
	"authorize_use_sandbox":         types.BoolType,
	"custom_ip":                     types.StringType,
	"ec_enabled":                    types.BoolType,
	"expire":                        types.StringType,
	"expire_number":                 types.Int64Type,
	"expire_unit":                   types.Int64Type,
	"facebook_app_id":               types.StringType,
	"facebook_app_secret":           types.StringType,
	"facebook_enabled":              types.BoolType,
	"facebook_scope_email":          types.BoolType,
	"gateway":                       types.StringType,
	"google_client_id":              types.StringType,
	"google_client_secret":          types.StringType,
	"google_domain":                 types.StringType,
	"google_enabled":                types.BoolType,
	"google_scope_email":            types.BoolType,
	"ippay_terminalid":              types.StringType,
	"ippay_use_sandbox":             types.BoolType,
	"merchantwarrior_apikey":        types.StringType,
	"merchantwarrior_apipassphrase": types.StringType,
	"merchantwarrior_merchantuuid":  types.StringType,
	"merchantwarrior_use_sandbox":   types.BoolType,
	"password":                      types.StringType,
	"password_enabled":              types.BoolType,
	"payment_enabled":               types.BoolType,
	"paypal_password":               types.StringType,
	"paypal_signature":              types.StringType,
	"paypal_use_sandbox":            types.BoolType,
	"paypal_username":               types.StringType,
	"portal_enabled":                types.BoolType,
	"portal_hostname":               types.StringType,
	"portal_use_hostname":           types.BoolType,
	"quickpay_agreementid":          types.StringType,
	"quickpay_apikey":               types.StringType,
	"quickpay_merchantid":           types.StringType,
	"quickpay_testmode":             types.BoolType,
	"radius_auth_type":              types.StringType,
	"radius_disconnect_enabled":     types.BoolType,
	"radius_disconnect_port":        types.Int64Type,
	"radius_enabled":                types.BoolType,
	"radiusprofile_id":              types.StringType,
	"redirect_enabled":              types.BoolType,
	"redirect_https":                types.BoolType,
	"redirect_to_https":             types.BoolType,
	"redirect_url":                  types.StringType,
	"restricted_dns_enabled":        types.BoolType,
	"restricted_dns_servers":        types.ListType{ElemType: types.StringType},
	"stripe_api_key":                types.StringType,
	"voucher_customized":            types.BoolType,
	"voucher_enabled":               types.BoolType,
	"wechat_app_id":                 types.StringType,
	"wechat_app_secret":             types.StringType,
	"wechat_enabled":                types.BoolType,
	"wechat_secret_key":             types.StringType,
	"wechat_shop_id":                types.StringType,
}

// guestAccessKitSpec maps this section's attributes of the generated
// guest_access schema (resource_setting/setting_resource_gen.go's
// "guest_access" SingleNestedAttribute) onto settings.GuestAccess. Elide
// judgments follow resourcekit.ElideProblems' schema-driven rule: every
// plain string field below is Optional+Computed, and ElideProblems'
// zeroIsRejected runs each attribute's own validators against "" to decide --
// auth, expire, gateway and radius_auth_type each carry a derived
// OneOf/RegexMatches that rejects "", so they want NullZero; portal_hostname's
// derived pattern (^[a-zA-Z0-9.-]+$|^$) explicitly admits "" via its own
// alternation, and radiusprofile_id and redirect_url carry no validator at
// all, so all three want KeepZero. Every bool field carries no Elide at all,
// matching resourcekit's own elideExempt (a false is a value, not an
// absence). expire_number, expire_unit and radius_disconnect_port are
// Optional+Computed Int64 attributes, and zeroIsRejected only ever inspects
// a StringAttribute's validators (an Int64 range or pattern constraint can't
// drive it), so KeepZero is what the check demands for all three -- matching
// lcm's brightness/idle_timeout and syslog's port/netconsole_port. OmitZero
// is the separate, write-side #303 guard: an unknown (unset Optional+Computed)
// value's ValueInt64Pointer() resolves to a pointer to zero, which the
// controller's own validator rejects for all three (expire_number requires a
// leading 1-9 or exactly 1000000, expire_unit is the enum 1/60/1440,
// radius_disconnect_port has a minimum of 1), so a zero must never reach the
// wire.
//
// The 18 x_-prefixed fields (guestAccessSecret) are Optional+Computed+
// Sensitive StringFields with Elide: KeepZero -- not NullZero like
// radius.secret or snmp's community/password, because none of the 18 has
// any entry in SettingGuestAccess's own constraint table (go-unifi's
// settings/validation.generated.go), so none carries a validator that would
// reject "" and make ElideProblems want NullZero. That also means an
// explicit empty string is a legal, distinguishable write for every one of
// them: nothing here enforces a minimum length. The unconfigured case --
// where the field must never surface a controller-held value the
// practitioner never set -- is handled separately by guestAccessAfterReceive
// below, exactly the way radiusAfterReceive and snmpAfterReceive plan-
// condition their own secrets independent of Elide.
//
// Task 4's 20 fields are every one KeepZero, and every one a plain
// Optional+Computed StringField or BoolField (bools carry no Elide at all,
// matching resourcekit's own elideExempt) except restricted_dns_servers, a
// StringListField. 19 of the 20 have no entry in the constraint table, so
// the same "no validator rejects empty" reasoning the 18 secrets get above
// applies unchanged. custom_ip is the exception with an entry: its pattern
// carries a "|^$" alternation that explicitly admits "", the same shape
// portal_hostname's own derived pattern has above, so it wants KeepZero for
// the same reason, not despite having a validator at all. restricted_dns_servers
// carries the identical pattern as a per-element validator (see this file's
// own top comment), but ElideProblems' zeroIsRejected only ever inspects a
// schema.StringAttribute's validators, never a collection's element
// validators, so it always returns false for a ListAttribute regardless of
// what the elements require -- KeepZero is what that check demands here too,
// matching doh's server_names. None of the 20 needed guestAccessAfterReceive's
// treatment: unlike the 18 secrets, nothing here is a credential the
// controller might echo back for a section the practitioner never
// configured, so there is no leak to guard against.
func guestAccessKitSpec() resourcekit.Spec[settingGuestAccessModel, settings.GuestAccess] {
	return resourcekit.Spec[settingGuestAccessModel, settings.GuestAccess]{
		TypeName: "setting_guest_access",
		Subject:  "Guest Access Setting",
		New:      func() *settings.GuestAccess { return &settings.GuestAccess{} },
		Fields: []resourcekit.Field[settingGuestAccessModel, settings.GuestAccess]{
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "auth",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Auth },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Auth },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "auth_url",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.AuthUrl },
				SDK:   func(s *settings.GuestAccess) *string { return &s.AuthUrl },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_authorize_loginid",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.AuthorizeLoginid },
				SDK:   func(s *settings.GuestAccess) *string { return &s.AuthorizeLoginid },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_authorize_transactionkey",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.AuthorizeTransactionkey },
				SDK:   func(s *settings.GuestAccess) *string { return &s.AuthorizeTransactionkey },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "authorize_use_sandbox",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.AuthorizeUseSandbox },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.AuthorizeUseSandbox },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "custom_ip",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.CustomIP },
				SDK:   func(s *settings.GuestAccess) *string { return &s.CustomIP },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "ec_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.EcEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.EcEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "expire",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Expire },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Expire },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:     "expire_number",
				Model:    func(m *settingGuestAccessModel) *types.Int64 { return &m.ExpireNumber },
				SDK:      func(s *settings.GuestAccess) **int64 { return &s.ExpireNumber },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:     "expire_unit",
				Model:    func(m *settingGuestAccessModel) *types.Int64 { return &m.ExpireUnit },
				SDK:      func(s *settings.GuestAccess) **int64 { return &s.ExpireUnit },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "facebook_app_id",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.FacebookAppID },
				SDK:   func(s *settings.GuestAccess) *string { return &s.FacebookAppID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_facebook_app_secret",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.FacebookAppSecret },
				SDK:   func(s *settings.GuestAccess) *string { return &s.FacebookAppSecret },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "facebook_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.FacebookEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.FacebookEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "facebook_scope_email",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.FacebookScopeEmail },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.FacebookScopeEmail },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "gateway",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Gateway },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Gateway },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "google_client_id",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.GoogleClientID },
				SDK:   func(s *settings.GuestAccess) *string { return &s.GoogleClientID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_google_client_secret",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.GoogleClientSecret },
				SDK:   func(s *settings.GuestAccess) *string { return &s.GoogleClientSecret },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "google_domain",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.GoogleDomain },
				SDK:   func(s *settings.GuestAccess) *string { return &s.GoogleDomain },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "google_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.GoogleEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.GoogleEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "google_scope_email",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.GoogleScopeEmail },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.GoogleScopeEmail },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_ippay_terminalid",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.IPpayTerminalid },
				SDK:   func(s *settings.GuestAccess) *string { return &s.IPpayTerminalid },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "ippay_use_sandbox",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.IPpayUseSandbox },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.IPpayUseSandbox },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_merchantwarrior_apikey",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.MerchantwarriorApikey },
				SDK:   func(s *settings.GuestAccess) *string { return &s.MerchantwarriorApikey },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_merchantwarrior_apipassphrase",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.MerchantwarriorApipassphrase },
				SDK:   func(s *settings.GuestAccess) *string { return &s.MerchantwarriorApipassphrase },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_merchantwarrior_merchantuuid",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.MerchantwarriorMerchantuuid },
				SDK:   func(s *settings.GuestAccess) *string { return &s.MerchantwarriorMerchantuuid },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "merchantwarrior_use_sandbox",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.MerchantwarriorUseSandbox },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.MerchantwarriorUseSandbox },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_password",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Password },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Password },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "password_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PasswordEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PasswordEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "payment_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PaymentEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PaymentEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_paypal_password",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.PaypalPassword },
				SDK:   func(s *settings.GuestAccess) *string { return &s.PaypalPassword },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_paypal_signature",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.PaypalSignature },
				SDK:   func(s *settings.GuestAccess) *string { return &s.PaypalSignature },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "paypal_use_sandbox",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PaypalUseSandbox },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PaypalUseSandbox },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_paypal_username",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.PaypalUsername },
				SDK:   func(s *settings.GuestAccess) *string { return &s.PaypalUsername },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "portal_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PortalEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PortalEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "portal_hostname",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.PortalHostname },
				SDK:   func(s *settings.GuestAccess) *string { return &s.PortalHostname },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "portal_use_hostname",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PortalUseHostname },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PortalUseHostname },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_quickpay_agreementid",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.QuickpayAgreementid },
				SDK:   func(s *settings.GuestAccess) *string { return &s.QuickpayAgreementid },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_quickpay_apikey",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.QuickpayApikey },
				SDK:   func(s *settings.GuestAccess) *string { return &s.QuickpayApikey },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_quickpay_merchantid",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.QuickpayMerchantid },
				SDK:   func(s *settings.GuestAccess) *string { return &s.QuickpayMerchantid },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "quickpay_testmode",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.QuickpayTestmode },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.QuickpayTestmode },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radius_auth_type",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.RADIUSAuthType },
				SDK:   func(s *settings.GuestAccess) *string { return &s.RADIUSAuthType },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radius_disconnect_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RADIUSDisconnectEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RADIUSDisconnectEnabled },
			},
			resourcekit.Int64PtrField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:     "radius_disconnect_port",
				Model:    func(m *settingGuestAccessModel) *types.Int64 { return &m.RADIUSDisconnectPort },
				SDK:      func(s *settings.GuestAccess) **int64 { return &s.RADIUSDisconnectPort },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radius_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RADIUSEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RADIUSEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radiusprofile_id",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.RADIUSProfileID },
				SDK:   func(s *settings.GuestAccess) *string { return &s.RADIUSProfileID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RedirectEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RedirectEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_https",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RedirectHttps },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RedirectHttps },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_to_https",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RedirectToHttps },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RedirectToHttps },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_url",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.RedirectUrl },
				SDK:   func(s *settings.GuestAccess) *string { return &s.RedirectUrl },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "restricted_dns_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RestrictedDNSEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RestrictedDNSEnabled },
			},
			resourcekit.StringListField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "restricted_dns_servers",
				Model: func(m *settingGuestAccessModel) *types.List { return &m.RestrictedDNSServers },
				SDK:   func(s *settings.GuestAccess) *[]string { return &s.RestrictedDNSServers },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_stripe_api_key",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.StripeApiKey },
				SDK:   func(s *settings.GuestAccess) *string { return &s.StripeApiKey },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "voucher_customized",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.VoucherCustomized },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.VoucherCustomized },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "voucher_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.VoucherEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.VoucherEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "wechat_app_id",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.WechatAppID },
				SDK:   func(s *settings.GuestAccess) *string { return &s.WechatAppID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_wechat_app_secret",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.WechatAppSecret },
				SDK:   func(s *settings.GuestAccess) *string { return &s.WechatAppSecret },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "wechat_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.WechatEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.WechatEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "x_wechat_secret_key",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.WechatSecretKey },
				SDK:   func(s *settings.GuestAccess) *string { return &s.WechatSecretKey },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "wechat_shop_id",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.WechatShopID },
				SDK:   func(s *settings.GuestAccess) *string { return &s.WechatShopID },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// guestAccessAfterReceive plan-conditions this section's 18 x_-prefixed
// fields exactly the way radiusAfterReceive and snmpAfterReceive plan-
// condition their own secrets: a field the plan (on write) or the prior
// state (on read) never named comes back null, no matter what the
// controller echoed for it, so an unconfigured credential can never land in
// state. A named field surfaces whatever Spec.ToModel already decoded off
// the wire -- the controller's own echo, pinned verbatim by Task 0's live
// probe -- not the value the practitioner typed, mirroring
// radiusAfterReceive's own comment on why that distinction matters. None of
// the section's non-secret fields need this treatment: they have no
// controller-echo hazard to guard against.
func guestAccessAfterReceive(
	_ context.Context, _ *settings.GuestAccess, model *settingGuestAccessModel, prior settingGuestAccessModel,
) diag.Diagnostics {
	if prior.AuthorizeLoginid.IsNull() || prior.AuthorizeLoginid.IsUnknown() {
		model.AuthorizeLoginid = types.StringNull()
	}
	if prior.AuthorizeTransactionkey.IsNull() || prior.AuthorizeTransactionkey.IsUnknown() {
		model.AuthorizeTransactionkey = types.StringNull()
	}
	if prior.FacebookAppSecret.IsNull() || prior.FacebookAppSecret.IsUnknown() {
		model.FacebookAppSecret = types.StringNull()
	}
	if prior.GoogleClientSecret.IsNull() || prior.GoogleClientSecret.IsUnknown() {
		model.GoogleClientSecret = types.StringNull()
	}
	if prior.IPpayTerminalid.IsNull() || prior.IPpayTerminalid.IsUnknown() {
		model.IPpayTerminalid = types.StringNull()
	}
	if prior.MerchantwarriorApikey.IsNull() || prior.MerchantwarriorApikey.IsUnknown() {
		model.MerchantwarriorApikey = types.StringNull()
	}
	if prior.MerchantwarriorApipassphrase.IsNull() || prior.MerchantwarriorApipassphrase.IsUnknown() {
		model.MerchantwarriorApipassphrase = types.StringNull()
	}
	if prior.MerchantwarriorMerchantuuid.IsNull() || prior.MerchantwarriorMerchantuuid.IsUnknown() {
		model.MerchantwarriorMerchantuuid = types.StringNull()
	}
	if prior.Password.IsNull() || prior.Password.IsUnknown() {
		model.Password = types.StringNull()
	}
	if prior.PaypalPassword.IsNull() || prior.PaypalPassword.IsUnknown() {
		model.PaypalPassword = types.StringNull()
	}
	if prior.PaypalSignature.IsNull() || prior.PaypalSignature.IsUnknown() {
		model.PaypalSignature = types.StringNull()
	}
	if prior.PaypalUsername.IsNull() || prior.PaypalUsername.IsUnknown() {
		model.PaypalUsername = types.StringNull()
	}
	if prior.QuickpayAgreementid.IsNull() || prior.QuickpayAgreementid.IsUnknown() {
		model.QuickpayAgreementid = types.StringNull()
	}
	if prior.QuickpayApikey.IsNull() || prior.QuickpayApikey.IsUnknown() {
		model.QuickpayApikey = types.StringNull()
	}
	if prior.QuickpayMerchantid.IsNull() || prior.QuickpayMerchantid.IsUnknown() {
		model.QuickpayMerchantid = types.StringNull()
	}
	if prior.StripeApiKey.IsNull() || prior.StripeApiKey.IsUnknown() {
		model.StripeApiKey = types.StringNull()
	}
	if prior.WechatAppSecret.IsNull() || prior.WechatAppSecret.IsUnknown() {
		model.WechatAppSecret = types.StringNull()
	}
	if prior.WechatSecretKey.IsNull() || prior.WechatSecretKey.IsUnknown() {
		model.WechatSecretKey = types.StringNull()
	}
	return nil
}

// guestAccessNestedSchema is the guest_access SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance checks
// -- built for a whole resource's top-level schema -- can run against one
// section of unifi_setting instead.
func guestAccessNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	guestAccess := built.Attributes["guest_access"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // guest_access is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: guestAccess.Attributes}
}

// guestAccessKitBackend binds guestAccessKitSpec to a client: Read is
// GetSetting[*GuestAccess], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set instead of a read-modify-write
// whole-document PUT.
func guestAccessKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GuestAccess] {
	return resourcekit.Backend[settings.GuestAccess]{
		Read: func(ctx context.Context, site, _ string) (*settings.GuestAccess, error) {
			_, guestAccess, err := ui.GetSetting[*settings.GuestAccess](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return guestAccess, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GuestAccess, fields ...string,
		) (*settings.GuestAccess, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// guestAccessKitSection builds the guest_access entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func guestAccessKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := guestAccessKitSpec()
	spec.Backend = guestAccessKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGuestAccessModel, settings.GuestAccess]{
		SectionName:  "guest_access",
		Get:          func(m *settingResourceModel) *types.Object { return &m.GuestAccess },
		Set:          func(m *settingResourceModel, o types.Object) { m.GuestAccess = o },
		AttrTypes:    guestAccessAttrTypes,
		Spec:         spec,
		AfterReceive: guestAccessAfterReceive,
	}
}
