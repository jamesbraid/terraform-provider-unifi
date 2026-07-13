package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestGuestAccessFieldSplit pins the spec's modeled/secret/preserved split
// against the live go-unifi struct so a future go-unifi bump that adds,
// removes, or renames a GuestAccess field fails here first, not as a
// silent schema gap. See
// docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md
// (rev. 2: 56 modeled / 18 secret / 41 preserved).
func TestGuestAccessFieldSplit(t *testing.T) {
	typ := reflect.TypeOf(settings.GuestAccess{})
	all := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous { // BaseSetting
			continue
		}
		all[f.Name] = true
	}
	if len(all) != 97 {
		t.Fatalf("settings.GuestAccess has %d fields, want 97 — go-unifi drifted; reconcile the spec before proceeding", len(all))
	}

	// wantModeled: the 56 Go field names from the spec's rev. 2 "Modeled"
	// table, including the four rev. 2 promotions (AuthUrl, EcEnabled,
	// CustomIP, RedirectHttps).
	wantModeled := map[string]bool{
		"Auth":                         true,
		"AuthUrl":                      true,
		"PortalEnabled":                true,
		"PortalUseHostname":            true,
		"PortalHostname":               true,
		"CustomIP":                     true,
		"EcEnabled":                    true,
		"Expire":                       true,
		"ExpireNumber":                 true,
		"ExpireUnit":                   true,
		"RedirectEnabled":              true,
		"RedirectUrl":                  true,
		"RedirectToHttps":              true,
		"RedirectHttps":                true,
		"AllowedSubnet":                true,
		"RestrictedSubnet":             true,
		"RestrictedDNSEnabled":         true,
		"RestrictedDNSServers":         true,
		"PasswordEnabled":              true,
		"Password":                     true,
		"VoucherEnabled":               true,
		"RADIUSEnabled":                true,
		"RADIUSProfileID":              true,
		"RADIUSAuthType":               true,
		"RADIUSDisconnectEnabled":      true,
		"RADIUSDisconnectPort":         true,
		"FacebookEnabled":              true,
		"FacebookAppID":                true,
		"FacebookAppSecret":            true,
		"GoogleEnabled":                true,
		"GoogleClientID":               true,
		"GoogleClientSecret":           true,
		"WechatEnabled":                true,
		"WechatAppID":                  true,
		"WechatAppSecret":              true,
		"WechatSecretKey":              true,
		"PaymentEnabled":               true,
		"Gateway":                      true,
		"PaypalUsername":               true,
		"PaypalPassword":               true,
		"PaypalSignature":              true,
		"PaypalUseSandbox":             true,
		"StripeApiKey":                 true,
		"AuthorizeLoginid":             true,
		"AuthorizeTransactionkey":      true,
		"AuthorizeUseSandbox":          true,
		"QuickpayMerchantid":           true,
		"QuickpayApikey":               true,
		"QuickpayAgreementid":          true,
		"QuickpayTestmode":             true,
		"MerchantwarriorMerchantuuid":  true,
		"MerchantwarriorApikey":        true,
		"MerchantwarriorApipassphrase": true,
		"MerchantwarriorUseSandbox":    true,
		"IPpayTerminalid":              true,
		"IPpayUseSandbox":              true,
	}

	// wantSecret: the 18 modeled leaves that are WriteOnlySecret-class
	// (Optional+Computed+Sensitive), a subset of wantModeled.
	wantSecret := map[string]bool{
		"Password":                     true,
		"FacebookAppSecret":            true,
		"GoogleClientSecret":           true,
		"WechatAppSecret":              true,
		"WechatSecretKey":              true,
		"PaypalUsername":               true,
		"PaypalPassword":               true,
		"PaypalSignature":              true,
		"StripeApiKey":                 true,
		"AuthorizeLoginid":             true,
		"AuthorizeTransactionkey":      true,
		"QuickpayMerchantid":           true,
		"QuickpayApikey":               true,
		"QuickpayAgreementid":          true,
		"MerchantwarriorMerchantuuid":  true,
		"MerchantwarriorApikey":        true,
		"MerchantwarriorApipassphrase": true,
		"IPpayTerminalid":              true,
	}

	if len(wantModeled) != 56 {
		t.Fatalf("wantModeled has %d entries, want 56", len(wantModeled))
	}
	if len(wantSecret) != 18 {
		t.Fatalf("wantSecret has %d entries, want 18", len(wantSecret))
	}
	for name := range wantSecret {
		if !wantModeled[name] {
			t.Errorf("wantSecret contains %q which is not in wantModeled", name)
		}
	}
	for name := range wantModeled {
		if !all[name] {
			t.Errorf("wantModeled contains %q which is not a settings.GuestAccess field", name)
		}
	}
	preserved := 0
	for name := range all {
		if !wantModeled[name] {
			preserved++
		}
	}
	if preserved != 41 {
		t.Errorf("preserved field count = %d, want 41", preserved)
	}

	// FacebookWifiGwSecret is the 19th credential-like field, deliberately
	// preserved (not modeled) — see spec Key Decision 1. Assert it explicitly
	// so a future accidental promotion/demotion is caught here too.
	if wantModeled["FacebookWifiGwSecret"] {
		t.Error("FacebookWifiGwSecret must stay preserved, not modeled (spec Key Decision 1)")
	}
	if wantSecret["FacebookWifiGwSecret"] {
		t.Error("FacebookWifiGwSecret must not be in wantSecret")
	}
}

