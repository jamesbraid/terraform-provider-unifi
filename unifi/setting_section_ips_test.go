package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// buildIpsGoldenModel builds the settingIpsModel matching TestGolden_ips's
// representative model (setting_golden_test.go), wrapped as a types.Object
// for use as model.Ips. Both suppression_alerts and suppression_whitelist
// are set, matching the golden's "suppression" wrapper being present.
func buildIpsGoldenModel(t *testing.T, ctx context.Context) types.Object {
	t.Helper()

	enabledCategories, diags := types.ListValueFrom(ctx, types.StringType, []string{"botcc", "tor"})
	if diags.HasError() {
		t.Fatalf("building enabled_categories: %v", diags)
	}
	enabledNetworks, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a"})
	if diags.HasError() {
		t.Fatalf("building enabled_networks: %v", diags)
	}
	honeypot, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsHoneypotAttrTypes},
		[]settingIpsHoneypotModel{{
			IPAddress: types.StringValue("192.0.2.20"),
			NetworkID: types.StringValue("net-a"),
			Version:   types.StringValue("v4"),
		}})
	if diags.HasError() {
		t.Fatalf("building honeypot: %v", diags)
	}
	tracking, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsTrackingAttrTypes},
		[]settingIpsTrackingModel{{
			Direction: types.StringValue("both"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("192.0.2.30"),
		}})
	if diags.HasError() {
		t.Fatalf("building tracking: %v", diags)
	}
	alerts, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsAlertAttrTypes},
		[]settingIpsAlertModel{{
			Category:  types.StringValue("malware"),
			Gid:       types.Int64Value(1),
			ID:        types.Int64Value(2001),
			Signature: types.StringValue("ET MALWARE test signature"),
			Type:      types.StringValue("track"),
			Tracking:  tracking,
		}})
	if diags.HasError() {
		t.Fatalf("building suppression_alerts: %v", diags)
	}
	whitelist, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsWhitelistAttrTypes},
		[]settingIpsWhitelistModel{{
			Direction: types.StringValue("src"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("192.0.2.40"),
		}})
	if diags.HasError() {
		t.Fatalf("building suppression_whitelist: %v", diags)
	}

	m := settingIpsModel{
		AdvancedFilteringPreference:         types.StringValue("manual"),
		ContentFilteringBlockingPageEnabled: types.BoolValue(true),
		EnabledCategories:                   enabledCategories,
		EnabledNetworks:                     enabledNetworks,
		Honeypot:                            honeypot,
		HoneypotEnabled:                     types.BoolValue(true),
		IPSMode:                             types.StringValue("ips"),
		MemoryOptimized:                     types.BoolValue(false),
		RestrictTorrents:                    types.BoolValue(true),
		SuppressionWhitelist:                whitelist,
		SuppressionAlerts:                   alerts,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, ipsAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building ips object: %v", objDiags)
	}
	return obj
}

// TestIpsSection_GoldenReproduction proves overlay() reproduces the Task-18
// golden PUT body for the representative model used to capture that golden
// (TestGolden_ips) — including gluing the suppression_alerts/
// suppression_whitelist wire wrapper and the alert->tracking double-nest.
func TestIpsSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := ipsSection{}

	model := settingResourceModel{Ips: buildIpsGoldenModel(t, ctx)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "ips" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "ips")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenIps)
}

