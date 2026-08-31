package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestGuestAccessSettingRoundTrip exercises this task's 21 attributes
// through the Spec's own ToSDK/ToModel: model -> go-unifi setting -> model
// preserves every field.
func TestGuestAccessSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()

	in := &settingGuestAccessModel{
		Auth:                    types.StringValue("hotspot"),
		EcEnabled:               types.BoolValue(true),
		Expire:                  types.StringValue("custom"),
		ExpireNumber:            types.Int64Value(30),
		ExpireUnit:              types.Int64Value(60),
		Gateway:                 types.StringValue("stripe"),
		PasswordEnabled:         types.BoolValue(true),
		PaymentEnabled:          types.BoolValue(true),
		PortalEnabled:           types.BoolValue(true),
		PortalHostname:          types.StringValue("guest.example.com"),
		PortalUseHostname:       types.BoolValue(true),
		RADIUSAuthType:          types.StringValue("mschapv2"),
		RADIUSDisconnectEnabled: types.BoolValue(true),
		RADIUSDisconnectPort:    types.Int64Value(3799),
		RADIUSEnabled:           types.BoolValue(true),
		RADIUSProfileID:         types.StringValue("radius-profile-id"),
		RedirectEnabled:         types.BoolValue(true),
		RedirectHttps:           types.BoolValue(true),
		RedirectToHttps:         types.BoolValue(false),
		RedirectUrl:             types.StringValue("https://example.com/welcome"),
		VoucherEnabled:          types.BoolValue(true),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}

	want := &settings.GuestAccess{
		Auth:                    "hotspot",
		EcEnabled:               true,
		Expire:                  "custom",
		ExpireNumber:            int64Ptr(30),
		ExpireUnit:              int64Ptr(60),
		Gateway:                 "stripe",
		PasswordEnabled:         true,
		PaymentEnabled:          true,
		PortalEnabled:           true,
		PortalHostname:          "guest.example.com",
		PortalUseHostname:       true,
		RADIUSAuthType:          "mschapv2",
		RADIUSDisconnectEnabled: true,
		RADIUSDisconnectPort:    int64Ptr(3799),
		RADIUSEnabled:           true,
		RADIUSProfileID:         "radius-profile-id",
		RedirectEnabled:         true,
		RedirectHttps:           true,
		RedirectToHttps:         false,
		RedirectUrl:             "https://example.com/welcome",
		VoucherEnabled:          true,
	}
	if sdk.Auth != want.Auth || sdk.EcEnabled != want.EcEnabled || sdk.Expire != want.Expire ||
		sdk.ExpireNumber == nil || *sdk.ExpireNumber != *want.ExpireNumber ||
		sdk.ExpireUnit == nil || *sdk.ExpireUnit != *want.ExpireUnit ||
		sdk.Gateway != want.Gateway || sdk.PasswordEnabled != want.PasswordEnabled ||
		sdk.PaymentEnabled != want.PaymentEnabled || sdk.PortalEnabled != want.PortalEnabled ||
		sdk.PortalHostname != want.PortalHostname || sdk.PortalUseHostname != want.PortalUseHostname ||
		sdk.RADIUSAuthType != want.RADIUSAuthType || sdk.RADIUSDisconnectEnabled != want.RADIUSDisconnectEnabled ||
		sdk.RADIUSDisconnectPort == nil || *sdk.RADIUSDisconnectPort != *want.RADIUSDisconnectPort ||
		sdk.RADIUSEnabled != want.RADIUSEnabled || sdk.RADIUSProfileID != want.RADIUSProfileID ||
		sdk.RedirectEnabled != want.RedirectEnabled || sdk.RedirectHttps != want.RedirectHttps ||
		sdk.RedirectToHttps != want.RedirectToHttps || sdk.RedirectUrl != want.RedirectUrl ||
		sdk.VoucherEnabled != want.VoucherEnabled {
		t.Fatalf("ToSDK = %+v, want %+v", sdk, want)
	}

	var out settingGuestAccessModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.Auth != in.Auth || out.EcEnabled != in.EcEnabled || out.Expire != in.Expire ||
		out.ExpireNumber != in.ExpireNumber || out.ExpireUnit != in.ExpireUnit ||
		out.Gateway != in.Gateway || out.PasswordEnabled != in.PasswordEnabled ||
		out.PaymentEnabled != in.PaymentEnabled || out.PortalEnabled != in.PortalEnabled ||
		out.PortalHostname != in.PortalHostname || out.PortalUseHostname != in.PortalUseHostname ||
		out.RADIUSAuthType != in.RADIUSAuthType || out.RADIUSDisconnectEnabled != in.RADIUSDisconnectEnabled ||
		out.RADIUSDisconnectPort != in.RADIUSDisconnectPort || out.RADIUSEnabled != in.RADIUSEnabled ||
		out.RADIUSProfileID != in.RADIUSProfileID || out.RedirectEnabled != in.RedirectEnabled ||
		out.RedirectHttps != in.RedirectHttps || out.RedirectToHttps != in.RedirectToHttps ||
		out.RedirectUrl != in.RedirectUrl || out.VoucherEnabled != in.VoucherEnabled {
		t.Errorf("guest_access round-trip mismatch: %+v", out)
	}
}

func int64Ptr(v int64) *int64 { return &v }