// guestAccessCoreScalarsModel builds a settingGuestAccessModel with the 38
// non-secret leaves set to representative synthetic values (all 18 secret
// leaves, added in Task 3, are left at their zero value here — this test
// predates Task 3's secret surface).
func guestAccessCoreScalarsModel() settingGuestAccessModel {
	return settingGuestAccessModel{
		Auth:                      types.StringValue("hotspot"),
		AuthUrl:                   types.StringValue("https://auth.example.internal/guest"),
		PortalEnabled:             types.BoolValue(true),
		PortalUseHostname:         types.BoolValue(true),
		PortalHostname:            types.StringValue("guest.example.internal"),
		CustomIP:                  types.StringValue("192.0.2.10"),
		EcEnabled:                 types.BoolValue(true),
		Expire:                    types.StringValue("480"),
		ExpireNumber:              types.Int64Value(8),
		ExpireUnit:                types.Int64Value(60),
		RedirectEnabled:           types.BoolValue(true),
		RedirectUrl:               types.StringValue("https://welcome.example.com/"),
		RedirectToHttps:           types.BoolValue(true),
		RedirectHttps:             types.BoolValue(false),
		AllowedSubnet:             types.StringValue("10.20.30.0/24"),
		RestrictedSubnet:          types.StringValue("10.20.31.0/24"),
		RestrictedDNSEnabled:      types.BoolValue(true),
		RestrictedDNSServers:      mustStringList(ctxBG(), "192.0.2.1", "198.51.100.1"),
		PasswordEnabled:           types.BoolValue(false),
		VoucherEnabled:            types.BoolValue(true),
		RADIUSEnabled:             types.BoolValue(true),
		RADIUSProfileID:           types.StringValue("radius-profile-example"),
		RADIUSAuthType:            types.StringValue("chap"),
		RADIUSDisconnectEnabled:   types.BoolValue(true),
		RADIUSDisconnectPort:      types.Int64Value(3799),
		FacebookEnabled:           types.BoolValue(true),
		FacebookAppID:             types.StringValue("example-app-id-123"),
		GoogleEnabled:             types.BoolValue(true),
		GoogleClientID:            types.StringValue("example-app-id-123"),
		WechatEnabled:             types.BoolValue(false),
		WechatAppID:               types.StringValue("example-app-id-123"),
		PaymentEnabled:            types.BoolValue(true),
		Gateway:                   types.StringValue("paypal"),
		PaypalUseSandbox:          types.BoolValue(true),
		AuthorizeUseSandbox:       types.BoolValue(true),
		QuickpayTestmode:          types.BoolValue(true),
		MerchantwarriorUseSandbox: types.BoolValue(true),
		IPpayUseSandbox:           types.BoolValue(true),
	}
}

func ctxBG() context.Context { return context.Background() }

// mustStringList builds a types.List of types.StringType from vals, panicking
// on error (test-only helper; the codebase's other section tests follow the
// same must-helper convention, e.g. dpiObject in setting_engine_lifecycle_test.go).
func mustStringList(ctx context.Context, vals ...string) types.List {
	l, diags := types.ListValueFrom(ctx, types.StringType, vals)
	if diags.HasError() {
		panic(diags.Errors())
	}
	return l
}