// TestIpsSection_DecodeRoundTrip proves decode() reads a snapshot section's
// fields, including the "suppression" wire wrapper (unwrapped to
// suppression_alerts/suppression_whitelist) and the alert's nested tracking
// list, into model.Ips.
func TestIpsSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := ipsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ips"},
		Data: map[string]any{
			"ips_mode":                                "ips",
			"advanced_filtering_preference":           "manual",
			"content_filtering_blocking_page_enabled": true,
			"honeypot_enabled":                        true,
			"restrict_torrents":                       true,
			"memory_optimized":                        false,
			"enabled_categories":                      []any{"botcc", "tor"},
			"enabled_networks":                        []any{"net-a"},
			"honeypot": []any{
				map[string]any{
					"ip_address": "192.0.2.20",
					"network_id": "net-a",
					"version":    "v4",
				},
			},
			"suppression": map[string]any{
				"alerts": []any{
					map[string]any{
						"category":  "malware",
						"gid":       float64(1),
						"id":        float64(2001),
						"signature": "ET MALWARE test signature",
						"type":      "track",
						"tracking": []any{
							map[string]any{
								"direction": "both",
								"mode":      "ip",
								"value":     "192.0.2.30",
							},
						},
					},
				},
				"whitelist": []any{
					map[string]any{
						"direction": "src",
						"mode":      "ip",
						"value":     "192.0.2.40",
					},
				},
			},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Ips.IsNull() || model.Ips.IsUnknown() {
		t.Fatalf("model.Ips is null/unknown after decode")
	}

	var got settingIpsModel
	if diags := model.Ips.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingIpsModel: %v", diags)
	}

	if got.IPSMode.ValueString() != "ips" {
		t.Errorf("IPSMode = %q, want %q", got.IPSMode.ValueString(), "ips")
	}
	if got.AdvancedFilteringPreference.ValueString() != "manual" {
		t.Errorf("AdvancedFilteringPreference = %q, want %q", got.AdvancedFilteringPreference.ValueString(), "manual")
	}
	if !got.ContentFilteringBlockingPageEnabled.ValueBool() {
		t.Errorf("ContentFilteringBlockingPageEnabled = %v, want true", got.ContentFilteringBlockingPageEnabled)
	}
	if !got.HoneypotEnabled.ValueBool() {
		t.Errorf("HoneypotEnabled = %v, want true", got.HoneypotEnabled)
	}
	if !got.RestrictTorrents.ValueBool() {
		t.Errorf("RestrictTorrents = %v, want true", got.RestrictTorrents)
	}
	if got.MemoryOptimized.ValueBool() {
		t.Errorf("MemoryOptimized = %v, want false", got.MemoryOptimized)
	}

	var enabledCategories []string
	if diags := got.EnabledCategories.ElementsAs(ctx, &enabledCategories, false); diags.HasError() {
		t.Fatalf("extracting EnabledCategories: %v", diags)
	}
	if len(enabledCategories) != 2 || enabledCategories[0] != "botcc" || enabledCategories[1] != "tor" {
		t.Errorf("EnabledCategories = %v, want [botcc tor]", enabledCategories)
	}

	var enabledNetworks []string
	if diags := got.EnabledNetworks.ElementsAs(ctx, &enabledNetworks, false); diags.HasError() {
		t.Fatalf("extracting EnabledNetworks: %v", diags)
	}
	if len(enabledNetworks) != 1 || enabledNetworks[0] != "net-a" {
		t.Errorf("EnabledNetworks = %v, want [net-a]", enabledNetworks)
	}

	if got.Honeypot.IsNull() || got.Honeypot.IsUnknown() {
		t.Fatalf("Honeypot is null/unknown after decode")
	}
	var honeypot []settingIpsHoneypotModel
	if diags := got.Honeypot.ElementsAs(ctx, &honeypot, false); diags.HasError() {
		t.Fatalf("extracting Honeypot: %v", diags)
	}
	if len(honeypot) != 1 {
		t.Fatalf("Honeypot = %v, want 1 element", honeypot)
	}
	if honeypot[0].IPAddress.ValueString() != "192.0.2.20" {
		t.Errorf("Honeypot[0].IPAddress = %q, want %q", honeypot[0].IPAddress.ValueString(), "192.0.2.20")
	}
	if honeypot[0].NetworkID.ValueString() != "net-a" {
		t.Errorf("Honeypot[0].NetworkID = %q, want %q", honeypot[0].NetworkID.ValueString(), "net-a")
	}
	if honeypot[0].Version.ValueString() != "v4" {
		t.Errorf("Honeypot[0].Version = %q, want %q", honeypot[0].Version.ValueString(), "v4")
	}

	// suppression_alerts, unwrapped from the wire "suppression.alerts".
	if got.SuppressionAlerts.IsNull() || got.SuppressionAlerts.IsUnknown() {
		t.Fatalf("SuppressionAlerts is null/unknown after decode")
	}
	var alerts []settingIpsAlertModel
	if diags := got.SuppressionAlerts.ElementsAs(ctx, &alerts, false); diags.HasError() {
		t.Fatalf("extracting SuppressionAlerts: %v", diags)
	}
	if len(alerts) != 1 {
		t.Fatalf("SuppressionAlerts = %v, want 1 element", alerts)
	}
	a := alerts[0]
	if a.Category.ValueString() != "malware" {
		t.Errorf("SuppressionAlerts[0].Category = %q, want %q", a.Category.ValueString(), "malware")
	}
	if a.Gid.ValueInt64() != 1 {
		t.Errorf("SuppressionAlerts[0].Gid = %d, want 1", a.Gid.ValueInt64())
	}
	if a.ID.ValueInt64() != 2001 {
		t.Errorf("SuppressionAlerts[0].ID = %d, want 2001", a.ID.ValueInt64())
	}
	if a.Signature.ValueString() != "ET MALWARE test signature" {
		t.Errorf("SuppressionAlerts[0].Signature = %q, want %q", a.Signature.ValueString(), "ET MALWARE test signature")
	}
	if a.Type.ValueString() != "track" {
		t.Errorf("SuppressionAlerts[0].Type = %q, want %q", a.Type.ValueString(), "track")
	}
	if a.Tracking.IsNull() || a.Tracking.IsUnknown() {
		t.Fatalf("SuppressionAlerts[0].Tracking is null/unknown after decode")
	}
	var tracking []settingIpsTrackingModel
	if diags := a.Tracking.ElementsAs(ctx, &tracking, false); diags.HasError() {
		t.Fatalf("extracting Tracking: %v", diags)
	}
	if len(tracking) != 1 {
		t.Fatalf("Tracking = %v, want 1 element", tracking)
	}
	if tracking[0].Direction.ValueString() != "both" {
		t.Errorf("Tracking[0].Direction = %q, want %q", tracking[0].Direction.ValueString(), "both")
	}
	if tracking[0].Mode.ValueString() != "ip" {
		t.Errorf("Tracking[0].Mode = %q, want %q", tracking[0].Mode.ValueString(), "ip")
	}
	if tracking[0].Value.ValueString() != "192.0.2.30" {
		t.Errorf("Tracking[0].Value = %q, want %q", tracking[0].Value.ValueString(), "192.0.2.30")
	}

	// suppression_whitelist, unwrapped from the wire "suppression.whitelist".
	if got.SuppressionWhitelist.IsNull() || got.SuppressionWhitelist.IsUnknown() {
		t.Fatalf("SuppressionWhitelist is null/unknown after decode")
	}
	var whitelist []settingIpsWhitelistModel
	if diags := got.SuppressionWhitelist.ElementsAs(ctx, &whitelist, false); diags.HasError() {
		t.Fatalf("extracting SuppressionWhitelist: %v", diags)
	}
	if len(whitelist) != 1 {
		t.Fatalf("SuppressionWhitelist = %v, want 1 element", whitelist)
	}
	if whitelist[0].Direction.ValueString() != "src" {
		t.Errorf("SuppressionWhitelist[0].Direction = %q, want %q", whitelist[0].Direction.ValueString(), "src")
	}
	if whitelist[0].Mode.ValueString() != "ip" {
		t.Errorf("SuppressionWhitelist[0].Mode = %q, want %q", whitelist[0].Mode.ValueString(), "ip")
	}
	if whitelist[0].Value.ValueString() != "192.0.2.40" {
		t.Errorf("SuppressionWhitelist[0].Value = %q, want %q", whitelist[0].Value.ValueString(), "192.0.2.40")
	}
}

