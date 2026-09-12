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
// brief) and voucher_customized. That totals 59 of the 92. Task 5 added the
// remaining 31 portal_customized_* fields, leaving only allowed_subnet_ and
// restricted_subnet_ (the two withdrawn subnet fields) in
// provider-codegen/policy/setting.json's top-level "omitted" list as
// "GuestAccess.<field>" -- Task 5's own diff was exactly "move its 31 fields
// from omitted to managed" rather than a rewrite of this file's member list.
// See that policy file's "guest_access" grouping. Task 5's own brief also
// named five already-shipped fields (facebook_scope_email,
// google_scope_email, google_domain, wechat_shop_id, voucher_customized) as
// part of its scope; all five were already managed by Task 4 above, so
// Task 5's actual diff is exactly the 31 portal_customized_* fields, not 36.
//
// Task 5's 31 portal_customized_* fields are 8 hex-colour strings, 3 enum
// strings, 10 free-text strings, 6 bools, 1 string list
// (portal_customized_languages) and 3 nullable integers. All 8 hex-colour
// fields and all 3 enum fields carry a derived validator: the hex pattern
// (`^#[a-zA-Z0-9]{6}$|^#[a-zA-Z0-9]{3}$|^$`) admits an empty string via its
// own alternation, so those 8 want KeepZero; the 3 enums
// (portal_customized_bg_type, portal_customized_logo_position,
// portal_customized_welcome_text_position) have no empty option in their
// value set, so they want NullZero. None of the 10 free-text fields or 6
// bools carries a constraint-table entry, matching every other
// non-validated field in this section. portal_customized_languages follows
// restricted_dns_servers' own precedent (Task 4, above): a per-element
// pattern (`^[a-z]{2}([_-][a-zA-Z]{2,4})*$`) hand-composed as
// listvalidator.ValueStringsAre(controllerregex.Matches(...)) in
// policy/setting.json, because the compiler's deriver is scoped to scalar
// attributes (internal/providercompiler/compile.go) and never reaches a
// collection's element type.
//
// The 3 nullable integers do not behave alike, and this is the one fact in
// the group nothing but a direct check of SettingGuestAccess's constraint
// table would catch: portal_customized_box_opacity's pattern
// (`^[1-9][0-9]?$|^100$|^$`) and portal_customized_logo_size's pattern
// (`6[4-9]|[7-9][0-9]|1[0-8][0-9]|19[0-2]`) both reject a literal "0", so
// both set OmitZero -- the same #303 write-side guard as expire_number,
// expire_unit and radius_disconnect_port above. portal_customized_box_radius's
// pattern (`[0-9]|[1-4][0-9]|50`) accepts "0" (its Min is 0, not 1), so it
// sets no OmitZero at all: eliding its zero would silently discard a
// practitioner's explicit "square corners" with no error at any layer, the
// value would simply never reach the controller. All three still carry
// Elide: KeepZero, matching every other Int64PtrField in this section:
// resourcekit.ElideProblems' zeroIsRejected only ever inspects a
// schema.StringAttribute's validators, so it can't drive NullZero for an
// Int64Attribute regardless of what its own pattern says -- setting NullZero
// on any of the three fails
// TestEverySettingSectionPassesTheConformanceInstruments/guest_access's own
// ElideProblems check. Like box_opacity and logo_size, all three get no
// derived validator either (int64validator.Between(1, 100),
// int64validator.Between(0, 50) and int64validator.Between(64, 192) are
// hand-written in policy/setting.json instead), following the same
// established precedent as lcm's brightness and idle_timeout
// (setting_lcm_descriptor.go) -- a hand int64validator.Between never
// conflicts with the compiler's redundancy gate, which only refuses a hand
// validator of the same *kind* the deriver would also produce
// (int64validator.OneOf from an enumerated value set, never .Between from
// bounds).
//
// TestGuestAccessPortalAppearanceOmitsAZeroTheControllerRejects pins all
// three: an explicit 0 (and a null or unknown plan value) never reaches the
// wire for box_opacity or logo_size, and an explicit 0 always does for
// box_radius. See that test's own comment for why box_radius's *unknown*
// case still writes a literal 0 into the SDK struct (Int64PtrField's own
// ToSDK behaviour absent OmitZero) without that ever reaching the
// controller: SetInPlan reports false for both null and unknown, so the
// masked write's field list leaves portal_customized_box_radius out
// regardless of what sits in the struct.
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

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

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
		Fields:   settingGuestAccessGenFields(),
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
	if prior.IppayTerminalid.IsNull() || prior.IppayTerminalid.IsUnknown() {
		model.IppayTerminalid = types.StringNull()
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
