package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func Test_guestAccessModelToData_core(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	redirect, d := types.ObjectValueFrom(ctx, guestAccessRedirectAttrTypes,
		settingGuestAccessRedirectModel{
			UseHttps: types.BoolValue(true),
			ToHttps:  types.BoolValue(false),
			URL:      types.StringValue("https://example.com/welcome"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	langs, d := types.ListValueFrom(ctx, types.StringType, []string{"en"})
	if d.HasError() {
		t.Fatal(d)
	}
	pc, d := types.ObjectValueFrom(ctx, guestAccessPortalCustomizationAttrTypes,
		settingGuestAccessPortalCustomizationModel{
			Customized:             types.BoolValue(true),
			AuthenticationText:     types.StringNull(),
			BgColor:                types.StringValue("#005ED9"),
			BgImageEnabled:         types.BoolNull(),
			BgImageFileID:          types.StringNull(),
			BgImageTile:            types.BoolNull(),
			BgType:                 types.StringValue("color"),
			BoxColor:               types.StringNull(),
			BoxLinkColor:           types.StringNull(),
			BoxOpacity:             types.Int64Value(90),
			BoxRadius:              types.Int64Null(),
			BoxTextColor:           types.StringNull(),
			ButtonColor:            types.StringNull(),
			ButtonText:             types.StringNull(),
			ButtonTextColor:        types.StringNull(),
			Languages:              langs,
			LinkColor:              types.StringNull(),
			LogoEnabled:            types.BoolNull(),
			LogoFileID:             types.StringNull(),
			LogoPosition:           types.StringNull(),
			LogoSize:               types.Int64Null(),
			SuccessText:            types.StringNull(),
			TextColor:              types.StringNull(),
			Title:                  types.StringValue("Guest WiFi"),
			Tos:                    types.StringNull(),
			TosEnabled:             types.BoolNull(),
			UnsplashAuthorName:     types.StringNull(),
			UnsplashAuthorUsername: types.StringNull(),
			WelcomeText:            types.StringNull(),
			WelcomeTextEnabled:     types.BoolNull(),
			WelcomeTextPosition:    types.StringNull(),
		})
	if d.HasError() {
		t.Fatal(d)
	}

	obj, d := types.ObjectValueFrom(ctx, guestAccessAttrTypes, settingGuestAccessModel{
		Auth:                types.StringValue("hotspot"),
		Expire:              types.Int64Value(480),
		ExpireNumber:        types.Int64Value(8),
		ExpireUnit:          types.Int64Value(60),
		PortalEnabled:       types.BoolValue(true),
		RedirectEnabled:     types.BoolValue(true),
		Redirect:            redirect,
		PortalCustomization: pc,
		// Everything else null: it must not be written.
		AllowedSubnet:     types.StringNull(),
		RestrictedSubnet:  types.StringNull(),
		AuthUrl:           types.StringNull(),
		CustomIP:          types.StringNull(),
		EcEnabled:         types.BoolNull(),
		PortalHostname:    types.StringNull(),
		PortalUseHostname: types.BoolNull(),
		TemplateEngine:    types.StringNull(),
		VoucherCustomized: types.BoolNull(),
		VoucherEnabled:    types.BoolNull(),

		Facebook:             types.ObjectNull(guestAccessFacebookAttrTypes),
		FacebookEnabled:      types.BoolNull(),
		FacebookWifi:         types.ObjectNull(guestAccessFacebookWifiAttrTypes),
		Google:               types.ObjectNull(guestAccessGoogleAttrTypes),
		GoogleEnabled:        types.BoolNull(),
		Password:             types.StringNull(),
		PasswordEnabled:      types.BoolNull(),
		Radius:               types.ObjectNull(guestAccessRadiusAttrTypes),
		RadiusEnabled:        types.BoolNull(),
		RestrictedDNSEnabled: types.BoolNull(),
		RestrictedDNSServers: types.ListNull(types.StringType),
		Wechat:               types.ObjectNull(guestAccessWechatAttrTypes),
		WechatEnabled:        types.BoolNull(),

		Authorize:       types.ObjectNull(guestAccessAuthorizeAttrTypes),
		IPpay:           types.ObjectNull(guestAccessIPpayAttrTypes),
		MerchantWarrior: types.ObjectNull(guestAccessMerchantWarriorAttrTypes),
		PaymentEnabled:  types.BoolNull(),
		PaymentGateway:  types.StringNull(),
		Paypal:          types.ObjectNull(guestAccessPaypalAttrTypes),
		Quickpay:        types.ObjectNull(guestAccessQuickpayAttrTypes),
		Stripe:          types.ObjectNull(guestAccessStripeAttrTypes),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	var m settingGuestAccessModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		t.Fatal(diags)
	}

	// Live controllers store restricted_subnet_1..3 which go-unifi does not
	// model: the raw merge must preserve them verbatim.
	data := map[string]any{
		"restricted_subnet_1": "192.168.0.0/16",
		"auth":                "none",
		"template_engine":     "angular",
	}

	guestAccessModelToData(ctx, &m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["restricted_subnet_1"] != "192.168.0.0/16" {
		t.Fatal("unmodeled restricted_subnet_1 was clobbered")
	}
	if data["template_engine"] != "angular" {
		t.Fatal("null template_engine overwrote remote value")
	}
	if data["auth"] != "hotspot" {
		t.Fatalf("auth = %v", data["auth"])
	}
	if data["expire"] != int64(480) || data["expire_number"] != int64(8) ||
		data["expire_unit"] != int64(60) {
		t.Fatalf("expire fields wrong: %v", data)
	}
	if data["portal_enabled"] != true || data["redirect_enabled"] != true {
		t.Fatalf("portal/redirect enabled wrong: %v", data)
	}
	if data["redirect_url"] != "https://example.com/welcome" ||
		data["redirect_https"] != true || data["redirect_to_https"] != false {
		t.Fatalf("redirect fields wrong: %v", data)
	}
	if data["portal_customized"] != true ||
		data["portal_customized_bg_color"] != "#005ED9" ||
		data["portal_customized_title"] != "Guest WiFi" ||
		data["portal_customized_box_opacity"] != int64(90) {
		t.Fatalf("portal_customized fields wrong: %v", data)
	}
	pcLangs, ok := data["portal_customized_languages"].([]string)
	if !ok || len(pcLangs) != 1 || pcLangs[0] != "en" {
		t.Fatalf("portal_customized_languages = %v", data["portal_customized_languages"])
	}
	if _, present := data["portal_hostname"]; present {
		t.Fatal("null portal_hostname should not be written")
	}
	if _, present := data["portal_customized_tos"]; present {
		t.Fatal("null portal_customization.tos should not be written")
	}
}

func Test_guestAccessDataToModel_liveShape(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	// Shape (not values) of a live UDM guest_access document. Numbers are
	// float64 exactly as encoding/json decodes them — including "expire",
	// which the generated go-unifi struct wrongly types as string
	// (TODO(go-unifi)): this test pins the raw-read workaround.
	data := map[string]any{
		"_id":                           "aaaaaaaaaaaaaaaaaaaaaaaa",
		"key":                           "guest_access",
		"auth":                          "none",
		"ec_enabled":                    true,
		"expire":                        float64(480),
		"expire_number":                 float64(8),
		"expire_unit":                   float64(60),
		"portal_enabled":                false,
		"portal_use_hostname":           false,
		"portal_customized":             false,
		"portal_customized_bg_color":    "#005ED9",
		"portal_customized_bg_type":     "color",
		"portal_customized_box_opacity": float64(100),
		"portal_customized_languages":   []any{"en"},
		"portal_customized_title":       "UniFi Guest WiFi",
		"redirect_enabled":              false,
		"redirect_https":                true,
		"redirect_to_https":             false,
		"redirect_url":                  "",
		"restricted_subnet_1":           "192.168.0.0/16",
		"template_engine":               "angular",
	}

	m := guestAccessDataToModel(ctx, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.Auth.ValueString() != "none" {
		t.Fatalf("auth = %v", m.Auth)
	}
	if m.Expire.ValueInt64() != 480 || m.ExpireNumber.ValueInt64() != 8 ||
		m.ExpireUnit.ValueInt64() != 60 {
		t.Fatalf("expire fields = %v/%v/%v", m.Expire, m.ExpireNumber, m.ExpireUnit)
	}
	if !m.EcEnabled.ValueBool() || m.PortalEnabled.ValueBool() {
		t.Fatalf("ec/portal enabled = %v/%v", m.EcEnabled, m.PortalEnabled)
	}
	if m.TemplateEngine.ValueString() != "angular" {
		t.Fatalf("template_engine = %v", m.TemplateEngine)
	}
	if !m.RestrictedSubnet.IsNull() {
		t.Fatalf("restricted_subnet should be null (only indexed variants present), got %v", m.RestrictedSubnet)
	}

	if m.PortalCustomization.IsNull() {
		t.Fatal("portal_customization should be present")
	}
	var pc settingGuestAccessPortalCustomizationModel
	diags.Append(m.PortalCustomization.As(ctx, &pc, basetypes.ObjectAsOptions{})...)
	if pc.BgColor.ValueString() != "#005ED9" || pc.BoxOpacity.ValueInt64() != 100 ||
		pc.Title.ValueString() != "UniFi Guest WiFi" {
		t.Fatalf("portal_customization = %+v", pc)
	}
	if pc.Languages.IsNull() || len(pc.Languages.Elements()) != 1 {
		t.Fatalf("languages = %v", pc.Languages)
	}

	if m.Redirect.IsNull() {
		t.Fatal("redirect should be present (redirect_https key exists)")
	}
	var r settingGuestAccessRedirectModel
	diags.Append(m.Redirect.As(ctx, &r, basetypes.ObjectAsOptions{})...)
	if !r.UseHttps.ValueBool() || r.ToHttps.ValueBool() || !r.URL.IsNull() {
		t.Fatalf("redirect = %+v", r)
	}
}

func Test_guestAccessDataToModel_absentBlocks(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := guestAccessDataToModel(ctx, map[string]any{"auth": "none"}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !m.PortalCustomization.IsNull() {
		t.Fatal("portal_customization should be null when no portal_customized key exists")
	}
	if !m.Redirect.IsNull() {
		t.Fatal("redirect should be null when no redirect keys exist")
	}
	if !m.Expire.IsNull() {
		t.Fatal("expire should be null when absent")
	}
}

func Test_settingResource_Schema_guestAccess(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["guest_access"]; !ok {
		t.Fatal("schema is missing the guest_access section attribute")
	}
}

func Test_guestAccessModelToData_authProviders(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	google, d := types.ObjectValueFrom(ctx, guestAccessGoogleAttrTypes,
		settingGuestAccessGoogleModel{
			ClientID:     types.StringValue("client-id"),
			ClientSecret: types.StringValue("client-secret"),
			Domain:       types.StringValue("example.com"),
			ScopeEmail:   types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	radius, d := types.ObjectValueFrom(ctx, guestAccessRadiusAttrTypes,
		settingGuestAccessRadiusModel{
			AuthType:          types.StringValue("chap"),
			DisconnectEnabled: types.BoolValue(true),
			DisconnectPort:    types.Int64Value(3799),
			ProfileID:         types.StringValue("rp1"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	dns, d := types.ListValueFrom(ctx, types.StringType, []string{"1.1.1.1", "8.8.8.8"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingGuestAccessModel{
		Password:             types.StringValue("guest-pass"),
		PasswordEnabled:      types.BoolValue(true),
		Google:               google,
		GoogleEnabled:        types.BoolValue(true),
		Radius:               radius,
		RestrictedDNSServers: dns,
		RestrictedDNSEnabled: types.BoolValue(true),
		// All other blocks null.
		Facebook:     types.ObjectNull(guestAccessFacebookAttrTypes),
		FacebookWifi: types.ObjectNull(guestAccessFacebookWifiAttrTypes),
		Wechat:       types.ObjectNull(guestAccessWechatAttrTypes),
	}

	data := map[string]any{"unmodeled_field": "keep"}
	guestAccessModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["x_password"] != "guest-pass" || data["password_enabled"] != true {
		t.Fatalf("password fields wrong: %v", data)
	}
	if data["google_client_id"] != "client-id" ||
		data["x_google_client_secret"] != "client-secret" ||
		data["google_domain"] != "example.com" ||
		data["google_scope_email"] != true || data["google_enabled"] != true {
		t.Fatalf("google fields wrong: %v", data)
	}
	if data["radius_auth_type"] != "chap" || data["radius_disconnect_enabled"] != true ||
		data["radius_disconnect_port"] != int64(3799) || data["radiusprofile_id"] != "rp1" {
		t.Fatalf("radius fields wrong: %v", data)
	}
	servers, ok := data["restricted_dns_servers"].([]string)
	if !ok || len(servers) != 2 || data["restricted_dns_enabled"] != true {
		t.Fatalf("restricted dns fields wrong: %v", data)
	}
	if _, present := data["facebook_app_id"]; present {
		t.Fatal("null facebook block should not write keys")
	}
	if _, present := data["wechat_app_id"]; present {
		t.Fatal("null wechat block should not write keys")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_guestAccessDataToModel_authPresence(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	data := map[string]any{
		"auth":                   "hotspot",
		"password_enabled":       true,
		"x_password":             "guest-pass",
		"google_client_id":       "client-id",
		"x_google_client_secret": "client-secret",
		"radiusprofile_id":       "rp1",
		"radius_disconnect_port": float64(3799),
	}

	m := guestAccessDataToModel(ctx, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.Password.ValueString() != "guest-pass" || !m.PasswordEnabled.ValueBool() {
		t.Fatalf("password = %v enabled = %v", m.Password, m.PasswordEnabled)
	}
	if m.Google.IsNull() {
		t.Fatal("google block should materialize")
	}
	var g settingGuestAccessGoogleModel
	diags.Append(m.Google.As(ctx, &g, basetypes.ObjectAsOptions{})...)
	if g.ClientID.ValueString() != "client-id" ||
		g.ClientSecret.ValueString() != "client-secret" {
		t.Fatalf("google = %+v", g)
	}
	if m.Radius.IsNull() {
		t.Fatal("radius block should materialize")
	}
	var r settingGuestAccessRadiusModel
	diags.Append(m.Radius.As(ctx, &r, basetypes.ObjectAsOptions{})...)
	if r.ProfileID.ValueString() != "rp1" || r.DisconnectPort.ValueInt64() != 3799 {
		t.Fatalf("radius = %+v", r)
	}
	if !m.Facebook.IsNull() || !m.FacebookWifi.IsNull() || !m.Wechat.IsNull() {
		t.Fatal("absent provider blocks should stay null")
	}
	if !m.RestrictedDNSServers.IsNull() {
		t.Fatal("absent restricted_dns_servers should stay null")
	}
}

// mkGuestAccessObj builds a minimal-but-complete guest_access object for the
// preserve tests below: top-level password plus the three nested blocks that
// carry secrets, with every other attribute null so ObjectValueFrom succeeds.
func mkGuestAccessObj(t *testing.T, password, fbSecret, fwSecret, googleSecret, wechatSecret, wechatKey types.String) types.Object {
	t.Helper()
	ctx := context.Background()

	facebook, d := types.ObjectValueFrom(ctx, guestAccessFacebookAttrTypes, settingGuestAccessFacebookModel{
		AppID:      types.StringValue("fb-app-id"),
		AppSecret:  fbSecret,
		ScopeEmail: types.BoolNull(),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	facebookWifi, d := types.ObjectValueFrom(ctx, guestAccessFacebookWifiAttrTypes, settingGuestAccessFacebookWifiModel{
		BlockHttps:    types.BoolNull(),
		GatewayID:     types.StringValue("gw-id"),
		GatewayName:   types.StringValue("gw-name"),
		GatewaySecret: fwSecret,
	})
	if d.HasError() {
		t.Fatal(d)
	}
	google, d := types.ObjectValueFrom(ctx, guestAccessGoogleAttrTypes, settingGuestAccessGoogleModel{
		ClientID:     types.StringValue("client-id"),
		ClientSecret: googleSecret,
		Domain:       types.StringNull(),
		ScopeEmail:   types.BoolNull(),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	wechat, d := types.ObjectValueFrom(ctx, guestAccessWechatAttrTypes, settingGuestAccessWechatModel{
		AppID:     types.StringValue("wechat-app-id"),
		AppSecret: wechatSecret,
		SecretKey: wechatKey,
		ShopID:    types.StringNull(),
	})
	if d.HasError() {
		t.Fatal(d)
	}

	obj, d := types.ObjectValueFrom(ctx, guestAccessAttrTypes, settingGuestAccessModel{
		AllowedSubnet:        types.StringNull(),
		RestrictedSubnet:     types.StringNull(),
		Auth:                 types.StringNull(),
		AuthUrl:              types.StringNull(),
		CustomIP:             types.StringNull(),
		EcEnabled:            types.BoolNull(),
		Expire:               types.Int64Null(),
		ExpireNumber:         types.Int64Null(),
		ExpireUnit:           types.Int64Null(),
		PortalCustomization:  types.ObjectNull(guestAccessPortalCustomizationAttrTypes),
		PortalEnabled:        types.BoolNull(),
		PortalHostname:       types.StringNull(),
		PortalUseHostname:    types.BoolNull(),
		Redirect:             types.ObjectNull(guestAccessRedirectAttrTypes),
		RedirectEnabled:      types.BoolNull(),
		TemplateEngine:       types.StringNull(),
		VoucherCustomized:    types.BoolNull(),
		VoucherEnabled:       types.BoolNull(),
		Facebook:             facebook,
		FacebookEnabled:      types.BoolNull(),
		FacebookWifi:         facebookWifi,
		Google:               google,
		GoogleEnabled:        types.BoolNull(),
		Password:             password,
		PasswordEnabled:      types.BoolNull(),
		Radius:               types.ObjectNull(guestAccessRadiusAttrTypes),
		RadiusEnabled:        types.BoolNull(),
		RestrictedDNSEnabled: types.BoolNull(),
		RestrictedDNSServers: types.ListNull(types.StringType),
		Wechat:               wechat,
		WechatEnabled:        types.BoolNull(),

		Authorize:       types.ObjectNull(guestAccessAuthorizeAttrTypes),
		IPpay:           types.ObjectNull(guestAccessIPpayAttrTypes),
		MerchantWarrior: types.ObjectNull(guestAccessMerchantWarriorAttrTypes),
		PaymentEnabled:  types.BoolNull(),
		PaymentGateway:  types.StringNull(),
		Paypal:          types.ObjectNull(guestAccessPaypalAttrTypes),
		Quickpay:        types.ObjectNull(guestAccessQuickpayAttrTypes),
		Stripe:          types.ObjectNull(guestAccessStripeAttrTypes),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	return obj
}

// mkGuestAccessObjWithPayment builds a minimal-but-complete guest_access
// object like mkGuestAccessObj, but with the stripe and paypal payment
// gateway blocks populated instead of the auth-provider blocks — covering
// the preserve extension over gateway credentials (Task 5's obligation
// beyond the brief).
func mkGuestAccessObjWithPayment(t *testing.T, stripeKey, paypalUsername, paypalPassword, paypalSignature types.String) types.Object {
	t.Helper()
	ctx := context.Background()

	stripe, d := types.ObjectValueFrom(ctx, guestAccessStripeAttrTypes, settingGuestAccessStripeModel{
		APIKey: stripeKey,
	})
	if d.HasError() {
		t.Fatal(d)
	}
	paypal, d := types.ObjectValueFrom(ctx, guestAccessPaypalAttrTypes, settingGuestAccessPaypalModel{
		Username:   paypalUsername,
		Password:   paypalPassword,
		Signature:  paypalSignature,
		UseSandbox: types.BoolNull(),
	})
	if d.HasError() {
		t.Fatal(d)
	}

	obj, d := types.ObjectValueFrom(ctx, guestAccessAttrTypes, settingGuestAccessModel{
		AllowedSubnet:        types.StringNull(),
		RestrictedSubnet:     types.StringNull(),
		Auth:                 types.StringNull(),
		AuthUrl:              types.StringNull(),
		CustomIP:             types.StringNull(),
		EcEnabled:            types.BoolNull(),
		Expire:               types.Int64Null(),
		ExpireNumber:         types.Int64Null(),
		ExpireUnit:           types.Int64Null(),
		PortalCustomization:  types.ObjectNull(guestAccessPortalCustomizationAttrTypes),
		PortalEnabled:        types.BoolNull(),
		PortalHostname:       types.StringNull(),
		PortalUseHostname:    types.BoolNull(),
		Redirect:             types.ObjectNull(guestAccessRedirectAttrTypes),
		RedirectEnabled:      types.BoolNull(),
		TemplateEngine:       types.StringNull(),
		VoucherCustomized:    types.BoolNull(),
		VoucherEnabled:       types.BoolNull(),
		Facebook:             types.ObjectNull(guestAccessFacebookAttrTypes),
		FacebookEnabled:      types.BoolNull(),
		FacebookWifi:         types.ObjectNull(guestAccessFacebookWifiAttrTypes),
		Google:               types.ObjectNull(guestAccessGoogleAttrTypes),
		GoogleEnabled:        types.BoolNull(),
		Password:             types.StringNull(),
		PasswordEnabled:      types.BoolNull(),
		Radius:               types.ObjectNull(guestAccessRadiusAttrTypes),
		RadiusEnabled:        types.BoolNull(),
		RestrictedDNSEnabled: types.BoolNull(),
		RestrictedDNSServers: types.ListNull(types.StringType),
		Wechat:               types.ObjectNull(guestAccessWechatAttrTypes),
		WechatEnabled:        types.BoolNull(),

		Authorize:       types.ObjectNull(guestAccessAuthorizeAttrTypes),
		IPpay:           types.ObjectNull(guestAccessIPpayAttrTypes),
		MerchantWarrior: types.ObjectNull(guestAccessMerchantWarriorAttrTypes),
		PaymentEnabled:  types.BoolNull(),
		PaymentGateway:  types.StringNull(),
		Paypal:          paypal,
		Quickpay:        types.ObjectNull(guestAccessQuickpayAttrTypes),
		Stripe:          stripe,
	})
	if d.HasError() {
		t.Fatal(d)
	}
	return obj
}

func Test_preserveGuestAccessSecrets_preservesWhenFreshNull(t *testing.T) {
	prior := mkGuestAccessObj(t,
		types.StringValue("prior-password"),
		types.StringValue("prior-fb-secret"),
		types.StringValue("prior-fw-secret"),
		types.StringValue("prior-google-secret"),
		types.StringValue("prior-wechat-secret"),
		types.StringValue("prior-wechat-key"),
	)
	fresh := mkGuestAccessObj(t,
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
	)

	merged := preserveGuestAccessSecrets(prior, fresh)
	if merged.IsNull() || merged.IsUnknown() {
		t.Fatal("merged object should not be null/unknown")
	}

	pw := merged.Attributes()["password"].(types.String)
	if pw.ValueString() != "prior-password" {
		t.Fatalf("password not preserved: %v", pw)
	}

	fb := merged.Attributes()["facebook"].(types.Object)
	if fb.Attributes()["app_secret"].(types.String).ValueString() != "prior-fb-secret" {
		t.Fatalf("facebook.app_secret not preserved: %v", fb.Attributes()["app_secret"])
	}
	if fb.Attributes()["app_id"].(types.String).ValueString() != "fb-app-id" {
		t.Fatal("facebook.app_id should be untouched (fresh, non-secret)")
	}

	fw := merged.Attributes()["facebook_wifi"].(types.Object)
	if fw.Attributes()["gateway_secret"].(types.String).ValueString() != "prior-fw-secret" {
		t.Fatalf("facebook_wifi.gateway_secret not preserved: %v", fw.Attributes()["gateway_secret"])
	}

	g := merged.Attributes()["google"].(types.Object)
	if g.Attributes()["client_secret"].(types.String).ValueString() != "prior-google-secret" {
		t.Fatalf("google.client_secret not preserved: %v", g.Attributes()["client_secret"])
	}

	w := merged.Attributes()["wechat"].(types.Object)
	if w.Attributes()["app_secret"].(types.String).ValueString() != "prior-wechat-secret" {
		t.Fatalf("wechat.app_secret not preserved: %v", w.Attributes()["app_secret"])
	}
	if w.Attributes()["secret_key"].(types.String).ValueString() != "prior-wechat-key" {
		t.Fatalf("wechat.secret_key not preserved: %v", w.Attributes()["secret_key"])
	}
}

func Test_preserveGuestAccessSecrets_echoWinsWhenFreshSet(t *testing.T) {
	prior := mkGuestAccessObj(t,
		types.StringValue("prior-password"),
		types.StringValue("prior-fb-secret"),
		types.StringValue("prior-fw-secret"),
		types.StringValue("prior-google-secret"),
		types.StringValue("prior-wechat-secret"),
		types.StringValue("prior-wechat-key"),
	)
	fresh := mkGuestAccessObj(t,
		types.StringValue("echoed-password"),
		types.StringValue("echoed-fb-secret"),
		types.StringValue("echoed-fw-secret"),
		types.StringValue("echoed-google-secret"),
		types.StringValue("echoed-wechat-secret"),
		types.StringValue("echoed-wechat-key"),
	)

	merged := preserveGuestAccessSecrets(prior, fresh)

	pw := merged.Attributes()["password"].(types.String)
	if pw.ValueString() != "echoed-password" {
		t.Fatalf("echoed password should win, got %v", pw)
	}
	fb := merged.Attributes()["facebook"].(types.Object)
	if fb.Attributes()["app_secret"].(types.String).ValueString() != "echoed-fb-secret" {
		t.Fatalf("echoed facebook.app_secret should win, got %v", fb.Attributes()["app_secret"])
	}
	fw := merged.Attributes()["facebook_wifi"].(types.Object)
	if fw.Attributes()["gateway_secret"].(types.String).ValueString() != "echoed-fw-secret" {
		t.Fatalf("echoed facebook_wifi.gateway_secret should win, got %v", fw.Attributes()["gateway_secret"])
	}
	g := merged.Attributes()["google"].(types.Object)
	if g.Attributes()["client_secret"].(types.String).ValueString() != "echoed-google-secret" {
		t.Fatalf("echoed google.client_secret should win, got %v", g.Attributes()["client_secret"])
	}
	w := merged.Attributes()["wechat"].(types.Object)
	if w.Attributes()["app_secret"].(types.String).ValueString() != "echoed-wechat-secret" {
		t.Fatalf("echoed wechat.app_secret should win, got %v", w.Attributes()["app_secret"])
	}
	if w.Attributes()["secret_key"].(types.String).ValueString() != "echoed-wechat-key" {
		t.Fatalf("echoed wechat.secret_key should win, got %v", w.Attributes()["secret_key"])
	}
}

func Test_preserveGuestAccessSecrets_nullPriorPassthrough(t *testing.T) {
	fresh := mkGuestAccessObj(t,
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
	)

	// No prior object at all (e.g. first read on Create): fresh passes
	// through untouched.
	if got := preserveGuestAccessSecrets(types.ObjectNull(guestAccessAttrTypes), fresh); !got.Equal(fresh) {
		t.Fatalf("null prior should pass fresh through, got %v", got)
	}

	// Prior present but every secret field null: nothing to preserve.
	priorNoSecrets := mkGuestAccessObj(t,
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
	)
	if got := preserveGuestAccessSecrets(priorNoSecrets, fresh); !got.Equal(fresh) {
		t.Fatalf("secretless prior should pass fresh through, got %v", got)
	}

	// Unknown prior (e.g. during Create planning): fresh passes through.
	unknownPrior := types.ObjectUnknown(guestAccessAttrTypes)
	if got := preserveGuestAccessSecrets(unknownPrior, fresh); !got.Equal(fresh) {
		t.Fatalf("unknown prior should pass fresh through, got %v", got)
	}
}

// Test_preserveGuestAccessSecrets_paymentGateways_preservesWhenFreshNull
// covers the preserve extension added on top of the brief for Task 5: the
// stripe and paypal payment gateway credentials are write-only, exactly
// like the auth-provider secrets covered above, and must be carried forward
// from prior state when a controller read doesn't echo them back.
func Test_preserveGuestAccessSecrets_paymentGateways_preservesWhenFreshNull(t *testing.T) {
	prior := mkGuestAccessObjWithPayment(t,
		types.StringValue("prior-stripe-key"),
		types.StringValue("prior-paypal-user"),
		types.StringValue("prior-paypal-pass"),
		types.StringValue("prior-paypal-sig"),
	)
	fresh := mkGuestAccessObjWithPayment(t,
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
		types.StringNull(),
	)

	merged := preserveGuestAccessSecrets(prior, fresh)
	if merged.IsNull() || merged.IsUnknown() {
		t.Fatal("merged object should not be null/unknown")
	}

	stripe := merged.Attributes()["stripe"].(types.Object)
	if stripe.Attributes()["api_key"].(types.String).ValueString() != "prior-stripe-key" {
		t.Fatalf("stripe.api_key not preserved: %v", stripe.Attributes()["api_key"])
	}

	paypal := merged.Attributes()["paypal"].(types.Object)
	if paypal.Attributes()["username"].(types.String).ValueString() != "prior-paypal-user" {
		t.Fatalf("paypal.username not preserved: %v", paypal.Attributes()["username"])
	}
	if paypal.Attributes()["password"].(types.String).ValueString() != "prior-paypal-pass" {
		t.Fatalf("paypal.password not preserved: %v", paypal.Attributes()["password"])
	}
	if paypal.Attributes()["signature"].(types.String).ValueString() != "prior-paypal-sig" {
		t.Fatalf("paypal.signature not preserved: %v", paypal.Attributes()["signature"])
	}
}

func Test_preserveGuestAccessSecrets_paymentGateways_echoWinsWhenFreshSet(t *testing.T) {
	prior := mkGuestAccessObjWithPayment(t,
		types.StringValue("prior-stripe-key"),
		types.StringValue("prior-paypal-user"),
		types.StringValue("prior-paypal-pass"),
		types.StringValue("prior-paypal-sig"),
	)
	fresh := mkGuestAccessObjWithPayment(t,
		types.StringValue("echoed-stripe-key"),
		types.StringValue("echoed-paypal-user"),
		types.StringValue("echoed-paypal-pass"),
		types.StringValue("echoed-paypal-sig"),
	)

	merged := preserveGuestAccessSecrets(prior, fresh)

	stripe := merged.Attributes()["stripe"].(types.Object)
	if stripe.Attributes()["api_key"].(types.String).ValueString() != "echoed-stripe-key" {
		t.Fatalf("echoed stripe.api_key should win, got %v", stripe.Attributes()["api_key"])
	}

	paypal := merged.Attributes()["paypal"].(types.Object)
	if paypal.Attributes()["username"].(types.String).ValueString() != "echoed-paypal-user" {
		t.Fatalf("echoed paypal.username should win, got %v", paypal.Attributes()["username"])
	}
	if paypal.Attributes()["password"].(types.String).ValueString() != "echoed-paypal-pass" {
		t.Fatalf("echoed paypal.password should win, got %v", paypal.Attributes()["password"])
	}
	if paypal.Attributes()["signature"].(types.String).ValueString() != "echoed-paypal-sig" {
		t.Fatalf("echoed paypal.signature should win, got %v", paypal.Attributes()["signature"])
	}
}

func Test_settingResource_Schema_guestAccess_sensitive(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)

	attrRaw, ok := resp.Schema.Attributes["guest_access"]
	if !ok {
		t.Fatal("schema is missing the guest_access section attribute")
	}
	guestAccess, ok := attrRaw.(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("guest_access attribute is %T, want SingleNestedAttribute", attrRaw)
	}

	// Top-level password must be Sensitive.
	pw, ok := guestAccess.Attributes["password"].(schema.StringAttribute)
	if !ok || !pw.Sensitive {
		t.Fatal("guest_access.password must be a Sensitive string attribute")
	}

	nestedSensitive := map[string][]string{
		"facebook":      {"app_secret"},
		"facebook_wifi": {"gateway_secret"},
		"google":        {"client_secret"},
		"wechat":        {"app_secret", "secret_key"},
	}
	for blockName, secretNames := range nestedSensitive {
		blockRaw, ok := guestAccess.Attributes[blockName]
		if !ok {
			t.Fatalf("guest_access.%s attribute missing", blockName)
		}
		block, ok := blockRaw.(schema.SingleNestedAttribute)
		if !ok {
			t.Fatalf("guest_access.%s is %T, want SingleNestedAttribute", blockName, blockRaw)
		}
		for _, secretName := range secretNames {
			a, ok := block.Attributes[secretName].(schema.StringAttribute)
			if !ok || !a.Sensitive {
				t.Fatalf("guest_access.%s.%s must be a Sensitive string attribute", blockName, secretName)
			}
		}
	}

	// google.client_id is deliberately NOT sensitive: OAuth client IDs are
	// not secrets.
	googleBlock := guestAccess.Attributes["google"].(schema.SingleNestedAttribute)
	clientID, ok := googleBlock.Attributes["client_id"].(schema.StringAttribute)
	if !ok {
		t.Fatal("guest_access.google.client_id attribute missing")
	}
	if clientID.Sensitive {
		t.Fatal("guest_access.google.client_id must NOT be Sensitive (OAuth client IDs aren't secrets)")
	}

	// Payment gateway credentials must all be Sensitive.
	paymentSensitive := map[string][]string{
		"authorize":        {"login_id", "transaction_key"},
		"ippay":            {"terminal_id"},
		"merchant_warrior": {"api_key", "api_passphrase", "merchant_uuid"},
		"paypal":           {"username", "password", "signature"},
		"quickpay":         {"agreement_id", "api_key", "merchant_id"},
		"stripe":           {"api_key"},
	}
	for blockName, secretNames := range paymentSensitive {
		blockRaw, ok := guestAccess.Attributes[blockName]
		if !ok {
			t.Fatalf("guest_access.%s attribute missing", blockName)
		}
		block, ok := blockRaw.(schema.SingleNestedAttribute)
		if !ok {
			t.Fatalf("guest_access.%s is %T, want SingleNestedAttribute", blockName, blockRaw)
		}
		for _, secretName := range secretNames {
			a, ok := block.Attributes[secretName].(schema.StringAttribute)
			if !ok || !a.Sensitive {
				t.Fatalf("guest_access.%s.%s must be a Sensitive string attribute", blockName, secretName)
			}
		}
	}

	// use_sandbox flags are not secrets.
	for _, blockName := range []string{"authorize", "ippay", "merchant_warrior", "paypal", "quickpay"} {
		block := guestAccess.Attributes[blockName].(schema.SingleNestedAttribute)
		useSandbox, ok := block.Attributes["use_sandbox"].(schema.BoolAttribute)
		if !ok {
			t.Fatalf("guest_access.%s.use_sandbox attribute missing", blockName)
		}
		if useSandbox.Sensitive {
			t.Fatalf("guest_access.%s.use_sandbox must NOT be Sensitive", blockName)
		}
	}
}

func Test_guestAccessModelToData_paymentGateways(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	paypal, d := types.ObjectValueFrom(ctx, guestAccessPaypalAttrTypes,
		settingGuestAccessPaypalModel{
			Username:   types.StringValue("merchant@example.com"),
			Password:   types.StringValue("paypal-pass"),
			Signature:  types.StringValue("paypal-sig"),
			UseSandbox: types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	quickpay, d := types.ObjectValueFrom(ctx, guestAccessQuickpayAttrTypes,
		settingGuestAccessQuickpayModel{
			AgreementID: types.StringValue("agreement"),
			APIKey:      types.StringValue("qp-key"),
			MerchantID:  types.StringValue("merchant"),
			UseSandbox:  types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingGuestAccessModel{
		PaymentEnabled: types.BoolValue(true),
		PaymentGateway: types.StringValue("paypal"),
		Paypal:         paypal,
		Quickpay:       quickpay,
		Authorize:      types.ObjectNull(guestAccessAuthorizeAttrTypes),
		IPpay:          types.ObjectNull(guestAccessIPpayAttrTypes),
		MerchantWarrior: types.ObjectNull(
			guestAccessMerchantWarriorAttrTypes),
		Stripe: types.ObjectNull(guestAccessStripeAttrTypes),
	}

	data := map[string]any{"unmodeled_field": "keep"}
	guestAccessModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["payment_enabled"] != true || data["gateway"] != "paypal" {
		t.Fatalf("payment fields wrong: %v", data)
	}
	if data["x_paypal_username"] != "merchant@example.com" ||
		data["x_paypal_password"] != "paypal-pass" ||
		data["x_paypal_signature"] != "paypal-sig" ||
		data["paypal_use_sandbox"] != true {
		t.Fatalf("paypal fields wrong: %v", data)
	}
	if data["x_quickpay_agreementid"] != "agreement" ||
		data["x_quickpay_apikey"] != "qp-key" ||
		data["x_quickpay_merchantid"] != "merchant" ||
		data["quickpay_testmode"] != true {
		t.Fatalf("quickpay fields wrong: %v", data)
	}
	if _, present := data["x_stripe_api_key"]; present {
		t.Fatal("null stripe block should not write keys")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_guestAccessDataToModel_paymentPresence(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	data := map[string]any{
		"payment_enabled":  true,
		"gateway":          "stripe",
		"x_stripe_api_key": "stripe-key",
	}

	m := guestAccessDataToModel(ctx, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.PaymentEnabled.ValueBool() || m.PaymentGateway.ValueString() != "stripe" {
		t.Fatalf("payment = %v gateway = %v", m.PaymentEnabled, m.PaymentGateway)
	}
	if m.Stripe.IsNull() {
		t.Fatal("stripe block should materialize")
	}
	var s settingGuestAccessStripeModel
	diags.Append(m.Stripe.As(ctx, &s, basetypes.ObjectAsOptions{})...)
	if s.APIKey.ValueString() != "stripe-key" {
		t.Fatalf("stripe = %+v", s)
	}
	if !m.Paypal.IsNull() || !m.Quickpay.IsNull() || !m.Authorize.IsNull() ||
		!m.IPpay.IsNull() || !m.MerchantWarrior.IsNull() {
		t.Fatal("absent gateway blocks should stay null")
	}
}
