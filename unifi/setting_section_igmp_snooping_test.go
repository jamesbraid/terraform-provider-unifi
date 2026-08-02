package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestIgmpSnoopingSection_GoldenReproduction proves overlay() reproduces the
// Task-19 golden PUT body (TestGolden_igmp_snooping) — but unlike every
// earlier section, this is the first read-modify-write (RMW) case: the
// golden contains fields the model does NOT expose at all (querier_mode,
// switches, and the always-present zero-value flood_known_protocols /
// forward_unknown_mcast_router_ports). Those come from the controller's
// existing section data, not from the model. So the snapshot base here is
// SEEDED with those fields (not empty) before overlay() merges the model's
// enabled/network_ids on top — reproducing the golden byte-for-byte proves
// both the model writes and the RMW preservation in one test.
func TestIgmpSnoopingSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := igmpSnoopingSection{}

	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a", "net-b"})
	if diags.HasError() {
		t.Fatalf("building network_ids: %v", diags)
	}

	m := settingIgmpSnoopingModel{
		Enabled:    types.BoolValue(true),
		NetworkIDs: networkIDs,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, igmpSnoopingAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building igmp_snooping object: %v", objDiags)
	}

	model := settingResourceModel{IgmpSnooping: obj}
	prior := settingResourceModel{}
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "igmp_snooping"},
		Data: map[string]any{
			"querier_mode":                       "auto",
			"switches":                           []any{"switch-1"},
			"flood_known_protocols":              false,
			"forward_unknown_mcast_router_ports": false,
		},
	}})

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "igmp_snooping" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "igmp_snooping")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenIgmpSnooping)
}

// TestIgmpSnoopingSection_DecodeRoundTrip proves decode() reads the two
// modeled leaves (enabled, network_ids) from a snapshot section's data,
// ignoring unmodeled fields (querier_mode, switches) that may be present
// alongside them — their presence must not break decode.
func TestIgmpSnoopingSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := igmpSnoopingSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "igmp_snooping"},
		Data: map[string]any{
			"enabled":      true,
			"network_ids":  []any{"net-a", "net-b"},
			"querier_mode": "auto",
			"switches":     []any{"switch-1"},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.IgmpSnooping.IsNull() || model.IgmpSnooping.IsUnknown() {
		t.Fatalf("model.IgmpSnooping is null/unknown after decode")
	}

	var got settingIgmpSnoopingModel
	if diags := model.IgmpSnooping.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingIgmpSnoopingModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.NetworkIDs.IsNull() || got.NetworkIDs.IsUnknown() {
		t.Fatalf("NetworkIDs is null/unknown after decode")
	}
	var networkIDs []string
	if diags := got.NetworkIDs.ElementsAs(ctx, &networkIDs, false); diags.HasError() {
		t.Fatalf("extracting NetworkIDs: %v", diags)
	}
	if len(networkIDs) != 2 || networkIDs[0] != "net-a" || networkIDs[1] != "net-b" {
		t.Errorf("NetworkIDs = %v, want [net-a net-b]", networkIDs)
	}
}

// TestIgmpSnoopingSection_Preservation proves overlay() preserves an
// unmodeled key already present in the snapshot's section data — the RMW
// mechanism this section is the first to exercise.
func TestIgmpSnoopingSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := igmpSnoopingSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "igmp_snooping"},
		Data: map[string]any{
			"querier_mode": "auto",
			"x_unmanaged":  "keep",
		},
	}})

	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a"})
	if diags.HasError() {
		t.Fatalf("building network_ids: %v", diags)
	}
	m := settingIgmpSnoopingModel{
		Enabled:    types.BoolValue(true),
		NetworkIDs: networkIDs,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, igmpSnoopingAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building igmp_snooping object: %v", objDiags)
	}

	model := settingResourceModel{IgmpSnooping: obj}
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
	if got, ok := rs.Data["querier_mode"]; !ok || got != "auto" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "querier_mode", got, ok, "auto")
	}
}

// TestIgmpSnoopingSection_NetworkIdsListSemantics proves the list codec's
// present-vs-absent contract for network_ids: an absent key decodes to a
// null list, while an explicit empty JSON array decodes to an empty
// (non-null) list.
func TestIgmpSnoopingSection_NetworkIdsListSemantics(t *testing.T) {
	ctx := context.Background()
	sec := igmpSnoopingSection{}

	t.Run("absent", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "igmp_snooping"},
			Data:        map[string]any{},
		}})

		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingIgmpSnoopingModel
		if diags := model.IgmpSnooping.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingIgmpSnoopingModel: %v", diags)
		}

		if !got.NetworkIDs.IsNull() {
			t.Errorf("NetworkIDs = %v, want null when absent", got.NetworkIDs)
		}
	})

	t.Run("present empty", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "igmp_snooping"},
			Data: map[string]any{
				"network_ids": []any{},
			},
		}})

		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingIgmpSnoopingModel
		if diags := model.IgmpSnooping.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingIgmpSnoopingModel: %v", diags)
		}

		if got.NetworkIDs.IsNull() {
			t.Errorf("NetworkIDs is null, want present-empty (non-null) list")
		}
		if got.NetworkIDs.IsUnknown() {
			t.Errorf("NetworkIDs is unknown, want present-empty (non-null) list")
		}
		var networkIDs []string
		if diags := got.NetworkIDs.ElementsAs(ctx, &networkIDs, false); diags.HasError() {
			t.Fatalf("extracting NetworkIDs: %v", diags)
		}
		if len(networkIDs) != 0 {
			t.Errorf("NetworkIDs = %v, want empty", networkIDs)
		}
	})
}

// TestIgmpSnoopingSection_NotConfigured proves overlay() returns configured
// == false and a zero-value RawSetting when model.IgmpSnooping is null.
func TestIgmpSnoopingSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := igmpSnoopingSection{}

	model := settingResourceModel{IgmpSnooping: types.ObjectNull(igmpSnoopingAttrTypes)}
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

// TestIgmpSnoopingSection_InterfaceWiring is a light structural check that
// the section is registered and key()/attrName() both return "igmp_snooping"
// (no key/attrName divergence for this section).
func TestIgmpSnoopingSection_InterfaceWiring(t *testing.T) {
	sec := igmpSnoopingSection{}
	if sec.key() != "igmp_snooping" {
		t.Errorf("key() = %q, want %q", sec.key(), "igmp_snooping")
	}
	if sec.attrName() != "igmp_snooping" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "igmp_snooping")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "igmp_snooping" && s.attrName() == "igmp_snooping" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "igmp_snooping")
	}
}