// TestGuestAccessSection_GoldenReproduction seeds a representative
// settingGuestAccessModel (all 38 non-secret fields set to spec-listed
// synthetic values, including the four rev. 2 fields auth_url/ec_enabled/
// custom_ip/redirect_https) plus an RMW base map containing a handful of the
// 41 preserved fields, calls overlay(), and asserts the resulting
// RawSetting.Data contains every configured modeled field at its correct
// wire key and the seeded preserved fields unchanged.
func TestGuestAccessSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "guest_access"},
		Data: map[string]any{
			"template_engine":            "angular",
			"portal_customized_bg_color": "#112233",
			"wechat_shop_id":             "shop-example-001",
		},
	}})

	m := guestAccessCoreScalarsModel()
	obj, diags := types.ObjectValueFrom(ctx, guestAccessAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building guest_access object: %v", diags)
	}

	rs, configured, oDiags := guestAccessSection{}.overlay(ctx, settingResourceModel{GuestAccess: obj}, settingResourceModel{}, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	if !configured {
		t.Fatal("overlay() configured = false, want true")
	}

	wantScalars := map[string]any{
		"auth":                        "hotspot",
		"auth_url":                    "https://auth.example.internal/guest",
		"portal_enabled":              true,
		"portal_use_hostname":         true,
		"portal_hostname":             "guest.example.internal",
		"custom_ip":                   "192.0.2.10",
		"ec_enabled":                  true,
		"expire":                      "480",
		"expire_number":               float64(8),
		"expire_unit":                 float64(60),
		"redirect_enabled":            true,
		"redirect_url":                "https://welcome.example.com/",
		"redirect_to_https":           true,
		"redirect_https":              false,
		"allowed_subnet_":             "10.20.30.0/24",
		"restricted_subnet_":          "10.20.31.0/24",
		"restricted_dns_enabled":      true,
		"password_enabled":            false,
		"voucher_enabled":             true,
		"radius_enabled":              true,
		"radiusprofile_id":            "radius-profile-example",
		"radius_auth_type":            "chap",
		"radius_disconnect_enabled":   true,
		"radius_disconnect_port":      float64(3799),
		"facebook_enabled":            true,
		"facebook_app_id":             "example-app-id-123",
		"google_enabled":              true,
		"google_client_id":            "example-app-id-123",
		"wechat_enabled":              false,
		"wechat_app_id":               "example-app-id-123",
		"payment_enabled":             true,
		"gateway":                     "paypal",
		"paypal_use_sandbox":          true,
		"authorize_use_sandbox":       true,
		"quickpay_testmode":           true,
		"merchantwarrior_use_sandbox": true,
		"ippay_use_sandbox":           true,
	}
	for k, want := range wantScalars {
		got, ok := rs.Data[k]
		if !ok {
			t.Errorf("wire key %q missing from overlay output", k)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("wire key %q = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}

	gotDNS, ok := rs.Data["restricted_dns_servers"].([]any)
	if !ok {
		t.Errorf("restricted_dns_servers = %#v, want []any", rs.Data["restricted_dns_servers"])
	} else if !reflect.DeepEqual(gotDNS, []any{"192.0.2.1", "198.51.100.1"}) {
		t.Errorf("restricted_dns_servers = %v, want [192.0.2.1 198.51.100.1]", gotDNS)
	}

	// Preserved fields (RMW base) must survive untouched.
	if got := rs.Data["template_engine"]; got != "angular" {
		t.Errorf("template_engine = %v, want unchanged %q", got, "angular")
	}
	if got := rs.Data["portal_customized_bg_color"]; got != "#112233" {
		t.Errorf("portal_customized_bg_color = %v, want unchanged %q", got, "#112233")
	}
	if got := rs.Data["wechat_shop_id"]; got != "shop-example-001" {
		t.Errorf("wechat_shop_id = %v, want unchanged %q", got, "shop-example-001")
	}
}

// TestGuestAccessSection_DecodeRoundTrip_CoreScalars proves decode() maps
// every one of the 38 non-secret leaves (Task 2 scope) from the wire back
// into the model, and that guestAccessAttrTypes carries no preserved field.
func TestGuestAccessSection_DecodeRoundTrip_CoreScalars(t *testing.T) {
	ctx := context.Background()

	data := map[string]any{
		"auth":                        "hotspot",
		"auth_url":                    "https://auth.example.internal/guest",
		"portal_enabled":              true,
		"portal_use_hostname":         true,
		"portal_hostname":             "guest.example.internal",
		"custom_ip":                   "192.0.2.10",
		"ec_enabled":                  true,
		"expire":                      "480",
		"expire_number":               float64(8),
		"expire_unit":                 float64(60),
		"redirect_enabled":            true,
		"redirect_url":                "https://welcome.example.com/",
		"redirect_to_https":           true,
		"redirect_https":              false,
		"allowed_subnet_":             "10.20.30.0/24",
		"restricted_subnet_":          "10.20.31.0/24",
		"restricted_dns_enabled":      true,
		"restricted_dns_servers":      []any{"192.0.2.1", "198.51.100.1"},
		"password_enabled":            false,
		"voucher_enabled":             true,
		"radius_enabled":              true,
		"radiusprofile_id":            "radius-profile-example",
		"radius_auth_type":            "chap",
		"radius_disconnect_enabled":   true,
		"radius_disconnect_port":      float64(3799),
		"facebook_enabled":            true,
		"facebook_app_id":             "example-app-id-123",
		"google_enabled":              true,
		"google_client_id":            "example-app-id-123",
		"wechat_enabled":              false,
		"wechat_app_id":               "example-app-id-123",
		"payment_enabled":             true,
		"gateway":                     "paypal",
		"paypal_use_sandbox":          true,
		"authorize_use_sandbox":       true,
		"quickpay_testmode":           true,
		"merchantwarrior_use_sandbox": true,
		"ippay_use_sandbox":           true,
		// Preserved fields must never leak into the decoded model.
		"template_engine":            "angular",
		"portal_customized_bg_color": "#112233",
	}
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "guest_access"},
		Data:        data,
	}})

	var model settingResourceModel
	diags := guestAccessSection{}.decode(ctx, snap, settingResourceModel{}, &model)
	if diags.HasError() {
		t.Fatalf("decode: %v", diags)
	}

	var m settingGuestAccessModel
	diags = model.GuestAccess.As(ctx, &m, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("As: %v", diags)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Auth", m.Auth.ValueString(), "hotspot"},
		{"AuthUrl", m.AuthUrl.ValueString(), "https://auth.example.internal/guest"},
		{"PortalHostname", m.PortalHostname.ValueString(), "guest.example.internal"},
		{"CustomIP", m.CustomIP.ValueString(), "192.0.2.10"},
		{"Expire", m.Expire.ValueString(), "480"},
		{"RedirectUrl", m.RedirectUrl.ValueString(), "https://welcome.example.com/"},
		{"AllowedSubnet", m.AllowedSubnet.ValueString(), "10.20.30.0/24"},
		{"RestrictedSubnet", m.RestrictedSubnet.ValueString(), "10.20.31.0/24"},
		{"RADIUSProfileID", m.RADIUSProfileID.ValueString(), "radius-profile-example"},
		{"RADIUSAuthType", m.RADIUSAuthType.ValueString(), "chap"},
		{"FacebookAppID", m.FacebookAppID.ValueString(), "example-app-id-123"},
		{"GoogleClientID", m.GoogleClientID.ValueString(), "example-app-id-123"},
		{"WechatAppID", m.WechatAppID.ValueString(), "example-app-id-123"},
		{"Gateway", m.Gateway.ValueString(), "paypal"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if !m.PortalEnabled.ValueBool() || !m.PortalUseHostname.ValueBool() || !m.EcEnabled.ValueBool() {
		t.Error("bool leaves did not round-trip true as expected")
	}
	if m.ExpireNumber.ValueInt64() != 8 || m.ExpireUnit.ValueInt64() != 60 {
		t.Errorf("ExpireNumber/ExpireUnit = %d/%d, want 8/60", m.ExpireNumber.ValueInt64(), m.ExpireUnit.ValueInt64())
	}
	if m.RADIUSDisconnectPort.ValueInt64() != 3799 {
		t.Errorf("RADIUSDisconnectPort = %d, want 3799", m.RADIUSDisconnectPort.ValueInt64())
	}

	var dnsList []string
	diags = m.RestrictedDNSServers.ElementsAs(ctx, &dnsList, false)
	if diags.HasError() {
		t.Fatalf("ElementsAs: %v", diags)
	}
	if !reflect.DeepEqual(dnsList, []string{"192.0.2.1", "198.51.100.1"}) {
		t.Errorf("RestrictedDNSServers = %v, want [192.0.2.1 198.51.100.1]", dnsList)
	}

	// guestAccessAttrTypes carries all 56 modeled leaves: 38 non-secret (this
	// test's scope) + 18 secret (added alongside, tested separately by
	// TestGuestAccessSection_SecretMatrix and friends).
	if len(guestAccessAttrTypes) != 56 {
		t.Errorf("len(guestAccessAttrTypes) = %d, want 56", len(guestAccessAttrTypes))
	}
}

// TestGuestAccessSection_NotConfigured asserts overlay()/isConfigured() both
// report "not configured" for a null GuestAccess model, matching every
// sibling section's contract.
func TestGuestAccessSection_NotConfigured(t *testing.T) {
	model := settingResourceModel{GuestAccess: types.ObjectNull(guestAccessAttrTypes)}
	_, configured, diags := guestAccessSection{}.overlay(context.Background(), model, settingResourceModel{}, newRawSettings(nil))
	if configured {
		t.Error("overlay() configured = true, want false for null GuestAccess")
	}
	if diags.HasError() {
		t.Errorf("unexpected diags: %v", diags)
	}
	if (guestAccessSection{}).isConfigured(model) {
		t.Error("isConfigured() = true, want false for null GuestAccess")
	}
}

// guestAccessSecretCase is one row of the exhaustive secret matrix: a
// modeled secret leaf's tfsdk name, its controller wire key, and two
// distinct synthetic values used across the preserve/rotate sub-tests.
type guestAccessSecretCase struct {
	leaf       string
	wireKey    string
	priorValue string
	newValue   string
}

// guestAccessSecretCases is the 18-leaf table the exhaustive secret-matrix
// test and the carryBestEffort chaining regression test both iterate.
var guestAccessSecretCases = []guestAccessSecretCase{
	{"password", "x_password", "prior-password", "new-password"},
	{"facebook_app_secret", "x_facebook_app_secret", "prior-fb-secret", "new-fb-secret"},
	{"google_client_secret", "x_google_client_secret", "prior-google-secret", "new-google-secret"},
	{"wechat_app_secret", "x_wechat_app_secret", "prior-wechat-app-secret", "new-wechat-app-secret"},
	{"wechat_secret_key", "x_wechat_secret_key", "prior-wechat-secret-key", "new-wechat-secret-key"},
	{"paypal_username", "x_paypal_username", "prior-paypal-user", "new-paypal-user"},
	{"paypal_password", "x_paypal_password", "prior-paypal-pass", "new-paypal-pass"},
	{"paypal_signature", "x_paypal_signature", "prior-paypal-sig", "new-paypal-sig"},
	{"stripe_api_key", "x_stripe_api_key", "prior-stripe-key", "new-stripe-key"},
	{"authorize_loginid", "x_authorize_loginid", "prior-authorize-login", "new-authorize-login"},
	{"authorize_transactionkey", "x_authorize_transactionkey", "prior-authorize-txn", "new-authorize-txn"},
	{"quickpay_merchantid", "x_quickpay_merchantid", "prior-quickpay-merchant", "new-quickpay-merchant"},
	{"quickpay_apikey", "x_quickpay_apikey", "prior-quickpay-key", "new-quickpay-key"},
	{"quickpay_agreementid", "x_quickpay_agreementid", "prior-quickpay-agreement", "new-quickpay-agreement"},
	{"merchantwarrior_merchantuuid", "x_merchantwarrior_merchantuuid", "prior-mw-uuid", "new-mw-uuid"},
	{"merchantwarrior_apikey", "x_merchantwarrior_apikey", "prior-mw-key", "new-mw-key"},
	{"merchantwarrior_apipassphrase", "x_merchantwarrior_apipassphrase", "prior-mw-pass", "new-mw-pass"},
	{"ippay_terminalid", "x_ippay_terminalid", "prior-ippay-terminal", "new-ippay-terminal"},
}

// guestAccessFullModelWithSecrets returns a fully-populated
// settingGuestAccessModel: the 38 non-secret leaves from
// guestAccessCoreScalarsModel plus all 18 secret leaves set to each case's
// priorValue (used as the "prior" object base across the secret-matrix
// sub-tests).
func guestAccessFullModelWithSecrets(t *testing.T, values map[string]string) settingGuestAccessModel {
	t.Helper()
	m := guestAccessCoreScalarsModel()
	get := func(leaf, fallback string) types.String {
		if v, ok := values[leaf]; ok {
			return types.StringValue(v)
		}
		return types.StringValue(fallback)
	}
	m.Password = get("password", "")
	m.FacebookAppSecret = get("facebook_app_secret", "")
	m.GoogleClientSecret = get("google_client_secret", "")
	m.WechatAppSecret = get("wechat_app_secret", "")
	m.WechatSecretKey = get("wechat_secret_key", "")
	m.PaypalUsername = get("paypal_username", "")
	m.PaypalPassword = get("paypal_password", "")
	m.PaypalSignature = get("paypal_signature", "")
	m.StripeApiKey = get("stripe_api_key", "")
	m.AuthorizeLoginid = get("authorize_loginid", "")
	m.AuthorizeTransactionkey = get("authorize_transactionkey", "")
	m.QuickpayMerchantid = get("quickpay_merchantid", "")
	m.QuickpayApikey = get("quickpay_apikey", "")
	m.QuickpayAgreementid = get("quickpay_agreementid", "")
	m.MerchantwarriorMerchantuuid = get("merchantwarrior_merchantuuid", "")
	m.MerchantwarriorApikey = get("merchantwarrior_apikey", "")
	m.MerchantwarriorApipassphrase = get("merchantwarrior_apipassphrase", "")
	m.IPpayTerminalid = get("ippay_terminalid", "")
	return m
}

// guestAccessPriorValues returns a leaf->priorValue map from
// guestAccessSecretCases, for building a "prior" object with every leaf at
// its distinct prior value.
func guestAccessPriorValues() map[string]string {
	out := make(map[string]string, len(guestAccessSecretCases))
	for _, tc := range guestAccessSecretCases {
		out[tc.leaf] = tc.priorValue
	}
	return out
}

// guestAccessObjectFrom builds a types.Object from m, failing the test on
// any diagnostic.
func guestAccessObjectFrom(t *testing.T, ctx context.Context, m settingGuestAccessModel) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, guestAccessAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building guest_access object: %v", diags)
	}
	return obj
}