// TestIpsSection_SuppressionAbsent proves that when the snapshot's "ips"
// section data has no "suppression" key at all, decode() yields null
// SuppressionAlerts/SuppressionWhitelist, and overlay()ing a model whose two
// suppression lists are null onto a base without "suppression" produces a
// PUT body with NO "suppression" key (the "don't add an empty wrapper"
// rule).
func TestIpsSection_SuppressionAbsent(t *testing.T) {
	ctx := context.Background()
	sec := ipsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ips"},
		Data: map[string]any{
			"ips_mode": "ids",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingIpsModel
	if diags := model.Ips.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingIpsModel: %v", diags)
	}
	if !got.SuppressionAlerts.IsNull() {
		t.Errorf("SuppressionAlerts = %v, want null when suppression absent", got.SuppressionAlerts)
	}
	if !got.SuppressionWhitelist.IsNull() {
		t.Errorf("SuppressionWhitelist = %v, want null when suppression absent", got.SuppressionWhitelist)
	}

	// Now overlay this decoded (null-suppression) model back onto the same
	// (suppression-less) base: the PUT body must not introduce an empty
	// "suppression" key.
	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if _, ok := rs.Data["suppression"]; ok {
		t.Errorf("rs.Data[%q] present = %v, want absent (no empty wrapper)", "suppression", rs.Data["suppression"])
	}
}

