package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenEtherLighting = `{"key":"ether_lighting","network_overrides":[{"key":"net-a","raw_color_hex":"00ff88"}],"speed_overrides":[{"key":"GbE","raw_color_hex":"0088ff"}]}`

func etherLightingRepresentativeModel(ctx context.Context, t *testing.T) types.Object {
	t.Helper()
	networkOverrides, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}, []settingEtherLightingNetworkOverrideModel{
		{Key: types.StringValue("net-a"), RawColorHex: types.StringValue("00ff88")},
	})
	if diags.HasError() {
		t.Fatalf("building network_overrides list: %v", diags)
	}
	speedOverrides, diags2 := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}, []settingEtherLightingSpeedOverrideModel{
		{Key: types.StringValue("GbE"), RawColorHex: types.StringValue("0088ff")},
	})
	if diags2.HasError() {
		t.Fatalf("building speed_overrides list: %v", diags2)
	}
	m := settingEtherLightingModel{
		NetworkOverrides: networkOverrides,
		SpeedOverrides:   speedOverrides,
	}
	obj, diags3 := types.ObjectValueFrom(ctx, etherLightingAttrTypes, m)
	if diags3.HasError() {
		t.Fatalf("building ether_lighting object: %v", diags3)
	}
	return obj
}

func TestEtherLightingSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := etherLightingSection{}

	model := settingResourceModel{EtherLighting: etherLightingRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "ether_lighting" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "ether_lighting")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenEtherLighting)
}

func TestEtherLightingSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := etherLightingSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ether_lighting"},
		Data: map[string]any{
			"network_overrides": []any{
				map[string]any{"key": "net-a", "raw_color_hex": "00ff88"},
			},
			"speed_overrides": []any{
				map[string]any{"key": "GbE", "raw_color_hex": "0088ff"},
			},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingEtherLightingModel
	if diags := model.EtherLighting.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingEtherLightingModel: %v", diags)
	}

	var netOverrides []settingEtherLightingNetworkOverrideModel
	if diags := got.NetworkOverrides.ElementsAs(ctx, &netOverrides, false); diags.HasError() {
		t.Fatalf("extracting NetworkOverrides: %v", diags)
	}
	if len(netOverrides) != 1 || netOverrides[0].Key.ValueString() != "net-a" || netOverrides[0].RawColorHex.ValueString() != "00ff88" {
		t.Errorf("NetworkOverrides = %+v, want [{net-a 00ff88}]", netOverrides)
	}

	var speedOverrides []settingEtherLightingSpeedOverrideModel
	if diags := got.SpeedOverrides.ElementsAs(ctx, &speedOverrides, false); diags.HasError() {
		t.Fatalf("extracting SpeedOverrides: %v", diags)
	}
	if len(speedOverrides) != 1 || speedOverrides[0].Key.ValueString() != "GbE" || speedOverrides[0].RawColorHex.ValueString() != "0088ff" {
		t.Errorf("SpeedOverrides = %+v, want [{GbE 0088ff}]", speedOverrides)
	}
}

func TestEtherLightingSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := etherLightingSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ether_lighting"},
		Data: map[string]any{
			"x_unmanaged": "keep",
		},
	}})

	model := settingResourceModel{EtherLighting: etherLightingRepresentativeModel(ctx, t)}
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

func TestEtherLightingSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := etherLightingSection{}

	model := settingResourceModel{EtherLighting: types.ObjectNull(etherLightingAttrTypes)}
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

func TestEtherLightingSection_InterfaceWiring(t *testing.T) {
	sec := etherLightingSection{}
	if sec.key() != "ether_lighting" {
		t.Errorf("key() = %q, want %q", sec.key(), "ether_lighting")
	}
	if sec.attrName() != "ether_lighting" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "ether_lighting")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(etherLightingSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("etherLightingSection not found in settingSections registry")
	}
}