// TestGuestAccessSpecOmitsUnsetOmitZeroInts is the #303 write-side guard
// pinned for guest_access's three trap fields: expire_number, expire_unit
// and radius_disconnect_port each reject a literal 0 at the controller
// (expire_number wants a leading 1-9 or exactly 1000000, expire_unit is the
// enum 1/60/1440, radius_disconnect_port has a minimum of 1), so a null or
// unknown plan value -- and an explicit 0 -- must never reach the wire as a
// pointer to zero. This is the class of bug the plan cites as costing four
// live round trips in the WLAN work; a non-zero value is asserted too, so
// the guard is confirmed to skip only zero/absent, not every value.
func TestGuestAccessSpecOmitsUnsetOmitZeroInts(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()

	cases := []struct {
		name  string
		value types.Int64
	}{
		{"null", types.Int64Null()},
		{"unknown", types.Int64Unknown()},
		{"explicit zero", types.Int64Value(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &settingGuestAccessModel{
				ExpireNumber:         tc.value,
				ExpireUnit:           tc.value,
				RADIUSDisconnectPort: tc.value,
			}
			sdk, diags := spec.ToSDK(ctx, in)
			if diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			if sdk.ExpireNumber != nil {
				t.Errorf("expire_number = %v, want omitted (never a literal 0) for a %s plan value",
					*sdk.ExpireNumber, tc.name)
			}
			if sdk.ExpireUnit != nil {
				t.Errorf("expire_unit = %v, want omitted (never a literal 0) for a %s plan value",
					*sdk.ExpireUnit, tc.name)
			}
			if sdk.RADIUSDisconnectPort != nil {
				t.Errorf("radius_disconnect_port = %v, want omitted (never a literal 0) for a %s plan value",
					*sdk.RADIUSDisconnectPort, tc.name)
			}
		})
	}

	t.Run("a real value reaches the wire", func(t *testing.T) {
		in := &settingGuestAccessModel{
			ExpireNumber:         types.Int64Value(30),
			ExpireUnit:           types.Int64Value(60),
			RADIUSDisconnectPort: types.Int64Value(3799),
		}
		sdk, diags := spec.ToSDK(ctx, in)
		if diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if sdk.ExpireNumber == nil || *sdk.ExpireNumber != 30 {
			t.Errorf("expire_number = %v, want 30 (OmitZero must not suppress a real value)", sdk.ExpireNumber)
		}
		if sdk.ExpireUnit == nil || *sdk.ExpireUnit != 60 {
			t.Errorf("expire_unit = %v, want 60 (OmitZero must not suppress a real value)", sdk.ExpireUnit)
		}
		if sdk.RADIUSDisconnectPort == nil || *sdk.RADIUSDisconnectPort != 3799 {
			t.Errorf("radius_disconnect_port = %v, want 3799 (OmitZero must not suppress a real value)",
				sdk.RADIUSDisconnectPort)
		}
	})
}