// setObjectStringLeaf returns a copy of obj with leaf's value replaced,
// preserving every other attribute untouched. Used to build focused
// null/unknown/rotated plan objects on top of a common base without
// re-deriving the whole 56-leaf struct per sub-test.
func setObjectStringLeaf(t *testing.T, obj types.Object, leaf string, v types.String) types.Object {
	t.Helper()
	ctx := context.Background()
	attrTypes := obj.AttributeTypes(ctx)
	attrs := obj.Attributes()
	out := make(map[string]attr.Value, len(attrs))
	for k, val := range attrs {
		out[k] = val
	}
	out[leaf] = v
	newObj, diags := types.ObjectValue(attrTypes, out)
	if diags.HasError() {
		t.Fatalf("rebuilding object with %s replaced: %v", leaf, diags)
	}
	return newObj
}

// getObjectStringLeaf reads a single string leaf out of obj's Attributes().
func getObjectStringLeaf(t *testing.T, obj types.Object, leaf string) types.String {
	t.Helper()
	v, ok := obj.Attributes()[leaf].(types.String)
	if !ok {
		t.Fatalf("leaf %q is not a types.String in object", leaf)
	}
	return v
}

// TestGuestAccessSection_SecretMatrix exhaustively exercises every one of
// the 18 modeled secret leaves independently: preserve/null,
// preserve/unknown, rotate/non-empty, and rotate/empty (all 18 leaves carry
// no go-unifi length-lower-bound, so rotate/empty is reachable through
// config for all of them — see spec Key Decision 2a / plan Task 3). Each
// sub-test also asserts every OTHER (sibling) secret leaf in the same
// object is unaffected, proving carryGuestAccessSecrets's per-leaf
// independence via overlay's own delete-on-unset / verbatim-on-set logic
// (overlay, not carryBestEffort, is what Create/Update actually calls; the
// carry helper is exercised separately below).
func TestGuestAccessSection_SecretMatrix(t *testing.T) {
	if len(guestAccessSecretCases) != 18 {
		t.Fatalf("guestAccessSecretCases has %d entries, want 18", len(guestAccessSecretCases))
	}

	ctx := context.Background()

	for _, tc := range guestAccessSecretCases {
		t.Run(tc.leaf+"/preserve_null", func(t *testing.T) {
			priorModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			priorObj := guestAccessObjectFrom(t, ctx, priorModel)
			prior := settingResourceModel{GuestAccess: priorObj}

			planModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			planObj := guestAccessObjectFrom(t, ctx, planModel)
			planObj = setObjectStringLeaf(t, planObj, tc.leaf, types.StringNull())
			model := settingResourceModel{GuestAccess: planObj}

			rs, configured, diags := guestAccessSection{}.overlay(ctx, model, prior, newRawSettings(nil))
			if diags.HasError() {
				t.Fatalf("overlay: %v", diags)
			}
			if !configured {
				t.Fatal("overlay() configured = false, want true")
			}
			// A null config leaf must delete the wire key entirely — never
			// re-send a masked value, and never write an empty string.
			if _, ok := rs.Data[tc.wireKey]; ok {
				t.Errorf("wire key %q present after null config, want deleted", tc.wireKey)
			}
			assertSiblingSecretsUnaffected(t, rs, tc.leaf)
		})

		t.Run(tc.leaf+"/preserve_unknown", func(t *testing.T) {
			priorModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			priorObj := guestAccessObjectFrom(t, ctx, priorModel)
			prior := settingResourceModel{GuestAccess: priorObj}

			planModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			planObj := guestAccessObjectFrom(t, ctx, planModel)
			planObj = setObjectStringLeaf(t, planObj, tc.leaf, types.StringUnknown())
			model := settingResourceModel{GuestAccess: planObj}

			rs, configured, diags := guestAccessSection{}.overlay(ctx, model, prior, newRawSettings(nil))
			if diags.HasError() {
				t.Fatalf("overlay: %v", diags)
			}
			if !configured {
				t.Fatal("overlay() configured = false, want true")
			}
			if _, ok := rs.Data[tc.wireKey]; ok {
				t.Errorf("wire key %q present after unknown config, want deleted", tc.wireKey)
			}
			assertSiblingSecretsUnaffected(t, rs, tc.leaf)
		})

		t.Run(tc.leaf+"/rotate_non_empty", func(t *testing.T) {
			priorModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			priorObj := guestAccessObjectFrom(t, ctx, priorModel)
			prior := settingResourceModel{GuestAccess: priorObj}

			planModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			planObj := guestAccessObjectFrom(t, ctx, planModel)
			planObj = setObjectStringLeaf(t, planObj, tc.leaf, types.StringValue(tc.newValue))
			model := settingResourceModel{GuestAccess: planObj}

			rs, configured, diags := guestAccessSection{}.overlay(ctx, model, prior, newRawSettings(nil))
			if diags.HasError() {
				t.Fatalf("overlay: %v", diags)
			}
			if !configured {
				t.Fatal("overlay() configured = false, want true")
			}
			got, ok := rs.Data[tc.wireKey]
			if !ok {
				t.Fatalf("wire key %q missing after rotate, want %q", tc.wireKey, tc.newValue)
			}
			if got != tc.newValue {
				t.Errorf("wire key %q = %v, want %q", tc.wireKey, got, tc.newValue)
			}
			assertSiblingSecretsUnaffected(t, rs, tc.leaf)
		})

		t.Run(tc.leaf+"/rotate_empty", func(t *testing.T) {
			priorModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			priorObj := guestAccessObjectFrom(t, ctx, priorModel)
			prior := settingResourceModel{GuestAccess: priorObj}

			planModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
			planObj := guestAccessObjectFrom(t, ctx, planModel)
			planObj = setObjectStringLeaf(t, planObj, tc.leaf, types.StringValue(""))
			model := settingResourceModel{GuestAccess: planObj}

			rs, configured, diags := guestAccessSection{}.overlay(ctx, model, prior, newRawSettings(nil))
			if diags.HasError() {
				t.Fatalf("overlay: %v", diags)
			}
			if !configured {
				t.Fatal("overlay() configured = false, want true")
			}
			got, ok := rs.Data[tc.wireKey]
			if !ok {
				t.Fatalf("wire key %q missing after rotate-to-empty, want explicit \"\"", tc.wireKey)
			}
			if got != "" {
				t.Errorf("wire key %q = %v, want explicit empty string", tc.wireKey, got)
			}
			assertSiblingSecretsUnaffected(t, rs, tc.leaf)
		})
	}
}

