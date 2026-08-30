package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
// pins the shape ahead of that. The count is this task's own 21 -- the
// other 71 of settings.GuestAccess's 92 fields are still deferred (see
// setting_guest_access_descriptor.go's own comment).
func TestGuestAccessNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["guest_access"]; !ok {
		t.Fatal(`the generated setting schema has no "guest_access" attribute`)
	}
	nested := guestAccessNestedSchema(ctx)
	if len(nested.Attributes) != 21 {
		t.Errorf("guest_access has %d attribute(s), want 21; update guestAccessKitSpec and this count together",
			len(nested.Attributes))
	}
}
