package unifi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

var ipsHoneypotObject = types.ObjectType{AttrTypes: map[string]attr.Type{
	"ip_address": types.StringType,
	"network_id": types.StringType,
	"version":    types.StringType,
}}

// A configuration naming part of the ips block must not rewrite the rest of it.
//
// Four of settings.Ips's bools carry no omitempty, and the mapper assigns each
// only when the plan value is neither null nor unknown. Writing
// `ips { ips_mode = "ids" }` leaves the other four out of the configuration
// with nothing to supply a value: each is Optional+Computed with no default and
// no leaf plan modifier, and the parent block's UseStateForUnknown answers only
// the case where the whole block is absent. Before the base was taken, the body
// was
//
//	{"content_filtering_blocking_page_enabled":false,"honeypot_enabled":false,
//	 "ips_mode":"ids","memory_optimized":false,"restrict_torrents":false}
//
// NO MASK CATCHES THIS, which is why it is asserted here rather than with the
// wire masks. A mask asks whether the resource assigns a field. This one does.
// What it does not do is assign it on every path.
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
			body := ipsWireBody(t, arrival.value, nil, types.ListNull(ipsHoneypotObject))

			// CONTROL FIRST: without it, every absence below would also be
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

// Taking the base is what makes this possible, so it is asserted in the same
// commit: the honeypot loop APPENDS, and the base now arrives carrying the
// controller's list, so a configured list has to replace it rather than grow it
// by one entry on every apply.
func TestIpsHoneypotReplacesTheControllersList(t *testing.T) {
	remote := []settings.SettingIpsHoneypot{
		{IPAddress: "10.0.0.1", NetworkID: "net-remote", Version: "v4"},
	}
	configured, diags := types.ListValueFrom(context.Background(), ipsHoneypotObject,
		[]settingIpsHoneypotModel{{
			IPAddress: types.StringValue("10.0.0.2"),
			NetworkID: types.StringValue("net-configured"),
			Version:   types.StringValue("v4"),
		}})
	if diags.HasError() {
		t.Fatalf("building the configured list: %v", diags)
	}

	body := ipsWireBody(t, types.BoolValue(true), remote, configured)

	// CONTROL: the configured entry must be there at all.
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

// MOVED OUT OF setting_resource_test.go, WHICH IS A SHARED SCENARIO OWNER.
//
// That file is lent to the RELEASED tree by the controller differential, so
// everything in it has to compile against v0.101.2 as well as against the
// candidate. These two exercise a candidate-side mapper directly, so giving
// ipsModelToSetting its base parameter broke the graft --
// TestPreparingTheReleasedTreeVetsEveryLayer caught it, an hour before a
// campaign would have.
//
// A scenario owner is lent for the acceptance scenarios it owns; a unit test of
// an internal signature is a passenger, and it is the passenger that pins the
// signature. Moving them here is the fix rather than a workaround: the file
// keeps its 54 scenarios lendable and these keep testing what they tested.
//
// They pass an empty base, which preserves exactly what they asserted before --
// with nothing to inherit, the overlay and a fresh object agree.
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

// TestIpsSuppressionAlertsRoundTrip guards #275: signature alert suppression
// (incl. gid/id pointers and the nested tracking list) round-trips model<->setting.
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
