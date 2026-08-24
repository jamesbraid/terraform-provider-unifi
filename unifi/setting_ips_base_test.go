package unifi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// A configuration naming part of the ips block must not rewrite the rest of
// it. Four of settings.Ips's bools carry no omitempty, and the mapper
// assigns each only when the plan value is neither null nor unknown; each
// is Optional+Computed with no default and no leaf plan modifier, and the
// parent block's UseStateForUnknown only covers the whole-block-absent
// case. No mask catches this: a mask asks whether the resource assigns a
// field at all, not whether it assigns it on every path.
func TestIpsPartialBlockKeepsWhatTheControllerHolds(t *testing.T) {
	// Both arrivals, because the guard tests for both and the result must not
	// depend on which one a partial block produces.
	for _, arrival := range []struct {
		name  string
		value types.Bool
	}{
		{"unknown", types.BoolUnknown()},
		{"null", types.BoolNull()},
	} {
		t.Run(arrival.name, func(t *testing.T) {
			body := ipsWireBody(t, arrival.value, nil,
				types.ListNull(types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}))

			// Control first: without it, every absence below would also be
			// satisfied by a mapper that returned an empty object.
			if !strings.Contains(body, `"ips_mode":"ids"`) {
				t.Fatalf("the configured ips_mode is not on the wire, so nothing below "+
					"is a measurement of the unset fields.\n%s", body)
			}
			for _, wire := range []string{
				"honeypot_enabled",
				"restrict_torrents",
				"content_filtering_blocking_page_enabled",
				"memory_optimized",
			} {
				if strings.Contains(body, `"`+wire+`":false`) {
					t.Errorf("%s went out as false; the controller held it true and the "+
						"configuration never mentioned it.\n%s", wire, body)
				}
			}
		})
	}
}

// The honeypot loop appends, and the base arrives carrying the controller's
// list, so a configured list has to replace it rather than grow it by one
// entry on every apply.
func TestIpsHoneypotReplacesTheControllersList(t *testing.T) {
	remote := []settings.SettingIpsHoneypot{
		{IPAddress: "10.0.0.1", NetworkID: "net-remote", Version: "v4"},
	}
	configured, diags := types.ListValueFrom(context.Background(),
		types.ObjectType{AttrTypes: ipsHoneypotAttrTypes},
		[]settingIpsHoneypotModel{{
			IPAddress: types.StringValue("10.0.0.2"),
			NetworkID: types.StringValue("net-configured"),
			Version:   types.StringValue("v4"),
		}})
	if diags.HasError() {
		t.Fatalf("building the configured list: %v", diags)
	}

	body := ipsWireBody(t, types.BoolValue(true), remote, configured)

	// Control: the configured entry must be there at all.
	if !strings.Contains(body, "net-configured") {
		t.Fatalf("the configured honeypot is not on the wire, so the assertion below "+
			"would pass against a mapper that dropped the list entirely.\n%s", body)
	}
	if strings.Contains(body, "net-remote") {
		t.Errorf("the controller's honeypot entry is still on the wire beside the "+
			"configured one, so the list grows by one on every apply.\n%s", body)
	}
}