// TestGuestAccessPortalAppearanceOmitsAZeroTheControllerRejects is Task 5's
// own version of the #303 write-side guard, and the one test in this group
// nothing else would catch: portal_customized_box_opacity (minimum 1) and
// portal_customized_logo_size (minimum 64) both reject a literal zero at the
// controller, so a null, unknown or explicit-zero plan value must never
// reach the wire as a pointer to zero -- but portal_customized_box_radius's
// own constraint has a minimum of 0, so an explicit 0 is a legal, meaningful
// corner radius (square corners) that must reach the wire rather than being
// silently elided. Getting the third field wrong produces no error at any
// layer: the value simply never arrives at the controller. See
// TestGuestAccessSpecOmitsUnsetOmitZeroInts above for Task 2's three
// (expire_number, expire_unit, radius_disconnect_port), which all reject
// zero and all set OmitZero; this task's three do not behave alike.
func TestGuestAccessPortalAppearanceOmitsAZeroTheControllerRejects(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()

	cases := []struct {
		name  string
		value types.Int64
	}{
		{"null", types.Int64Null()},
		{"unknown", types.Int64Unknown()},
		{"explicit zero", types.Int64Value(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &settingGuestAccessModel{
				PortalCustomizedBoxOpacity: tc.value,
				PortalCustomizedLogoSize:   tc.value,
			}
			sdk, diags := spec.ToSDK(ctx, in)
			if diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			if sdk.PortalCustomizedBoxOpacity != nil {
				t.Errorf("portal_customized_box_opacity = %v, want omitted (never a literal 0) for a %s "+
					"plan value", *sdk.PortalCustomizedBoxOpacity, tc.name)
			}
			if sdk.PortalCustomizedLogoSize != nil {
				t.Errorf("portal_customized_logo_size = %v, want omitted (never a literal 0) for a %s "+
					"plan value", *sdk.PortalCustomizedLogoSize, tc.name)
			}
		})
	}

	// Unlike box_opacity and logo_size, box_radius sets no OmitZero, so its
	// ToSDK behaves like any plain Int64PtrField (see that type's own ToSDK
	// comment): null stays nil, but unknown collapses to a pointer to 0, the
	// same as an explicit zero -- Int64PtrField has no way to tell "the plan
	// left this alone" from "the plan set this to 0" once OmitZero is off.
	// That collapse is harmless here specifically because it is legal:
	// SetInPlan still reports false for both null and unknown, so the masked
	// write's own field list leaves portal_customized_box_radius out
	// regardless of what value sits in the SDK struct -- the point this
	// subtest actually pins is the explicit-zero case, which SetInPlan does
	// report true, and which must reach the wire as 0 rather than being
	// dropped the way box_opacity's and logo_size's would be.
	t.Run("box_radius's own zero is legal and must reach the wire", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				in := &settingGuestAccessModel{PortalCustomizedBoxRADIUS: tc.value}
				sdk, diags := spec.ToSDK(ctx, in)
				if diags.HasError() {
					t.Fatalf("ToSDK: %v", diags)
				}
				switch tc.name {
				case "null":
					if sdk.PortalCustomizedBoxRADIUS != nil {
						t.Errorf("portal_customized_box_radius = %v, want nil for a null plan value",
							*sdk.PortalCustomizedBoxRADIUS)
					}
				default: // "unknown" and "explicit zero" both collapse to a pointer to 0
					if sdk.PortalCustomizedBoxRADIUS == nil || *sdk.PortalCustomizedBoxRADIUS != 0 {
						t.Errorf("portal_customized_box_radius = %v, want a pointer to 0 for a %s plan "+
							"value -- square corners is a legal, distinguishable value the controller "+
							"accepts, not something to omit", sdk.PortalCustomizedBoxRADIUS, tc.name)
					}
				}
				// Int64PtrField.SetInPlan (field.go) is exactly this: a value
				// counts as "in plan" only when neither null nor unknown. The
				// masked write includes portal_customized_box_radius only when
				// this is true, which is what keeps the unknown case's harmless
				// wire zero (above) from ever reaching the controller.
				if got, want := !tc.value.IsNull() && !tc.value.IsUnknown(), tc.name == "explicit zero"; got != want {
					t.Errorf("SetInPlan for a %s plan value = %v, want %v -- the masked write includes "+
						"portal_customized_box_radius only when SetInPlan is true", tc.name, got, want)
				}
			})
		}
	})

	t.Run("a real value reaches the wire for all three", func(t *testing.T) {
		in := &settingGuestAccessModel{
			PortalCustomizedBoxOpacity: types.Int64Value(80),
			PortalCustomizedBoxRADIUS:  types.Int64Value(12),
			PortalCustomizedLogoSize:   types.Int64Value(96),
		}
		sdk, diags := spec.ToSDK(ctx, in)
		if diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if sdk.PortalCustomizedBoxOpacity == nil || *sdk.PortalCustomizedBoxOpacity != 80 {
			t.Errorf("portal_customized_box_opacity = %v, want 80 (OmitZero must not suppress a real value)",
				sdk.PortalCustomizedBoxOpacity)
		}
		if sdk.PortalCustomizedBoxRADIUS == nil || *sdk.PortalCustomizedBoxRADIUS != 12 {
			t.Errorf("portal_customized_box_radius = %v, want 12", sdk.PortalCustomizedBoxRADIUS)
		}
		if sdk.PortalCustomizedLogoSize == nil || *sdk.PortalCustomizedLogoSize != 96 {
			t.Errorf("portal_customized_logo_size = %v, want 96 (OmitZero must not suppress a real value)",
				sdk.PortalCustomizedLogoSize)
		}
	})
}

// TestGuestAccessBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the
// unit half of guest_access's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs guestAccessKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the field the mask named plus "key".
// gateway is guest_access's other field on the wire test double below --
// omitempty on settings.GuestAccess, so an unmasked encode would leave it
// out regardless; portal_enabled instead (no omitempty, forced onto every
// unmasked encode) is what makes this a masked write and not a
// whole-document one.
func TestGuestAccessBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("the provider sent a body that is not an object: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	backend := guestAccessKitBackend(api)
	sdk := &settings.GuestAccess{Gateway: "stripe"}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "gateway"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "gateway": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// portal_enabled is guest_access's other field -- present on
	// settings.GuestAccess with no omitempty, so an unmasked encode would
	// always carry it. Its absence here is what makes this a masked write
	// and not a whole-document one.
	if _, ok := body["portal_enabled"]; ok {
		t.Error(`PUT body carries "portal_enabled", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestGuestAccessKitSpecConformance runs the same conformance instruments
// every other kit descriptor's test applies (see
// setting_mgmt_descriptor_test.go's TestMgmtKitSpecConformance), scoped to
// guest_access's own nested schema rather than a whole resource's, since
// guest_access is one section of unifi_setting rather than a surface of
// its own.
func TestGuestAccessKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := guestAccessNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestGuestAccessNestedSchemaHasExactlyItsAttributes guards
// guestAccessNestedSchema's type assertion against a generator regression:
// "guest_access" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this
// pins the shape ahead of that. The count is Task 2's 21, Task 3's 18
// x_-prefixed fields (guestAccessSecret), Task 4's 20 and Task 5's 31
// portal_customized_* fields -- the only two of settings.GuestAccess's 92
// fields still deferred are allowed_subnet_ and restricted_subnet_,
// withdrawn after a live apply against the pinned controller rejected both
// (see setting_guest_access_descriptor.go's own comment).
func TestGuestAccessNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["guest_access"]; !ok {
		t.Fatal(`the generated setting schema has no "guest_access" attribute`)
	}
	nested := guestAccessNestedSchema(ctx)
	if len(nested.Attributes) != 90 {
		t.Errorf("guest_access has %d attribute(s), want 90; update guestAccessKitSpec and this count together",
			len(nested.Attributes))
	}
}

// TestGuestAccessNetworkScopingSocialLoginPaymentAndStragglersRoundTrip
// exercises Task 4's 20 fields through the Spec's own ToSDK/ToModel: model ->
// go-unifi setting -> model preserves every field. Shaped like
// TestGuestAccessSettingRoundTrip, Task 2's own version of this test, kept
// separate rather than folded into it so each task's own round trip stays
// readable against its own field list. allowed_subnet_ and restricted_subnet_
// are not here: the brief's original 22 named them, but a live apply against
// the pinned controller (10.6.101) rejected both with api.err.InvalidKey, so
// setting_guest_access_descriptor.go withdrew them back to
// policy/setting.json's omitted list rather than ship a write that always
// fails.
func TestGuestAccessNetworkScopingSocialLoginPaymentAndStragglersRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()

	restrictedDNSServers, diags := types.ListValueFrom(ctx, types.StringType, []string{"1.1.1.1", "8.8.8.8"})
	if diags.HasError() {
		t.Fatalf("building restricted_dns_servers: %v", diags)
	}

	in := &settingGuestAccessModel{
		AuthUrl:                   types.StringValue("https://auth.example.com/login"),
		AuthorizeUseSandbox:       types.BoolValue(true),
		CustomIP:                  types.StringValue("203.0.113.5"),
		FacebookAppID:             types.StringValue("fb-app-id"),
		FacebookEnabled:           types.BoolValue(true),
		FacebookScopeEmail:        types.BoolValue(true),
		GoogleClientID:            types.StringValue("google-client-id"),
		GoogleDomain:              types.StringValue("example.com"),
		GoogleEnabled:             types.BoolValue(true),
		GoogleScopeEmail:          types.BoolValue(true),
		IPpayUseSandbox:           types.BoolValue(true),
		MerchantwarriorUseSandbox: types.BoolValue(true),
		PaypalUseSandbox:          types.BoolValue(true),
		QuickpayTestmode:          types.BoolValue(true),
		RestrictedDNSEnabled:      types.BoolValue(true),
		RestrictedDNSServers:      restrictedDNSServers,
		VoucherCustomized:         types.BoolValue(true),
		WechatAppID:               types.StringValue("wechat-app-id"),
		WechatEnabled:             types.BoolValue(true),
		WechatShopID:              types.StringValue("wechat-shop-id"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}

	want := &settings.GuestAccess{
		AuthUrl:                   "https://auth.example.com/login",
		AuthorizeUseSandbox:       true,
		CustomIP:                  "203.0.113.5",
		FacebookAppID:             "fb-app-id",
		FacebookEnabled:           true,
		FacebookScopeEmail:        true,
		GoogleClientID:            "google-client-id",
		GoogleDomain:              "example.com",
		GoogleEnabled:             true,
		GoogleScopeEmail:          true,
		IPpayUseSandbox:           true,
		MerchantwarriorUseSandbox: true,
		PaypalUseSandbox:          true,
		QuickpayTestmode:          true,
		RestrictedDNSEnabled:      true,
		RestrictedDNSServers:      []string{"1.1.1.1", "8.8.8.8"},
		VoucherCustomized:         true,
		WechatAppID:               "wechat-app-id",
		WechatEnabled:             true,
		WechatShopID:              "wechat-shop-id",
	}
	if sdk.AuthUrl != want.AuthUrl ||
		sdk.AuthorizeUseSandbox != want.AuthorizeUseSandbox || sdk.CustomIP != want.CustomIP ||
		sdk.FacebookAppID != want.FacebookAppID || sdk.FacebookEnabled != want.FacebookEnabled ||
		sdk.FacebookScopeEmail != want.FacebookScopeEmail || sdk.GoogleClientID != want.GoogleClientID ||
		sdk.GoogleDomain != want.GoogleDomain || sdk.GoogleEnabled != want.GoogleEnabled ||
		sdk.GoogleScopeEmail != want.GoogleScopeEmail || sdk.IPpayUseSandbox != want.IPpayUseSandbox ||
		sdk.MerchantwarriorUseSandbox != want.MerchantwarriorUseSandbox ||
		sdk.PaypalUseSandbox != want.PaypalUseSandbox || sdk.QuickpayTestmode != want.QuickpayTestmode ||
		sdk.RestrictedDNSEnabled != want.RestrictedDNSEnabled ||
		!slices.Equal(sdk.RestrictedDNSServers, want.RestrictedDNSServers) ||
		sdk.VoucherCustomized != want.VoucherCustomized ||
		sdk.WechatAppID != want.WechatAppID || sdk.WechatEnabled != want.WechatEnabled ||
		sdk.WechatShopID != want.WechatShopID {
		t.Fatalf("ToSDK = %+v, want %+v", sdk, want)
	}

	var out settingGuestAccessModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.AuthUrl != in.AuthUrl ||
		out.AuthorizeUseSandbox != in.AuthorizeUseSandbox || out.CustomIP != in.CustomIP ||
		out.FacebookAppID != in.FacebookAppID || out.FacebookEnabled != in.FacebookEnabled ||
		out.FacebookScopeEmail != in.FacebookScopeEmail || out.GoogleClientID != in.GoogleClientID ||
		out.GoogleDomain != in.GoogleDomain || out.GoogleEnabled != in.GoogleEnabled ||
		out.GoogleScopeEmail != in.GoogleScopeEmail || out.IPpayUseSandbox != in.IPpayUseSandbox ||
		out.MerchantwarriorUseSandbox != in.MerchantwarriorUseSandbox ||
		out.PaypalUseSandbox != in.PaypalUseSandbox || out.QuickpayTestmode != in.QuickpayTestmode ||
		out.RestrictedDNSEnabled != in.RestrictedDNSEnabled ||
		!out.RestrictedDNSServers.Equal(in.RestrictedDNSServers) ||
		out.VoucherCustomized != in.VoucherCustomized ||
		out.WechatAppID != in.WechatAppID || out.WechatEnabled != in.WechatEnabled ||
		out.WechatShopID != in.WechatShopID {
		t.Errorf("guest_access Task 4 round-trip mismatch: %+v", out)
	}
}

// TestGuestAccessPortalAppearanceRoundTrip exercises Task 5's 31
// portal_customized_* fields through the Spec's own ToSDK/ToModel: model ->
// go-unifi setting -> model preserves every field. Shaped like
// TestGuestAccessSettingRoundTrip and
// TestGuestAccessNetworkScopingSocialLoginPaymentAndStragglersRoundTrip, kept
// separate so each task's own round trip stays readable against its own
// field list.
func TestGuestAccessPortalAppearanceRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()

	languages, diags := types.ListValueFrom(ctx, types.StringType, []string{"en", "zh-CN"})
	if diags.HasError() {
		t.Fatalf("building portal_customized_languages: %v", diags)
	}

	in := &settingGuestAccessModel{
		PortalCustomized:                       types.BoolValue(true),
		PortalCustomizedAuthenticationText:     types.StringValue("Please sign in to continue"),
		PortalCustomizedBgColor:                types.StringValue("#ffffff"),
		PortalCustomizedBgImageEnabled:         types.BoolValue(true),
		PortalCustomizedBgImageFilename:        types.StringValue("background.jpg"),
		PortalCustomizedBgImageTile:            types.BoolValue(true),
		PortalCustomizedBgType:                 types.StringValue("image"),
		PortalCustomizedBoxColor:               types.StringValue("#000000"),
		PortalCustomizedBoxLinkColor:           types.StringValue("#111111"),
		PortalCustomizedBoxOpacity:             types.Int64Value(80),
		PortalCustomizedBoxRADIUS:              types.Int64Value(12),
		PortalCustomizedBoxTextColor:           types.StringValue("#222222"),
		PortalCustomizedButtonColor:            types.StringValue("#333333"),
		PortalCustomizedButtonText:             types.StringValue("Continue"),
		PortalCustomizedButtonTextColor:        types.StringValue("#444444"),
		PortalCustomizedLanguages:              languages,
		PortalCustomizedLinkColor:              types.StringValue("#555555"),
		PortalCustomizedLogoEnabled:            types.BoolValue(true),
		PortalCustomizedLogoFilename:           types.StringValue("logo.png"),
		PortalCustomizedLogoPosition:           types.StringValue("center"),
		PortalCustomizedLogoSize:               types.Int64Value(96),
		PortalCustomizedSuccessText:            types.StringValue("You're connected"),
		PortalCustomizedTextColor:              types.StringValue("#666666"),
		PortalCustomizedTitle:                  types.StringValue("Welcome"),
		PortalCustomizedTos:                    types.StringValue("Terms apply"),
		PortalCustomizedTosEnabled:             types.BoolValue(true),
		PortalCustomizedUnsplashAuthorName:     types.StringValue("Jane Doe"),
		PortalCustomizedUnsplashAuthorUsername: types.StringValue("janedoe"),
		PortalCustomizedWelcomeText:            types.StringValue("Welcome to our guest network"),
		PortalCustomizedWelcomeTextEnabled:     types.BoolValue(true),
		PortalCustomizedWelcomeTextPosition:    types.StringValue("under_logo"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}

	want := &settings.GuestAccess{
		PortalCustomized:                       true,
		PortalCustomizedAuthenticationText:     "Please sign in to continue",
		PortalCustomizedBgColor:                "#ffffff",
		PortalCustomizedBgImageEnabled:         true,
		PortalCustomizedBgImageFilename:        "background.jpg",
		PortalCustomizedBgImageTile:            true,
		PortalCustomizedBgType:                 "image",
		PortalCustomizedBoxColor:               "#000000",
		PortalCustomizedBoxLinkColor:           "#111111",
		PortalCustomizedBoxOpacity:             int64Ptr(80),
		PortalCustomizedBoxRADIUS:              int64Ptr(12),
		PortalCustomizedBoxTextColor:           "#222222",
		PortalCustomizedButtonColor:            "#333333",
		PortalCustomizedButtonText:             "Continue",
		PortalCustomizedButtonTextColor:        "#444444",
		PortalCustomizedLanguages:              []string{"en", "zh-CN"},
		PortalCustomizedLinkColor:              "#555555",
		PortalCustomizedLogoEnabled:            true,
		PortalCustomizedLogoFilename:           "logo.png",
		PortalCustomizedLogoPosition:           "center",
		PortalCustomizedLogoSize:               int64Ptr(96),
		PortalCustomizedSuccessText:            "You're connected",
		PortalCustomizedTextColor:              "#666666",
		PortalCustomizedTitle:                  "Welcome",
		PortalCustomizedTos:                    "Terms apply",
		PortalCustomizedTosEnabled:             true,
		PortalCustomizedUnsplashAuthorName:     "Jane Doe",
		PortalCustomizedUnsplashAuthorUsername: "janedoe",
		PortalCustomizedWelcomeText:            "Welcome to our guest network",
		PortalCustomizedWelcomeTextEnabled:     true,
		PortalCustomizedWelcomeTextPosition:    "under_logo",
	}
	if sdk.PortalCustomized != want.PortalCustomized ||
		sdk.PortalCustomizedAuthenticationText != want.PortalCustomizedAuthenticationText ||
		sdk.PortalCustomizedBgColor != want.PortalCustomizedBgColor ||
		sdk.PortalCustomizedBgImageEnabled != want.PortalCustomizedBgImageEnabled ||
		sdk.PortalCustomizedBgImageFilename != want.PortalCustomizedBgImageFilename ||
		sdk.PortalCustomizedBgImageTile != want.PortalCustomizedBgImageTile ||
		sdk.PortalCustomizedBgType != want.PortalCustomizedBgType ||
		sdk.PortalCustomizedBoxColor != want.PortalCustomizedBoxColor ||
		sdk.PortalCustomizedBoxLinkColor != want.PortalCustomizedBoxLinkColor ||
		sdk.PortalCustomizedBoxOpacity == nil || *sdk.PortalCustomizedBoxOpacity != *want.PortalCustomizedBoxOpacity ||
		sdk.PortalCustomizedBoxRADIUS == nil || *sdk.PortalCustomizedBoxRADIUS != *want.PortalCustomizedBoxRADIUS ||
		sdk.PortalCustomizedBoxTextColor != want.PortalCustomizedBoxTextColor ||
		sdk.PortalCustomizedButtonColor != want.PortalCustomizedButtonColor ||
		sdk.PortalCustomizedButtonText != want.PortalCustomizedButtonText ||
		sdk.PortalCustomizedButtonTextColor != want.PortalCustomizedButtonTextColor ||
		!slices.Equal(sdk.PortalCustomizedLanguages, want.PortalCustomizedLanguages) ||
		sdk.PortalCustomizedLinkColor != want.PortalCustomizedLinkColor ||
		sdk.PortalCustomizedLogoEnabled != want.PortalCustomizedLogoEnabled ||
		sdk.PortalCustomizedLogoFilename != want.PortalCustomizedLogoFilename ||
		sdk.PortalCustomizedLogoPosition != want.PortalCustomizedLogoPosition ||
		sdk.PortalCustomizedLogoSize == nil || *sdk.PortalCustomizedLogoSize != *want.PortalCustomizedLogoSize ||
		sdk.PortalCustomizedSuccessText != want.PortalCustomizedSuccessText ||
		sdk.PortalCustomizedTextColor != want.PortalCustomizedTextColor ||
		sdk.PortalCustomizedTitle != want.PortalCustomizedTitle ||
		sdk.PortalCustomizedTos != want.PortalCustomizedTos ||
		sdk.PortalCustomizedTosEnabled != want.PortalCustomizedTosEnabled ||
		sdk.PortalCustomizedUnsplashAuthorName != want.PortalCustomizedUnsplashAuthorName ||
		sdk.PortalCustomizedUnsplashAuthorUsername != want.PortalCustomizedUnsplashAuthorUsername ||
		sdk.PortalCustomizedWelcomeText != want.PortalCustomizedWelcomeText ||
		sdk.PortalCustomizedWelcomeTextEnabled != want.PortalCustomizedWelcomeTextEnabled ||
		sdk.PortalCustomizedWelcomeTextPosition != want.PortalCustomizedWelcomeTextPosition {
		t.Fatalf("ToSDK = %+v, want %+v", sdk, want)
	}

	var out settingGuestAccessModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.PortalCustomized != in.PortalCustomized ||
		out.PortalCustomizedAuthenticationText != in.PortalCustomizedAuthenticationText ||
		out.PortalCustomizedBgColor != in.PortalCustomizedBgColor ||
		out.PortalCustomizedBgImageEnabled != in.PortalCustomizedBgImageEnabled ||
		out.PortalCustomizedBgImageFilename != in.PortalCustomizedBgImageFilename ||
		out.PortalCustomizedBgImageTile != in.PortalCustomizedBgImageTile ||
		out.PortalCustomizedBgType != in.PortalCustomizedBgType ||
		out.PortalCustomizedBoxColor != in.PortalCustomizedBoxColor ||
		out.PortalCustomizedBoxLinkColor != in.PortalCustomizedBoxLinkColor ||
		out.PortalCustomizedBoxOpacity != in.PortalCustomizedBoxOpacity ||
		out.PortalCustomizedBoxRADIUS != in.PortalCustomizedBoxRADIUS ||
		out.PortalCustomizedBoxTextColor != in.PortalCustomizedBoxTextColor ||
		out.PortalCustomizedButtonColor != in.PortalCustomizedButtonColor ||
		out.PortalCustomizedButtonText != in.PortalCustomizedButtonText ||
		out.PortalCustomizedButtonTextColor != in.PortalCustomizedButtonTextColor ||
		!out.PortalCustomizedLanguages.Equal(in.PortalCustomizedLanguages) ||
		out.PortalCustomizedLinkColor != in.PortalCustomizedLinkColor ||
		out.PortalCustomizedLogoEnabled != in.PortalCustomizedLogoEnabled ||
		out.PortalCustomizedLogoFilename != in.PortalCustomizedLogoFilename ||
		out.PortalCustomizedLogoPosition != in.PortalCustomizedLogoPosition ||
		out.PortalCustomizedLogoSize != in.PortalCustomizedLogoSize ||
		out.PortalCustomizedSuccessText != in.PortalCustomizedSuccessText ||
		out.PortalCustomizedTextColor != in.PortalCustomizedTextColor ||
		out.PortalCustomizedTitle != in.PortalCustomizedTitle ||
		out.PortalCustomizedTos != in.PortalCustomizedTos ||
		out.PortalCustomizedTosEnabled != in.PortalCustomizedTosEnabled ||
		out.PortalCustomizedUnsplashAuthorName != in.PortalCustomizedUnsplashAuthorName ||
		out.PortalCustomizedUnsplashAuthorUsername != in.PortalCustomizedUnsplashAuthorUsername ||
		out.PortalCustomizedWelcomeText != in.PortalCustomizedWelcomeText ||
		out.PortalCustomizedWelcomeTextEnabled != in.PortalCustomizedWelcomeTextEnabled ||
		out.PortalCustomizedWelcomeTextPosition != in.PortalCustomizedWelcomeTextPosition {
		t.Errorf("guest_access Task 5 round-trip mismatch: %+v", out)
	}
}

// TestGuestAccessRestrictedDNSServersValidatorRejectsAndAcceptsIPAddresses
// pins restricted_dns_servers' per-element validator against the generated
// schema itself, not just the Spec: an element that is not an IP address (or
// the empty string SettingGuestAccess's own constraint pattern also admits)
// is rejected, and a list of real IP addresses passes. Shaped like
// TestVpnServerKitResource_dns_servers_length (vpn_server_resource_test.go),
// the established way this codebase probes a schema-level list validator
// directly rather than through the Spec.
func TestGuestAccessRestrictedDNSServersValidatorRejectsAndAcceptsIPAddresses(t *testing.T) {
	ctx := context.Background()
	nested := guestAccessNestedSchema(ctx)
	attr, ok := nested.Attributes["restricted_dns_servers"]
	if !ok {
		t.Fatal(`guest_access is missing attribute "restricted_dns_servers"`)
	}
	listAttr, ok := attr.(schema.ListAttribute)
	if !ok {
		t.Fatalf("restricted_dns_servers is a %T, want schema.ListAttribute", attr)
	}

	validate := func(t *testing.T, elements []string) diag.Diagnostics {
		t.Helper()
		list, d := types.ListValueFrom(ctx, types.StringType, elements)
		if d.HasError() {
			t.Fatalf("building the list value: %v", d)
		}
		var diags diag.Diagnostics
		for _, v := range listAttr.Validators {
			resp := &validator.ListResponse{}
			v.ValidateList(ctx, validator.ListRequest{
				Path:        path.Root("restricted_dns_servers"),
				ConfigValue: list,
			}, resp)
			diags.Append(resp.Diagnostics...)
		}
		return diags
	}

	if diags := validate(t, []string{"1.1.1.1", "not-an-ip"}); !diags.HasError() {
		t.Error("[\"1.1.1.1\", \"not-an-ip\"] passed validation; want an error, " +
			"\"not-an-ip\" is not an IP address")
	}
	if diags := validate(t, []string{"1.1.1.1", "8.8.8.8", ""}); diags.HasError() {
		t.Errorf("[\"1.1.1.1\", \"8.8.8.8\", \"\"] failed validation: %v; the controller's own "+
			"constraint pattern admits the empty string via its own \"|^$\" alternation", diags)
	}
}

// guestAccessSecretAccessor is one x_-prefixed StringField's Model/SDK pair,
// as guestAccessKitSpec itself declares it -- derived from the spec rather
// than transcribed a second time, so a field the spec ever drops, renames or
// re-orders is reflected here automatically instead of two lists silently
// drifting apart.
type guestAccessSecretAccessor struct {
	Wire  string
	Model func(*settingGuestAccessModel) *types.String
	SDK   func(*settings.GuestAccess) *string
}

// guestAccessSecretFieldAccessors extracts every x_-prefixed StringField out
// of spec.Fields. Used by the three tests below so each iterates the same 18
// fields Task 0 measured, with no hand-maintained field list of its own to
// go stale.
func guestAccessSecretFieldAccessors(
	spec resourcekit.Spec[settingGuestAccessModel, settings.GuestAccess],
) []guestAccessSecretAccessor {
	var out []guestAccessSecretAccessor
	for _, field := range spec.Fields {
		sf, ok := field.(resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess])
		if !ok || !strings.HasPrefix(sf.Wire, "x_") {
			continue
		}
		out = append(out, guestAccessSecretAccessor{Wire: sf.Wire, Model: sf.Model, SDK: sf.SDK})
	}
	return out
}

// TestGuestAccessAfterReceiveKeepsThePlansSecretWhenNamed is
// TestSnmpAfterReceiveKeepsThePlansSecretWhenNamed's shape run across all 18
// of guest_access's x_-prefixed fields instead of two: an unconfigured field
// (prior null or unknown) always comes back null, no matter what the
// controller echoed for it, and a configured one surfaces whatever
// Spec.ToModel already decoded off the wire -- the controller's own echo --
// not the prior string. Every one of the 18 gets its own subtest pair so a
// mistake in one field's Wire/Model/SDK wiring cannot hide behind the other
// 17 passing.
func TestGuestAccessAfterReceiveKeepsThePlansSecretWhenNamed(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()
	fields := guestAccessSecretFieldAccessors(spec)
	if len(fields) != 18 {
		t.Fatalf("found %d x_-prefixed StringField(s) in guestAccessKitSpec, want 18 "+
			"(guestAccessSecret's own count)", len(fields))
	}

	for _, f := range fields {
		t.Run(f.Wire, func(t *testing.T) {
			t.Run("null prior comes back null regardless of the controller's echo", func(t *testing.T) {
				sdk := &settings.GuestAccess{}
				*f.SDK(sdk) = "remote-value"
				var model settingGuestAccessModel
				if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
					t.Fatalf("ToModel: %v", diags)
				}
				var prior settingGuestAccessModel // every types.String zero value is null
				if diags := guestAccessAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
					t.Fatalf("guestAccessAfterReceive: %v", diags)
				}
				if got := f.Model(&model); !got.IsNull() {
					t.Errorf("%s = %q, want null when unconfigured (regardless of the controller's live value)",
						f.Wire, got.ValueString())
				}
			})

			// A config-absent Optional+Computed attribute is not null at plan
			// time, it is UNKNOWN -- the framework's own "marking computed
			// attribute that is null in the config as unknown" behaviour,
			// confirmed live in the guest_access acceptance test's debug log.
			// The null-prior case above alone leaves this arm unexercised:
			// deleting the "|| prior.X.IsUnknown()" half of every one of the
			// 18 guards in guestAccessAfterReceive still passes every other
			// test in this file, because nothing else ever hands it an
			// unknown prior. This subtest is what actually pins the arm
			// production depends on.
			t.Run("unknown prior comes back null regardless of the controller's echo", func(t *testing.T) {
				sdk := &settings.GuestAccess{}
				*f.SDK(sdk) = "remote-value"
				var model settingGuestAccessModel
				if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
					t.Fatalf("ToModel: %v", diags)
				}
				var prior settingGuestAccessModel
				*f.Model(&prior) = types.StringUnknown()
				if diags := guestAccessAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
					t.Fatalf("guestAccessAfterReceive: %v", diags)
				}
				if got := f.Model(&model); !got.IsNull() {
					t.Errorf("%s = %q, want null when the plan left it unknown (regardless of the "+
						"controller's live value)", f.Wire, got.ValueString())
				}
			})

			t.Run("configured surfaces the controller's own echo, not the prior string", func(t *testing.T) {
				sdk := &settings.GuestAccess{}
				*f.SDK(sdk) = "the-value"
				var model settingGuestAccessModel
				if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
					t.Fatalf("ToModel: %v", diags)
				}
				// prior names the field with a DIFFERENT string than the wire
				// holds -- the point of this case is that guestAccessAfterReceive
				// does not restore "old"; it leaves the controller's own decoded
				// echo alone, the same distinction radiusAfterReceive's and
				// snmpAfterReceive's own comments make.
				var prior settingGuestAccessModel
				*f.Model(&prior) = types.StringValue("old-value")
				if diags := guestAccessAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
					t.Fatalf("guestAccessAfterReceive: %v", diags)
				}
				if got := f.Model(&model).ValueString(); got != "the-value" {
					t.Errorf("%s = %q, want %q (the controller's own echo, not the prior string)",
						f.Wire, got, "the-value")
				}
			})
		})
	}
}

// TestGuestAccessSecretElideKeepsAnExplicitEmptyString pins the one place
// the section's 18 x_-prefixed fields diverge from radius.secret and snmp's
// community/password: none of the 18 has any entry at all in
// SettingGuestAccess's own constraint table (go-unifi's
// settings/validation.generated.go), so none carries a validator that would
// reject "", and guestAccessKitSpec's Elide: KeepZero (not NullZero) is what
// resourcekit.ElideProblems' schema-driven rule demands as a result -- also
// asserted indirectly by TestGuestAccessKitSpecConformance's own
// ElideProblems check, which would fail if this were ever set to NullZero.
// So an explicit empty string the controller echoes back is a real,
// distinguishable value here, not folded into null. A non-empty value is
// asserted too, so a KeepZero regression to NullZero would fail this test
// even if the conformance check somehow didn't.
func TestGuestAccessSecretElideKeepsAnExplicitEmptyString(t *testing.T) {
	ctx := context.Background()
	spec := guestAccessKitSpec()
	fields := guestAccessSecretFieldAccessors(spec)
	if len(fields) != 18 {
		t.Fatalf("found %d x_-prefixed StringField(s) in guestAccessKitSpec, want 18", len(fields))
	}

	for _, f := range fields {
		t.Run(f.Wire, func(t *testing.T) {
			sdk := &settings.GuestAccess{}
			*f.SDK(sdk) = ""
			var model settingGuestAccessModel
			if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			got := f.Model(&model)
			if got.IsNull() {
				t.Errorf("%s decoded from an empty wire value is null, want the real empty string "+
					"(KeepZero, not NullZero -- none of the 18 carries a validator that rejects \"\")", f.Wire)
			} else if got.ValueString() != "" {
				t.Errorf("%s = %q, want the empty string", f.Wire, got.ValueString())
			}

			*f.SDK(sdk) = "a-real-value"
			var model2 settingGuestAccessModel
			if diags := spec.ToModel(ctx, sdk, &model2, ""); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			if got := f.Model(&model2).ValueString(); got != "a-real-value" {
				t.Errorf("%s = %q, want %q", f.Wire, got, "a-real-value")
			}
		})
	}
}
