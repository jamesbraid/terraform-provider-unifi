package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
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