// assertSiblingSecretsUnaffected asserts every secret wire key OTHER than
// exceptLeaf carries its expected prior value in rs.Data — proving the leaf
// under test in the enclosing sub-test didn't disturb any sibling secret.
// All non-exempt leaves in this test file's fixtures are configured (never
// null) with their prior value, so every sibling's wire key is expected
// present and equal to its priorValue.
func assertSiblingSecretsUnaffected(t *testing.T, rs settings.RawSetting, exceptLeaf string) {
	t.Helper()
	for _, sib := range guestAccessSecretCases {
		if sib.leaf == exceptLeaf {
			continue
		}
		got, ok := rs.Data[sib.wireKey]
		if !ok {
			t.Errorf("sibling wire key %q missing, want unaffected prior value %q", sib.wireKey, sib.priorValue)
			continue
		}
		if got != sib.priorValue {
			t.Errorf("sibling wire key %q = %v, want unaffected prior value %q", sib.wireKey, got, sib.priorValue)
		}
	}
}

// TestGuestAccessSection_SecretDecodeNeverReadsAPI mirrors
// TestRadiusSection_DecodeRoundTrip's "masked wire value must not leak"
// shape, generalized across all 18 secret leaves: seed data[wireKey] =
// "MASKED" for every leaf and a distinct priorModel value per leaf; assert
// decode() yields every prior value, never "MASKED", for all 18.
func TestGuestAccessSection_SecretDecodeNeverReadsAPI(t *testing.T) {
	ctx := context.Background()

	priorModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
	priorObj := guestAccessObjectFrom(t, ctx, priorModel)
	prior := settingResourceModel{GuestAccess: priorObj}

	data := map[string]any{}
	for _, tc := range guestAccessSecretCases {
		data[tc.wireKey] = "MASKED"
	}
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "guest_access"},
		Data:        data,
	}})

	var model settingResourceModel
	diags := guestAccessSection{}.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode: %v", diags)
	}

	var m settingGuestAccessModel
	diags = model.GuestAccess.As(ctx, &m, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("As: %v", diags)
	}

	byLeaf := map[string]types.String{
		"password":                      m.Password,
		"facebook_app_secret":           m.FacebookAppSecret,
		"google_client_secret":          m.GoogleClientSecret,
		"wechat_app_secret":             m.WechatAppSecret,
		"wechat_secret_key":             m.WechatSecretKey,
		"paypal_username":               m.PaypalUsername,
		"paypal_password":               m.PaypalPassword,
		"paypal_signature":              m.PaypalSignature,
		"stripe_api_key":                m.StripeApiKey,
		"authorize_loginid":             m.AuthorizeLoginid,
		"authorize_transactionkey":      m.AuthorizeTransactionkey,
		"quickpay_merchantid":           m.QuickpayMerchantid,
		"quickpay_apikey":               m.QuickpayApikey,
		"quickpay_agreementid":          m.QuickpayAgreementid,
		"merchantwarrior_merchantuuid":  m.MerchantwarriorMerchantuuid,
		"merchantwarrior_apikey":        m.MerchantwarriorApikey,
		"merchantwarrior_apipassphrase": m.MerchantwarriorApipassphrase,
		"ippay_terminalid":              m.IPpayTerminalid,
	}
	for _, tc := range guestAccessSecretCases {
		got := byLeaf[tc.leaf]
		if got.ValueString() != tc.priorValue {
			t.Errorf("%s = %q, want %q (prior) — masked wire value must not leak", tc.leaf, got.ValueString(), tc.priorValue)
		}
	}
}

