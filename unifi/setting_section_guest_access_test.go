package unifi

import (
	"context"
	"reflect"
	"testing"

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

	// At this task's tip, guestAccessAttrTypes carries only the 38 non-secret
	// leaves — Task 3 adds the 18 secret leaves to the same map/struct, at
	// which point this becomes 56 (see that task's own assertion).
	if len(guestAccessAttrTypes) != 38 {
		t.Errorf("len(guestAccessAttrTypes) = %d, want 38", len(guestAccessAttrTypes))
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
