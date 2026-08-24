package unifi

import (
	"strings"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// Adopted from the hand-written resource's udm_update_fix_test.go, adapted
// to sanitizeRadioForUpdate's home in device_descriptor.go and to the
// fork's pinned go-unifi SDK.
//
// Not ported: every assisted_roaming_* case. The upstream fix also
// sanitized DeviceRadioTable.AssistedRoamingEnabled/AssistedRoamingRssi,
// but this fork's go-unifi has no such fields (see dropAssistedRoaming) --
// UniFi Network 10.x dropped assisted roaming from the radio_table API
// surface this SDK targets.
//
// Not ported: TestBuildMinimalUpdateDevice_UsesProvidedPortOverrides. The
// hand-written buildMinimalUpdateDevice doesn't exist in the kit -- device
// writes go through a masked UpdateDeviceFields instead of a whole-object
// PUT -- and its intent (an update with no declared port_override blocks
// must never send `port_overrides: null`) is already held by
// Test_portOverridesForUpdate_noDeclaredBlocks against portOverridesForUpdate.

func TestSanitizeRadioForUpdate(t *testing.T) {
	cases := []struct {
		name string
		in   unifi.DeviceRadioTable
		want func(unifi.DeviceRadioTable) bool
	}{
		{
			"min_rssi 0 dropped when disabled",
			unifi.DeviceRadioTable{MinRssiEnabled: false, MinRssi: ptrInt64(0)},
			func(r unifi.DeviceRadioTable) bool { return r.MinRssi == nil },
		},
		{
			"min_rssi kept when enabled+valid",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(-82)},
			func(r unifi.DeviceRadioTable) bool { return r.MinRssi != nil && *r.MinRssi == -82 },
		},
		{
			"min_rssi >=0 dropped even if enabled",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(0)},
			func(r unifi.DeviceRadioTable) bool { return r.MinRssi == nil },
		},
		{
			"min_rssi out-of-range high (-10) dropped",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(-10)},
			func(r unifi.DeviceRadioTable) bool { return r.MinRssi == nil },
		},
		{
			"min_rssi out-of-range low (-95) dropped",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(-95)},
			func(r unifi.DeviceRadioTable) bool { return r.MinRssi == nil },
		},
		{
			"min_rssi boundary -90 kept",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(-90)},
			func(r unifi.DeviceRadioTable) bool { return r.MinRssi != nil },
		},
		{
			"maxsta 0 dropped",
			unifi.DeviceRadioTable{Maxsta: ptrInt64(0)},
			func(r unifi.DeviceRadioTable) bool { return r.Maxsta == nil },
		},
		{
			"maxsta 201 out-of-range dropped",
			unifi.DeviceRadioTable{Maxsta: ptrInt64(201)},
			func(r unifi.DeviceRadioTable) bool { return r.Maxsta == nil },
		},
		{
			"maxsta 200 boundary kept",
			unifi.DeviceRadioTable{Maxsta: ptrInt64(200)},
			func(r unifi.DeviceRadioTable) bool { return r.Maxsta != nil && *r.Maxsta == 200 },
		},
		{
			"sens_level 0 dropped when disabled",
			unifi.DeviceRadioTable{SensLevelEnabled: false, SensLevel: ptrInt64(0)},
			func(r unifi.DeviceRadioTable) bool { return r.SensLevel == nil },
		},
		{
			"sens_level out-of-range (-10) dropped even if enabled",
			unifi.DeviceRadioTable{SensLevelEnabled: true, SensLevel: ptrInt64(-10)},
			func(r unifi.DeviceRadioTable) bool { return r.SensLevel == nil },
		},
		{
			"sens_level in-range (-70) kept when enabled",
			unifi.DeviceRadioTable{SensLevelEnabled: true, SensLevel: ptrInt64(-70)},
			func(r unifi.DeviceRadioTable) bool { return r.SensLevel != nil },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.in
			_ = sanitizeRadioForUpdate(
				"ng",
				&r,
			) // diagnostic-emission behavior is covered by TestSanitizeRadioForUpdate_WarnsWhenEnabledAndOutOfRange
			if !c.want(r) {
				t.Fatalf("sanitize failed: %+v", r)
			}
		})
	}
}

// TestSanitizeRadioForUpdate_WarnsWhenEnabledAndOutOfRange: an out-of-range
// value is dropped either way (the controller would reject it), but if the
// field was enabled -- the user actually declared and turned on that
// setting -- the drop must be visible as a warning, not a silent no-op.
// Disabled (or simply unset) fields drop silently, same as before.
func TestSanitizeRadioForUpdate_WarnsWhenEnabledAndOutOfRange(t *testing.T) {
	cases := []struct {
		name      string
		in        unifi.DeviceRadioTable
		wantWarn  bool
		wantField string
	}{
		{
			"min_rssi enabled+out-of-range warns",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(-10)},
			true,
			"min_rssi",
		},
		{
			"min_rssi disabled+out-of-range silent",
			unifi.DeviceRadioTable{MinRssiEnabled: false, MinRssi: ptrInt64(-10)},
			false,
			"",
		},
		{
			"min_rssi enabled+in-range silent",
			unifi.DeviceRadioTable{MinRssiEnabled: true, MinRssi: ptrInt64(-80)},
			false,
			"",
		},
		{
			"maxsta out-of-range (non-zero) warns",
			unifi.DeviceRadioTable{Maxsta: ptrInt64(201)},
			true,
			"maxsta",
		},
		{"maxsta in-range silent", unifi.DeviceRadioTable{Maxsta: ptrInt64(50)}, false, ""},
		// maxsta=0 is the controller's "unset" sentinel (Optional+Computed,
		// UseStateForUnknown) -- flows back on every update of a device that never
		// configured maxsta. Must stay silent, not warn on every unrelated update.
		{
			"maxsta=0 (controller unset sentinel) silent, not warned",
			unifi.DeviceRadioTable{Maxsta: ptrInt64(0)},
			false,
			"",
		},
		{
			"sens_level enabled+out-of-range warns",
			unifi.DeviceRadioTable{SensLevelEnabled: true, SensLevel: ptrInt64(-10)},
			true,
			"sens_level",
		},
		{
			"sens_level disabled+out-of-range silent",
			unifi.DeviceRadioTable{SensLevelEnabled: false, SensLevel: ptrInt64(-10)},
			false,
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.in
			diags := sanitizeRadioForUpdate("ng", &r)
			if c.wantWarn && len(diags) == 0 {
				t.Fatalf("expected a warning diagnostic, got none")
			}
			if !c.wantWarn && len(diags) != 0 {
				t.Fatalf("expected no diagnostics, got: %+v", diags)
			}
			if c.wantWarn {
				found := false
				for _, d := range diags {
					if strings.Contains(d.Detail(), c.wantField) &&
						strings.Contains(d.Detail(), "ng") {
						found = true
					}
				}
				if !found {
					t.Fatalf(
						"expected a warning mentioning field %q and radio %q, got: %+v",
						c.wantField,
						"ng",
						diags,
					)
				}
			}
		})
	}
}