// TestGuestAccessSection_CarryBestEffortSecrets builds a plan with some of
// the 18 secrets null/unknown and others rotated (including at least one
// explicit empty-string rotation), a dst seeded with distinct prior values
// for all 18, calls carryBestEffort, and asserts: null/unknown-in-plan
// leaves keep dst's (prior) value; set-in-plan leaves (including explicit
// empty string) take plan's value; every non-secret leaf comes from plan.
// This is the regression test for the "must pass ORIGINAL prior, not the
// accumulating out, as arg2" chaining bug carryGuestAccessSecrets's own doc
// comment calls out: it specifically asserts a leaf that is null-in-plan
// and appears AFTER a rotated-in-plan leaf in guestAccessSecretLeaves' order
// still correctly resolves to prior, not to a zero value or a sibling's
// value.
func TestGuestAccessSection_CarryBestEffortSecrets(t *testing.T) {
	if len(guestAccessSecretLeaves) != 18 {
		t.Fatalf("guestAccessSecretLeaves has %d entries, want 18", len(guestAccessSecretLeaves))
	}

	ctx := context.Background()

	// dst (prior) has every secret at its distinct prior value.
	dstModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
	dstObj := guestAccessObjectFrom(t, ctx, dstModel)
	dst := settingResourceModel{GuestAccess: dstObj}

	// plan starts from the same prior values, then:
	//  - the FIRST leaf in guestAccessSecretLeaves' order is rotated to a
	//    new value;
	//  - the LAST leaf in guestAccessSecretLeaves' order (appearing AFTER
	//    the rotated one in loop order) is left null in plan — this is the
	//    exact shape the chaining bug would mishandle: if
	//    carryGuestAccessSecrets threaded "out" as prior instead of the
	//    original "prior" parameter, this null leaf could resolve to the
	//    first leaf's rotated value instead of its own true prior value.
	rotatedLeaf := guestAccessSecretLeaves[0]
	nullLeaf := guestAccessSecretLeaves[len(guestAccessSecretLeaves)-1]
	var rotatedNewValue, emptyLeaf string
	for _, tc := range guestAccessSecretCases {
		if tc.leaf == rotatedLeaf {
			rotatedNewValue = tc.newValue
		}
	}
	// Pick a third leaf (distinct from rotatedLeaf/nullLeaf) for an explicit
	// empty-string rotation.
	for _, l := range guestAccessSecretLeaves {
		if l != rotatedLeaf && l != nullLeaf {
			emptyLeaf = l
			break
		}
	}

	planModel := guestAccessFullModelWithSecrets(t, guestAccessPriorValues())
	planObj := guestAccessObjectFrom(t, ctx, planModel)
	planObj = setObjectStringLeaf(t, planObj, rotatedLeaf, types.StringValue(rotatedNewValue))
	planObj = setObjectStringLeaf(t, planObj, nullLeaf, types.StringNull())
	planObj = setObjectStringLeaf(t, planObj, emptyLeaf, types.StringValue(""))
	plan := settingResourceModel{GuestAccess: planObj}

	diags := (guestAccessSection{}).carryBestEffort(&dst, plan)
	if diags.HasError() {
		t.Fatalf("carryBestEffort: %v", diags)
	}

	got := getObjectStringLeaf(t, dst.GuestAccess, rotatedLeaf)
	if got.ValueString() != rotatedNewValue {
		t.Errorf("rotated leaf %s = %q, want plan's %q", rotatedLeaf, got.ValueString(), rotatedNewValue)
	}

	var priorForNullLeaf string
	for _, tc := range guestAccessSecretCases {
		if tc.leaf == nullLeaf {
			priorForNullLeaf = tc.priorValue
		}
	}
	got = getObjectStringLeaf(t, dst.GuestAccess, nullLeaf)
	if got.ValueString() != priorForNullLeaf {
		t.Errorf("null-in-plan leaf %s (last in loop order, after a rotated leaf) = %q, want ORIGINAL prior %q — chaining bug: carryGuestAccessSecrets must always pass the original prior, never the accumulating out, as arg2", nullLeaf, got.ValueString(), priorForNullLeaf)
	}

	got = getObjectStringLeaf(t, dst.GuestAccess, emptyLeaf)
	if got.ValueString() != "" || got.IsNull() {
		t.Errorf("explicit-empty-in-plan leaf %s = %v, want a known empty string (kept from plan, not prior)", emptyLeaf, got)
	}

	// A non-secret leaf must come straight from plan.
	var dstM settingGuestAccessModel
	diagsAs := dst.GuestAccess.As(ctx, &dstM, basetypes.ObjectAsOptions{})
	if diagsAs.HasError() {
		t.Fatalf("As: %v", diagsAs)
	}
	if dstM.Auth.ValueString() != "hotspot" {
		t.Errorf("non-secret leaf Auth = %q, want plan's %q", dstM.Auth.ValueString(), "hotspot")
	}
}