// TestIpsSection_SuppressionPreserved proves that an existing "suppression"
// wrapper's unmodeled key survives overlay() when the model sets both
// suppression lists (wrapper preservation, not replacement).
func TestIpsSection_SuppressionPreserved(t *testing.T) {
	ctx := context.Background()
	sec := ipsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ips"},
		Data: map[string]any{
			"suppression": map[string]any{
				"alerts": []any{
					map[string]any{
						"category":  "old",
						"gid":       float64(9),
						"id":        float64(9999),
						"signature": "old-sig",
						"type":      "all",
						"tracking":  []any{},
					},
				},
				"whitelist": []any{
					map[string]any{
						"direction": "dest",
						"mode":      "subnet",
						"value":     "203.0.113.0/24",
					},
				},
				"x": "keep",
			},
		},
	}})

	alerts, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsAlertAttrTypes},
		[]settingIpsAlertModel{{
			Category:  types.StringValue("malware"),
			Gid:       types.Int64Value(1),
			ID:        types.Int64Value(2001),
			Signature: types.StringValue("ET MALWARE test signature"),
			Type:      types.StringValue("track"),
			Tracking:  types.ListNull(types.ObjectType{AttrTypes: ipsTrackingAttrTypes}),
		}})
	if diags.HasError() {
		t.Fatalf("building suppression_alerts: %v", diags)
	}
	whitelist, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsWhitelistAttrTypes},
		[]settingIpsWhitelistModel{{
			Direction: types.StringValue("src"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("192.0.2.40"),
		}})
	if diags.HasError() {
		t.Fatalf("building suppression_whitelist: %v", diags)
	}

	m := settingIpsModel{
		AdvancedFilteringPreference:         types.StringNull(),
		ContentFilteringBlockingPageEnabled: types.BoolNull(),
		EnabledCategories:                   types.ListNull(types.StringType),
		EnabledNetworks:                     types.ListNull(types.StringType),
		Honeypot:                            types.ListNull(types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}),
		HoneypotEnabled:                     types.BoolNull(),
		IPSMode:                             types.StringNull(),
		MemoryOptimized:                     types.BoolNull(),
		RestrictTorrents:                    types.BoolNull(),
		SuppressionAlerts:                   alerts,
		SuppressionWhitelist:                whitelist,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, ipsAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building ips object: %v", objDiags)
	}

	model := settingResourceModel{Ips: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	sup, ok := rs.Data["suppression"].(map[string]any)
	if !ok {
		t.Fatalf("rs.Data[%q] = %v (%T), want map[string]any", "suppression", rs.Data["suppression"], rs.Data["suppression"])
	}
	if got, ok := sup["x"]; !ok || got != "keep" {
		t.Errorf("suppression[%q] = %v (ok=%v), want %q", "x", got, ok, "keep")
	}
}

// TestIpsSection_Preservation proves overlay() preserves a top-level
// unmodeled key already present in the snapshot's "ips" section data.
func TestIpsSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := ipsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ips"},
		Data: map[string]any{
			"ips_mode":    "ids",
			"x_unmanaged": "keep",
		},
	}})

	model := settingResourceModel{Ips: buildIpsGoldenModel(t, ctx)}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

// TestIpsSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when model.Ips is null.
func TestIpsSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := ipsSection{}

	model := settingResourceModel{Ips: types.ObjectNull(ipsAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

// TestIpsSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "ips".
func TestIpsSection_InterfaceWiring(t *testing.T) {
	sec := ipsSection{}
	if sec.key() != "ips" {
		t.Errorf("key() = %q, want %q", sec.key(), "ips")
	}
	if sec.attrName() != "ips" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "ips")
	}

	var foundByKey, foundByAttrName bool
	for _, s := range settingSections {
		if s.key() == "ips" {
			foundByKey = true
		}
		if s.attrName() == "ips" {
			foundByAttrName = true
		}
	}
	if !foundByKey {
		t.Errorf("no section in settingSections registry has key() == %q", "ips")
	}
	if !foundByAttrName {
		t.Errorf("no section in settingSections registry has attrName() == %q", "ips")
	}
}