func ipsWireBody(
	t *testing.T,
	arrival types.Bool,
	remoteHoneypot []settings.SettingIpsHoneypot,
	configuredHoneypot types.List,
) string {
	t.Helper()
	ctx := context.Background()

	// The controller's live values, every one of the four ON, so a reset shows
	// up as a change rather than as agreement with a zero.
	base := &settings.Ips{
		IPsMode:                             "ids",
		HoneypotEnabled:                     true,
		RestrictTorrents:                    true,
		ContentFilteringBlockingPageEnabled: true,
		MemoryOptimized:                     true,
		Honeypot:                            remoteHoneypot,
	}

	model := &settingIpsModel{
		IPSMode:                             types.StringValue("ids"),
		AdvancedFilteringPreference:         types.StringNull(),
		ContentFilteringBlockingPageEnabled: arrival,
		HoneypotEnabled:                     arrival,
		MemoryOptimized:                     arrival,
		RestrictTorrents:                    arrival,
		EnabledCategories:                   types.ListNull(types.StringType),
		EnabledNetworks:                     types.ListNull(types.StringType),
		Honeypot:                            configuredHoneypot,
		SuppressionWhitelist:                types.ListNull(types.StringType),
		SuppressionAlerts:                   types.ListNull(types.StringType),
	}

	var diags diag.Diagnostics
	setting := (&settingResource{}).ipsModelToSetting(ctx, model, base, &diags)
	if diags.HasError() {
		t.Fatalf("mapping: %v", diags)
	}
	if setting == nil {
		t.Fatal("the mapper returned nothing")
	}
	raw, err := json.Marshal(setting)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// setting_resource_test.go is a shared scenario owner: the release-comparison
// harness lends it to the released tree, so everything there must compile
// against the released SDK as well as the candidate. These two test an
// internal signature (ipsModelToSetting's base parameter) directly, which
// isn't a scenario that file owns, so they live here instead -- passing an
// empty base, which preserves exactly what they asserted before the base
// parameter existed.
func Test_settingResource_ipsModelToSetting(t *testing.T) {
	r := &settingResource{}
	ctx := context.Background()

	t.Run("null fields produce empty setting", func(t *testing.T) {
		model := &settingIpsModel{
			IPSMode:          types.StringNull(),
			HoneypotEnabled:  types.BoolNull(),
			RestrictTorrents: types.BoolNull(),
		}
		var diags diag.Diagnostics
		got := r.ipsModelToSetting(ctx, model, &settings.Ips{}, &diags)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.IPsMode != "" {
			t.Errorf("IPsMode should be empty, got %q", got.IPsMode)
		}
	})

	t.Run("ips_mode and restrict_torrents set", func(t *testing.T) {
		model := &settingIpsModel{
			IPSMode:          types.StringValue("disabled"),
			RestrictTorrents: types.BoolValue(true),
			HoneypotEnabled:  types.BoolNull(),
		}
		var diags diag.Diagnostics
		got := r.ipsModelToSetting(ctx, model, &settings.Ips{}, &diags)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if got.IPsMode != "disabled" {
			t.Errorf("IPsMode = %q, want disabled", got.IPsMode)
		}
		if !got.RestrictTorrents {
			t.Error("RestrictTorrents should be true")
		}
	})
}

// TestIpsSuppressionAlertsRoundTrip checks that signature alert suppression
// (including gid/id pointers and the nested tracking list) round-trips
// model<->setting.
func TestIpsSuppressionAlertsRoundTrip(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	r := &settingResource{}

	tracking, _ := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsTrackingAttrTypes},
		[]settingIpsTrackingModel{{
			Direction: types.StringValue("both"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("10.0.0.5"),
		}})
	alerts, _ := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsAlertAttrTypes},
		[]settingIpsAlertModel{{
			Category:  types.StringValue("malware"),
			Gid:       types.Int64Value(1),
			ID:        types.Int64Value(2001),
			Signature: types.StringValue("ET MALWARE"),
			Type:      types.StringValue("track"),
			Tracking:  tracking,
		}})

	model := &settingIpsModel{
		EnabledCategories:    types.ListNull(types.StringType),
		EnabledNetworks:      types.ListNull(types.StringType),
		Honeypot:             types.ListNull(types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}),
		SuppressionWhitelist: types.ListNull(types.ObjectType{AttrTypes: ipsWhitelistAttrTypes}),
		SuppressionAlerts:    alerts,
	}
	setting := r.ipsModelToSetting(ctx, model, &settings.Ips{}, &diags)
	if diags.HasError() {
		t.Fatalf("modelToSetting: %v", diags)
	}

	if !ipsSuppressionConfigured(model) {
		t.Fatal("suppression should be reported as configured")
	}
	suppression := r.ipsSuppressionModelToSetting(ctx, model, &diags)
	if diags.HasError() {
		t.Fatalf("suppressionModelToSetting: %v", diags)
	}
	if suppression == nil || len(suppression.Alerts) != 1 {
		t.Fatalf("alerts not built: %+v", suppression)
	}
	a := suppression.Alerts[0]
	if a.Category != "malware" || a.Gid == nil || *a.Gid != 1 || a.ID == nil || *a.ID != 2001 ||
		a.Type != "track" || len(a.Tracking) != 1 || a.Tracking[0].Value != "10.0.0.5" {
		t.Fatalf("alert mismatch: %+v", a)
	}

	out := r.ipsSettingToModel(ctx, setting, suppression, model, &diags)
	if diags.HasError() {
		t.Fatalf("settingToModel: %v", diags)
	}
	var outAlerts []settingIpsAlertModel
	out.SuppressionAlerts.ElementsAs(ctx, &outAlerts, false)
	if len(outAlerts) != 1 || outAlerts[0].Signature.ValueString() != "ET MALWARE" ||
		outAlerts[0].Gid.ValueInt64() != 1 {
		t.Errorf("read-back alerts mismatch: %+v", outAlerts)
	}
}
